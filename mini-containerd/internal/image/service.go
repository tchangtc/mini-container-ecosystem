package image

import (
	"context"

	imagesv1 "github.com/containerd/containerd/api/services/images/v1"
	containerdimages "github.com/containerd/containerd/v2/core/images"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Service implements containerd's Images gRPC service.
type Service struct {
	imagesv1.UnimplementedImagesServer
	store *Store
}

// NewService creates a new images gRPC service.
func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (svc *Service) Get(ctx context.Context, req *imagesv1.GetImageRequest) (*imagesv1.GetImageResponse, error) {
	img, err := svc.store.Get(req.Name)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "image %q: %v", req.Name, err)
	}
	return &imagesv1.GetImageResponse{Image: ToGRPC(img)}, nil
}

func (svc *Service) List(ctx context.Context, req *imagesv1.ListImagesRequest) (*imagesv1.ListImagesResponse, error) {
	imgs := svc.store.List()
	var protoImgs []*imagesv1.Image
	for _, img := range imgs {
		protoImgs = append(protoImgs, ToGRPC(img))
	}
	return &imagesv1.ListImagesResponse{Images: protoImgs}, nil
}

func (svc *Service) Create(ctx context.Context, req *imagesv1.CreateImageRequest) (*imagesv1.CreateImageResponse, error) {
	if req.Image == nil {
		return nil, status.Error(codes.InvalidArgument, "image is required")
	}
	img, err := svc.store.Create(fromGRPC(req.Image))
	if err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "create %q: %v", req.Image.Name, err)
	}
	return &imagesv1.CreateImageResponse{Image: ToGRPC(img)}, nil
}

func (svc *Service) Update(ctx context.Context, req *imagesv1.UpdateImageRequest) (*imagesv1.UpdateImageResponse, error) {
	existing, err := svc.store.Get(req.Image.Name)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "image %q: %v", req.Image.Name, err)
	}
	if req.Image.Labels != nil {
		if existing.Labels == nil {
			existing.Labels = make(map[string]string)
		}
		for k, v := range req.Image.Labels {
			existing.Labels[k] = v
		}
	}
	return &imagesv1.UpdateImageResponse{Image: ToGRPC(existing)}, nil
}

func (svc *Service) Delete(ctx context.Context, req *imagesv1.DeleteImageRequest) (*emptypb.Empty, error) {
	if err := svc.store.Delete(req.Name); err != nil {
		return nil, status.Errorf(codes.NotFound, "image %q: %v", req.Name, err)
	}
	return &emptypb.Empty{}, nil
}

// fromGRPC converts a gRPC proto Image to the internal containerd Image type.
func fromGRPC(proto *imagesv1.Image) containerdimages.Image {
	return containerdimages.Image{
		Name:   proto.Name,
		Labels: proto.Labels,
	}
}
