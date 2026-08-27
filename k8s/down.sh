#!/usr/bin/env bash
# 清理 gorder namespace 及其所有资源。
# 保留 kind cluster 本身,如果要整个销毁 cluster 请用:
#   kind delete cluster --name gorder

set -euo pipefail

NS=gorder

echo "Deleting namespace $NS..."
kubectl delete namespace $NS --wait=true --timeout=60s 2>&1 | tail -3

echo "Done."
