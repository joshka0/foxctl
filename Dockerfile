# Production multi-stage build for foxctl
# Default binary is non-CGO. Turso is the canonical SQLite-family storage
# backend, so the runtime image ships one canonical binary.

# ── Builder ──────────────────────────────────────────────────────────────────
FROM golang:1.26.1-bookworm AS builder

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      make git && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build non-CGO binary (default) and foxctl-mail
RUN CGO_ENABLED=0 make build

# ── Runtime ──────────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/bin/foxctl /usr/local/bin/foxctl
COPY --from=builder /src/bin/foxctl-mail /usr/local/bin/foxctl-mail

# Create writable directories expected by the deployment
RUN mkdir -p /var/cache/foxctl /var/log/foxctl /tmp && \
    chown 1000:1000 /var/cache/foxctl /var/log/foxctl /tmp

USER 1000:1000
EXPOSE 8080

ENTRYPOINT ["foxctl"]
CMD ["web", "serve", "--port=8080"]
