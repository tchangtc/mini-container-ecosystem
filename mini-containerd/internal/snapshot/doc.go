// Package snapshot implements the snapshot service using Linux overlayfs.
// It manages a tree of read-only layer snapshots with a single writable active snapshot.
// Operations: Prepare, Commit, Mount, Unmount, Remove.
//
// Phase 2.3 — to be implemented.
package snapshot
