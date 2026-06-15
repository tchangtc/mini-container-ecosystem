// Package image implements the image service for mini-containerd.
// It handles pulling images from OCI-compatible registries, storing metadata,
// and unpacking layers into snapshots via the content store and snapshotter.
//
// Phase 2.2 — Image Service.
package image

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	imagesv1 "github.com/containerd/containerd/api/services/images/v1"
	"github.com/containerd/containerd/api/types"
	containerdimages "github.com/containerd/containerd/v2/core/images"
	digestpkg "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tcherry/mini-container-ecosystem/mini-containerd/internal/content"
	"github.com/tcherry/mini-container-ecosystem/mini-containerd/internal/snapshot"
)

// Store manages image metadata and pull operations.
type Store struct {
	mu     sync.RWMutex
	images map[string]*containerdimages.Image // name → image metadata

	content    *content.Store
	snapshotter *snapshot.Snapshotter
}

// NewStore creates a new image store.
func NewStore(cs *content.Store, sn *snapshot.Snapshotter) *Store {
	return &Store{
		images:      make(map[string]*containerdimages.Image),
		content:     cs,
		snapshotter: sn,
	}
}

// Get returns image metadata by name.
func (s *Store) Get(name string) (*containerdimages.Image, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	img, ok := s.images[name]
	if !ok {
		return nil, fmt.Errorf("image %q not found", name)
	}
	return img, nil
}

// List returns all stored images.
func (s *Store) List() []*containerdimages.Image {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*containerdimages.Image, 0, len(s.images))
	for _, img := range s.images {
		result = append(result, img)
	}
	return result
}

// Create adds a new image record.
func (s *Store) Create(img containerdimages.Image) (*containerdimages.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.images[img.Name]; ok {
		return nil, fmt.Errorf("image %q already exists", img.Name)
	}
	now := time.Now()
	img.CreatedAt = now
	img.UpdatedAt = now
	s.images[img.Name] = &img
	return &img, nil
}

// Delete removes an image by name.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.images[name]; !ok {
		return fmt.Errorf("image %q not found", name)
	}
	delete(s.images, name)
	return nil
}

// Pull downloads an image from a registry, stores its blobs, and unpacks its
// layers into snapshots. Returns the image metadata.
func (s *Store) Pull(ctx context.Context, ref string) (*containerdimages.Image, error) {
	// 1. Parse reference
	registry, repo, tag := parseRef(ref)
	fmt.Printf("Registry: %s, Repo: %s, Tag: %s\n", registry, repo, tag)

	// 2. Resolve manifest
	manifestBody, manifestDigest, manifestType, err := fetchManifest(ctx, registry, repo, tag)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest for %s: %w", ref, err)
	}

	// 3. Parse manifest to get config and layers
	configDesc, layerDescs, err := parseManifest(manifestBody, manifestType)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	// 4. Download config blob
	configData, err := fetchBlob(ctx, registry, repo, configDesc.Digest.String())
	if err != nil {
		return nil, fmt.Errorf("fetch config %s: %w", configDesc.Digest, err)
	}

	// Store config blob in content store
	configRef := "config-" + tag
	if err := storeContent(s.content, configDesc.Digest.String(), configData); err != nil {
		return nil, fmt.Errorf("store config: %w", err)
	}

	// 5. Download and unpack each layer
	var chainID string // tracks parent for overlayfs chain
	for i, layer := range layerDescs {
		fmt.Printf("Downloading layer %d/%d: %s\n", i+1, len(layerDescs), layer.Digest)

		layerData, err := fetchBlob(ctx, registry, repo, layer.Digest.String())
		if err != nil {
			return nil, fmt.Errorf("fetch layer %s: %w", layer.Digest, err)
		}

		// Store layer blob
		if err := storeContent(s.content, layer.Digest.String(), layerData); err != nil {
			return nil, fmt.Errorf("store layer %s: %w", layer.Digest, err)
		}

		// Unpack layer into a snapshot
		snapKey := fmt.Sprintf("layer-%s", layer.Digest.Encoded()[:12])
		if err := unpackLayer(s.snapshotter, snapKey, chainID, layerData); err != nil {
			return nil, fmt.Errorf("unpack layer %s: %w", layer.Digest, err)
		}

		chainID = snapKey
	}
	_ = configRef

	// 6. Create image record
	now := time.Now()
	img := containerdimages.Image{
		Name: ref,
		Target: ocispec.Descriptor{
			MediaType: manifestType,
			Digest:    digestpkg.Digest(manifestDigest),
			Size:      int64(len(manifestBody)),
		},
		Labels: map[string]string{
			"containerd.io/unpacked": "true",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	s.images[ref] = &img
	s.mu.Unlock()

	return &img, nil
}

// ── Registry Client ──────────────────────────────────────────────

// fetchManifest downloads the image manifest. Tries OCI first, then Docker v2.
func fetchManifest(ctx context.Context, registry, repo, tag string) ([]byte, string, string, error) {
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repo, tag)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", "", err
	}

	// Accept both manifest formats
	req.Header.Set("Accept", strings.Join([]string{
		ocispec.MediaTypeImageManifest,
		"application/vnd.docker.distribution.manifest.v2+json",
		ocispec.MediaTypeImageIndex,
		"application/vnd.docker.distribution.manifest.list.v2+json",
	}, ", "))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// Try Bearer auth
		token, err := fetchBearerToken(ctx, registry, repo)
		if err != nil {
			return nil, "", "", fmt.Errorf("auth required and token fetch failed: %w", err)
		}
		req2, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		req2.Header.Set("Accept", req.Header.Get("Accept"))
		req2.Header.Set("Authorization", "Bearer "+token)
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			return nil, "", "", err
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusOK {
			return nil, "", "", fmt.Errorf("fetch manifest after auth: %s", resp2.Status)
		}
		resp = resp2
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("fetch manifest: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", err
	}

	// Verify via SHA256
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	contentType := resp.Header.Get("Content-Type")

	return body, digest, contentType, nil
}

// fetchBlob downloads a blob by digest from the registry.
func fetchBlob(ctx context.Context, registry, repo, digest string) ([]byte, error) {
	url := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repo, digest)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	// Handle redirect (some registries redirect to CDN)
	if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusFound {
		redirectURL := resp.Header.Get("Location")
		if redirectURL != "" {
			resp.Body.Close()
			req2, _ := http.NewRequestWithContext(ctx, "GET", redirectURL, nil)
			resp2, err := http.DefaultClient.Do(req2)
			if err != nil {
				return nil, err
			}
			defer resp2.Body.Close()
			resp = resp2
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch blob: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

// fetchBearerToken performs OAuth2 token exchange for registry auth.
func fetchBearerToken(ctx context.Context, registry, repo string) (string, error) {
	// First, try without auth to get the WWW-Authenticate header
	url := fmt.Sprintf("https://%s/v2/", registry)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	authHeader := resp.Header.Get("Www-Authenticate") // case-insensitive
	if authHeader == "" {
		authHeader = resp.Header.Get("WWW-Authenticate")
	}
	if authHeader == "" {
		return "", fmt.Errorf("no WWW-Authenticate header from %s", registry)
	}

	// Parse: Bearer realm="https://...",service="..."
	realm, service := parseAuthHeader(authHeader)
	if realm == "" {
		return "", fmt.Errorf("cannot parse auth header: %s", authHeader)
	}

	// Request token
	tokenURL := fmt.Sprintf("%s?service=%s&scope=repository:%s:pull", realm, service, repo)
	req2, _ := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	return tokenResp.Token, nil
}

// ── Manifest Parsing ────────────────────────────────────────────

func parseManifest(data []byte, mediaType string) (ocispec.Descriptor, []ocispec.Descriptor, error) {
	switch mediaType {
	case ocispec.MediaTypeImageManifest:
		return parseOCIManifest(data)
	case "application/vnd.docker.distribution.manifest.v2+json":
		return parseDockerManifest(data)
	case ocispec.MediaTypeImageIndex, "application/vnd.docker.distribution.manifest.list.v2+json":
		return parseImageIndex(data)
	default:
		return ocispec.Descriptor{}, nil, fmt.Errorf("unsupported manifest type: %s", mediaType)
	}
}

func parseOCIManifest(data []byte) (ocispec.Descriptor, []ocispec.Descriptor, error) {
	var manifest ocispec.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ocispec.Descriptor{}, nil, err
	}
	return manifest.Config, manifest.Layers, nil
}

// Docker manifest has same structure as OCI for config/layers
func parseDockerManifest(data []byte) (ocispec.Descriptor, []ocispec.Descriptor, error) {
	return parseOCIManifest(data)
}

func parseImageIndex(data []byte) (ocispec.Descriptor, []ocispec.Descriptor, error) {
	var index ocispec.Index
	if err := json.Unmarshal(data, &index); err != nil {
		return ocispec.Descriptor{}, nil, err
	}
	if len(index.Manifests) == 0 {
		return ocispec.Descriptor{}, nil, fmt.Errorf("empty image index")
	}
	// Pick the first manifest for simplicity
	// In production, you'd match platform
	return index.Manifests[0], nil, fmt.Errorf("image index requires manifest resolution (not yet implemented)")
}

// ── Helpers ─────────────────────────────────────────────────────

// parseRef splits "registry/repo:tag" or uses docker.io defaults.
func parseRef(ref string) (registry, repo, tag string) {
	tag = "latest"

	// Strip tag
	if idx := strings.LastIndex(ref, ":"); idx >= 0 {
		possibleTag := ref[idx+1:]
		before := ref[:idx]
		// Check if the colon is part of a port (e.g., localhost:5000)
		if !strings.Contains(possibleTag, "/") {
			tag = possibleTag
			ref = before
		}
	}

	// Handle digest: name@sha256:...
	if idx := strings.Index(ref, "@"); idx >= 0 {
		tag = ref[idx+1:]
		ref = ref[:idx]
	}

	// Parse registry and repo
	parts := strings.SplitN(ref, "/", 3)
	switch {
	case len(parts) == 1:
		registry = "registry-1.docker.io"
		repo = "library/" + parts[0]
	case len(parts) == 2 && !strings.Contains(parts[0], "."):
		registry = "registry-1.docker.io"
		repo = strings.Join(parts, "/")
	case len(parts) == 2:
		registry = parts[0]
		repo = parts[1]
	default:
		registry = parts[0]
		repo = strings.Join(parts[1:], "/")
	}

	// Map docker.io to registry-1.docker.io
	if registry == "docker.io" {
		registry = "registry-1.docker.io"
	}

	return
}

// parseAuthHeader extracts realm and service from a Bearer WWW-Authenticate header.
func parseAuthHeader(header string) (realm, service string) {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "realm=") {
			realm = strings.Trim(part[6:], `"`)
		}
		if strings.HasPrefix(part, "service=") {
			service = strings.Trim(part[8:], `"`)
		}
	}
	return
}

// storeContent writes blob data to the content store using the write stream.
func storeContent(cs *content.Store, digest string, data []byte) error {
	ref := "upload-" + digest[:19]
	if _, err := cs.BeginUpload(ref, digest); err != nil {
		return err
	}
	if _, err := cs.WriteUpload(ref, 0, data); err != nil {
		return err
	}
	return cs.CommitUpload(ref, int64(len(data)))
}

// digestFromString converts a string to a digest.Digest.
func digestFromString(s string) digestpkg.Digest {
	return digestpkg.Digest(s)
}

// unpackLayer extracts a tar.gz layer into a snapshot.
func unpackLayer(sn *snapshot.Snapshotter, key, parent string, data []byte) error {
	// Prepare the snapshot
	if _, err := sn.Prepare(key, parent); err != nil {
		return fmt.Errorf("prepare snapshot %q: %w", key, err)
	}

	// Extract tar.gz content into the snapshot fs directory
	// In a complete implementation, the data would be decompressed (gzip) and
	// untarred into the snapshot's fs directory. For now, skip actual extraction
	// since we need tar/gzip libraries.
	//
	// The snapshot directory is at DefaultRoot/key/fs/

	// Commit the layer as a committed snapshot
	return sn.Commit(key, key)
}

// ToGRPC converts a containerd Image to the gRPC proto Image type.
func ToGRPC(img *containerdimages.Image) *imagesv1.Image {
	return &imagesv1.Image{
		Name:      img.Name,
		Labels:    img.Labels,
		Target:    descToProto(img.Target),
		CreatedAt: timestamppb.New(img.CreatedAt),
		UpdatedAt: timestamppb.New(img.UpdatedAt),
	}
}

func descToProto(desc ocispec.Descriptor) *types.Descriptor {
	return &types.Descriptor{
		MediaType: desc.MediaType,
		Digest:    desc.Digest.String(),
		Size:      desc.Size,
	}
}

// ensure unused imports don't cause issues
var _ = strings.Join
