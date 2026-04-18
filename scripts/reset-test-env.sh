#!/usr/bin/env bash
# scripts/reset-test-env.sh — 压测前后清理脚本
#
# 把 Redis / MySQL / RabbitMQ / MongoDB 全部重置到干净状态,
# 让下一次压测从 "预热 → warmup → k6 run" 开始能看到可预期的结果。
#
# 不会: 重启 Go 服务进程 (那个你要自己手动 kill + restart,
#       因为 Air hot reload 有时候不靠谱,尤其改 yaml 时)
# 不会: 删 RabbitMQ 队列本身 (只 purge 消息,避免参数冲突)
#
# 用法:
#   bash scripts/reset-test-env.sh          # 标准清理
#   bash scripts/reset-test-env.sh --hard   # 标准清理 + 删队列 (解决参数冲突时用)

set -e

MYSQL_CONTAINER=${MYSQL_CONTAINER:-gorder-mysql-1}
REDIS_CONTAINER=${REDIS_CONTAINER:-gorder-redis-1}
RABBITMQ_CONTAINER=${RABBITMQ_CONTAINER:-gorder-rabbit-mq-1}
MONGO_CONTAINER=${MONGO_CONTAINER:-gorder-order-mongo-1}

HARD_MODE=0
if [[ "${1:-}" == "--hard" ]]; then
  HARD_MODE=1
fi

# ANSI colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log() { echo -e "${CYAN}[reset]${NC} $*"; }
ok()  { echo -e "${GREEN}  ✓${NC} $*"; }
warn() { echo -e "${YELLOW}  !${NC} $*"; }

# ------------------------------------------------------------
# 1. Redis
# ------------------------------------------------------------
log "Redis: 清空所有 key (FLUSHDB)..."
if docker exec "$REDIS_CONTAINER" redis-cli FLUSHDB >/dev/null 2>&1; then
  ok "Redis flushed"
else
  warn "Redis FLUSHDB failed (container $REDIS_CONTAINER running?)"
fi

# ------------------------------------------------------------
# 2. MySQL — 重置秒杀库存 + 清 reserved_for_flash
# ------------------------------------------------------------
log "MySQL: 重置 o_stock 到初始状态..."
if docker exec -i "$MYSQL_CONTAINER" mysql -uroot -proot gorder <<'EOF' >/dev/null 2>&1
UPDATE o_stock
SET quantity = CASE product_id
  WHEN 'prod_U9k6VcIEwQb83T' THEN 1000
  WHEN 'prod_U9k6gLFpAGaHQl' THEN 500
  ELSE quantity
END,
reserved_for_flash = 0,
flash_expires_at = NULL;
EOF
then
  ok "o_stock reset to seed quantities (1000 / 500), reserved_for_flash = 0"
else
  warn "MySQL reset failed"
fi

# ------------------------------------------------------------
# 3. RabbitMQ — purge 所有测试相关队列
# ------------------------------------------------------------
log "RabbitMQ: purge 测试相关队列..."
QUEUES=(
  order.created
  order.refund
  order.paid
  order.payment.delay
  order.payment.timeout
  flash.order.created
  flash.expire.delay
  flash.expired
  dlq
)

for q in "${QUEUES[@]}"; do
  if docker exec "$RABBITMQ_CONTAINER" rabbitmqctl purge_queue "$q" >/dev/null 2>&1; then
    ok "purged $q"
  else
    warn "$q (doesn't exist or not purgeable — probably OK)"
  fi
done

# ------------------------------------------------------------
# 4. MongoDB — 删掉所有测试订单
# ------------------------------------------------------------
log "MongoDB: 删除所有 load-test-* / heavy-* / ramp-* 订单..."
if docker exec "$MONGO_CONTAINER" mongosh \
  -u root -p password --authenticationDatabase admin --quiet <<'EOF' >/dev/null 2>&1
use order
db.orders.deleteMany({ customer_id: /^(load-test|heavy|ramp|sustained)-/ })
EOF
then
  ok "test orders deleted"
else
  warn "Mongo delete failed (container running? auth OK?)"
fi

# ------------------------------------------------------------
# 5. HARD MODE: 删掉可能参数冲突的 delay 队列
# ------------------------------------------------------------
if [[ $HARD_MODE -eq 1 ]]; then
  log "HARD mode: 删除 delay 队列 (避免 x-message-ttl 参数冲突)..."
  for q in order.payment.delay order.payment.timeout flash.expire.delay flash.expired; do
    if docker exec "$RABBITMQ_CONTAINER" rabbitmqctl delete_queue "$q" >/dev/null 2>&1; then
      ok "deleted $q"
    else
      warn "$q (may not exist)"
    fi
  done
  warn "记得: 删除队列后必须重启 Order + Stock + Payment 服务,让它们重新声明队列"
fi

# ------------------------------------------------------------
# 结果确认
# ------------------------------------------------------------
echo ""
log "最终状态:"

echo ""
echo -e "${YELLOW}  [MySQL o_stock]${NC}"
docker exec "$MYSQL_CONTAINER" mysql -uroot -proot gorder -e \
  "SELECT product_id, quantity, reserved_for_flash, flash_expires_at FROM o_stock;" 2>/dev/null | sed 's/^/    /'

echo ""
echo -e "${YELLOW}  [RabbitMQ 队列 (只显示非零)]${NC}"
docker exec "$RABBITMQ_CONTAINER" rabbitmqctl list_queues name messages consumers 2>/dev/null \
  | awk '$2 != "0" || $1 == "name" { print "    " $0 }'

echo ""
echo -e "${YELLOW}  [Redis key 数量]${NC}"
DBSIZE=$(docker exec "$REDIS_CONTAINER" redis-cli DBSIZE 2>/dev/null | awk '{print $NF}')
echo "    $DBSIZE keys"

echo ""
echo -e "${GREEN}环境已重置,准备下一轮压测!${NC}"
echo ""
echo "下一步:"
echo "  1. (可选) 如果改了 consumer.go / application.go 之类的源码,kill + 重启服务:"
echo "       pgrep -f 'order/main|stock/main|payment/main|kitchen/main' | xargs kill -9 2>/dev/null"
echo "       然后在各自终端重新 go run main.go"
echo ""
echo "  2. 预热秒杀库存 (100 件, TTL 30 分钟):"
echo "       curl -X POST http://localhost:9090/flash-sale/warmup \\"
echo "         -H 'Content-Type: application/json' \\"
echo "         -d '{\"items\":[{\"id\":\"prod_U9k6VcIEwQb83T\",\"quantity\":100}],\"ttl_seconds\":1800}'"
echo ""
echo "  3. 跑压测:"
echo "       k6 run flash-sale-burst.js       # 标准 1万 请求"
echo "       k6 run flash-sale-heavy.js       # 5万 请求 + 2000 VU"
echo "       k6 run flash-sale-ramp.js        # ramping 找崩溃点"
