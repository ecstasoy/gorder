 # syntax=docker/dockerfile:1
 ARG SERVICE

  # -------- builder --------
  FROM golang:1.26-alpine AS builder
  ARG SERVICE
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
      CGO_ENABLED=0 GOOS=linux go build \
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