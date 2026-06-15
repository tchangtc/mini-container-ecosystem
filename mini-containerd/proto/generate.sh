#!/usr/bin/env bash
# Generate Go code from proto definitions.
# Requires: protoc, protoc-gen-go, protoc-gen-go-grpc
#
# Install tools:
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

set -euo pipefail
cd "$(dirname "$0")"

PROTOS=(content image snapshot task)

for proto in "${PROTOS[@]}"; do
    echo "==> Generating ${proto}.proto..."
    protoc \
        --go_out=../api \
        --go_opt=paths=source_relative \
        --go-grpc_out=../api \
        --go-grpc_opt=paths=source_relative \
        "${proto}.proto"

    # Move generated files into sub-packages
    mkdir -p "../api/${proto}"
    mv "../api/${proto}.pb.go" "../api/${proto}/" 2>/dev/null || true
    mv "../api/${proto}_grpc.pb.go" "../api/${proto}/" 2>/dev/null || true
done

echo "==> Done. Generated files in api/{content,image,snapshot,task}/"
