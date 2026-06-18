// Package api implements a Docker-compatible REST API over Unix socket,
// backed by mini-containerd for container and image operations.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/oci"

	"github.com/tcherry/mini-container-ecosystem/mini-docker/internal/builder"
	"github.com/tcherry/mini-container-ecosystem/mini-docker/pkg/registry"
)

// Server implements the Docker REST API.
type Server struct {
	client  *containerd.Client
	handler http.Handler
}

// NewServer creates a new REST API server.
func NewServer(client *containerd.Client) *Server {
	s := &Server{client: client}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.24/containers/json", s.listContainers)
	mux.HandleFunc("/v1.24/containers/create", s.createContainer)
	mux.HandleFunc("/v1.24/containers/", s.handleContainer)
	mux.HandleFunc("/v1.24/images/json", s.listImages)
	mux.HandleFunc("/v1.24/images/create", s.pullImage)
	mux.HandleFunc("/v1.24/build", s.buildImage)
	mux.HandleFunc("/_ping", s.ping)
	s.handler = mux
	return s
}

// ListenAndServe starts the HTTP server on the given Unix socket.
func (s *Server) ListenAndServe(socket string) error {
	os.Remove(socket)
	dir := socket[:strings.LastIndex(socket, "/")]
	os.MkdirAll(dir, 0o755)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "mini-docker API listening on %s\n", socket)
	return http.Serve(listener, s.handler)
}

// ── Handlers ────────────────────────────────────────────────────

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"message":"mini-docker is running"}`))
}

func (s *Server) listContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := s.client.Containers(r.Context())
	if err != nil {
		httpError(w, err, 500)
		return
	}
	var result []map[string]interface{}
	for _, c := range containers {
		result = append(result, map[string]interface{}{
			"Id":    c.ID(),
			"Image": "",
		})
	}
	writeJSON(w, result)
}

func (s *Server) createContainer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Image string   `json:"Image"`
		Cmd   []string `json:"Cmd"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	ref := normalizeRef(body.Image)

	// Pull image via our registry package
	if err := registry.Pull(r.Context(), s.client, ref); err != nil {
		httpError(w, fmt.Errorf("pull: %w", err), 500)
		return
	}
	image, err := s.client.GetImage(r.Context(), ref)
	if err != nil {
		httpError(w, fmt.Errorf("get image: %w", err), 500)
		return
	}

	id := fmt.Sprintf("docker-%d", os.Getpid())
	container, err := s.client.NewContainer(r.Context(), id,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(id, image),
		containerd.WithNewSpec(
			oci.WithImageConfig(image),
			oci.WithProcessArgs(body.Cmd...),
		),
	)
	if err != nil {
		httpError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"Id": container.ID()})
}

func (s *Server) handleContainer(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1.24/containers/"), "/")
	if len(parts) < 2 {
		httpError(w, fmt.Errorf("invalid path"), 400)
		return
	}
	id, action := parts[0], parts[1]

	container, err := s.client.LoadContainer(r.Context(), id)
	if err != nil {
		httpError(w, err, 404)
		return
	}

	switch action {
	case "start":
		task, err := container.NewTask(r.Context(), cio.NewCreator(cio.WithStdio))
		if err != nil {
			httpError(w, err, 500)
			return
		}
		if err := task.Start(r.Context()); err != nil {
			httpError(w, err, 500)
			return
		}
		writeJSON(w, map[string]interface{}{"status": "started"})
	case "stop":
		task, _ := container.Task(r.Context(), nil)
		if task != nil {
			task.Kill(r.Context(), 9)
		}
		writeJSON(w, map[string]interface{}{"status": "stopped"})
	case "json":
		writeJSON(w, map[string]interface{}{"Id": container.ID()})
	default:
		httpError(w, fmt.Errorf("unknown action: %s", action), 400)
	}
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	imgs, err := s.client.ImageService().List(r.Context())
	if err != nil {
		httpError(w, err, 500)
		return
	}
	var result []map[string]interface{}
	for _, img := range imgs {
		result = append(result, map[string]interface{}{
			"Id":       img.Target.Digest.String(),
			"RepoTags": []string{img.Name},
		})
	}
	writeJSON(w, result)
}

func (s *Server) pullImage(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("fromImage")
	if ref == "" {
		httpError(w, fmt.Errorf("fromImage required"), 400)
		return
	}
	if err := registry.Pull(r.Context(), s.client, normalizeRef(ref)); err != nil {
		httpError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "pulled", "image": ref})
}

func (s *Server) buildImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, fmt.Errorf("POST required"), 405)
		return
	}
	body, _ := io.ReadAll(r.Body)
	tag := r.URL.Query().Get("t")
	b := builder.NewBuilder(s.client)
	result, err := b.Build(r.Context(), string(body), os.Stderr, tag)
	if err != nil {
		httpError(w, err, 500)
		return
	}
	writeJSON(w, map[string]interface{}{
		"imageId": result.ImageID,
		"layers":  result.Layers,
	})
}

// ── Helpers ─────────────────────────────────────────────────────

func normalizeRef(raw string) string {
	tag := "latest"
	if idx := strings.LastIndex(raw, ":"); idx >= 0 {
		after := raw[idx+1:]
		if !strings.Contains(after, "/") {
			tag = after
			raw = raw[:idx]
		}
	}
	parts := strings.SplitN(raw, "/", 3)
	switch {
	case len(parts) == 1:
		return "docker.io/library/" + parts[0] + ":" + tag
	case len(parts) == 2 && !strings.Contains(parts[0], "."):
		return "docker.io/" + strings.Join(parts, "/") + ":" + tag
	default:
		reg := parts[0]
		if reg == "docker.io" {
			reg = "registry-1.docker.io"
		}
		return reg + "/" + strings.Join(parts[1:], "/") + ":" + tag
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
