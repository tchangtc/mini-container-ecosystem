// Package snapshot implements an overlayfs-based snapshotter for container
// root filesystems. It provides Prepare, Commit, Mount, and Remove operations
// on a tree of snapshot layers.
//
// The snapshotter is compatible with containerd v2's snapshot gRPC service.
package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	snapshotsv1 "github.com/containerd/containerd/api/services/snapshots/v1"
	"github.com/containerd/containerd/api/types"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DefaultRoot is the default root directory for snapshot storage.
const DefaultRoot = "/tmp/mini-containerd/data/snapshot"

// Kind mirrors the protobuf snapshot kind.
type Kind = snapshotsv1.Kind

const (
	KindView      = snapshotsv1.Kind_VIEW
	KindActive    = snapshotsv1.Kind_ACTIVE
	KindCommitted = snapshotsv1.Kind_COMMITTED
)

// SnapshotInfo holds metadata about a snapshot.
type SnapshotInfo struct {
	Name      string
	Parent    string
	Kind      Kind
	Labels    map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ToProto converts SnapshotInfo to the protobuf Info type.
func (s SnapshotInfo) ToProto() *snapshotsv1.Info {
	return &snapshotsv1.Info{
		Name:      s.Name,
		Parent:    s.Parent,
		Kind:      s.Kind,
		Labels:    s.Labels,
		CreatedAt: timestamppb.New(s.CreatedAt),
		UpdatedAt: timestamppb.New(s.UpdatedAt),
	}
}

// Snapshots is a tree using a minimal implementation similar to containerd's snapshotter.
// Active snapshots have a writable upper directory and can be mounted via overlayfs.
// Committed snapshots are read-only and serve as lower layers for child snapshots.
type Snapshotter struct {
	mu   sync.RWMutex
	root string
	// snapshots maps key → snapshot info
	snapshots map[string]*SnapshotInfo
	// mounts tracks currently mounted snapshots
	mounts map[string]string // key → mount target
}

// NewSnapshotter creates a new overlayfs snapshotter rooted at the given path.
func NewSnapshotter(root string) (*Snapshotter, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create snapshot root %q: %w", root, err)
	}
	return &Snapshotter{
		root:      root,
		snapshots: make(map[string]*SnapshotInfo),
		mounts:    make(map[string]string),
	}, nil
}

// getSnapDir returns the filesystem path for a snapshot key.
func (s *Snapshotter) getSnapDir(key string) string {
	return filepath.Join(s.root, key)
}

// getSnapFSPath returns the path to the actual filesystem layer (fs subdirectory).
func (s *Snapshotter) getSnapFSPath(key string) string {
	return filepath.Join(s.root, key, "fs")
}

// parentChain returns the ordered list of committed parent keys from the
// given key up to the root of the tree (most recent first).
func (s *Snapshotter) parentChain(key string) []string {
	var chain []string
	current := key
	for {
		info, ok := s.snapshots[current]
		if !ok || info.Parent == "" {
			break
		}
		parent := info.Parent
		chain = append(chain, parent)
		current = parent
	}
	return chain
}

// Prepare creates a new active snapshot. The snapshot is created from the
// given parent if specified. The returned mounts can be used to mount the
// snapshot for write access.
func (s *Snapshotter) Prepare(key, parent string) ([]*types.Mount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate parent if specified
	if parent != "" {
		parentInfo, ok := s.snapshots[parent]
		if !ok {
			return nil, fmt.Errorf("parent snapshot %q not found", parent)
		}
		if parentInfo.Kind != KindCommitted {
			return nil, fmt.Errorf("parent %q is not committed (kind=%v)", parent, parentInfo.Kind)
		}
	}

	// Check if this key already exists
	if _, ok := s.snapshots[key]; ok {
		return nil, fmt.Errorf("snapshot %q already exists", key)
	}

	// Create the snapshot directory
	snapDir := s.getSnapDir(key)
	fsDir := s.getSnapFSPath(key)
	workDir := filepath.Join(snapDir, "work")

	for _, dir := range []string{fsDir, workDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create snapshot dirs for %q: %w", key, err)
		}
	}

	// Record the snapshot
	now := time.Now()
	s.snapshots[key] = &SnapshotInfo{
		Name:      key,
		Parent:    parent,
		Kind:      KindActive,
		Labels:    make(map[string]string),
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Return mounts for overlayfs
	return s.mountsWithoutLock(key)
}

// View creates a read-only snapshot of a parent. Similar to Prepare but the
// result is a read-only mount (no upperdir).
func (s *Snapshotter) View(key, parent string) ([]*types.Mount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if parent == "" {
		return nil, fmt.Errorf("view requires a parent snapshot")
	}

	parentInfo, ok := s.snapshots[parent]
	if !ok {
		return nil, fmt.Errorf("parent snapshot %q not found", parent)
	}
	if parentInfo.Kind != KindCommitted {
		return nil, fmt.Errorf("parent %q is not committed", parent)
	}

	if _, ok := s.snapshots[key]; ok {
		return nil, fmt.Errorf("snapshot %q already exists", key)
	}

	now := time.Now()
	s.snapshots[key] = &SnapshotInfo{
		Name:      key,
		Parent:    parent,
		Kind:      KindView,
		Labels:    make(map[string]string),
		CreatedAt: now,
		UpdatedAt: now,
	}

	return s.mountsWithoutLock(key)
}

// Commit freezes an active snapshot into a committed (read-only) snapshot.
// The key is renamed to name.
func (s *Snapshotter) Commit(key, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.snapshots[key]
	if !ok {
		return fmt.Errorf("snapshot %q not found", key)
	}
	if info.Kind != KindActive {
		return fmt.Errorf("snapshot %q is not active (kind=%v)", key, info.Kind)
	}

	// Rename if name differs from key
	if name != key {
		oldDir := s.getSnapDir(key)
		newDir := s.getSnapDir(name)
		if err := os.Rename(oldDir, newDir); err != nil {
			return fmt.Errorf("rename snapshot %q → %q: %w", key, name, err)
		}
		delete(s.snapshots, key)
	}

	// Update metadata
	info.Name = name
	info.Kind = KindCommitted
	info.UpdatedAt = time.Now()
	if name != key {
		s.snapshots[name] = info
	}

	return nil
}

// Remove deletes a snapshot. The snapshot must not have children.
func (s *Snapshotter) Remove(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.snapshots[key]
	if !ok {
		return fmt.Errorf("snapshot %q not found", key)
	}

	// Check for children
	for _, other := range s.snapshots {
		if other.Parent == key {
			return fmt.Errorf("cannot remove %q: has child %q", key, other.Name)
		}
	}

	// Remove filesystem
	snapDir := s.getSnapDir(key)
	if err := os.RemoveAll(snapDir); err != nil {
		return fmt.Errorf("remove snapshot dir %q: %w", snapDir, err)
	}

	delete(s.snapshots, key)
	_ = info
	return nil
}

// Mounts returns the mount points for the given snapshot key as containerd
// mount types. For active snapshots this includes an overlayfs mount with
// upperdir. For committed snapshots, this returns a bind mount.
func (s *Snapshotter) Mounts(key string) ([]*types.Mount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mountsWithoutLock(key)
}

// mountsWithoutLock generates overlayfs mount entries. Must hold at least RLock.
func (s *Snapshotter) mountsWithoutLock(key string) ([]*types.Mount, error) {
	info, ok := s.snapshots[key]
	if !ok {
		return nil, fmt.Errorf("snapshot %q not found", key)
	}

	var mounts []*types.Mount

	if info.Kind == KindCommitted {
		// Committed snapshots use a bind mount to the fs directory
		fsDir := s.getSnapFSPath(key)
		mounts = append(mounts, &types.Mount{
			Type:    "bind",
			Source:  fsDir,
			Options: []string{"rbind", "rprivate"},
		})
	} else {
		// Active/View snapshots use overlayfs
		// Collect parent layers (from root to leaf)
		parents := s.parentChain(key)
		var lowerDirs []string
		for _, p := range parents {
			lowerDirs = append(lowerDirs, s.getSnapFSPath(p))
		}

		if len(lowerDirs) == 0 {
			// No parent — direct bind mount for initial layer
			fsDir := s.getSnapFSPath(key)
			mounts = append(mounts, &types.Mount{
				Type:    "bind",
				Source:  fsDir,
				Options: []string{"rbind", "rprivate"},
			})
		} else if info.Kind == KindView {
			// View: read-only overlay (no upperdir)
			opts := []string{
				fmt.Sprintf("lowerdir=%s", strings.Join(lowerDirs, ":")),
				"index=off",
			}
			mounts = append(mounts, &types.Mount{
				Type:    "overlay",
				Source:  "overlay",
				Options: opts,
			})
		} else {
			// Active: writable overlay with upperdir + workdir
			snapDir := s.getSnapDir(key)
			upperDir := s.getSnapFSPath(key)
			workDir := filepath.Join(snapDir, "work")

			opts := []string{
				fmt.Sprintf("lowerdir=%s", strings.Join(lowerDirs, ":")),
				fmt.Sprintf("upperdir=%s", upperDir),
				fmt.Sprintf("workdir=%s", workDir),
				"index=off",
			}
			mounts = append(mounts, &types.Mount{
				Type:    "overlay",
				Source:  "overlay",
				Options: opts,
			})
		}
	}

	return mounts, nil
}

// Stat returns the snapshot info for a key.
func (s *Snapshotter) Stat(key string) (*SnapshotInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, ok := s.snapshots[key]
	if !ok {
		return nil, fmt.Errorf("snapshot %q not found", key)
	}
	return info, nil
}

// Update updates the labels on a snapshot.
func (s *Snapshotter) Update(key string, labels map[string]string) (*SnapshotInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, ok := s.snapshots[key]
	if !ok {
		return nil, fmt.Errorf("snapshot %q not found", key)
	}

	if info.Labels == nil {
		info.Labels = make(map[string]string)
	}
	for k, v := range labels {
		info.Labels[k] = v
	}
	info.UpdatedAt = time.Now()
	return info, nil
}

// List returns all snapshots matching the snapshotter filter.
func (s *Snapshotter) List(snapshotter string) []*SnapshotInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*SnapshotInfo
	for _, info := range s.snapshots {
		result = append(result, info)
	}
	return result
}

// Usage returns disk usage for a snapshot (simplified — returns stat info).
func (s *Snapshotter) Usage(key string) (int64, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.snapshots[key]
	if !ok {
		return 0, 0, fmt.Errorf("snapshot %q not found", key)
	}

	snapDir := s.getSnapDir(key)
	var totalSize int64
	filepath.Walk(snapDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	return totalSize, totalSize, nil
}

// Cleanup removes unused committed snapshots (those with no active children).
// In this simplified implementation, it's a no-op — removal is explicit.
func (s *Snapshotter) Cleanup() error {
	return nil
}

// ensure syscall import is used
var _ = syscall.Mount
