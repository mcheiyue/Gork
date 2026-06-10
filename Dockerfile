# Builder
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY app ./app
COPY cmd ./cmd

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/grok2api \
    ./cmd/grok2api

# Runtime
FROM alpine:3.22

ENV TZ=Asia/Shanghai \
    SERVER_HOST=0.0.0.0 \
    SERVER_PORT=8000 \
    SERVER_WORKERS=1 \
    DATA_DIR=/app/data \
    LOG_DIR=/app/logs

RUN apk add --no-cache \
    tzdata \
    ca-certificates \
    wget \
    && update-ca-certificates

WORKDIR /app

COPY --from=builder /out/grok2api /app/grok2api
COPY pyproject.toml config.defaults.toml ./
COPY app/statics ./app/statics
COPY scripts/entrypoint.sh scripts/init_storage.sh ./scripts/

RUN mkdir -p /app/data /app/logs \
    && chmod +x /app/grok2api /app/scripts/entrypoint.sh /app/scripts/init_storage.sh

EXPOSE 8000

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["sh", "-c", "wget -qO /dev/null http://127.0.0.1:${PORT:-${SERVER_PORT:-8000}}/health || exit 1"]

ENTRYPOINT ["/app/scripts/entrypoint.sh"]
CMD ["sh", "-c", "HOST=${HOST:-${SERVER_HOST:-0.0.0.0}} PORT=${PORT:-${SERVER_PORT:-8000}} exec /app/grok2api"]
