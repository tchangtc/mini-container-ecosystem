// mini-containerd — minimal container runtime daemon.
// Implements enough of containerd v2's gRPC API for mini-nerdctl to work.
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	containersv1 "github.com/containerd/containerd/api/services/containers/v1"
	contentv1 "github.com/containerd/containerd/api/services/content/v1"
	eventsv1 "github.com/containerd/containerd/api/services/events/v1"
	imagesv1 "github.com/containerd/containerd/api/services/images/v1"
	introspectionv1 "github.com/containerd/containerd/api/services/introspection/v1"
	leasesv1 "github.com/containerd/containerd/api/services/leases/v1"
	namespacesv1 "github.com/containerd/containerd/api/services/namespaces/v1"
	snapshotsv1 "github.com/containerd/containerd/api/services/snapshots/v1"
	tasksv1 "github.com/containerd/containerd/api/services/tasks/v1"
	transferv1 "github.com/containerd/containerd/api/services/transfer/v1"
	versionv1 "github.com/containerd/containerd/api/services/version/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/tcherry/mini-container-ecosystem/mini-containerd/internal/containers"
	"github.com/tcherry/mini-container-ecosystem/mini-containerd/internal/content"
	"github.com/tcherry/mini-container-ecosystem/mini-containerd/internal/image"
	"github.com/tcherry/mini-container-ecosystem/mini-containerd/internal/services"
	"github.com/tcherry/mini-container-ecosystem/mini-containerd/internal/snapshot"
	"github.com/tcherry/mini-container-ecosystem/mini-containerd/internal/task"
)

const (
	defaultSocket = "/tmp/mini-containerd/containerd.sock"
	dataRoot      = "/tmp/mini-containerd/data"
)

func main() {
	fmt.Fprintf(os.Stderr, "mini-containerd — all services\n")

	_ = os.MkdirAll(dataRoot, 0o755)

	// Core services
	contentStore, _ := content.NewStore(content.DefaultRoot)
	snapshotter, _ := snapshot.NewSnapshotter(snapshot.DefaultRoot)
	imageStore := image.NewStore(contentStore, snapshotter)
	taskMgr := task.NewManager()

	// Auxiliary services
	containerStore := containers.NewStore()
	nsStore := services.NewNamespaceStore()

	fmt.Fprintf(os.Stderr, "services: content snapshot image task containers namespaces version leases events introspection\n")

	grpcServer := grpc.NewServer()

	// Core
	contentv1.RegisterContentServer(grpcServer, content.NewService(contentStore))
	imagesv1.RegisterImagesServer(grpcServer, image.NewService(imageStore))
	snapshotsv1.RegisterSnapshotsServer(grpcServer, snapshot.NewService(snapshotter))
	tasksv1.RegisterTasksServer(grpcServer, task.NewService(taskMgr))

	// Auxiliary
	containersv1.RegisterContainersServer(grpcServer, containers.NewService(containerStore))
	namespacesv1.RegisterNamespacesServer(grpcServer, services.NewNamespaceService(nsStore))
	versionv1.RegisterVersionServer(grpcServer, services.NewVersionService())
	leasesv1.RegisterLeasesServer(grpcServer, services.NewLeasesService())
	eventsv1.RegisterEventsServer(grpcServer, services.NewEventsService())
	introspectionv1.RegisterIntrospectionServer(grpcServer, services.NewIntrospectionService())
	transferv1.RegisterTransferServer(grpcServer, services.NewTransferService())

	reflection.Register(grpcServer)

	// Socket
	os.Remove(defaultSocket)
	os.MkdirAll(defaultSocket[:len(defaultSocket)-len("/containerd.sock")], 0o755)

	listener, err := net.Listen("unix", defaultSocket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\nShutting down...\n")
		grpcServer.GracefulStop()
	}()

	fmt.Fprintf(os.Stderr, "Listening on %s\n", defaultSocket)
	if err := grpcServer.Serve(listener); err != nil {
		os.Exit(1)
	}
}
