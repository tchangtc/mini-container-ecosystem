#!/usr/bin/env bash
# Tests for mini-kubepray deploy script.
# Validates argument parsing, inventory loading, and dry-run execution.
set -euo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KUBEPRAY_DIR="${TEST_DIR}/.."
PASS=0; FAIL=0

green() { echo -e "\033[0;32m$*\033[0m"; }
red()   { echo -e "\033[0;31m$*\033[0m"; }

assert_ok() {
    local desc="$1"; shift
    if "$@" >/dev/null 2>&1; then
        green "  ✓ $desc"; PASS=$((PASS+1))
    else
        red "  ✗ $desc (exit code: $?)"; FAIL=$((FAIL+1))
    fi
}

assert_fail() {
    local desc="$1"; shift
    if "$@" >/dev/null 2>&1; then
        red "  ✗ $desc (expected failure, got success)"; FAIL=$((FAIL+1))
    else
        green "  ✓ $desc"; PASS=$((PASS+1))
    fi
}

echo "=== mini-kubepray tests ==="

# ── Argument Parsing ─────────────────────────────────────────────
echo "--- Argument parsing ---"
assert_fail "deploy.sh without args shows help (exit 1 = usage displayed)" \
    bash "${KUBEPRAY_DIR}/deploy.sh" 2>&1
assert_fail "deploy.sh with invalid command shows error" \
    bash "${KUBEPRAY_DIR}/deploy.sh" nonexistent 2>&1
assert_ok "deploy.sh init --dry-run works" \
    bash "${KUBEPRAY_DIR}/deploy.sh" init --dry-run 2>&1
assert_ok "deploy.sh add-node --dry-run works" \
    bash "${KUBEPRAY_DIR}/deploy.sh" add-node --dry-run 2>&1
assert_ok "deploy.sh reset --dry-run works" \
    bash "${KUBEPRAY_DIR}/deploy.sh" reset --dry-run <<< "yes" 2>&1
assert_ok "deploy.sh status works" \
    bash "${KUBEPRAY_DIR}/deploy.sh" status 2>&1

# ── Inventory Validation ─────────────────────────────────────────
echo "--- Inventory validation ---"
assert_fail "Missing inventory causes error" \
    bash -c "MINIKUBE_INVENTORY=/nonexistent/hosts.ini bash ${KUBEPRAY_DIR}/deploy.sh init --dry-run" 2>&1

# Copy and fix inventory for testing
cp "${KUBEPRAY_DIR}/inventory/hosts.ini.example" "/tmp/test-hosts.ini"
sed -i 's/CONTROL_PLANE_NODES="192.168.1.10"/CONTROL_PLANE_NODES="localhost"/' /tmp/test-hosts.ini
sed -i 's/WORKER_NODES=.*/WORKER_NODES=""/' /tmp/test-hosts.ini
assert_ok "Valid inventory loads" \
    bash -c "MINIKUBE_INVENTORY=/tmp/test-hosts.ini bash ${KUBEPRAY_DIR}/deploy.sh init --dry-run" 2>&1

# ── Script Syntax ────────────────────────────────────────────────
echo "--- Script syntax checks ---"
for script in "${KUBEPRAY_DIR}/scripts/"*.sh "${KUBEPRAY_DIR}/deploy.sh"; do
    assert_ok "bash -n $(basename "$script")" bash -n "$script"
done

# ── Kubeadm Config ───────────────────────────────────────────────
echo "--- Config validation ---"
assert_ok "kubeadm config exists" test -f "${KUBEPRAY_DIR}/config/kubeadm-init.yaml"
# Check config exists and has expected content
assert_ok "kubeadm config exists and has apiVersion" \
    grep -q "kubeadm.k8s.io" "${KUBEPRAY_DIR}/config/kubeadm-init.yaml"

# ── Dry-run Init Flow ────────────────────────────────────────────
echo "--- Dry-run init flow ---"
output=$(bash "${KUBEPRAY_DIR}/deploy.sh" init --dry-run 2>&1) || true
assert_ok "Dry-run mentions pre-flight" echo "$output" | grep -q "pre-flight"
assert_ok "Dry-run mentions control-plane" echo "$output" | grep -q "control-plane"
assert_ok "Dry-run mentions CNI" echo "$output" | grep -q "CNI"

# ── Summary ──────────────────────────────────────────────────────
echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[[ $FAIL -eq 0 ]] && green "All tests passed!" || red "Some tests failed"
exit $FAIL
