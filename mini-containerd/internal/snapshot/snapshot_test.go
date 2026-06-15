package snapshot_test

import (
	"testing"

	"github.com/tcherry/mini-container-ecosystem/mini-containerd/internal/snapshot"
)

func TestPrepareCommitRemove(t *testing.T) {
	s, err := snapshot.NewSnapshotter(t.TempDir())
	if err != nil {
		t.Fatalf("NewSnapshotter: %v", err)
	}

	// Prepare an active snapshot
	mounts, err := s.Prepare("layer1", "")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(mounts) == 0 {
		t.Fatal("expected mounts from Prepare")
	}

	// Verify it exists
	info, err := s.Stat("layer1")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Kind != snapshot.KindActive {
		t.Fatalf("expected active, got %v", info.Kind)
	}

	// Commit it
	if err := s.Commit("layer1", "layer1"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	info, err = s.Stat("layer1")
	if err != nil {
		t.Fatalf("Stat after commit: %v", err)
	}
	if info.Kind != snapshot.KindCommitted {
		t.Fatalf("expected committed, got %v", info.Kind)
	}

	// Remove
	if err := s.Remove("layer1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	_, err = s.Stat("layer1")
	if err == nil {
		t.Fatal("expected error after remove")
	}
}

func TestPrepareWithParent(t *testing.T) {
	s, err := snapshot.NewSnapshotter(t.TempDir())
	if err != nil {
		t.Fatalf("NewSnapshotter: %v", err)
	}

	// Create and commit base layer
	if _, err := s.Prepare("base", ""); err != nil {
		t.Fatalf("Prepare base: %v", err)
	}
	if err := s.Commit("base", "base"); err != nil {
		t.Fatalf("Commit base: %v", err)
	}

	// Create child layer with parent
	mounts, err := s.Prepare("child", "base")
	if err != nil {
		t.Fatalf("Prepare child: %v", err)
	}

	// Child should have overlay mounts with lowerdir from parent
	if len(mounts) == 0 {
		t.Fatal("expected overlay mounts for child")
	}

	info, err := s.Stat("child")
	if err != nil {
		t.Fatalf("Stat child: %v", err)
	}
	if info.Parent != "base" {
		t.Fatalf("expected parent 'base', got %q", info.Parent)
	}
}

func TestParentMustBeCommitted(t *testing.T) {
	s, err := snapshot.NewSnapshotter(t.TempDir())
	if err != nil {
		t.Fatalf("NewSnapshotter: %v", err)
	}

	// Create active snapshot (not committed)
	if _, err := s.Prepare("active", ""); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	// Try to use active snapshot as parent → should fail
	_, err = s.Prepare("child", "active")
	if err == nil {
		t.Fatal("expected error using active snapshot as parent")
	}
}

func TestCannotRemoveParentOfChild(t *testing.T) {
	s, err := snapshot.NewSnapshotter(t.TempDir())
	if err != nil {
		t.Fatalf("NewSnapshotter: %v", err)
	}

	// Create base and commit
	if _, err := s.Prepare("base", ""); err != nil {
		t.Fatalf("Prepare base: %v", err)
	}
	if err := s.Commit("base", "base"); err != nil {
		t.Fatalf("Commit base: %v", err)
	}

	// Create child
	if _, err := s.Prepare("child", "base"); err != nil {
		t.Fatalf("Prepare child: %v", err)
	}

	// Try to remove base → should fail (has child)
	err = s.Remove("base")
	if err == nil {
		t.Fatal("expected error removing snapshot with child")
	}
}

func TestCommitRename(t *testing.T) {
	s, err := snapshot.NewSnapshotter(t.TempDir())
	if err != nil {
		t.Fatalf("NewSnapshotter: %v", err)
	}

	// Prepare with temp key
	if _, err := s.Prepare("tmp-abc123", ""); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	// Commit with a permanent name
	if err := s.Commit("tmp-abc123", "sha256:deadbeef"); err != nil {
		t.Fatalf("Commit rename: %v", err)
	}

	// Old key should not exist
	_, err = s.Stat("tmp-abc123")
	if err == nil {
		t.Fatal("expected old key to not exist after rename commit")
	}

	// New name should exist
	info, err := s.Stat("sha256:deadbeef")
	if err != nil {
		t.Fatalf("Stat new name: %v", err)
	}
	if info.Name != "sha256:deadbeef" {
		t.Fatalf("expected name to be updated")
	}
}

func TestView(t *testing.T) {
	s, err := snapshot.NewSnapshotter(t.TempDir())
	if err != nil {
		t.Fatalf("NewSnapshotter: %v", err)
	}

	// Create and commit base
	if _, err := s.Prepare("base", ""); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := s.Commit("base", "base"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Create a view (read-only snapshot)
	mounts, err := s.View("view1", "base")
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if len(mounts) == 0 {
		t.Fatal("expected mounts from View")
	}

	info, err := s.Stat("view1")
	if err != nil {
		t.Fatalf("Stat view: %v", err)
	}
	if info.Kind != snapshot.KindView {
		t.Fatalf("expected view kind, got %v", info.Kind)
	}
	if info.Parent != "base" {
		t.Fatalf("expected parent 'base', got %q", info.Parent)
	}
}
