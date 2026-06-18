# mini-container-ecosystem

> Build minimal versions of core container ecosystem components from scratch — understand the internals by building them.

[简体中文](README.zh-CN.md) | [繁體中文](README.zh-TW.md)

---

## Overview

**mini-container-ecosystem** is an educational project that reimplements the core components of the container ecosystem as simplified versions. Each component targets a real-world counterpart (with source code studied under `/kubernetes-ecosystem/`), preserving the essential architecture and algorithms while stripping away plugin systems, multi-tenancy, production-grade fault tolerance, and other engineering complexities.

The central question this project answers: **How do you run containers on a single machine?**

| Component | Real Counterpart | Real Scale | Mini Keeps | Difficulty | Status |
|-----------|-----------------|------------|------------|------------|--------|
| [mini-nerdctl](./mini-nerdctl/) | nerdctl (779 files) | 30+ commands, 65 pkgs | 9 commands, 5 pkgs, ~1,300 LOC | ⭐⭐ | ✅ Done |
| [mini-containerd](./mini-containerd/) | containerd (5,280 files) | 16 gRPC services, plugins | 11 services, ~3,200 LOC, 9 tests | ⭐⭐⭐⭐⭐ | ✅ Done |
| [mini-docker](./mini-docker/) | Docker Engine | Full engine | Builder + REST + Registry, ~780 LOC | ⭐⭐⭐⭐ | ✅ Done |
| [mini-kueue](./mini-kueue/) | kueue (8,811 files) | 10 CRDs, 10+ Job types | 4 CRDs, 1 Job, FIFO | ⭐⭐⭐ | 🚧 In Progress |
| [mini-kubepray](./mini-kubepray/) | kubespray (28 Roles) | Multi-engine, multi-CNI | containerd + Flannel | ⭐ | 🚧 In Progress |

## Architecture & Dependencies

```
mini-nerdctl ──gRPC──▶ mini-containerd
mini-docker  ──gRPC──▶ mini-containerd
mini-kueue   ──▶ k8s API server (standalone)
mini-kubepray ──▶ kubeadm + containerd (calls system binaries)
```

### Relationship to mini-kubernetes

This project is the **container runtime foundation** for a larger endeavor. The sibling project **mini-kubernetes** builds the orchestration layer (apiserver, scheduler, kubelet, controller-manager, kube-proxy, kubeadm) on top of it. The bridge between the two is the **CRI gRPC interface**:

```
mini-container-ecosystem               mini-kubernetes
═══════════════════════                 ══════════════

mini-nerdctl ──▶ mini-containerd ◀── mini-kubelet (CRI)
mini-docker  ──▶ mini-containerd    mini-apiserver
                                     mini-scheduler
mini-kueue   ──▶ k8s API            mini-ctrl-mgr
mini-kubepray ──▶ kubeadm           mini-kube-proxy
                                     mini-kubeadm
```

## Component Deep-Dive

### mini-nerdctl — CLI

**Target:** nerdctl (779 files → 9 commands, 5 packages)
**Phase 1 |** ~1,300 lines of Go

- ✅ **Kept:** `run`, `exec`, `ps`, `logs`, `stop`, `rm`, `pull`, `images`, `rmi`
- ❌ **Cut:** compose, IPFS, rootless, healthcheck, BuildKit, manifest, checkpoint, login, network, volume

A thin CLI layer that translates user-friendly commands into containerd gRPC calls. Nine cobra commands backed by 5 packages (config, container, image, reference, runtime).

### mini-containerd — Runtime

**Target:** containerd (5,280 files → 11 gRPC services)
**Phase 2 |** ~3,200 lines of Go, 9 tests

- ✅ **Kept (4 core proto-defined services):** Content (blob storage), Image (image management), Snapshot (overlayfs), Task (process management)
- ✅ **Kept (7 auxiliary services for containerd v2 client compatibility):** Containers, Version, Namespaces, Leases (noop), Events (noop), Introspection, Transfer (noop)
- ✅ **Storage:** BoltDB metadata store
- ❌ **Cut:** plugin system, CRI, NRI, sandbox, streaming, diff, multi-namespace

The core runtime. Images are stored as OCI content-addressed blobs; containers run via Linux namespaces and cgroups managed through the task service. BoltDB holds all metadata. Auxiliary noop services satisfy the containerd v2 client handshake without adding implementation complexity.

### mini-docker — Builder + REST API

**Target:** Docker Engine
**Phase 3 |** ~780 lines of Go

- ✅ **Kept (Phase 3.1):** Dockerfile builder — parser (9 instructions: FROM/RUN/COPY/CMD/ENTRYPOINT/ENV/WORKDIR/EXPOSE/LABEL) and layer-by-layer executor
- ✅ **Kept (Phase 3.4):** Docker-compatible REST API over Unix socket (7 endpoints: `/containers/*`, `/images/*`, `/build`, `/_ping`)
- ✅ **Kept:** OCI registry pull with bearer-token authentication
- 🚧 **Planned:** bridge networking (Phase 3.2), volume management (Phase 3.3)
- ❌ **Cut:** Swarm, overlay networking, multi-storage drivers, BuildKit, plugin system, multi-stage builds

A Docker-compatible daemon. The builder parses Dockerfiles and executes them layer-by-layer (RUN commands run in temp directories, kaniko-style). The REST API serves container and image operations. Image pulling goes directly to OCI registries with bearer-token authentication. Network and volume subsystems are designed but not yet implemented (stub packages only).

### mini-kueue — Resource Scheduling

**Target:** kueue (8,811 files → 4 CRDs)
**Phase 4 |** estimated 2-3 months

- ✅ **Kept:** Workload, ClusterQueue, LocalQueue, ResourceFlavor, batch/Job, FIFO scheduling
- ❌ **Cut:** Cohort, AdmissionCheck, MultiKueue, Topology, FairSharing, Preemption, 10+ Job types

A Kubernetes-native resource quota manager built on the Operator pattern. Workloads enter LocalQueues, ClusterQueues check resource availability, and a FIFO scheduler admits them when quota is available.

### mini-kubepray — Cluster Deployment

**Target:** kubespray (28 Roles → 4 scripts)
**Phase 5 |** estimated 2-3 weeks

- ✅ **Kept:** containerd, Flannel, kubeadm init/join
- ❌ **Cut:** other container engines, other CNIs, cloud plugins, upgrade/recovery

Four shell scripts that bootstrap a Kubernetes cluster: system preparation, containerd installation, kubeadm init/join, and CNI setup.

## Quick Start

```bash
# Build all components
make build

# Run mini-containerd
make run-containerd

# Use mini-nerdctl
./bin/mini-nerdctl pull alpine:latest
./bin/mini-nerdctl run alpine:latest echo hello

# Run mini-docker
make run-dockerd
curl --unix-socket /tmp/mini-docker/docker.sock http://localhost/_ping
```

## Implementation Roadmap

| Phase | Component | Effort | Status |
|-------|-----------|--------|--------|
| 1 | mini-nerdctl | 2-3 weeks | ✅ Done |
| 2 | mini-containerd | 4-6 months | ✅ Done |
| 3 | mini-docker | 2-3 months | ✅ Done |
| 4 | mini-kueue | 2-3 months | 🚧 In Progress |
| 5 | mini-kubepray | 2-3 weeks | 🚧 In Progress |

## Design Patterns

Each component embodies a classic systems-design pattern:

| Component | Pattern | In One Line |
|-----------|--------|-------------|
| mini-nerdctl | API Facade | Wrap gRPC calls into a user-friendly CLI |
| mini-containerd | Layered Abstraction | content → image → snapshot → task, each layer independent |
| mini-docker | Builder Pattern | Incremental layer build with caching; temp containers as build environments |
| mini-kueue | Queue Scheduling | FIFO → resource check → admit |
| mini-kubepray | Phase Pipeline | prep → container → kubeadm → CNI, sequential phases |

---

© 2026 [tcherry](https://github.com/tcherry). Released under the [MIT License](LICENSE).
