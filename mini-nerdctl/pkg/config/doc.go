// Package config holds mini-nerdctl configuration, loaded from environment
// variables and command-line flags (no TOML config file for the mini version).
//
// Key settings:
//   - ContainerdAddress: gRPC socket path (default: /run/containerd/containerd.sock)
//   - Namespace: containerd namespace (default: "default")
//   - Snapshotter: snapshot driver (default: "overlayfs")
package config
