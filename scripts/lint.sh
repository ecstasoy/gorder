#!/usr/bin/env bash

set -euo pipefail

source ./scripts/lib.sh

# golangci-lint 必须用**不低于各模块 go.mod 声明版本**的 Go 构建，否则 typecheck
# 阶段会对整个依赖树报 "package requires newer Go version"，真正的 lint 规则一条
# 都跑不到 —— 这正是 v1.62.2 (go1.23 构建) 在 go1.26 模块上发生的事。
# 预编译二进制的构建版本不由我们控制，所以统一走 go install：用本机 / CI 的
# toolchain 现编，版本天然对齐。
readonly LINT_VERSION="v2.13.1"
readonly LINT_PKG="github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${LINT_VERSION}"

# NEW_FROM 非空时只报告相对该 revision 新引入的问题（CI 的 PR 门用这个模式）。
# 留空则全量报告。用法: NEW_FROM=origin/main ./scripts/lint.sh
NEW_FROM=${NEW_FROM:-}

function install_if_not_exist() {
  local tool_name=$1
  local install_url=$2
  if command -v "$tool_name" &>/dev/null; then
    log_callout "$tool_name is already installed."
  else
    log_cmd "$tool_name is not installed. Installing..."
    run go install "$install_url"
  fi
}

install_if_not_exist go-cleanarch github.com/roblaszczak/go-cleanarch@latest

NEED_INSTALL=true
if command -v golangci-lint >/dev/null 2>&1; then
  CURRENT_VERSION="v$(golangci-lint version --short 2>/dev/null || true)"
  log_callout "golangci-lint ${CURRENT_VERSION} already installed."
  [ "$CURRENT_VERSION" == "$LINT_VERSION" ] && NEED_INSTALL=false
fi
if [ "$NEED_INSTALL" == true ]; then
  log_cmd "installing golangci-lint $LINT_VERSION (built with $(go env GOVERSION))"
  run go install "$LINT_PKG"
fi

# ---------------------------------------------------------------------------
# 分层依赖检查 (adapters → domain，绝不反向)
# ---------------------------------------------------------------------------
# TODO(debt): stock 服务目前有 3 处 app → infra 的违规 —— warmup_activity.go /
#             warmup_flash_stock.go / get_items.go 都 import 了 infra/integration。
#             修掉之前先不阻断，但输出保留，别当没看见。
# CI 把它拆成独立的 job 展示，所以那边设 SKIP_CLEANARCH=1 避免重复跑。
if [ "${SKIP_CLEANARCH:-0}" != "1" ]; then
  if ! go-cleanarch; then
    log_warning "go-cleanarch 发现分层违规 (见上)，属已知债务，暂不阻断。"
  fi
fi

# ---------------------------------------------------------------------------
# 逐模块 golangci-lint
# ---------------------------------------------------------------------------
# 格式检查也在这里 —— .golangci.yaml 的 formatters 段启用了 goimports，并且
# 通过 exclusions 排除生成代码。所以不需要（也不应该）再单独跑裸 goimports：
# 老版本那句 `goimports -w -l .` 会就地改写工作区，还会把 genproto/*.pb.go
# 一起格式化掉。
RUN_ARGS=(run --config "$ROOT_DIR/.golangci.yaml")
if [ -n "$NEW_FROM" ]; then
  log_info "只报告相对 $NEW_FROM 新引入的问题"
  RUN_ARGS+=(--new-from-merge-base "$NEW_FROM")
fi

log_info "lint modules: $(modules)"

FAILED=()
while read -r module; do
  log_info "==> internal/$module"
  if ! (cd "./internal/$module" && golangci-lint "${RUN_ARGS[@]}"); then
    FAILED+=("$module")
  fi
done < <(modules)

if [ ${#FAILED[@]} -gt 0 ]; then
  log_error "lint failed in: ${FAILED[*]}"
  exit 1
fi

log_success "lint passed."
