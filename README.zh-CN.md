# mini-container-ecosystem

> 从零实现容器生态核心组件的简易版本——通过亲手构建来理解容器内部原理。

[English](README.md) | [繁體中文](README.zh-TW.md)

---

## 项目概述

**mini-container-ecosystem** 是一个教育项目，以精简版本重新实现容器生态的核心组件。每个组件对标真实项目（参考源码位于 `/kubernetes-ecosystem/`），保留核心架构和算法，去掉插件系统、多租户、生产级容错等工程复杂性。

本项目回答的核心问题是：**如何在一台机器上运行容器？**

| 组件 | 对标项目 | 真实规模 | Mini 保留 | 难度 | 状态 |
|------|---------|---------|----------|------|------|
| [mini-nerdctl](./mini-nerdctl/) | nerdctl (779 文件) | 30+ 命令, 65 包 | 9 命令, 5 包, ~1,300 行 | ⭐⭐ | ✅ 已完成 |
| [mini-containerd](./mini-containerd/) | containerd (5,280 文件) | 16 gRPC 服务, 插件系统 | 11 服务, ~3,200 行, 9 测试 | ⭐⭐⭐⭐⭐ | ✅ 已完成 |
| [mini-docker](./mini-docker/) | Docker Engine | 完整引擎 | Builder + REST + Registry, ~780 行 | ⭐⭐⭐⭐ | ✅ 已完成 |
| [mini-kueue](./mini-kueue/) | kueue (8,811 文件) | 10 CRD, 10+ Job 类型 | 4 CRD, 1 Job, FIFO | ⭐⭐⭐ | 🚧 开发中 |
| [mini-kubepray](./mini-kubepray/) | kubespray (28 Roles) | 多引擎, 多 CNI | containerd + Flannel | ⭐ | 🚧 开发中 |

## 架构与依赖关系

```
mini-nerdctl ──gRPC──▶ mini-containerd
mini-docker  ──gRPC──▶ mini-containerd
mini-kueue   ──▶ k8s API server（独立）
mini-kubepray ──▶ kubeadm + containerd（调用系统二进制）
```

### 与 mini-kubernetes 的关系

本项目是更大蓝图的**容器运行时基础层**。兄弟项目 **mini-kubernetes** 在此基础上构建编排层（apiserver、scheduler、kubelet、controller-manager、kube-proxy、kubeadm）。两个项目的桥接点是 **CRI gRPC 接口**：

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

## 组件详解

### mini-nerdctl — 命令行工具

**对标：** nerdctl (779 文件 → 9 命令, 5 包)
**Phase 1 |** ~1,300 行 Go

- ✅ **保留：** `run`、`exec`、`ps`、`logs`、`stop`、`rm`、`pull`、`images`、`rmi`
- ❌ **砍掉：** compose、IPFS、rootless、healthcheck、BuildKit、manifest、checkpoint、login、network、volume

轻量 CLI 层，将用户友好的命令翻译为 containerd gRPC 调用。9 个 cobra 命令，由 5 个包（config、container、image、reference、runtime）支撑。

### mini-containerd — 容器运行时

**对标：** containerd (5,280 文件 → 11 gRPC 服务)
**Phase 2 |** ~3,200 行 Go，9 个测试

- ✅ **保留（4 个 proto 定义核心服务）：** Content（blob 存储）、Image（镜像管理）、Snapshot（overlayfs）、Task（进程管理）
- ✅ **保留（7 个辅助服务，满足 containerd v2 客户端兼容）：** Containers、Version、Namespaces、Leases（空操作）、Events（空操作）、Introspection、Transfer（空操作）
- ✅ **存储：** BoltDB 元数据存储
- ❌ **砍掉：** 插件系统、CRI、NRI、sandbox、streaming、diff、多命名空间

核心运行时。镜像以 OCI 内容寻址 blob 形式存储；容器通过 task 服务管理的 Linux namespace 和 cgroup 运行。BoltDB 存储所有元数据。辅助空操作服务用于满足 containerd v2 客户端握手协议，不增加实现复杂度。

### mini-docker — 构建引擎 + REST API

**对标：** Docker Engine
**Phase 3 |** ~780 行 Go

- ✅ **保留（Phase 3.1）：** Dockerfile 构建器 — 解析器（9 条指令：FROM/RUN/COPY/CMD/ENTRYPOINT/ENV/WORKDIR/EXPOSE/LABEL）和逐层执行器
- ✅ **保留（Phase 3.4）：** 兼容 Docker 的 REST API over Unix socket（7 个端点：`/containers/*`、`/images/*`、`/build`、`/_ping`）
- ✅ **保留：** OCI registry 镜像拉取，支持 Bearer Token 认证
- 🚧 **规划中：** bridge 网络（Phase 3.2）、卷管理（Phase 3.3）
- ❌ **砍掉：** Swarm、overlay 网络、多存储驱动、BuildKit、插件系统、多阶段构建

兼容 Docker 的守护进程。构建器解析 Dockerfile 并逐层执行（RUN 命令在临时目录中运行，类似 kaniko）。REST API 提供容器和镜像操作。镜像拉取直接对接 OCI registry。网络和卷子系统已设计但尚未实现（仅有桩包）。

### mini-kueue — 资源调度

**对标：** kueue (8,811 文件 → 4 CRD)
**Phase 4 |** 预计 2-3 月

- ✅ **保留：** Workload、ClusterQueue、LocalQueue、ResourceFlavor、batch/Job、FIFO 调度
- ❌ **砍掉：** Cohort、AdmissionCheck、MultiKueue、Topology、FairSharing、Preemption、10+ Job 类型

基于 Kubernetes Operator 模式的资源配额管理器。Workload 进入 LocalQueue，ClusterQueue 检查资源可用性，FIFO 调度器在配额充足时放行。

### mini-kubepray — 集群部署

**对标：** kubespray (28 Roles → 4 脚本)
**Phase 5 |** 预计 2-3 周

- ✅ **保留：** containerd、Flannel、kubeadm init/join
- ❌ **砍掉：** 其他容器引擎、其他 CNI、云插件、升级/恢复

四个 shell 脚本完成 Kubernetes 集群引导：系统准备、containerd 安装、kubeadm init/join、CNI 配置。

## 快速开始

```bash
# 构建所有组件
make build

# 运行 mini-containerd
make run-containerd

# 使用 mini-nerdctl
./bin/mini-nerdctl pull alpine:latest
./bin/mini-nerdctl run alpine:latest echo hello

# 运行 mini-docker
make run-dockerd
curl --unix-socket /tmp/mini-docker/docker.sock http://localhost/_ping
```

## 实施路线

| 阶段 | 组件 | 工期 | 状态 |
|------|------|------|------|
| 1 | mini-nerdctl | 2-3 周 | ✅ 已完成 |
| 2 | mini-containerd | 4-6 月 | ✅ 已完成 |
| 3 | mini-docker | 2-3 月 | ✅ 已完成 |
| 4 | mini-kueue | 2-3 月 | 🚧 开发中 |
| 5 | mini-kubepray | 2-3 周 | 🚧 开发中 |

## 设计模式

每个组件都体现了一种经典系统设计模式：

| 组件 | 模式 | 一句话概括 |
|------|------|----------|
| mini-nerdctl | API 外观 | 将 gRPC 调用封装为用户友好的 CLI |
| mini-containerd | 分层抽象 | content → image → snapshot → task，每层独立 |
| mini-docker | Builder 模式 | 逐层构建 + 缓存，临时容器做构建环境 |
| mini-kueue | 队列调度 | FIFO → 资源检查 → 准入 |
| mini-kubepray | Phase 流水线 | prep → container → kubeadm → CNI，阶段顺序执行 |

---

© 2026 [tcherry](https://github.com/tcherry). 基于 [MIT License](LICENSE) 发布。
