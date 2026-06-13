# mini-container-ecosystem

从零实现容器生态核心组件的简易版本。每个组件对标真实项目（参考 `/kubernetes-ecosystem/` 下的源码），保留核心架构和算法，去掉插件系统、多租户、生产级容错等工程复杂性。

## 组件

| 组件 | 对标项目 | 真实代码量 | Mini 保留 | 难度 | 工期 |
|------|---------|-----------|----------|------|------|
| [mini-nerdctl](./mini-nerdctl/) | nerdctl (779 文件) | 30+ 命令, 65 包 | 8 命令, ~20 包 | ⭐⭐ | 3 周 |
| [mini-containerd](./mini-containerd/) | containerd (5,280 文件) | 16 gRPC 服务, 插件系统 | 5 服务, BoltDB | ⭐⭐⭐⭐⭐ | 4-6 月 |
| [mini-docker](./mini-docker/) | Docker Engine | 完整引擎 | Builder + REST + Network | ⭐⭐⭐⭐ | 2-3 月 |
| [mini-kueue](./mini-kueue/) | kueue (8,811 文件) | 10 CRD, 10+ Job 集成 | 4 CRD, 1 Job, FIFO | ⭐⭐⭐ | 2-3 月 |
| [mini-kubepray](./mini-kubepray/) | kubespray (28 Roles) | 多引擎多CNI | containerd+Flannel | ⭐ | 2-3 周 |

## 依赖关系

```
mini-nerdctl ──gRPC──▶ mini-containerd
mini-docker  ──gRPC──▶ mini-containerd
mini-kueue   ──▶ k8s API server (独立)
mini-kubepray ──▶ kubeadm + containerd (调用系统二进制)
```

## 各组件精简策略

### mini-nerdctl（对标 nerdctl 779 文件 → ~20 包）
- ✅ 保留: `run`, `exec`, `ps`, `logs`, `stop`, `rm`, `pull`, `images`
- ❌ 砍掉: compose, IPFS, rootless, healthcheck, BuildKit, manifest, checkpoint, login

### mini-containerd（对标 containerd 5,280 文件 → 5 核心服务）
- ✅ 保留: content, image, snapshot, task, metadata (BoltDB)
- ❌ 砍掉: 插件系统, CRI, NRI, sandbox, transfer, streaming, events, diff, leases

### mini-docker
- ✅ 保留: Dockerfile builder (FROM/RUN/COPY/CMD), bridge 网络, REST API
- ❌ 砍掉: Swarm, overlay 网络, 多存储驱动, plugin 系统

### mini-kueue（对标 kueue 8,811 文件 → 4 CRD + FIFO）
- ✅ 保留: Workload, ClusterQueue, LocalQueue, ResourceFlavor, batch/Job, FIFO 调度
- ❌ 砍掉: MultiKueue, TAS, FairSharing, Preemption, Cohort, 10+ Job 类型

### mini-kubepray（对标 kubespray 28 Roles → 核心脚本）
- ✅ 保留: containerd, Flannel, kubeadm init/join
- ❌ 砍掉: 其他容器引擎, 其他 CNI, 云插件, 升级/恢复

## 快速开始

```bash
# 构建所有组件
make build

# 运行 mini-containerd
make run-containerd

# 使用 mini-nerdctl
./bin/mini-nerdctl pull alpine:latest
./bin/mini-nerdctl run alpine:latest echo hello
```

## 实施路线

1. **Phase 1** — mini-nerdctl（快速产出，熟悉 containerd API）
2. **Phase 2** — mini-containerd（核心攻坚）
3. **Phase 3** — mini-docker（在 containerd 之上集成）
4. **Phase 4** — mini-kueue（K8s Operator 模式）
5. **Phase 5** — mini-kubepray（收尾部署）
