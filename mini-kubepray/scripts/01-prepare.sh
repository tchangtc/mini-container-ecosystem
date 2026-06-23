#!/usr/bin/env bash
# 01-prepare.sh — node preparation (swap, kernel params, dependencies)
set -euo pipefail

echo "==> Disabling swap..."
swapoff -a
sed -i '/swap/d' /etc/fstab

echo "==> Loading kernel modules..."
cat <<EOF > /etc/modules-load.d/k8s.conf
overlay
br_netfilter
EOF
modprobe overlay
modprobe br_netfilter

echo "==> Setting sysctl params..."
cat <<EOF > /etc/sysctl.d/k8s.conf
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOF
sysctl --system > /dev/null

echo "==> Installing dependencies..."
if command -v apt-get &> /dev/null; then
    apt-get update -qq
    apt-get install -y -qq apt-transport-https ca-certificates curl
elif command -v yum &> /dev/null; then
    yum install -y yum-utils device-mapper-persistent-data lvm2
fi

echo "==> Node preparation done."
