// Package reference parses and normalizes container image references
// following the Docker/OCI distribution spec conventions.
package reference

import (
	"fmt"
	"strings"
)

const (
	defaultRegistry = "docker.io"
	defaultTag      = "latest"
	officialPrefix  = "library"
)

// ParseResult holds the decomposed components of an image reference.
type ParseResult struct {
	Registry string // e.g. "docker.io"
	Repo     string // e.g. "library/alpine"
	Tag      string // e.g. "latest"
}

// String returns the canonical "registry/repo:tag" form.
func (r ParseResult) String() string {
	return fmt.Sprintf("%s/%s:%s", r.Registry, r.Repo, r.Tag)
}

// Normalize takes a raw user input (e.g. "alpine", "nginx:1.25")
// and returns a fully qualified reference string suitable for containerd.
//
// Rules:
//   - "alpine"              → "docker.io/library/alpine:latest"
//   - "alpine:3.18"         → "docker.io/library/alpine:3.18"
//   - "nginx:latest"        → "docker.io/library/nginx:latest"
//   - "ghcr.io/foo/bar:v1"  → "ghcr.io/foo/bar:v1"        (already qualified)
//   - "docker.io/user/img"  → "docker.io/user/img:latest"  (add tag only)
func Normalize(raw string) string {
	r := Parse(raw)
	return r.String()
}

// Parse decomposes a raw reference into registry, repo, and tag.
func Parse(raw string) ParseResult {
	registry := defaultRegistry
	repo := raw
	tag := defaultTag

	// Strip tag (everything after last colon, unless colon is part of port or digest)
	if tagIdx, _ := splitTag(raw); tagIdx >= 0 {
		repo = raw[:tagIdx]
		tag = raw[tagIdx+1:]
	}

	// Determine if user specified a registry
	// A registry is present if:
	//   - Contains a dot (docker.io, ghcr.io)
	//   - Contains a colon (localhost:5000)
	//   - Contains "docker.io" explicitly
	parts := strings.SplitN(repo, "/", 3)
	if len(parts) >= 2 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		// First part looks like a registry
		registry = parts[0]
		repo = strings.Join(parts[1:], "/")
	} else if len(parts) == 1 {
		// Only image name — use official library
		repo = officialPrefix + "/" + parts[0]
	} else {
		// e.g. "library/alpine" → official docker image
		repo = strings.Join(parts, "/")
	}

	// Ensure official images are under library/
	if registry == defaultRegistry && !strings.Contains(repo, "/") {
		repo = officialPrefix + "/" + repo
	}

	return ParseResult{
		Registry: registry,
		Repo:     repo,
		Tag:      tag,
	}
}

// splitTag returns the index of the tag separator (last colon that's not
// part of a port number or digest algorithm). Returns -1 if no tag found.
func splitTag(raw string) (int, string) {
	// Check for digest first: name@sha256:...
	if idx := strings.Index(raw, "@"); idx >= 0 {
		// Has digest, look for tag before digest
		before := raw[:idx]
		if tagIdx := lastNonPortColon(before); tagIdx >= 0 {
			return tagIdx, before[tagIdx+1:]
		}
		return -1, ""
	}

	idx := lastNonPortColon(raw)

	// Don't treat trailing colon as a tag separator
	if idx >= 0 && idx == len(raw)-1 {
		return -1, ""
	}

	return idx, ""
}

// lastNonPortColon finds the last colon that isn't part of a port number.
// A port is identified by being immediately after a pattern like "host:port".
func lastNonPortColon(s string) int {
	lastColon := strings.LastIndex(s, ":")
	if lastColon < 0 {
		return -1
	}

	// If colon is followed by a slash, it's a port (e.g. localhost:5000/image)
	after := s[lastColon+1:]
	if slashIdx := strings.Index(after, "/"); slashIdx >= 0 {
		// Colon is followed by text then slash — could be port
		// Check if the part between colon and slash looks like a port number
		portStr := after[:slashIdx]
		if isPort(portStr) {
			// This colon is a port separator, look for tag before it
			before := s[:lastColon]
			if tagIdx := strings.LastIndex(before, ":"); tagIdx >= 0 {
				return tagIdx
			}
			return -1
		}
	}

	return lastColon
}

// isPort returns true if s looks like a port number.
func isPort(s string) bool {
	if len(s) == 0 || len(s) > 5 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
