package content

import (
	"context"
	"io"
	"time"

	contentv1 "github.com/containerd/containerd/api/services/content/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service implements containerd's content gRPC service (ContentServer).
// It wraps a Store and exposes it over gRPC.
type Service struct {
	contentv1.UnimplementedContentServer
	store *Store
}

// NewService creates a new content gRPC service.
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// Info returns metadata about a committed blob.
func (s *Service) Info(ctx context.Context, req *contentv1.InfoRequest) (*contentv1.InfoResponse, error) {
	info, err := s.store.Info(req.Digest)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "blob %q: %v", req.Digest, err)
	}
	return &contentv1.InfoResponse{Info: info.ToProto()}, nil
}

// Update updates mutable metadata (labels) on a blob.
func (s *Service) Update(ctx context.Context, req *contentv1.UpdateRequest) (*contentv1.UpdateResponse, error) {
	if req.Info == nil {
		return nil, status.Error(codes.InvalidArgument, "info is required")
	}
	info, err := s.store.Info(req.Info.Digest)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "blob %q: %v", req.Info.Digest, err)
	}
	// Only labels are mutable
	if req.Info.Labels != nil {
		if info.Labels == nil {
			info.Labels = make(map[string]string)
		}
		for k, v := range req.Info.Labels {
			info.Labels[k] = v
		}
		info.UpdatedAt = time.Now()
	}
	return &contentv1.UpdateResponse{Info: info.ToProto()}, nil
}

// List streams all committed blobs to the client.
func (s *Service) List(req *contentv1.ListContentRequest, stream contentv1.Content_ListServer) error {
	blobs := s.store.List(req.Filters)
	for _, info := range blobs {
		resp := &contentv1.ListContentResponse{Info: []*contentv1.Info{info.ToProto()}}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes a committed blob from the store.
func (s *Service) Delete(ctx context.Context, req *contentv1.DeleteContentRequest) (*emptypb.Empty, error) {
	if err := s.store.Delete(req.Digest); err != nil {
		return nil, status.Errorf(codes.NotFound, "blob %q: %v", req.Digest, err)
	}
	return &emptypb.Empty{}, nil
}

// Read streams blob content to the client, supporting offset and size.
func (s *Service) Read(req *contentv1.ReadContentRequest, stream contentv1.Content_ReadServer) error {
	f, err := s.store.Open(req.Digest)
	if err != nil {
		return status.Errorf(codes.NotFound, "blob %q: %v", req.Digest, err)
	}
	defer f.Close()

	// Seek to offset
	if req.Offset > 0 {
		if _, err := f.Seek(req.Offset, io.SeekStart); err != nil {
			return status.Errorf(codes.Internal, "seek: %v", err)
		}
	}

	// Stream in 32KB chunks
	buf := make([]byte, 32*1024)
	var sent int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			resp := &contentv1.ReadContentResponse{
				Offset: req.Offset + sent,
				Data:   buf[:n],
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
			sent += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return status.Errorf(codes.Internal, "read: %v", err)
		}
		// If size is set, stop after requested bytes
		if req.Size > 0 && sent >= req.Size {
			break
		}
	}
	return nil
}

// Status returns the upload status for a single reference.
func (s *Service) Status(ctx context.Context, req *contentv1.StatusRequest) (*contentv1.StatusResponse, error) {
	us := s.store.GetUpload(req.Ref)
	if us == nil {
		// Ref not in uploads — check if it's already committed
		if _, err := s.store.Info(req.Ref); err == nil {
			return nil, status.Errorf(codes.AlreadyExists, "ref %q already committed", req.Ref)
		}
		return nil, status.Errorf(codes.NotFound, "ref %q not found", req.Ref)
	}
	return &contentv1.StatusResponse{
		Status: &contentv1.Status{
			Ref:       us.Ref,
			Offset:    us.Offset,
			Total:     us.Total,
			Expected:  us.Expected,
			StartedAt: timestamppb.New(us.StartedAt),
			UpdatedAt: timestamppb.New(us.UpdatedAt),
		},
	}, nil
}

// ListStatuses returns the status of all active uploads.
func (s *Service) ListStatuses(ctx context.Context, req *contentv1.ListStatusesRequest) (*contentv1.ListStatusesResponse, error) {
	uploads := s.store.ListUploads()
	var statuses []*contentv1.Status
	for _, us := range uploads {
		statuses = append(statuses, &contentv1.Status{
			Ref:       us.Ref,
			Offset:    us.Offset,
			Total:     us.Total,
			Expected:  us.Expected,
			StartedAt: timestamppb.New(us.StartedAt),
			UpdatedAt: timestamppb.New(us.UpdatedAt),
		})
	}
	return &contentv1.ListStatusesResponse{Statuses: statuses}, nil
}

// Write handles a streaming blob upload. The client sends STAT, WRITE, or COMMIT
// actions. On COMMIT, the blob is digested, verified, and moved to permanent storage.
func (s *Service) Write(stream contentv1.Content_WriteServer) error {
	var (
		ref      string
		expected string
		uploaded bool
	)

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		// Auto-init ref from request if not yet set
		if ref == "" && req.Ref != "" {
			ref = req.Ref
			expected = req.Expected
			if _, err := s.store.BeginUpload(ref, expected); err != nil {
				return status.Errorf(codes.Internal, "begin upload: %v", err)
			}
		}

		switch req.Action {
		case contentv1.WriteAction_STAT:
			ref = req.Ref
			expected = req.Expected
			us, err := s.store.BeginUpload(ref, expected)
			if err != nil {
				return status.Errorf(codes.Internal, "begin upload: %v", err)
			}
			resp := &contentv1.WriteContentResponse{
				Action:    contentv1.WriteAction_STAT,
				Offset:    us.Offset,
				Total:     us.Total,
				StartedAt: timestamppb.New(us.StartedAt),
				UpdatedAt: timestamppb.New(us.UpdatedAt),
			}
			if err := stream.Send(resp); err != nil {
				return err
			}

		case contentv1.WriteAction_WRITE:
			if ref == "" && req.Ref != "" {
				ref = req.Ref
				expected = req.Expected
				// Auto-begin upload if client sends WRITE without STAT
				if _, err := s.store.BeginUpload(ref, expected); err != nil {
					return status.Errorf(codes.Internal, "begin upload: %v", err)
				}
			}
			if ref == "" {
				return status.Error(codes.InvalidArgument, "STAT must precede WRITE")
			}
			us, err := s.store.WriteUpload(ref, req.Offset, req.Data)
			if err != nil {
				return status.Errorf(codes.Internal, "write: %v", err)
			}
			uploaded = true
			resp := &contentv1.WriteContentResponse{
				Action: contentv1.WriteAction_WRITE,
				Offset: us.Offset,
			}
			if err := stream.Send(resp); err != nil {
				return err
			}

		case contentv1.WriteAction_COMMIT:
			if ref == "" {
				return status.Error(codes.InvalidArgument, "STAT must precede COMMIT")
			}
			// If no data was written, write whatever data we have (could be zero-length)
			if !uploaded && len(req.Data) > 0 {
				if _, err := s.store.WriteUpload(ref, req.Offset, req.Data); err != nil {
					return status.Errorf(codes.Internal, "final write: %v", err)
				}
			}
			if err := s.store.CommitUpload(ref, req.Total); err != nil {
				return status.Errorf(codes.Internal, "commit: %v", err)
			}
			us := s.store.GetUpload(ref)
			resp := &contentv1.WriteContentResponse{
				Action:    contentv1.WriteAction_COMMIT,
				Offset:    us.Offset,
				Digest:    us.Digest,
				Total:     us.Size,
				StartedAt: timestamppb.New(us.StartedAt),
				UpdatedAt: timestamppb.New(us.UpdatedAt),
			}
			// Send final response and close stream
			if err := stream.Send(resp); err != nil {
				return err
			}
			return nil

		default:
			return status.Errorf(codes.InvalidArgument, "unknown action: %v", req.Action)
		}
	}
}

// ensure Service implements the ContentServer interface
var _ contentv1.ContentServer = (*Service)(nil)
