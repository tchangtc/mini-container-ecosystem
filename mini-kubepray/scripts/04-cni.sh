#!/usr/bin/env bash
# 04-cni.sh — install CNI plugin (Flannel)
set -euo pipefail

echo "==> Installing Flannel CNI..."
kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml

echo "==> Waiting for CNI pods to be ready..."
kubectl -n kube-flannel wait --for=condition=ready pod -l app=flannel --timeout=120s

echo "==> CNI installed."
