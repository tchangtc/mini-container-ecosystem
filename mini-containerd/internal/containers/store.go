// Package containers implements the Containers gRPC service — in-memory
// container metadata CRUD compatible with containerd v2 API.
package containers

import (
	"context"
	"fmt"
	"sync"

	containersv1 "github.com/containerd/containerd/api/services/containers/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Store holds container metadata in memory.
type Store struct {
	mu   sync.RWMutex
	data map[string]*containersv1.Container
}

// NewStore creates an in-memory container store.
func NewStore() *Store {
	return &Store{data: make(map[string]*containersv1.Container)}
}

// Service implements the ContainersServer gRPC interface.
type Service struct {
	containersv1.UnimplementedContainersServer
	store *Store
}

// NewService creates a new containers gRPC service.
func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (svc *Service) Get(ctx context.Context, req *containersv1.GetContainerRequest) (*containersv1.GetContainerResponse, error) {
	svc.store.mu.RLock()
	defer svc.store.mu.RUnlock()
	c, ok := svc.store.data[req.ID]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "container %q not found", req.ID)
	}
	return &containersv1.GetContainerResponse{Container: c}, nil
}

func (svc *Service) List(ctx context.Context, req *containersv1.ListContainersRequest) (*containersv1.ListContainersResponse, error) {
	svc.store.mu.RLock()
	defer svc.store.mu.RUnlock()
	var result []*containersv1.Container
	for _, c := range svc.store.data {
		result = append(result, c)
	}
	return &containersv1.ListContainersResponse{Containers: result}, nil
}

func (svc *Service) ListStream(req *containersv1.ListContainersRequest, stream containersv1.Containers_ListStreamServer) error {
	svc.store.mu.RLock()
	defer svc.store.mu.RUnlock()
	for _, c := range svc.store.data {
		if err := stream.Send(&containersv1.ListContainerMessage{Container: c}); err != nil {
			return err
		}
	}
	return nil
}

func (svc *Service) Create(ctx context.Context, req *containersv1.CreateContainerRequest) (*containersv1.CreateContainerResponse, error) {
	if req.Container == nil {
		return nil, status.Error(codes.InvalidArgument, "container is required")
	}
	c := req.Container
	svc.store.mu.Lock()
	defer svc.store.mu.Unlock()
	if _, ok := svc.store.data[c.ID]; ok {
		return nil, status.Errorf(codes.AlreadyExists, "container %q exists", c.ID)
	}
	svc.store.data[c.ID] = c
	return &containersv1.CreateContainerResponse{Container: c}, nil
}

func (svc *Service) Update(ctx context.Context, req *containersv1.UpdateContainerRequest) (*containersv1.UpdateContainerResponse, error) {
	if req.Container == nil {
		return nil, status.Error(codes.InvalidArgument, "container is required")
	}
	svc.store.mu.Lock()
	defer svc.store.mu.Unlock()
	if _, ok := svc.store.data[req.Container.ID]; !ok {
		return nil, status.Errorf(codes.NotFound, "container %q not found", req.Container.ID)
	}
	svc.store.data[req.Container.ID] = req.Container
	return &containersv1.UpdateContainerResponse{Container: req.Container}, nil
}

func (svc *Service) Delete(ctx context.Context, req *containersv1.DeleteContainerRequest) (*emptypb.Empty, error) {
	svc.store.mu.Lock()
	defer svc.store.mu.Unlock()
	if _, ok := svc.store.data[req.ID]; !ok {
		return nil, status.Errorf(codes.NotFound, "container %q not found", req.ID)
	}
	delete(svc.store.data, req.ID)
	return &emptypb.Empty{}, nil
}

// Ensure import is used
var _ = fmt.Sprintf
