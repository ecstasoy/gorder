# syntax=docker/dockerfile:1
ARG SERVICE

# -------- builder --------
# --platform=$BUILDPLATFORM 让 builder 阶段**始终跑在原生架构上**，再靠
# GOARCH 交叉编译出目标架构的二进制。否则 buildx 做多架构时会用 QEMU 模拟整个
# 编译过程，Go 编译在模拟下慢一个数量级。
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG SERVICE
ARG TARGETARCH
RUN apk add --no-cache git
WORKDIR /src

# Prime module cache (deps layer — cached unless go.mod/go.sum changes)
COPY internal/common/go.mod internal/common/go.sum ./internal/common/
COPY internal/${SERVICE}/go.mod internal/${SERVICE}/go.sum ./internal/${SERVICE}/

WORKDIR /src/internal/${SERVICE}
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Source
WORKDIR /src
COPY internal/common ./internal/common
COPY internal/${SERVICE} ./internal/${SERVICE}

# Static binary
WORKDIR /src/internal/${SERVICE}
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
      -trimpath -ldflags="-s -w" \
      -o /out/service .

# -------- runtime --------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S -g 10001 app \
 && adduser -S -u 10001 -G app app

WORKDIR /app
COPY --from=builder /out/service /app/service
COPY internal/common/config/global.yaml /app/config/global.yaml

USER app
ENV CONFIG_DIR=/app/config

ENTRYPOINT ["/app/service"]
