.PHONY: all build clean test \
        build-nerdctl build-containerd build-docker build-kueue \
        run-containerd run-docker \
        proto demo

BIN_DIR := bin

all: build

# ── Proto generation ─────────────────────────────────────────────
proto:
	cd mini-containerd && bash proto/generate.sh

# ── Build targets ────────────────────────────────────────────────
build: build-nerdctl build-containerd build-docker build-kueue

build-nerdctl:
	cd mini-nerdctl && go build -o ../$(BIN_DIR)/mini-nerdctl ./cmd/nerdctl/

build-containerd: proto
	cd mini-containerd && go build -o ../$(BIN_DIR)/mini-containerd ./cmd/containerd/

build-docker:
	cd mini-docker && go build -o ../$(BIN_DIR)/mini-dockerd ./cmd/dockerd/

build-kueue:
	cd mini-kueue && go build -o ../$(BIN_DIR)/mini-kueue ./cmd/controller/

# ── Clean ────────────────────────────────────────────────────────
clean:
	rm -rf $(BIN_DIR)/
	rm -rf mini-containerd/api/content/
	rm -rf mini-containerd/api/image/
	rm -rf mini-containerd/api/snapshot/
	rm -rf mini-containerd/api/task/

# ── Test ─────────────────────────────────────────────────────────
test:
	@for mod in mini-nerdctl mini-containerd mini-docker mini-kueue; do \
		echo "=== Testing $$mod ==="; \
		cd $$mod && go test ./... || exit 1; \
		cd ..; \
	done

# ── Run ──────────────────────────────────────────────────────────
run-containerd: build-containerd
	cd mini-containerd && sudo ../$(BIN_DIR)/mini-containerd --config config/default.toml

run-docker: build-docker
	cd mini-docker && sudo ../$(BIN_DIR)/mini-dockerd

# ── Kueue CRD generation ─────────────────────────────────────────
generate-kueue:
	cd mini-kueue && controller-gen object paths="./..." crd output:crd:artifacts=config/crd

# ── Full demo ────────────────────────────────────────────────────
demo: build
	@echo "=== Starting mini-containerd ==="
	./$(BIN_DIR)/mini-containerd --config mini-containerd/config/default.toml &
	@sleep 1
	@echo "=== Pulling alpine image ==="
	./$(BIN_DIR)/mini-nerdctl pull alpine:latest
	@echo "=== Running container ==="
	./$(BIN_DIR)/mini-nerdctl run alpine:latest echo "Hello from mini-container-ecosystem!"
	@echo "=== Cleaning up ==="
	kill %1 2>/dev/null || true
