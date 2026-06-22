# mini-kueue

Kubernetes 资源配额与排队调度控制器的简化实现，对标 [kueue](https://github.com/kubernetes-sigs/kueue)（8,811 Go 文件 → 精简为 4 个 CRD + FIFO 调度）。

## 精简策略

| 保留 | 砍掉 |
|------|------|
| Workload, ClusterQueue, LocalQueue, ResourceFlavor | Cohort, AdmissionCheck, WorkloadPriorityClass |
| batch/Job 一种工作负载类型 | MultiKueue, ProvisioningRequest, Topology |
| FIFO 调度 | Ray, Kubeflow, Spark, MPIJob, AppWrapper 等 10+ Job 类型 |
| 单 Flavor 分配 | FairSharing, Preemption, Borrowing, TAS |

## 架构

```
┌──────────────────────────────────────────────┐
│                  CRDs                         │
│  Workload / ClusterQueue / LocalQueue / Flavor │
│  (apis/kueue/v1beta1/)                        │
├──────────────────────────────────────────────┤
│                                              │
│  internal/webhook/                           │
│  (ValidatingWebhook: 校验 Workload spec)      │
│  (MutatingWebhook: 注入 queue 标签)            │
│                                              │
│  internal/controller/                        │
│  (Workload 状态机 + ClusterQueue 资源管理)     │
│                                              │
│  internal/scheduler/                         │
│  (FIFO 调度循环: head-available → admit)      │
│                                              │
└──────────────────────────────────────────────┘
```

## 调度流程

```
1. Job 创建 → Workload 被 jobframework 自动生成
2. Webhook 校验 Workload 合法性
3. Workload Controller 将 Workload 加入对应 LocalQueue/ClusterQueue
4. Scheduler 触发调度循环:
   for each ClusterQueue:
       while head(ClusterQueue) fits in quota:
           admit(head)
           head.status = Admitted
           create Pod
```

## 构建与部署

```bash
# 生成 CRD manifests
controller-gen object paths="./..." crd output:crd:artifacts=config/crd

# 构建
go build -o mini-kueue ./cmd/controller/

# 部署
kubectl apply -f config/crd/
./mini-kueue --metrics-bind-address :8080
```
