// mini-docker daemon — Docker-compatible REST API over Unix socket,
// backed by mini-containerd for container operations.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	containerd "github.com/containerd/containerd/v2/client"

	"github.com/tcherry/mini-container-ecosystem/mini-docker/internal/api"
)

const (
	defaultContainerdAddr = "/tmp/mini-containerd/containerd.sock"
	defaultDockerSocket   = "/tmp/mini-docker/docker.sock"
)

func main() {
	fmt.Fprintf(os.Stderr, "mini-docker daemon — Phase 3\n")

	client, err := containerd.New(defaultContainerdAddr, containerd.WithDefaultNamespace("default"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: connect containerd at %s: %v\n", defaultContainerdAddr, err)
		os.Exit(1)
	}
	defer client.Close()
	fmt.Fprintf(os.Stderr, "Connected to containerd at %s\n", defaultContainerdAddr)

	server := api.NewServer(client)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\nShutting down...\n")
		os.Exit(0)
	}()

	if err := server.ListenAndServe(defaultDockerSocket); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
