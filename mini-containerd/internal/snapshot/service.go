package snapshot

import (
	"context"

	snapshotsv1 "github.com/containerd/containerd/api/services/snapshots/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Service implements containerd's Snapshots gRPC service.
type Service struct {
	snapshotsv1.UnimplementedSnapshotsServer
	s *Snapshotter
}

// NewService creates a new snapshots gRPC service.
func NewService(sn *Snapshotter) *Service {
	return &Service{s: sn}
}

func (svc *Service) Prepare(ctx context.Context, req *snapshotsv1.PrepareSnapshotRequest) (*snapshotsv1.PrepareSnapshotResponse, error) {
	mounts, err := svc.s.Prepare(req.Key, req.Parent)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "prepare: %v", err)
	}
	return &snapshotsv1.PrepareSnapshotResponse{Mounts: mounts}, nil
}

func (svc *Service) View(ctx context.Context, req *snapshotsv1.ViewSnapshotRequest) (*snapshotsv1.ViewSnapshotResponse, error) {
	mounts, err := svc.s.View(req.Key, req.Parent)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "view: %v", err)
	}
	return &snapshotsv1.ViewSnapshotResponse{Mounts: mounts}, nil
}

func (svc *Service) Mounts(ctx context.Context, req *snapshotsv1.MountsRequest) (*snapshotsv1.MountsResponse, error) {
	mounts, err := svc.s.Mounts(req.Key)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "mounts: %v", err)
	}
	return &snapshotsv1.MountsResponse{Mounts: mounts}, nil
}

func (svc *Service) Commit(ctx context.Context, req *snapshotsv1.CommitSnapshotRequest) (*emptypb.Empty, error) {
	if err := svc.s.Commit(req.Key, req.Name); err != nil {
		return nil, status.Errorf(codes.Internal, "commit: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (svc *Service) Remove(ctx context.Context, req *snapshotsv1.RemoveSnapshotRequest) (*emptypb.Empty, error) {
	if err := svc.s.Remove(req.Key); err != nil {
		return nil, status.Errorf(codes.Internal, "remove: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (svc *Service) Stat(ctx context.Context, req *snapshotsv1.StatSnapshotRequest) (*snapshotsv1.StatSnapshotResponse, error) {
	info, err := svc.s.Stat(req.Key)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "stat: %v", err)
	}
	return &snapshotsv1.StatSnapshotResponse{Info: info.ToProto()}, nil
}

func (svc *Service) Update(ctx context.Context, req *snapshotsv1.UpdateSnapshotRequest) (*snapshotsv1.UpdateSnapshotResponse, error) {
	if req.Info == nil {
		return nil, status.Errorf(codes.InvalidArgument, "info is required")
	}
	info, err := svc.s.Update(req.Info.Name, req.Info.Labels)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "update: %v", err)
	}
	return &snapshotsv1.UpdateSnapshotResponse{Info: info.ToProto()}, nil
}

func (svc *Service) List(req *snapshotsv1.ListSnapshotsRequest, stream snapshotsv1.Snapshots_ListServer) error {
	infos := svc.s.List(req.Snapshotter)
	for _, info := range infos {
		resp := &snapshotsv1.ListSnapshotsResponse{Info: []*snapshotsv1.Info{info.ToProto()}}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
	return nil
}

func (svc *Service) Usage(ctx context.Context, req *snapshotsv1.UsageRequest) (*snapshotsv1.UsageResponse, error) {
	size, inodes, err := svc.s.Usage(req.Key)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "usage: %v", err)
	}
	return &snapshotsv1.UsageResponse{Size: size, Inodes: inodes}, nil
}

func (svc *Service) Cleanup(ctx context.Context, req *snapshotsv1.CleanupRequest) (*emptypb.Empty, error) {
	if err := svc.s.Cleanup(); err != nil {
		return nil, status.Errorf(codes.Internal, "cleanup: %v", err)
	}
	return &emptypb.Empty{}, nil
}

var _ snapshotsv1.SnapshotsServer = (*Service)(nil)
