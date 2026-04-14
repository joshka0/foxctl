# Production multi-stage build for foxctl
# Default binary is non-CGO (pure Go); CGO binary with libsqlite3 vector
# search support is available as /usr/local/bin/foxctl-cgo

# ── Builder ──────────────────────────────────────────────────────────────────
FROM golang:1.26.1-bookworm AS builder

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      gcc libc6-dev libsqlite3-dev make git && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build non-CGO binary (default) and foxctl-mail
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
COPY --from=builder /src/bin/foxctl /usr/local/bin/foxctl
COPY --from=builder /src/bin/foxctl-cgo /usr/local/bin/foxctl-cgo
COPY --from=builder /src/bin/foxctl-mail /usr/local/bin/foxctl-mail

# Create writable directories expected by the deployment
RUN mkdir -p /var/cache/foxctl /var/log/foxctl /tmp && \
    chown 1000:1000 /var/cache/foxctl /var/log/foxctl /tmp

USER 1000:1000
EXPOSE 8080

ENTRYPOINT ["foxctl"]
CMD ["web", "serve", "--port=8080"]
