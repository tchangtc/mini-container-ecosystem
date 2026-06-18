// Package registry provides image pulling from OCI-compatible registries.
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	containerd "github.com/containerd/containerd/v2/client"
	contentv1 "github.com/containerd/containerd/api/services/content/v1"
	containerdimages "github.com/containerd/containerd/v2/core/images"
	digestpkg "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"google.golang.org/grpc"
)

// Pull downloads an image from a registry and stores blobs via the content store.
func Pull(ctx context.Context, client *containerd.Client, rawRef string) error {
	registry, repo, tag := parseRef(rawRef)

	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repo, tag)
	data, err := httpGet(ctx, manifestURL, "")
	if err != nil {
		token, tokErr := getAuthToken(ctx, registry, repo)
		if tokErr != nil {
			return fmt.Errorf("fetch manifest: %w (auth: %v)", err, tokErr)
		}
		data, err = httpGet(ctx, manifestURL, token)
		if err != nil {
			return fmt.Errorf("fetch manifest (auth): %w", err)
		}
	}

	var mf struct {
		Config struct{ Digest string `json:"digest"` } `json:"config"`
		Layers []struct{ Digest string `json:"digest"` } `json:"layers"`
	}
	if err := json.Unmarshal(data, &mf); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	conn := client.Conn()
	if conn == nil {
		return fmt.Errorf("containerd client has no active connection")
	}
	cc := contentv1.NewContentClient(conn.(*grpc.ClientConn))

	token, _ := getAuthToken(ctx, registry, repo)
	all := append([]string{mf.Config.Digest}, layerDigests(mf.Layers)...)
	for _, d := range all {
		blobURL := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repo, d)
		blob, err := httpGet(ctx, blobURL, token)
		if err != nil {
			return fmt.Errorf("download %s: %w", d, err)
		}
		if fmt.Sprintf("sha256:%x", sha256.Sum256(blob)) != d {
			return fmt.Errorf("digest mismatch: %s", d)
		}
		if err := writeContent(ctx, cc, d, blob); err != nil {
			return fmt.Errorf("store %s: %w", d, err)
		}
	}

	// Register image with image service
	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	_ = writeContent(ctx, cc, manifestDigest, data) // non-fatal
	imageSvc := client.ImageService()
	_, _ = imageSvc.Create(ctx, containerdimages.Image{
		Name: rawRef,
		Target: ocispec.Descriptor{
			MediaType: "application/vnd.oci.image.manifest.v1+json",
			Digest:    digestpkg.Digest(manifestDigest),
			Size:      int64(len(data)),
		},
	})
	return nil
}

func writeContent(ctx context.Context, cc contentv1.ContentClient, digest string, data []byte) error {
	stream, err := cc.Write(ctx)
	if err != nil {
		return fmt.Errorf("create write stream: %w", err)
	}
	ref := "pull-" + digest[:32]

	// STAT
	_ = stream.Send(&contentv1.WriteContentRequest{Action: contentv1.WriteAction_STAT, Ref: ref, Expected: digest})

	// WRITE in chunks
	for off := 0; off < len(data); off += 1024 * 1024 {
		end := off + 1024*1024
		if end > len(data) {
			end = len(data)
		}
		_ = stream.Send(&contentv1.WriteContentRequest{
			Action: contentv1.WriteAction_WRITE, Ref: ref,
			Offset: int64(off), Data: data[off:end],
		})
	}

	// COMMIT
	_ = stream.Send(&contentv1.WriteContentRequest{
		Action: contentv1.WriteAction_COMMIT, Ref: ref,
		Offset: int64(len(data)), Total: int64(len(data)), Expected: digest,
	})
	_ = stream.CloseSend()
	return nil
}

func httpGet(ctx context.Context, url, auth string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func getAuthToken(ctx context.Context, registry, repo string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("https://%s/v2/", registry), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	hdr := strings.TrimPrefix(resp.Header.Get("WWW-Authenticate"), "Bearer ")
	realm, service := "", ""
	for _, p := range strings.Split(hdr, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "realm=") {
			realm = strings.Trim(p[6:], `"`)
		}
		if strings.HasPrefix(p, "service=") {
			service = strings.Trim(p[8:], `"`)
		}
	}
	if realm == "" {
		return "", fmt.Errorf("no realm in %s", hdr)
	}
	url := fmt.Sprintf("%s?service=%s&scope=repository:%s:pull", realm, service, repo)
	req2, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()
	var tr struct{ Token string `json:"token"` }
	if err := json.NewDecoder(resp2.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	return tr.Token, nil
}

func parseRef(ref string) (reg, repo, tag string) {
	tag = "latest"
	if idx := strings.LastIndex(ref, ":"); idx >= 0 {
		a := ref[idx+1:]
		if !strings.Contains(a, "/") {
			tag = a
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
		r := parts[0]
		if r == "docker.io" {
			r = "registry-1.docker.io"
		}
		return r, strings.Join(parts[1:], "/"), tag
	}
}

func layerDigests(layers []struct{ Digest string `json:"digest"` }) []string {
	var d []string
	for _, l := range layers {
		d = append(d, l.Digest)
	}
	return d
}
