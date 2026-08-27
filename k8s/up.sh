#!/usr/bin/env bash
# 一键把 gorder 栈部署到 kind (kubernetes)
#
# 前提:
#   - kind cluster 已创建: kind create cluster --config k8s/kind-cluster.yaml
#   - 4 个镜像已 load 到 kind: kind load docker-image gorder-{order,stock,payment,kitchen}:dev --name gorder
#
# 用法: bash k8s/up.sh [--stripe-key KEY] [--endpoint-secret SECRET]
# 也可以提前 export STRIPE_KEY 和 ENDPOINT_STRIPE_SECRET

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NS=gorder

STRIPE_KEY=${STRIPE_KEY:-}
ENDPOINT_STRIPE_SECRET=${ENDPOINT_STRIPE_SECRET:-}

# 从 .env 读,如果未设
if [[ -z "$STRIPE_KEY" && -f "$ROOT/.env" ]]; then
  # shellcheck disable=SC1091
  source "$ROOT/.env"
fi

GREEN='\033[0;32m'; CYAN='\033[0;36m'; YELLOW='\033[1;33m'; NC='\033[0m'
log()  { echo -e "${CYAN}[up]${NC} $*"; }
ok()   { echo -e "${GREEN}  ✓${NC} $*"; }
warn() { echo -e "${YELLOW}  !${NC} $*"; }

# ---------------- namespace ----------------
log "Applying namespace..."
kubectl apply -f "$ROOT/k8s/manifests/00-namespace.yaml" >/dev/null
ok "namespace ${NS}"

# ---------------- config ConfigMaps ----------------
log "Creating ConfigMaps from files..."

kubectl -n $NS create configmap gorder-config \
  --from-file=global.yaml="$ROOT/internal/common/config/global.yaml" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
ok "gorder-config (global.yaml)"

kubectl -n $NS create configmap mysql-init-sql \
  --from-file=init.sql="$ROOT/init.sql" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
ok "mysql-init-sql (init.sql)"

kubectl -n $NS create configmap prometheus-config \
  --from-file=prometheus.yml="$ROOT/k8s/prometheus.yml" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
ok "prometheus-config (prometheus.yml — k8s variant)"

kubectl -n $NS create configmap grafana-provisioning-datasources \
  --from-file="$ROOT/grafana/provisioning/datasources/datasources.yaml" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
ok "grafana-provisioning-datasources"

kubectl -n $NS create configmap grafana-provisioning-dashboards \
  --from-file="$ROOT/grafana/provisioning/dashboards/dashboards.yaml" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
ok "grafana-provisioning-dashboards"

kubectl -n $NS create configmap grafana-dashboards \
  --from-file="$ROOT/grafana/dashboards/" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
ok "grafana-dashboards (all *.json)"

# ---------------- stripe Secret (optional) ----------------
if [[ -n "$STRIPE_KEY" ]]; then
  kubectl -n $NS create secret generic stripe \
    --from-literal=STRIPE_KEY="$STRIPE_KEY" \
    --from-literal=ENDPOINT_STRIPE_SECRET="${ENDPOINT_STRIPE_SECRET:-}" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  ok "stripe Secret (STRIPE_KEY set)"
else
  warn "STRIPE_KEY 未设置,stripe Secret 不创建(stock/payment 会读到空 key)"
fi

# ---------------- 按顺序 apply manifests ----------------
log "Applying infra..."
kubectl apply -f "$ROOT/k8s/manifests/" >/dev/null
ok "all manifests applied"

log "Waiting for infra Ready (up to 3 min)..."
for svc in consul rabbit-mq order-mongo mysql redis jaeger prometheus grafana; do
  echo -n "    $svc ... "
  if kubectl -n $NS wait --for=condition=available --timeout=180s "deploy/$svc" >/dev/null 2>&1; then
    echo -e "${GREEN}ready${NC}"
  else
    echo -e "${YELLOW}timeout (check: kubectl -n $NS describe deploy/$svc)${NC}"
  fi
done

log "Waiting for apps Ready..."
for svc in stock order payment kitchen; do
  echo -n "    $svc ... "
  if kubectl -n $NS wait --for=condition=available --timeout=180s "deploy/$svc" >/dev/null 2>&1; then
    echo -e "${GREEN}ready${NC}"
  else
    echo -e "${YELLOW}timeout (check: kubectl -n $NS describe deploy/$svc)${NC}"
  fi
done

echo
log "Pods:"
kubectl -n $NS get pods

echo
log "NodePorts (mapped to host via kind):"
cat <<EOF
  order:      http://localhost:9090
  stock:      http://localhost:9091
  payment:    http://localhost:9092
  kitchen:    http://localhost:9094
  Grafana:    http://localhost:3000  (admin/admin or anonymous viewer)
  Prometheus: http://localhost:9093
  Consul UI:  http://localhost:8500
  Jaeger UI:  http://localhost:16686
EOF
