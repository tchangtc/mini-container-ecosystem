// Package services provides lightweight auxiliary gRPC services required by
// the containerd v2 client handshake: version, namespaces, leases, events,
// and introspection.
package services

import (
	"context"
	"fmt"
	"sync"

	eventsv1 "github.com/containerd/containerd/api/services/events/v1"
	introspectionv1 "github.com/containerd/containerd/api/services/introspection/v1"
	leasesv1 "github.com/containerd/containerd/api/services/leases/v1"
	transferv1 "github.com/containerd/containerd/api/services/transfer/v1"
	namespacesv1 "github.com/containerd/containerd/api/services/namespaces/v1"
	versionv1 "github.com/containerd/containerd/api/services/version/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ── Version ─────────────────────────────────────────────────────

type VersionService struct {
	versionv1.UnimplementedVersionServer
}

func NewVersionService() *VersionService {
	return &VersionService{}
}

func (v *VersionService) Version(ctx context.Context, _ *emptypb.Empty) (*versionv1.VersionResponse, error) {
	return &versionv1.VersionResponse{
		Version:  "v2.0.0-mini",
		Revision: "mini-containerd",
	}, nil
}

// ── Namespaces ──────────────────────────────────────────────────

type NamespaceStore struct {
	mu   sync.RWMutex
	data map[string]*namespacesv1.Namespace
}

func NewNamespaceStore() *NamespaceStore {
	ns := &NamespaceStore{data: make(map[string]*namespacesv1.Namespace)}
	// Pre-create the default namespace
	ns.data["default"] = &namespacesv1.Namespace{Name: "default"}
	return ns
}

type NamespaceService struct {
	namespacesv1.UnimplementedNamespacesServer
	store *NamespaceStore
}

func NewNamespaceService(store *NamespaceStore) *NamespaceService {
	return &NamespaceService{store: store}
}

func (svc *NamespaceService) Get(ctx context.Context, req *namespacesv1.GetNamespaceRequest) (*namespacesv1.GetNamespaceResponse, error) {
	svc.store.mu.RLock()
	defer svc.store.mu.RUnlock()
	ns, ok := svc.store.data[req.Name]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "namespace %q not found", req.Name)
	}
	return &namespacesv1.GetNamespaceResponse{Namespace: ns}, nil
}

func (svc *NamespaceService) List(ctx context.Context, req *namespacesv1.ListNamespacesRequest) (*namespacesv1.ListNamespacesResponse, error) {
	svc.store.mu.RLock()
	defer svc.store.mu.RUnlock()
	var result []*namespacesv1.Namespace
	for _, ns := range svc.store.data {
		result = append(result, ns)
	}
	return &namespacesv1.ListNamespacesResponse{Namespaces: result}, nil
}

func (svc *NamespaceService) Create(ctx context.Context, req *namespacesv1.CreateNamespaceRequest) (*namespacesv1.CreateNamespaceResponse, error) {
	svc.store.mu.Lock()
	defer svc.store.mu.Unlock()
	if _, ok := svc.store.data[req.Namespace.Name]; ok {
		// Already exists — namespaces are idempotent
		return &namespacesv1.CreateNamespaceResponse{Namespace: svc.store.data[req.Namespace.Name]}, nil
	}
	svc.store.data[req.Namespace.Name] = req.Namespace
	return &namespacesv1.CreateNamespaceResponse{Namespace: req.Namespace}, nil
}

func (svc *NamespaceService) Update(ctx context.Context, req *namespacesv1.UpdateNamespaceRequest) (*namespacesv1.UpdateNamespaceResponse, error) {
	svc.store.mu.Lock()
	defer svc.store.mu.Unlock()
	if _, ok := svc.store.data[req.Namespace.Name]; !ok {
		return nil, status.Errorf(codes.NotFound, "namespace %q not found", req.Namespace.Name)
	}
	svc.store.data[req.Namespace.Name] = req.Namespace
	return &namespacesv1.UpdateNamespaceResponse{Namespace: req.Namespace}, nil
}

func (svc *NamespaceService) Delete(ctx context.Context, req *namespacesv1.DeleteNamespaceRequest) (*emptypb.Empty, error) {
	svc.store.mu.Lock()
	defer svc.store.mu.Unlock()
	delete(svc.store.data, req.Name)
	return &emptypb.Empty{}, nil
}

// ── Leases (noop) ───────────────────────────────────────────────

type LeasesService struct {
	leasesv1.UnimplementedLeasesServer
}

func NewLeasesService() *LeasesService {
	return &LeasesService{}
}

func (svc *LeasesService) Create(ctx context.Context, req *leasesv1.CreateRequest) (*leasesv1.CreateResponse, error) {
	return &leasesv1.CreateResponse{Lease: &leasesv1.Lease{ID: req.ID}}, nil
}

func (svc *LeasesService) Delete(ctx context.Context, req *leasesv1.DeleteRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (svc *LeasesService) List(ctx context.Context, req *leasesv1.ListRequest) (*leasesv1.ListResponse, error) {
	return &leasesv1.ListResponse{}, nil
}

func (svc *LeasesService) AddResource(ctx context.Context, req *leasesv1.AddResourceRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (svc *LeasesService) DeleteResource(ctx context.Context, req *leasesv1.DeleteResourceRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (svc *LeasesService) ListResources(ctx context.Context, req *leasesv1.ListResourcesRequest) (*leasesv1.ListResourcesResponse, error) {
	return &leasesv1.ListResourcesResponse{}, nil
}

// ── Events (noop) ───────────────────────────────────────────────

type EventsService struct {
	eventsv1.UnimplementedEventsServer
}

func NewEventsService() *EventsService {
	return &EventsService{}
}

func (svc *EventsService) Publish(ctx context.Context, req *eventsv1.PublishRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (svc *EventsService) Forward(ctx context.Context, req *eventsv1.ForwardRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

func (svc *EventsService) Subscribe(req *eventsv1.SubscribeRequest, stream eventsv1.Events_SubscribeServer) error {
	// No events to stream — just block until context cancelled
	<-stream.Context().Done()
	return nil
}

// ── Introspection ───────────────────────────────────────────────

type IntrospectionService struct {
	introspectionv1.UnimplementedIntrospectionServer
}

func NewIntrospectionService() *IntrospectionService {
	return &IntrospectionService{}
}

func (svc *IntrospectionService) Plugins(ctx context.Context, req *introspectionv1.PluginsRequest) (*introspectionv1.PluginsResponse, error) {
	return &introspectionv1.PluginsResponse{
		Plugins: []*introspectionv1.Plugin{
			{ID: "content", Type: "io.containerd.content.v1"},
			{ID: "snapshots", Type: "io.containerd.snapshotter.v1"},
			{ID: "tasks", Type: "io.containerd.task.v2"},
		},
	}, nil
}

func (svc *IntrospectionService) Server(ctx context.Context, req *emptypb.Empty) (*introspectionv1.ServerResponse, error) {
	return &introspectionv1.ServerResponse{}, nil
}

// ── Transfer ───────────────────────────────────────────────────

type TransferService struct {
	transferv1.UnimplementedTransferServer
}

func NewTransferService() *TransferService {
	return &TransferService{}
}

func (svc *TransferService) Transfer(ctx context.Context, req *transferv1.TransferRequest) (*emptypb.Empty, error) {
	// Accept and succeed — actual pull is done via content+image services directly
	return &emptypb.Empty{}, nil
}

// Ensure import is used
var _ = fmt.Sprintf
var _ = transferv1.TransferRequest{}
