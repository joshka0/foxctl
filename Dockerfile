# Production multi-stage build for agentctl
# Default binary is non-CGO (pure Go); CGO binary with libsqlite3 vector
# search support is available as /usr/local/bin/agentctl-cgo

# ── Builder ──────────────────────────────────────────────────────────────────
FROM golang:1.25-bookworm AS builder

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      gcc libc6-dev libsqlite3-dev make git && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build non-CGO binary (default) and agentctl-mail
RUN CGO_ENABLED=0 make build

# Build CGO binary (opt-in, includes -tags=libsqlite3 for vector support)
RUN make build-cgo

# ── Runtime ──────────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates libsqlite3-0 && \
    rm -rf /var/lib/apt/lists/*

# Copy binaries: non-CGO as default, CGO as opt-in alternative
COPY --from=builder /src/bin/agentctl /usr/local/bin/agentctl
COPY --from=builder /src/bin/agentctl-cgo /usr/local/bin/agentctl-cgo
COPY --from=builder /src/bin/agentctl-mail /usr/local/bin/agentctl-mail

# Create writable directories expected by the deployment
RUN mkdir -p /var/cache/agentctl /var/log/agentctl /tmp && \
    chown 1000:1000 /var/cache/agentctl /var/log/agentctl /tmp

USER 1000:1000
EXPOSE 8080

ENTRYPOINT ["agentctl"]
CMD ["web", "serve", "--port=8080"]
