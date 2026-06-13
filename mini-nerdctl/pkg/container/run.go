package container

import (
	"context"
	"fmt"
	"io"
	"os"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"
	"github.com/tcherry/mini-container-ecosystem/mini-nerdctl/pkg/reference"
)

// RunOpts holds options for running a container.
type RunOpts struct {
	// Image is the raw image reference (e.g. "alpine:latest").
	Image string
	// Cmd is the command to run (nil or empty → use image default).
	Cmd []string
	// Interactive enables stdin attachment.
	Interactive bool
	// TTY allocates a pseudo-TTY.
	TTY bool
	// Detach runs the container in the background.
	Detach bool
	// Rm removes the container after it exits (default true for foreground).
	Rm bool
	// Env adds environment variables (KEY=VALUE).
	Env []string
}

// Run creates a container from the given image, starts it, and optionally
// waits for it to exit. Returns the container ID and (if foreground) exit code.
func Run(ctx context.Context, client *containerd.Client, opts RunOpts, stdout, stderr io.Writer) (string, int, error) {
	ref := reference.Normalize(opts.Image)

	// 1. Ensure image exists and is unpacked
	image, err := client.GetImage(ctx, ref)
	if err != nil {
		// Pull if not found
		image, err = client.Pull(ctx, ref, containerd.WithPullUnpack)
		if err != nil {
			return "", -1, fmt.Errorf("pull image %q: %w", ref, err)
		}
	} else {
		// Image exists — ensure it's unpacked for snapshot use
		if unpackErr := image.Unpack(ctx, "overlayfs"); unpackErr != nil {
			return "", -1, fmt.Errorf("unpack image %q: %w", ref, unpackErr)
		}
	}

	// 2. Determine the command
	cmd := opts.Cmd
	if len(cmd) == 0 {
		// Use image default entrypoint+cmd
		cmd = nil
	}

	// 3. Generate a unique container ID
	id := generateID()

	// 4. Build OCI spec options
	var specOpts []oci.SpecOpts
	specOpts = append(specOpts, oci.WithImageConfig(image))
	if cmd != nil {
		specOpts = append(specOpts, oci.WithProcessArgs(cmd...))
	}
	for _, env := range opts.Env {
		specOpts = append(specOpts, oci.WithEnv([]string{env}))
	}

	// 5. Create container
	containerdOpts := []containerd.NewContainerOpts{
		containerd.WithImage(image),
		containerd.WithNewSnapshot(id, image),
		containerd.WithNewSpec(specOpts...),
	}

	container, err := client.NewContainer(ctx, id, containerdOpts...)
	if err != nil {
		return "", -1, fmt.Errorf("create container: %w", err)
	}

	// 6. Setup IO
	var ioOpts []cio.Opt
	if opts.Interactive || !opts.Detach {
		ioOpts = append(ioOpts, cio.WithStdio)
	}
	if opts.Detach {
		ioOpts = append(ioOpts, cio.WithStreams(os.Stdin, stdout, stderr))
	}
	ioCreator := cio.NewCreator(ioOpts...)

	// 7. Create and start task
	task, err := container.NewTask(ctx, ioCreator)
	if err != nil {
		container.Delete(ctx)
		return "", -1, fmt.Errorf("create task: %w", err)
	}

	if err := task.Start(ctx); err != nil {
		task.Delete(ctx)
		container.Delete(ctx)
		return "", -1, fmt.Errorf("start task: %w", err)
	}

	exitCode := 0
	if opts.Detach {
		// Background mode: return immediately
		return id, 0, nil
	}

	// 8. Wait for exit (foreground mode)
	exitStatus, err := task.Wait(ctx)
	if err != nil {
		return "", -1, fmt.Errorf("wait task: %w", err)
	}
	status := <-exitStatus
	code, _, err := status.Result()
	if err != nil {
		exitCode = -1
	} else {
		exitCode = int(code)
	}

	// 9. Cleanup
	if _, err := task.Delete(ctx); err != nil {
		return id, exitCode, fmt.Errorf("delete task: %w", err)
	}
	if err := container.Delete(ctx); err != nil {
		return id, exitCode, fmt.Errorf("delete container: %w", err)
	}

	return id, exitCode, nil
}
