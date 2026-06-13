// Package image provides image operations: pull, list, and remove.
package image

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	contentv1 "github.com/containerd/containerd/api/services/content/v1"
	containerdimages "github.com/containerd/containerd/v2/core/images"
	digestpkg "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"google.golang.org/grpc"

	"github.com/tcherry/mini-container-ecosystem/mini-nerdctl/pkg/reference"
)

// ImageInfo holds display-ready metadata about a stored image.
type ImageInfo struct {
	Name      string
	Digest    string
	Size      int64
	CreatedAt time.Time
}

// Pull downloads an image from a registry via direct HTTP, stores blobs
// via the content gRPC service, and registers the image.
func Pull(ctx context.Context, client *containerd.Client, rawRef string, progress io.Writer) error {
	ref := reference.Normalize(rawRef)
	fmt.Fprintf(progress, "Pulling %s...\n", ref)

	registry, repo, tag := parseRef(ref)
	_ = tag

	// Try fetching with auth
	authToken := ""
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repo, tag)
	resp, err := httpGet(ctx, manifestURL, authToken)
	if err != nil {
		return fmt.Errorf("fetch manifest: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// Get bearer token
		authToken, err = fetchAuthToken(ctx, registry, repo)
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		resp, _ = httpGet(ctx, manifestURL, authToken)
		defer resp.Body.Close()
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch manifest: %s", resp.Status)
	}
	manifestData, _ := io.ReadAll(resp.Body)
	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifestData))

	// 2. Parse manifest for config + layers
	var m ociManifest
	if err := json.Unmarshal(manifestData, &m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	// 3. Get the underlying gRPC connection for direct content calls
	gconn := client.Conn().(*grpc.ClientConn)
	contentClient := contentv1.NewContentClient(gconn)

	// 4. Download and store config + layers
	allDigests := append([]string{m.Config.Digest}, layersDigests(m.Layers)...)
	for i, digest := range allDigests {
		fmt.Fprintf(progress, "[%d/%d] %s\n", i+1, len(allDigests), digest[:30])

		blobURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repo, digest)
		data, err := downloadBlob(ctx, blobURL, authToken)
		if err != nil {
			return fmt.Errorf("download %s: %w", digest, err)
		}

		if err := writeContent(ctx, contentClient, digest, data); err != nil {
			return fmt.Errorf("store %s: %w", digest, err)
		}
	}

	fmt.Fprintf(progress, "Digest: %s\n", manifestDigest)
	fmt.Fprintf(progress, "Status: pull complete (%d layers)\n", len(allDigests))

	// Register image with the image service so it shows up in 'images' command
	// Create minimal image record
	imageSvc := client.ImageService()
	_, err = imageSvc.Create(ctx, containerdimages.Image{
		Name: ref,
		Target: ocispec.Descriptor{
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    digestpkg.Digest(manifestDigest),
			Size:      int64(len(manifestData)),
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		// Image might already exist — that's OK for idempotent pull
		fmt.Fprintf(progress, "Note: image already registered: %v\n", err)
	}
	return nil
}

// writeContent stores blob data via the content gRPC service STAT→WRITE→COMMIT.
func writeContent(ctx context.Context, c contentv1.ContentClient, digest string, data []byte) error {
	stream, err := c.Write(ctx)
	if err != nil {
		return err
	}

	ref := "pull-" + digest[:32]

	// Send STAT, WRITE, COMMIT in sequence
	// STAT
	if err := stream.Send(&contentv1.WriteContentRequest{
		Action:   contentv1.WriteAction_STAT,
		Ref:      ref,
		Expected: digest,
	}); err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if _, err := stream.Recv(); err != nil {
		return fmt.Errorf("stat recv: %w", err)
	}

	// WRITE in chunks
	chunkSize := 1024 * 1024
	for offset := 0; offset < len(data); offset += chunkSize {
		end := offset + chunkSize
		if end > len(data) {
			end = len(data)
		}
		if err := stream.Send(&contentv1.WriteContentRequest{
			Action: contentv1.WriteAction_WRITE,
			Ref:    ref,
			Offset: int64(offset),
			Data:   data[offset:end],
		}); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		if _, err := stream.Recv(); err != nil {
			return fmt.Errorf("write recv: %w", err)
		}
	}

	// COMMIT
	if err := stream.Send(&contentv1.WriteContentRequest{
		Action:   contentv1.WriteAction_COMMIT,
		Ref:      ref,
		Offset:   int64(len(data)),
		Total:    int64(len(data)),
		Expected: digest,
	}); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	if _, err := stream.Recv(); err != nil && err != io.EOF {
		return fmt.Errorf("commit recv: %w", err)
	}
	return stream.CloseSend()
}

// List returns all images in the containerd namespace.
func List(ctx context.Context, client *containerd.Client) ([]ImageInfo, error) {
	store := client.ImageService()
	all, err := store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	result := make([]ImageInfo, 0, len(all))
	for _, img := range all {
		result = append(result, ImageInfo{
			Name:      img.Name,
			Digest:    img.Target.Digest.String(),
			Size:      img.Target.Size,
			CreatedAt: img.CreatedAt,
		})
	}
	return result, nil
}

// Remove deletes an image by reference.
func Remove(ctx context.Context, client *containerd.Client, rawRef string) error {
	ref := reference.Normalize(rawRef)
	store := client.ImageService()
	all, err := store.List(ctx, "name=="+ref)
	if err != nil {
		return fmt.Errorf("lookup image %q: %w", ref, err)
	}
	if len(all) == 0 {
		return fmt.Errorf("image %q not found", ref)
	}
	return store.Delete(ctx, all[0].Name)
}

// ── helpers ─────────────────────────────────────────────────────

type ociManifest struct {
	Config struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"config"`
	Layers []struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"layers"`
}

func layersDigests(layers []struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}) []string {
	var d []string
	for _, l := range layers {
		d = append(d, l.Digest)
	}
	return d
}

func parseRef(ref string) (registry, repo, tag string) {
	tag = "latest"
	if idx := strings.LastIndex(ref, ":"); idx >= 0 {
		after := ref[idx+1:]
		if !strings.Contains(after, "/") {
			tag = after
			ref = ref[:idx]
		}
	}
	parts := strings.SplitN(ref, "/", 3)
	switch {
	case len(parts) == 1:
		return "registry-1.docker.io", "library/" + parts[0], tag
	case len(parts) == 2 && !strings.Contains(parts[0], "."):
		return "registry-1.docker.io", strings.Join(parts, "/"), tag
	default:
		reg := parts[0]
		if reg == "docker.io" {
			reg = "registry-1.docker.io"
		}
		return reg, strings.Join(parts[1:], "/"), tag
	}
}

func downloadBlob(ctx context.Context, url, authToken string) ([]byte, error) {
	return httpGetBody(ctx, url, authToken)
}

func httpGet(ctx context.Context, url, authToken string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	return http.DefaultClient.Do(req)
}

func httpGetBody(ctx context.Context, url, authToken string) ([]byte, error) {
	resp, err := httpGet(ctx, url, authToken)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func fetchAuthToken(ctx context.Context, registry, repo string) (string, error) {
	url := fmt.Sprintf("https://%s/v2/", registry)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	authHdr := resp.Header.Get("WWW-Authenticate")
	if authHdr == "" {
		return "", fmt.Errorf("no WWW-Authenticate header")
	}

	// Parse: Bearer realm="https://...",service="..."
	realm, service := parseAuthHeader(authHdr)
	if realm == "" {
		return "", fmt.Errorf("cannot parse auth header: %s", authHdr)
	}

	tokenURL := fmt.Sprintf("%s?service=%s&scope=repository:%s:pull", realm, service, repo)
	req2, _ := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()

	var tr struct{ Token string `json:"token"` }
	json.NewDecoder(resp2.Body).Decode(&tr)
	return tr.Token, nil
}

func parseAuthHeader(header string) (realm, service string) {
	// Strip "Bearer " prefix if present
	header = strings.TrimPrefix(header, "Bearer ")
	for _, p := range strings.Split(header, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "realm=") {
			realm = strings.Trim(p[6:], `"`)
		}
		if strings.HasPrefix(p, "service=") {
			service = strings.Trim(p[8:], `"`)
		}
	}
	return
}
