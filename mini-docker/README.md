# mini-docker

Docker 引擎的简化实现，在 mini-containerd 之上提供 Dockerfile 构建、bridge 网络、卷管理和 REST API。

## 精简策略

| 保留 | 砍掉 |
|------|------|
| Dockerfile builder (FROM/RUN/COPY/CMD...) | Swarm, overlay 网络, 多存储驱动 |
| bridge 网络 (veth pair + iptables NAT) | BuildKit, plugin 系统 |
| 绑定挂载 + 命名卷 | HEALTHCHECK, ARG, ADD, ONBUILD |
| REST API over Unix socket | 多阶段构建 (multi-stage build) |

## 架构

```
Docker CLI ──HTTP──▶ dockerd (REST API)
                       │
         ┌─────────────┼─────────────┐
         ▼             ▼             ▼
      Builder       Network       Volume
         │             │             │
         └─────────────┼─────────────┘
                       │ gRPC
                       ▼
               mini-containerd
```

## 子阶段

| 阶段 | 包 | 说明 |
|------|-----|------|
| 3.1 | `internal/builder/parser` | Dockerfile 解析 (9 条指令) |
| 3.1 | `internal/builder/executor` | 逐层执行 + 构建缓存 |
| 3.2 | `internal/network` | bridge 模式 (veth pair + iptables NAT) |
| 3.3 | `internal/volume` | 绑定挂载 + 命名卷管理 |
| 3.4 | `internal/api` | REST API over Unix socket |

## 构建与运行

```bash
go build -o mini-dockerd ./cmd/dockerd/
sudo ./mini-dockerd --containerd-addr /run/mini-containerd/containerd.sock
```
