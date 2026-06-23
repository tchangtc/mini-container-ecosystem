#!/usr/bin/env bash
# mini-kubepray — minimal Kubernetes cluster deployment tool.
# Deploys a single control-plane + N worker cluster using kubeadm.
#
# Usage:
#   bash deploy.sh init       # bootstrap a new cluster
#   bash deploy.sh add-node   # add worker nodes to existing cluster
#   bash deploy.sh reset      # tear down the cluster
#   bash deploy.sh init --dry-run  # simulate without executing
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DIR="${SCRIPT_DIR}/scripts"
CONFIG="${SCRIPT_DIR}/config/kubeadm-init.yaml"

# Allow override via environment variable for testing
INVENTORY="${MINIKUBE_INVENTORY:-${SCRIPT_DIR}/inventory/hosts.ini}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
DRY_RUN=false

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

# ── Argument parsing ─────────────────────────────────────────────
CMD="${1:-}"; shift || true
for arg in "$@"; do
    case "$arg" in
        --dry-run) DRY_RUN=true ;;
    esac
done

# ── Inventory ────────────────────────────────────────────────────
load_inventory() {
    if [[ ! -f "$INVENTORY" ]]; then
        log_error "Inventory not found: $INVENTORY"
        echo "  Copy inventory/hosts.ini.example → inventory/hosts.ini and edit it."
        exit 1
    fi
    source "$INVENTORY"
    CONTROL_PLANE_NODES="${CONTROL_PLANE_NODES:-}"
    WORKER_NODES="${WORKER_NODES:-}"
    K8S_VERSION="${K8S_VERSION:-1.29.0}"
    POD_CIDR="${POD_CIDR:-10.244.0.0/16}"
    SVC_CIDR="${SVC_CIDR:-10.96.0.0/12}"

    if [[ -z "$CONTROL_PLANE_NODES" ]]; then
        log_error "CONTROL_PLANE_NODES is empty — at least one control-plane node required"
        exit 1
    fi
    log_info "Control plane: $CONTROL_PLANE_NODES"
    log_info "Worker nodes:  ${WORKER_NODES:-none}"
    log_info "K8s version:   $K8S_VERSION"
}

# ── Remote execution ─────────────────────────────────────────────
run_remote() {
    local host="$1"; local script="$2"; local desc="$3"
    if $DRY_RUN; then
        log_info "[DRY-RUN] [$host] would run: $desc"
        return 0
    fi
    log_info "[$host] $desc"
    ssh -o StrictHostKeyChecking=no "$host" "bash -s" < "$script" || {
        log_error "[$host] $desc FAILED"
        return 1
    }
}

run_remote_cmd() {
    local host="$1"; local cmd="$2"; local desc="$3"
    if $DRY_RUN; then
        log_info "[DRY-RUN] [$host] would run: $desc"
        return 0
    fi
    log_info "[$host] $desc"
    ssh -o StrictHostKeyChecking=no "$host" "$cmd" || {
        log_error "[$host] $desc FAILED"
        return 1
    }
}

# ── Pre-flight ───────────────────────────────────────────────────
preflight() {
    log_info "Running pre-flight checks..."
    load_inventory

    # Check SSH connectivity
    local all_nodes="$CONTROL_PLANE_NODES $WORKER_NODES"
    for host in $all_nodes; do
        if $DRY_RUN; then
            log_info "[DRY-RUN] Would check SSH to $host"
            continue
        fi
        if ! ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 "$host" "echo ok" &>/dev/null; then
            log_error "Cannot SSH to $host — check SSH keys and network"
            exit 1
        fi
    done
    log_info "All nodes reachable via SSH"
}

# ── Init ─────────────────────────────────────────────────────────
cmd_init() {
    preflight
    log_info "=== Initializing Kubernetes cluster ==="

    local cp_node
    for host in $CONTROL_PLANE_NODES; do
        cp_node="$host"
        break  # Only first node is the init node
    done

    # Deploy to all nodes: prepare + container runtime
    for host in $CONTROL_PLANE_NODES $WORKER_NODES; do
        run_remote "$host" "${SCRIPTS_DIR}/01-prepare.sh" "System preparation"
        run_remote "$host" "${SCRIPTS_DIR}/02-container.sh" "Install containerd"
        run_remote "$host" "${SCRIPTS_DIR}/03-kubeadm.sh" "Install kubeadm/kubelet"
    done

    # Init control plane
    log_info "Initializing control plane on $cp_node..."
    if $DRY_RUN; then
        log_info "[DRY-RUN] Would run: kubeadm init --config=... on $cp_node"
    else
        # Generate kubeadm config with actual values
        local tmp_config="/tmp/kubeadm-init-$$.yaml"
        sed -e "s/v1.29.0/v${K8S_VERSION}/g" \
            -e "s|10.244.0.0/16|${POD_CIDR}|g" \
            -e "s|10.96.0.0/12|${SVC_CIDR}|g" \
            "$CONFIG" > "$tmp_config"
        scp "$tmp_config" "${cp_node}:/tmp/kubeadm-config.yaml"
        run_remote_cmd "$cp_node" "kubeadm init --config /tmp/kubeadm-config.yaml" "kubeadm init"
        rm "$tmp_config"
    fi

    # Get join command
    local join_cmd=""
    if ! $DRY_RUN; then
        join_cmd=$(run_remote_cmd "$cp_node" "kubeadm token create --print-join-command" "Generate join command" 2>/dev/null || true)
    fi

    # Setup kubectl locally (from first control-plane node)
    if ! $DRY_RUN && [[ -n "$cp_node" ]]; then
        mkdir -p "$HOME/.kube"
        scp "${cp_node}:/etc/kubernetes/admin.conf" "$HOME/.kube/config" 2>/dev/null || true
        log_info "kubectl configured — try: kubectl get nodes"
    fi

    # Join workers
    if $DRY_RUN; then
        for host in $WORKER_NODES; do
            log_info "[DRY-RUN] [$host] would join cluster"
        done
    elif [[ -n "$WORKER_NODES" ]]; then
        join_nodes "$join_cmd" "$WORKER_NODES"
    fi

    # Install CNI
    if ! $DRY_RUN && [[ -n "$cp_node" ]]; then
        run_remote "$cp_node" "${SCRIPTS_DIR}/04-cni.sh" "Install CNI (Flannel)"
    fi

    # Label worker nodes
    if ! $DRY_RUN; then
        for host in $WORKER_NODES; do
            local hostname=$(ssh "$host" hostname 2>/dev/null || echo "$host")
            kubectl label node "$hostname" node-role.kubernetes.io/worker="" --overwrite 2>/dev/null || true
        done
    fi

    log_info "=== Cluster initialized! ==="
    if ! $DRY_RUN; then
        echo "  Run: kubectl get nodes"
    fi
}

# ── Join ─────────────────────────────────────────────────────────
join_nodes() {
    local join_cmd="$1"; local nodes="$2"
    if [[ -z "$join_cmd" ]]; then
        log_error "No join command available — cannot add worker nodes"
        return 1
    fi
    for host in $nodes; do
        run_remote_cmd "$host" "$join_cmd" "Join cluster as worker"
    done
}

cmd_add_node() {
    load_inventory
    if [[ -z "${WORKER_NODES:-}" ]]; then
        log_error "No worker nodes defined in inventory"
        exit 1
    fi

    # Get join command from existing control plane
    local cp_node
    for host in $CONTROL_PLANE_NODES; do
        cp_node="$host"; break
    done
    if [[ -z "$cp_node" ]]; then
        log_error "No control-plane node defined — cannot get join token"
        exit 1
    fi

    local join_cmd
    join_cmd=$(run_remote_cmd "$cp_node" "kubeadm token create --print-join-command" "Generate join token" 2>/dev/null) || {
        log_error "Failed to get join command from $cp_node"
        exit 1
    }

    for host in $WORKER_NODES; do
        run_remote "$host" "${SCRIPTS_DIR}/01-prepare.sh" "System preparation"
        run_remote "$host" "${SCRIPTS_DIR}/02-container.sh" "Install containerd"
        run_remote "$host" "${SCRIPTS_DIR}/03-kubeadm.sh" "Install kubeadm/kubelet"
        run_remote_cmd "$host" "$join_cmd" "Join cluster"
    done
    log_info "Worker nodes added to cluster"
}

# ── Reset ────────────────────────────────────────────────────────
cmd_reset() {
    load_inventory
    log_warn "This will DESTROY the Kubernetes cluster on all nodes!"
    if ! $DRY_RUN; then
        read -rp "Continue? (yes/no): " confirm
        [[ "$confirm" != "yes" ]] && { log_info "Aborted."; exit 0; }
    fi

    for host in $CONTROL_PLANE_NODES $WORKER_NODES; do
        run_remote_cmd "$host" "kubeadm reset -f" "Reset kubeadm"
        run_remote_cmd "$host" "rm -rf /etc/cni /etc/kubernetes /var/lib/kubelet /var/lib/etcd" "Cleanup files"
    done
    rm -f "$HOME/.kube/config"
    log_info "Cluster reset complete"
}

# ── Health Check ─────────────────────────────────────────────────
cmd_status() {
    load_inventory
    log_info "Cluster status:"
    kubectl get nodes -o wide 2>/dev/null || log_warn "Cannot reach cluster (kubectl not configured?)"
    kubectl get pods -A 2>/dev/null | head -20 || true
}

# ── Main ─────────────────────────────────────────────────────────
case "$CMD" in
    init)     cmd_init ;;
    add-node) cmd_add_node ;;
    reset)    cmd_reset ;;
    status)   cmd_status ;;
    *)
        echo "Usage: $0 {init|add-node|reset|status} [--dry-run]"
        echo ""
        echo "Commands:"
        echo "  init      Bootstrap a new Kubernetes cluster"
        echo "  add-node  Add worker nodes to existing cluster"
        echo "  reset     Tear down the cluster (DESTRUCTIVE)"
        echo "  status    Show cluster health"
        echo ""
        echo "Options:"
        echo "  --dry-run  Simulate without executing any remote commands"
        exit 1
        ;;
esac
