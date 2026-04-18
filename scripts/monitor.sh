#!/usr/bin/env bash
# scripts/monitor.sh — 压测监控面板
#
# 用 tmux 一键开 6 个窗格,分别盯着:
#   1. RabbitMQ 队列(messages + consumers)
#   2. MySQL 连接池 + 行锁 + 秒杀库存
#   3. Redis 吞吐 + flash:stock 值
#   4. Order 服务 goroutine + 内存
#   5. Payment 服务 goroutine + 内存
#   6. 空闲,给你跑 k6 用
#
# 前提: brew install tmux
# 用法: bash scripts/monitor.sh
#
# 退出: tmux kill-session -t gorder-mon

set -e

SESSION="gorder-mon"

# 如果 session 已存在就直接 attach
if tmux has-session -t $SESSION 2>/dev/null; then
  tmux attach -t $SESSION
  exit 0
fi

# 创建新 session
tmux new-session -d -s $SESSION -n monitor

# 分 6 个窗格 (2x3 布局)
tmux split-window -h -t $SESSION:0
tmux split-window -v -t $SESSION:0.0
tmux split-window -v -t $SESSION:0.2
tmux split-window -v -t $SESSION:0.1
tmux split-window -v -t $SESSION:0.4

# 窗格 0 (左上): RabbitMQ 队列
tmux send-keys -t $SESSION:0.0 \
  "watch -n 1 'docker exec gorder-rabbit-mq-1 rabbitmqctl list_queues name messages consumers 2>/dev/null | column -t'" C-m

# 窗格 1 (左中): MySQL 状态 + 库存
tmux send-keys -t $SESSION:0.1 \
  'watch -n 1 '\''docker exec gorder-mysql-1 mysql -uroot -proot -e "SHOW STATUS WHERE Variable_name IN (\"Threads_connected\",\"Threads_running\",\"Innodb_row_lock_current_waits\",\"Queries\"); SELECT product_id, quantity, reserved_for_flash FROM gorder.o_stock;" 2>/dev/null'\''' C-m

# 窗格 2 (左下): Redis 吞吐
tmux send-keys -t $SESSION:0.2 \
  "docker exec gorder-redis-1 redis-cli --stat" C-m

# 窗格 3 (右上): Order 服务 /metrics 关键指标
tmux send-keys -t $SESSION:0.3 \
  "watch -n 1 'curl -s http://localhost:9090/metrics 2>/dev/null | grep -E \"^(go_goroutines|process_resident_memory_bytes|process_cpu_seconds_total|go_gc_duration_seconds_count)\" | head -15'" C-m

# 窗格 4 (右中): Payment 服务 /metrics
tmux send-keys -t $SESSION:0.4 \
  "watch -n 1 'curl -s http://localhost:9092/metrics 2>/dev/null | grep -E \"^(go_goroutines|process_resident_memory_bytes|go_gc_duration_seconds_count)\" | head -15'" C-m

# 窗格 5 (右下): 空终端,给你跑 k6
tmux send-keys -t $SESSION:0.5 \
  "echo '>>> 在这里跑 k6: k6 run flash-sale-heavy.js'" C-m

# Attach
tmux attach -t $SESSION
