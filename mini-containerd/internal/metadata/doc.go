// Package metadata implements BoltDB-backed persistent storage for mini-containerd.
// It stores container metadata, image references, and snapshot relationships.
// This is the simplest possible metadata store — a single BoltDB file at
// /var/lib/mini-containerd/meta.db with buckets for:
//
//   - containers:  containerID → Container{ID, Image, SnapshotKey, Spec, CreatedAt}
//   - images:      imageName  → Image{Name, ManifestDigest, ConfigDigest, Layers, Size}
//   - snapshots:   snapshotKey → Snapshot{Key, Parent, Kind, MountPath}
//
// Unlike real containerd's core/metadata package which has separate bolt
// implementations for each service, this single-file approach prioritizes
// simplicity over extensibility.
package metadata
