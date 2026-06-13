// Package container implements container lifecycle operations:
// run, list (ps), stop, and remove (rm).
package container

import (
	"context"
	"fmt"
	"math/rand"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
)

// ContainerInfo holds display-ready container metadata.
type ContainerInfo struct {
	ID     string
	Image  string
	Status string // "running", "stopped", "created", "paused"
	Pid    uint32
}

// generateID creates a short random container ID.
func generateID() string {
	const charset = "abcdef0123456789"
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, 16)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}

// List returns all containers with their current status.
func List(ctx context.Context, client *containerd.Client) ([]ContainerInfo, error) {
	containers, err := client.Containers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	var result []ContainerInfo
	for _, c := range containers {
		info := ContainerInfo{
			ID: c.ID(),
		}

		labels, err := c.Labels(ctx)
		if err == nil {
			info.Image = labels["io.containerd.image.name"]
		}

		task, err := c.Task(ctx, nil)
		if err != nil {
			info.Status = "created (no task)"
		} else {
			status, err := task.Status(ctx)
			if err != nil {
				info.Status = "unknown"
			} else {
				info.Status = string(status.Status)
				info.Pid = task.Pid()
			}
		}

		result = append(result, info)
	}
	return result, nil
}

// Stop stops a running container by sending SIGTERM, waiting for the specified
// timeout, then sending SIGKILL if the container is still running.
func Stop(ctx context.Context, client *containerd.Client, id string, timeout time.Duration) error {
	container, err := client.LoadContainer(ctx, id)
	if err != nil {
		return fmt.Errorf("load container %q: %w", id, err)
	}

	task, err := container.Task(ctx, cio.Load)
	if err != nil {
		return fmt.Errorf("get task for %q: %w (container may not be running)", id, err)
	}

	// Get current status
	status, err := task.Status(ctx)
	if err != nil {
		return fmt.Errorf("get status for %q: %w", id, err)
	}
	if status.Status == containerd.Stopped || status.Status == containerd.Created {
		return nil // already stopped or never started
	}

	// Send SIGTERM
	if err := task.Kill(ctx, syscall.SIGTERM); err != nil {
		return fmt.Errorf("kill SIGTERM %q: %w", id, err)
	}

	// Wait with timeout
	waitCh, err := task.Wait(ctx)
	if err != nil {
		return fmt.Errorf("wait for %q: %w", id, err)
	}

	timeoutCh := time.After(timeout)
	select {
	case <-waitCh:
		return nil // exited gracefully
	case <-timeoutCh:
		// Force kill
		if err := task.Kill(ctx, syscall.SIGKILL); err != nil {
			return fmt.Errorf("kill SIGKILL %q: %w", id, err)
		}
		<-waitCh
		return nil
	}
}

// Remove deletes a container. If force is true, the container is stopped first.
func Remove(ctx context.Context, client *containerd.Client, id string, force bool) error {
	container, err := client.LoadContainer(ctx, id)
	if err != nil {
		return fmt.Errorf("load container %q: %w", id, err)
	}

	// If force, attempt to stop first (ignore errors — container may already be stopped)
	if force {
		task, err := container.Task(ctx, nil)
		if err == nil {
			status, _ := task.Status(ctx)
			if status.Status == containerd.Running {
				task.Kill(ctx, syscall.SIGKILL)
				waitCh, _ := task.Wait(ctx)
				if waitCh != nil {
					<-waitCh
				}
			}
			task.Delete(ctx)
		}
	}

	if err := container.Delete(ctx); err != nil {
		return fmt.Errorf("delete container %q: %w", id, err)
	}
	return nil
}
