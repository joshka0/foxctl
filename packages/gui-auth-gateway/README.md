# gui-auth-gateway

`gui-auth-gateway` is a same-origin public gateway for `gui-agent`.

It hosts Better Auth at `/api/auth/*`, serves the built `packages/gui-agent/dist`
bundle, and proxies authenticated `/api/*` and `/ws/*` traffic to the private
`foxctl web serve` backend.

## Local smoke run

Build the GUI first:

```bash
bun run --cwd packages/gui-agent build
```

Run the gateway with log-only magic links and the default private backend at
`http://127.0.0.1:8090`:

```bash
PORT=3005 \
BETTER_AUTH_SECRET="$(openssl rand -base64 32)" \
BETTER_AUTH_URL="http://127.0.0.1:3005" \
GUI_AUTH_MAGIC_LINK_LOG_ONLY=true \
bun run --cwd packages/gui-auth-gateway dist/server.js
```

## Required environment

- `BETTER_AUTH_SECRET`
- `BETTER_AUTH_URL`

## Common environment

- `GUI_AUTH_UPSTREAM_URL`
- `GUI_AUTH_DATABASE_URL` or `FOXCTL_POSTGRES_DSN`
- `GUI_AUTH_SQLITE_PATH`
- `GUI_AUTH_ALLOWED_EMAILS`
- `GUI_AUTH_MAGIC_LINK_LOG_ONLY`
- `GUI_AUTH_SMTP_HOST`
- `GUI_AUTH_SMTP_PORT`
- `GUI_AUTH_SMTP_SECURE`
- `GUI_AUTH_SMTP_USER`
- `GUI_AUTH_SMTP_PASS`
- `GUI_AUTH_SMTP_FROM`
- `GUI_AUTH_SMTP_REPLY_TO`

## Forwarded Identity Headers

`gui-auth-gateway` is the trusted boundary for Better Auth browser identity.
For authenticated `/api/*` and `/ws/*` proxy traffic it strips client-supplied
identity headers, then forwards only session-derived Better Auth headers:

- `X-BetterAuth-User-ID`
- `X-BetterAuth-Email`
- `X-BetterAuth-User-Name`

The gateway also strips browser-supplied `X-Tailscale-*` and `X-Foxctl-*`
identity headers before proxying. Those headers are reserved for trusted
gateway/runtime boundaries and must not be accepted directly from public
browser clients.
