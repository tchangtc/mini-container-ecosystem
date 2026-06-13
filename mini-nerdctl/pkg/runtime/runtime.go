// Package runtime provides exec and logs operations on running containers.
package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// Exec runs a command inside a running container and returns the exit code.
func Exec(ctx context.Context, client *containerd.Client, containerID string, cmd []string, tty bool, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	container, err := client.LoadContainer(ctx, containerID)
	if err != nil {
		return -1, fmt.Errorf("load container %q: %w", containerID, err)
	}

	task, err := container.Task(ctx, cio.Load)
	if err != nil {
		return -1, fmt.Errorf("get task for %q: %w (container may not be running)", containerID, err)
	}

	// Generate a unique exec ID
	execID := "exec-" + randHex(4)

	// Build process spec
	processSpec := &specs.Process{
		Args:     cmd,
		Terminal: tty,
		Cwd:      "/",
		Env:      []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
	}

	// Setup IO for the exec process
	var ioOpts []cio.Opt
	if tty {
		ioOpts = append(ioOpts, cio.WithTerminal)
	}
	ioOpts = append(ioOpts, cio.WithStreams(stdin, stdout, stderr))
	ioCreator := cio.NewCreator(ioOpts...)

	process, err := task.Exec(ctx, execID, processSpec, ioCreator)
	if err != nil {
		return -1, fmt.Errorf("exec in %q: %w", containerID, err)
	}
	defer process.Delete(ctx)

	// Wait for the exec process to exit
	waitCh, err := process.Wait(ctx)
	if err != nil {
		return -1, fmt.Errorf("wait exec: %w", err)
	}

	status := <-waitCh
	code, _, err := status.Result()
	if err != nil {
		return -1, fmt.Errorf("exec result: %w", err)
	}
	return int(code), nil
}

// Logs returns a reader for the container's stdout.
// It reads from containerd's default IO FIFO directory.
func Logs(ctx context.Context, client *containerd.Client, containerID string) (io.ReadCloser, error) {
	container, err := client.LoadContainer(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("load container %q: %w", containerID, err)
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get task for %q: %w (container may not be running)", containerID, err)
	}

	// Use containerd's default IO namespace (matching the client namespace)
	// The FIFO is at: /run/containerd/io.containerd.runtime.v2.task/<ns>/<id>/stdout
	ns := "default" // matches client.WithDefaultNamespace("default")
	fifoPath := fmt.Sprintf("/run/containerd/io.containerd.runtime.v2.task/%s/%s/stdout", ns, containerID)

	f, err := os.Open(fifoPath)
	if err != nil {
		return nil, fmt.Errorf("log file for %q not available: %w", containerID, err)
	}

	// Suppress unused task variable warning — task holds the IO lifecycle reference
	_ = task

	return f, nil
}

// randHex returns a hex-encoded random string of 2*size bytes.
func randHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based on crypto failure (extremely rare)
		return fmt.Sprintf("%x", size)
	}
	return hex.EncodeToString(b)
}
