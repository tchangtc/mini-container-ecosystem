# mini-container-ecosystem

> 從零實作容器生態核心元件的簡化版本——透過親手構建來理解容器內部原理。

[English](README.md) | [简体中文](README.zh-CN.md)

---

## 專案概述

**mini-container-ecosystem** 是一個教育專案，以精簡版本重新實作容器生態的核心元件。每個元件對標真實專案（參考原始碼位於 `/kubernetes-ecosystem/`），保留核心架構與演算法，移除外掛系統、多租戶、生產級容錯等工程複雜性。

本專案回答的核心問題是：**如何在一臺機器上執行容器？**

| 元件 | 對標專案 | 真實規模 | Mini 保留 | 難度 | 狀態 |
|------|---------|---------|----------|------|------|
| [mini-nerdctl](./mini-nerdctl/) | nerdctl (779 檔案) | 30+ 命令, 65 套件 | 9 命令, 5 套件, ~1,300 行 | ⭐⭐ | ✅ 已完成 |
| [mini-containerd](./mini-containerd/) | containerd (5,280 檔案) | 16 gRPC 服務, 外掛系統 | 11 服務, ~3,200 行, 9 測試 | ⭐⭐⭐⭐⭐ | ✅ 已完成 |
| [mini-docker](./mini-docker/) | Docker Engine | 完整引擎 | Builder + REST + Registry, ~780 行 | ⭐⭐⭐⭐ | ✅ 已完成 |
| [mini-kueue](./mini-kueue/) | kueue (8,811 檔案) | 10 CRD, 10+ Job 類型 | 4 CRD, 1 Job, FIFO | ⭐⭐⭐ | 🚧 開發中 |
| [mini-kubepray](./mini-kubepray/) | kubespray (28 Roles) | 多引擎, 多 CNI | containerd + Flannel | ⭐ | 🚧 開發中 |

## 架構與依賴關係

```
mini-nerdctl ──gRPC──▶ mini-containerd
mini-docker  ──gRPC──▶ mini-containerd
mini-kueue   ──▶ k8s API server（獨立）
mini-kubepray ──▶ kubeadm + containerd（呼叫系統二進位檔）
```

### 與 mini-kubernetes 的關係

本專案是更大藍圖的**容器執行階段基礎層**。兄弟專案 **mini-kubernetes** 在此基礎上構建編排層（apiserver、scheduler、kubelet、controller-manager、kube-proxy、kubeadm）。兩個專案的橋接點是 **CRI gRPC 介面**：

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

## 元件詳解

### mini-nerdctl — 命令列工具

**對標：** nerdctl (779 檔案 → 9 命令, 5 套件)
**Phase 1 |** ~1,300 行 Go

- ✅ **保留：** `run`、`exec`、`ps`、`logs`、`stop`、`rm`、`pull`、`images`、`rmi`
- ❌ **移除：** compose、IPFS、rootless、healthcheck、BuildKit、manifest、checkpoint、login、network、volume

輕量級 CLI 層，將使用者友善的命令轉譯為 containerd gRPC 呼叫。9 個 cobra 命令，由 5 個套件（config、container、image、reference、runtime）支撐。

### mini-containerd — 容器執行階段

**對標：** containerd (5,280 檔案 → 11 gRPC 服務)
**Phase 2 |** ~3,200 行 Go，9 個測試

- ✅ **保留（4 個 proto 定義核心服務）：** Content（blob 儲存）、Image（映像管理）、Snapshot（overlayfs）、Task（程序管理）
- ✅ **保留（7 個輔助服務，滿足 containerd v2 客戶端相容）：** Containers、Version、Namespaces、Leases（空操作）、Events（空操作）、Introspection、Transfer（空操作）
- ✅ **儲存：** BoltDB 中繼資料儲存
- ❌ **移除：** 外掛系統、CRI、NRI、sandbox、streaming、diff、多命名空間

核心執行階段。映像以 OCI 內容定址 blob 形式儲存；容器透過 task 服務管理的 Linux namespace 與 cgroup 執行。BoltDB 儲存所有中繼資料。輔助空操作服務用於滿足 containerd v2 客戶端握手協定，不增加實作複雜度。

### mini-docker — 建構引擎 + REST API

**對標：** Docker Engine
**Phase 3 |** ~780 行 Go

- ✅ **保留（Phase 3.1）：** Dockerfile 建構器 — 解析器（9 條指令：FROM/RUN/COPY/CMD/ENTRYPOINT/ENV/WORKDIR/EXPOSE/LABEL）和逐層執行器
- ✅ **保留（Phase 3.4）：** 相容 Docker 的 REST API over Unix socket（7 個端點：`/containers/*`、`/images/*`、`/build`、`/_ping`）
- ✅ **保留：** OCI registry 映像拉取，支援 Bearer Token 驗證
- 🚧 **規劃中：** bridge 網路（Phase 3.2）、卷管理（Phase 3.3）
- ❌ **移除：** Swarm、overlay 網路、多儲存驅動、BuildKit、外掛系統、多階段建構

相容 Docker 的守護程序。建構器解析 Dockerfile 並逐層執行（RUN 命令在暫存目錄中執行，類似 kaniko）。REST API 提供容器和映像操作。映像拉取直接對接 OCI registry。網路和卷子系統已設計但尚未實作（僅有樁套件）。

### mini-kueue — 資源排程

**對標：** kueue (8,811 檔案 → 4 CRD)
**Phase 4 |** 預計 2-3 月

- ✅ **保留：** Workload、ClusterQueue、LocalQueue、ResourceFlavor、batch/Job、FIFO 排程
- ❌ **移除：** Cohort、AdmissionCheck、MultiKueue、Topology、FairSharing、Preemption、10+ Job 類型

基於 Kubernetes Operator 模式的資源配額管理器。Workload 進入 LocalQueue，ClusterQueue 檢查資源可用性，FIFO 排程器在配額充足時放行。

### mini-kubepray — 叢集部署

**對標：** kubespray (28 Roles → 4 指令稿)
**Phase 5 |** 預計 2-3 週

- ✅ **保留：** containerd、Flannel、kubeadm init/join
- ❌ **移除：** 其他容器引擎、其他 CNI、雲端外掛、升級/復原

四個 shell 指令稿完成 Kubernetes 叢集引導：系統準備、containerd 安裝、kubeadm init/join、CNI 設定。

## 快速開始

```bash
# 構建所有元件
make build

# 執行 mini-containerd
make run-containerd

# 使用 mini-nerdctl
./bin/mini-nerdctl pull alpine:latest
./bin/mini-nerdctl run alpine:latest echo hello

# 執行 mini-docker
make run-dockerd
curl --unix-socket /tmp/mini-docker/docker.sock http://localhost/_ping
```

## 實施路線

| 階段 | 元件 | 工期 | 狀態 |
|------|------|------|------|
| 1 | mini-nerdctl | 2-3 週 | ✅ 已完成 |
| 2 | mini-containerd | 4-6 月 | ✅ 已完成 |
| 3 | mini-docker | 2-3 月 | ✅ 已完成 |
| 4 | mini-kueue | 2-3 月 | 🚧 開發中 |
| 5 | mini-kubepray | 2-3 週 | 🚧 開發中 |

## 設計模式

每個元件都體現了一種經典系統設計模式：

| 元件 | 模式 | 一句話概括 |
|------|------|----------|
| mini-nerdctl | API 外觀 | 將 gRPC 呼叫封裝為使用者友善的 CLI |
| mini-containerd | 分層抽象 | content → image → snapshot → task，每層獨立 |
| mini-docker | Builder 模式 | 逐層建構 + 快取，暫存容器做建構環境 |
| mini-kueue | 佇列排程 | FIFO → 資源檢查 → 准入 |
| mini-kubepray | Phase 管線 | prep → container → kubeadm → CNI，階段循序執行 |

---

© 2026 [tcherry](https://github.com/tcherry). 基於 [MIT License](LICENSE) 釋出。
