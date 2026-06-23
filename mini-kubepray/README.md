# mini-kubepray

Kubernetes 集群自动化部署脚本集合，对标 [kubespray](https://github.com/kubernetes-sigs/kubespray)（28 个 Ansible Role → 精简为 4 个 Shell 脚本）。

## 精简策略

| 保留 | 砍掉 |
|------|------|
| containerd 容器运行时 | docker, cri-o, cri-dockerd, gvisor, kata, youki |
| Flannel CNI | calico, cilium, kube-ovn, kube-router, macvlan, multus |
| kubeadm init/join | 升级, 恢复, 节点驱逐 |
| Shell + SSH | Ansible, Python 依赖 |

## 脚本说明

| 脚本 | 对标 Ansible Role | 说明 |
|------|------------------|------|
| `01-prepare.sh` | `kubernetes/preinstall` | 关 swap, 加载 br_netfilter, 设置 sysctl |
| `02-container.sh` | `container-engine/containerd` | 安装 + 配置 containerd (systemd cgroup) |
| `03-kubeadm.sh` | `kubernetes/kubeadm` | 安装 kubeadm/kubelet/kubectl |
| `04-cni.sh` | `network_plugin/flannel` | 安装 Flannel CNI |
| `deploy.sh` | `playbooks/cluster.yml` | 入口脚本 (init/add-node/reset) |

注意：直接调用系统的 `kubeadm` 二进制，不自己实现 kubeadm。

## 使用

```bash
cp inventory/hosts.ini.example inventory/hosts.ini
vim inventory/hosts.ini

bash deploy.sh init       # 初始化集群
bash deploy.sh add-node   # 加入工作节点
bash deploy.sh reset      # 重置集群
```
