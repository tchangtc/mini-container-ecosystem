#!/usr/bin/env bash
# 02-container.sh — install container runtime (containerd)
set -euo pipefail

CONTAINERD_VERSION="${1:-1.7.0}"

echo "==> Installing containerd ${CONTAINERD_VERSION}..."

if command -v apt-get &> /dev/null; then
    # Debian/Ubuntu
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | apt-key add -
    add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
    apt-get update -qq
    apt-get install -y -qq containerd.io
elif command -v yum &> /dev/null; then
    # CentOS/RHEL
    yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
    yum install -y containerd.io
fi

echo "==> Configuring containerd..."
mkdir -p /etc/containerd
containerd config default > /etc/containerd/config.toml
# Enable systemd cgroup driver
sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml

echo "==> Starting containerd..."
systemctl enable containerd
systemctl restart containerd

echo "==> Container runtime installed."
