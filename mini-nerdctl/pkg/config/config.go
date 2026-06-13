// Package config holds mini-nerdctl configuration.
// Values are loaded from environment variables, with sensible defaults.
package config

import (
	"os"
)

// Config holds all configuration for mini-nerdctl.
type Config struct {
	// Address is the containerd gRPC socket path.
	Address string
	// Namespace is the containerd namespace to operate in.
	Namespace string
}

// Default returns a Config with sensible defaults.
func Default() Config {
	return Config{
		Address:   "/run/containerd/containerd.sock",
		Namespace: "default",
	}
}

// FromEnv loads configuration from environment variables,
// falling back to defaults for unset values.
func FromEnv() Config {
	cfg := Default()
	if addr := os.Getenv("CONTAINERD_ADDRESS"); addr != "" {
		cfg.Address = addr
	}
	if ns := os.Getenv("CONTAINERD_NAMESPACE"); ns != "" {
		cfg.Namespace = ns
	}
	return cfg
}
