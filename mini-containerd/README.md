# mini-containerd

容器运行时的简化实现，对标 [containerd](https://github.com/containerd/containerd)（5,280 Go 文件 → 精简为 5 个核心 gRPC 服务 + BoltDB 元数据）。

## 精简策略

| 保留 (5 服务) | 砍掉 |
|--------------|------|
| content (blob 存储) | 插件系统 (整个 plugins/ 目录) |
| image (镜像拉取/管理) | CRI (Kubernetes 容器运行时接口) |
| snapshot (overlayfs 快照) | NRI (节点资源接口) |
| task (容器进程管理) | sandbox, transfer, streaming, events, diff |
| metadata (BoltDB 持久化) | leases, introspection, metrics, namespaces |

## 架构

```
┌────────────────────────────────────────────┐
│              gRPC API Server                │
│  content / image / snapshot / task          │
│  (proto/ → api/ 代码生成)                    │
├────────────────────────────────────────────┤
│                                            │
│  internal/image/        internal/task/      │
│  (Registry + manifest   (namespace +        │
│   + 逐层解包)            cgroup v2)          │
│                                            │
│  internal/content/      internal/snapshot/  │
│  (文件系统 blob 存储)     (overlayfs)        │
│                                            │
│  internal/metadata/                        │
│  (BoltDB: containers/images/snapshots)      │
│                                            │
└────────────────────────────────────────────┘
```

## 子阶段

| 阶段 | 包 | 说明 | 关键接口 |
|------|-----|------|---------|
| 2.1 | `internal/content` | Blob 存储 (Write/Read/Info/Delete/List) | `content.Store` |
| 2.2 | `internal/image` | Registry v2 对接 + manifest 解析 + 解包 | `image.Service` |
| 2.3 | `internal/snapshot` | overlayfs Prepare/Commit/Mount | `snapshot.Snapshotter` |
| 2.4 | `internal/task` | Linux namespace 隔离 + cgroup v2 | `task.Service` |
| 2.5 | `internal/metadata` | BoltDB 持久化（整合以上 4 个服务的状态） | — |

## Proto

gRPC API 定义在 `proto/` 目录下有 4 个 .proto 文件：

```bash
bash proto/generate.sh    # → 生成 Go 代码到 api/{content,image,snapshot,task}/
```

## 构建与运行

```bash
bash proto/generate.sh
go build -o mini-containerd ./cmd/containerd/
sudo ./mini-containerd --config config/default.toml
```
