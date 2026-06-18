# 整体规划：mini-container-ecosystem + mini-kubernetes

> 基于 9 个参考仓库（`/home/tcherry/tc/kubernetes-ecosystem/`）的源码分析，制定两个 mini 项目的规划。
> 最后更新：2026-06-12

---

## 一、参考仓库全景

### 容器运行时层

| 仓库 | 代码量 | 用途 |
|------|--------|------|
| **containerd** | 6,406 files, 212M | 容器运行时：content/image/snapshot/task 核心服务 + 插件系统 + CRI |
| **nerdctl** | 918 files, 23M | containerd CLI：30+ 命令, compose/IPFS/rootless/BuildKit |

### Kubernetes 编排层

| 仓库 | 代码量 | 用途 |
|------|--------|------|
| **kubernetes** | 30,482 files, 1.7G | K8s 主仓库：apiserver/scheduler/controller-manager/kubelet/kube-proxy/kubeadm |
| **kubeadm** | 301 files, 6.5M | kubeadm operator + kinder 测试工具（非 kubeadm 二进制本体） |
| **kubespray** | 1,211 files, 43M | Ansible 集群部署：28 个 Role, 多容器引擎, 多 CNI |
| **kueue** | 11,011 files, 246M | 资源配额调度：10+ CRD, MultiKueue/TAS/FairSharing, 10+ Job 类型 |

### Kubernetes 框架层（staging / 库）

| 仓库 | 代码量 | 用途 |
|------|--------|------|
| **apiserver** | 1,277 files, 45M | 通用 API Server 框架：registry/store/admission 接口, etcd 存储, 请求链路 |
| **client-go** | 2,395 files, 56M | K8s API 客户端：Clientset, Informer, Workqueue, Discovery, LeaderElection |
| **controller-runtime** | 437 files, 54M | 控制器框架：Manager, Reconciler, Builder, Controller, Webhook, EnvTest |

---

## 二、两个 Mini 项目的定位

```
┌─────────────────────────────────────────────────┐
│              mini-container-ecosystem            │
│                                                  │
│  解决：怎么在一台机器上跑容器                      │
│                                                  │
│  组件：nerdctl → containerd → docker             │
│        kubepray (部署辅助)                        │
│        kueue (K8s 资源调度，边界)                  │
│                                                  │
│  总工期：8-10 月 (兼职)                           │
└──────────────────────┬───────────────────────────┘
                       │ CRI gRPC
┌──────────────────────▼───────────────────────────┐
│                 mini-kubernetes                  │
│                                                  │
│  解决：怎么在集群里调度容器                         │
│                                                  │
│  组件：apiserver, scheduler, controller-manager  │
│        kubelet, kubeadm, kube-proxy              │
│                                                  │
│  总工期：12-18 月 (兼职)                          │
└─────────────────────────────────────────────────┘
```

### 映射关系：参考仓库 → Mini 项目

```
nerdctl      ──精简──▶ mini-nerdctl          (container-ecosystem)
containerd   ──精简──▶ mini-containerd       (container-ecosystem) ← 核心基石
kubespray    ──精简──▶ mini-kubepray         (container-ecosystem)
kueue        ──精简──▶ mini-kueue            (container-ecosystem, 边界组件)

kubernetes   ──精简──▶ mini-kubernetes       全部 6 个组件
kubeadm      ──精简──▶ mini-kubeadm          (kubernetes)

apiserver           ──模式参考──▶ mini-apiserver 的 storage/registry/admission 设计
client-go           ──直接 import (不重写)
controller-runtime  ──模式参考──▶ mini-ctrl-mgr 和 mini-kueue 的 Reconciler 模式
```

---

## 三、三个层次的实现策略

| 层次 | 仓库 | 策略 | 原因 |
|------|------|------|------|
| **容器运行时** | containerd, nerdctl | **完整重写** | 核心学习目标，必须亲手实现 namespace/cgroup/overlayfs |
| **K8s 组件** | kubernetes 内部 6 组件 | **完整重写** | 理解编排原理：reconcile、调度、CRI 集成 |
| **K8s 框架层** | apiserver, client-go, ctrl-runtime | **模式参考 / 直接 import** | 工具库不重写；模式学到了，需要时引真库 |

---

## 四、mini-container-ecosystem 详细规划

### 组件总览

| 组件 | 对标 | 真实→Mini | 核心保留 |
|------|------|----------|---------|
| **mini-nerdctl** | nerdctl (779→~20包) | 30+命令→8命令 | run, exec, ps, logs, stop, rm, pull, images, rmi |
| **mini-containerd** | containerd (5,280→5服务) | 16服务→5服务 | content, image, snapshot, task, metadata(BoltDB) |
| **mini-docker** | Docker Engine | Builder+REST+Net | Dockerfile 解析, bridge 网络, REST API over Unix socket |
| **mini-kueue** | kueue (8,811→4CRD) | 10CRD→4CRD | Workload/ClusterQueue/LocalQueue/ResourceFlavor, FIFO调度 |
| **mini-kubepray** | kubespray (28 Role→4脚本) | 多引擎→containerd | prep/container/kubeadm/cni 4 个脚本 |

### 各组件精简策略

#### mini-nerdctl
- ✅ 保留：run, exec, ps, logs, stop, rm, pull, images, rmi（8 命令）
- ❌ 砍掉：compose, IPFS, rootless, healthcheck, BuildKit, manifest, checkpoint, login, network, volume

#### mini-containerd
- ✅ 保留：content (blob 存储), image (镜像管理), snapshot (overlayfs), task (进程管理), metadata (BoltDB)
- ❌ 砍掉：插件系统(整个 plugins/), CRI, NRI, sandbox, transfer, streaming, events, diff, leases, 多 namespace

#### mini-docker
- ✅ 保留：Dockerfile builder (FROM/RUN/COPY/CMD/ENV/WORKDIR/EXPOSE/LABEL), bridge 网络, REST API
- ❌ 砍掉：Swarm, overlay 网络, 多存储驱动, BuildKit, plugin 系统, 多阶段构建

#### mini-kueue
- ✅ 保留：Workload, ClusterQueue, LocalQueue, ResourceFlavor, batch/Job, FIFO 调度
- ❌ 砍掉：Cohort, AdmissionCheck, MultiKueue, Topology, FairSharing, Preemption, 10+ Job 类型

#### mini-kubepray
- ✅ 保留：containerd + Flannel + kubeadm init/join
- ❌ 砍掉：docker/cri-o/gvisor/kata, calico/cilium/10种CNI, 云插件, 升级/恢复

### 目录结构 (52 files)

```
mini-container-ecosystem/
├── README.md / LICENSE / .gitignore / Makefile / go.work
├── mini-nerdctl/      (8 files)
│   ├── cmd/nerdctl/main.go
│   ├── cmd/root.go                 ← 9 cobra commands
│   └── pkg/{client,container,image,task,logging,config}/
├── mini-containerd/   (15 files)
│   ├── cmd/containerd/main.go
│   ├── proto/{content,image,snapshot,task}.proto + generate.sh
│   ├── api/{content,image,snapshot,task}/doc.go
│   ├── config/default.toml
│   └── internal/{content,image,snapshot,task,metadata}/
├── mini-docker/       (8 files)
│   ├── cmd/dockerd/main.go
│   └── internal/{builder/{parser,executor},network,volume,api}/
├── mini-kueue/        (6 files)
│   ├── cmd/controller/main.go
│   ├── apis/kueue/v1beta1/doc.go
│   └── internal/{controller,scheduler,webhook}/
└── mini-kubepray/     (9 files)
    ├── deploy.sh
    ├── inventory/hosts.ini.example
    ├── config/kubeadm-init.yaml
    └── scripts/{01-prepare,02-container,03-kubeadm,04-cni}.sh
```

---

## 五、mini-kubernetes 详细规划

### 组件总览

| 组件 | 对标 | 真实→Mini | 核心保留 |
|------|------|----------|---------|
| **mini-apiserver** | kube-apiserver (~1,600→500文件) | 全API→core | Pod/Node/Service/CM/Secret/Event/NS, 单etcd, 无CRD/RBAC |
| **mini-kubelet** | kubelet (~800→400文件) | 全功能→核心 | podWorkers, CRI, 4种volume, status, probes |
| **mini-scheduler** | kube-scheduler (~262→100文件) | 20+plugin→5 | NodeName/NodeResources/NodeUnschedulable/Toleration/DefaultBinder |
| **mini-controller-manager** | kube-controller-manager (~650→80文件) | 35+ctrl→3 | ReplicaSet, Deployment, NodeLifecycle |
| **mini-kubeadm** | kubeadm (~347→80文件) | 全phase→5 | certs, kubeconfig, controlplane, etcd, kubelet |
| **mini-kube-proxy** | kube-proxy (~146→40文件) | 多mode→iptables | Services + EndpointSlice watch, iptables NAT |

### 各组件精简策略

#### mini-apiserver
- ✅ 保留：core API (Pod/Node/Service/ConfigMap/Secret/Event/Namespace), etcd3 存储, NamespaceLifecycle admission
- ❌ 砍掉：CRD, Aggregation, RBAC, Webhook, Audit, FlowControl, OpenAPI, 多版本 API

#### mini-kubelet
- ✅ 保留：podWorkers (Pod 状态机), CRI (→ mini-containerd), 4 种 volume, status 上报, liveness/readiness probes
- ❌ 砍掉：Device Plugin, DRA, cgroup v2 管理, Eviction, Checkpoint, OOM Watcher, PodSecurity

#### mini-scheduler
- ✅ 保留：NodeName, NodeResources (Fit+Score), NodeUnschedulable, TaintToleration, DefaultBinder, FIFO 队列
- ❌ 砍掉：Preemption, Gang Scheduling, DRA, Score Normalization, Priority Queue, 15+ 其他插件

#### mini-controller-manager
- ✅ 保留：ReplicaSet (reconcile 范例), Deployment (滚动更新), NodeLifecycle (节点健康)
- ❌ 砍掉：20+ 其他 controller (Job, CronJob, DaemonSet, StatefulSet, GC, Endpoint, Namespace, etc.)

#### mini-kubeadm
- ✅ 保留：certs, kubeconfig, controlplane, etcd, kubelet (5 phases)
- ❌ 砍掉：upgrade, reset, join 完整流程, addon (CoreDNS/kube-proxy)

#### mini-kube-proxy
- ✅ 保留：iptables mode (KUBE-SERVICES → KUBE-SVC → KUBE-SEP)
- ❌ 砍掉：ipvs, nftables, winkernel

### 目录结构 (52 files)

```
mini-kubernetes/
├── README.md / LICENSE / .gitignore / Makefile / go.work
├── mini-apiserver/              (10 files)
│   ├── cmd/apiserver/main.go
│   ├── pkg/registry/core/{pod,node,service,configmap,secret,event,namespace}/
│   ├── pkg/storage/   (Storage.Interface)
│   ├── pkg/etcd/      (etcd3 实现)
│   └── pkg/admission/ (NamespaceLifecycle)
├── mini-kubelet/                (11 files)
│   ├── cmd/kubelet/main.go
│   ├── pkg/podworkers/    (Pod 状态机)
│   ├── pkg/cri/           (→ mini-containerd)
│   ├── pkg/volume/{emptydir,hostpath,secret,configmap}/
│   ├── pkg/status/        (status 上报)
│   ├── pkg/probe/         (liveness/readiness)
│   └── pkg/runtime/       (高层 Runtime 接口)
├── mini-scheduler/              (9 files)
│   ├── cmd/scheduler/main.go
│   ├── pkg/framework/     (Plugin 接口)
│   ├── pkg/plugins/{nodename,noderesources,nodeunschedulable,toleration,defaultbinder}/
│   └── pkg/queue/         (FIFO)
├── mini-controller-manager/     (5 files)
│   ├── cmd/controller-manager/main.go
│   └── pkg/controllers/{replicaset,deployment,nodelifecycle}/
├── mini-kubeadm/                (7 files)
│   ├── cmd/kubeadm/main.go
│   └── pkg/phases/{certs,kubeconfig,controlplane,etcd,kubelet}/
└── mini-kube-proxy/             (3 files)
    ├── cmd/kube-proxy/main.go
    └── pkg/iptables/
```

---

## 六、两个项目的桥接

```
mini-kubernetes                    mini-container-ecosystem
══════════════                     ═══════════════════════

mini-apiserver ──etcd──▶ 存储
      │
mini-scheduler ──▶ 选择 Node
      │
mini-ctrl-mgr ──▶ reconcile
      │
mini-kubelet ──watch──▶ 发现 Pod
      │
      │    ═══════ 桥接点 ═══════
      │
      │    CRI gRPC:
      │    RunPodSandbox()
      │    CreateContainer()
      │    StartContainer()
      │    StopContainer()
      │
      ▼
mini-containerd
      │
      ├── snapshot (overlayfs)
      ├── task     (namespace + cgroup)
      └── image    (registry)
```

---

## 七、完整实施顺序

| # | 阶段 | 项目 | 工期 | 产出 | 依赖 |
|---|------|------|------|------|------|
| 1 | mini-nerdctl | container-ecosystem | 2-3周 | CLI 工具，8 命令 | 系统 containerd |
| 2 | mini-containerd | container-ecosystem | 4-6月 | 核心运行时，5 gRPC 服务 | 无 |
| 3 | mini-docker | container-ecosystem | 2-3月 | Dockerfile 构建 + REST API | mini-containerd |
| 4 | mini-kubeadm | kubernetes | ~1月 | 集群 bootstrap，5 phases | mini-containerd (CRI) |
| 5 | mini-apiserver | kubernetes | 3-4月 | REST API + etcd 存储 | etcd |
| 6 | mini-scheduler | kubernetes | 1-2月 | 5 plugin 调度框架 | mini-apiserver |
| 7 | **mini-kubelet** | kubernetes | 3-4月 | **★ 桥接两个项目 ★** | mini-apiserver + mini-containerd |
| 8 | mini-ctrl-mgr | kubernetes | 1-2月 | RS/Deployment reconciler | mini-apiserver |
| 9 | mini-kube-proxy | kubernetes | 2-3周 | iptables Service 网络 | mini-apiserver |
| 10 | mini-kueue | container-ecosystem | 2-3月 | 4 CRD 资源调度 | mini-apiserver |
| 11 | mini-kubepray | container-ecosystem | 2-3周 | 一键部署脚本 | kubeadm 二进制 |

**总工期：20-30 月 (兼职)，10-15 月 (全职)**

---

## 八、核心设计模式总结

每个 mini 组件对应一个关键系统设计模式：

| 组件 | 核心模式 | 一句话 |
|------|---------|--------|
| mini-nerdctl | API 封装 | 把 gRPC 调用封装成用户友好的 CLI |
| mini-containerd | 分层抽象 | content→image→snapshot→task，每层独立 |
| mini-docker | Builder 模式 | 逐层构建 + 缓存，临时容器做构建环境 |
| mini-apiserver | REST 策略模式 | 每个资源实现 Getter/Lister/Creater 等接口 |
| mini-kubelet | 状态机 | Pod: SyncPod → Running → Terminating → Terminated |
| mini-scheduler | 插件框架 | Filter → Score → Bind，插件独立组合 |
| mini-ctrl-mgr | Reconciler 循环 | 观察 → 比较 → 修正，声明式 |
| mini-kubeadm | Phase 流水线 | certs → kubeconfig → manifests → kubelet |
| mini-kube-proxy | 事件驱动 | watch API → 翻译为内核规则 |
| mini-kueue | 队列调度 | FIFO → 资源检查 → admit |
