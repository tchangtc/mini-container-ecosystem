# mini-nerdctl

containerd CLI 的简化实现（8 命令, ~600 行 Go）, 对标 [nerdctl](https://github.com/containerd/nerdctl)（779 Go 文件）。

## 实现状态

| 命令 | 状态 | 需要 root | 说明 |
|------|------|----------|------|
| `pull <image>` | ✅ 完成 | ❌ | 拉取并解包镜像（含进度输出） |
| `images` | ✅ 完成 | ❌ | 列表显示镜像 (名称/Digest/大小/创建时间) |
| `rmi <image>` | ✅ 完成 | ❌ | 删除镜像 + 同步清理 |
| `ps` | ✅ 完成 | ❌ | 列表显示容器 (ID/镜像/状态/PID) |
| `run <image> [cmd]` | ✅ 完成 | ⚠️ **需要** | 创建+启动容器（overlay mount 需要 CAP_SYS_ADMIN） |
| `exec <id> <cmd>` | ✅ 完成 | ⚠️ **需要** | 在运行容器中执行命令 |
| `logs <id>` | ✅ 完成 | ⚠️ **需要** | 读取容器 stdout |
| `stop <id>` | ✅ 完成 | ❌ | SIGTERM → 10s 超时 → SIGKILL |
| `rm <id>` | ✅ 完成 | ❌ | 删除容器；`-f` 强制停止后删除 |

## 架构

```
cmd/nerdctl/main.go       入口 (package main)
cmd/root.go               cobra 命令注册 + 参数绑定
pkg/
  config/config.go        配置 (socket/namespace, 从环境变量加载)
  reference/reference.go  镜像引用解析 (alpine → docker.io/library/alpine:latest)
  image/image.go          pull / list / remove
  container/run.go        run (pull → create → start → wait → cleanup)
  container/container.go  ps / stop / rm
  runtime/runtime.go      exec / logs
```

## 构建

```bash
go build -o mini-nerdctl ./cmd/nerdctl/
```

## 使用

```bash
# 确保可以访问 containerd socket (用户需在 containerd 组)
./mini-nerdctl images

# 从可访问的 registry 拉取镜像
./mini-nerdctl pull registry.example.com/my-image:tag

# 运行容器（需要 root）
sudo ./mini-nerdctl run alpine:latest echo hello

# 指定 containerd 地址
export CONTAINERD_ADDRESS=/run/containerd/containerd.sock
export CONTAINERD_NAMESPACE=default
```

## 权限说明

| 命令 | 不需要 root |
|------|-----------|
| pull, images, rmi, ps, stop, rm | ✅ 纯 gRPC 操作 |
| run, exec, logs | ⚠️ containerd 的 overlay snapshot 需要 CAP_SYS_ADMIN, 用 `sudo` 或以 root 运行 |

## 依赖

```
github.com/containerd/containerd/v2 v2.2.2  (gRPC client)
github.com/spf13/cobra v1.8.1              (CLI)
github.com/opencontainers/runtime-spec     (OCI spec types)
```
