# Start Here: OpenAPI Skill & Plugins

This document expands the brief OpenAPI + plugin notes in `AGENTS.md` and
points to the canonical specs.

---

## OpenAPI Skill (`http/openapi`)

The `http/openapi` skill is the **tier-1 generic HTTP client** for agentctl. It
can call any OpenAPI 3.x operation using a spec loaded from:

- A file path
- CAS (`sha256:<hex>`)
- Named memory (`memory:<name>`)

Key behavior (see `docs/spec/core_profile_v1.md` §3 and
`docs/spec/openapi_skill.md`):

- Input fields:
  - `spec` – `path | sha256:<hex> | memory:<name>`
  - `operationId` – target operation ID from the OpenAPI doc
  - `params` – `path/query/header/body` map
  - `paging` – pagination strategy & limits
  - `retry` – backoff config for 429/5xx
  - `auth` – auth selection/overrides
  - `dry_run` – emit request plan only, no network call
- Output:
  - JSON envelope with response summary
  - Large bodies CAS-wrapped with `data.artifact` + `meta.cas_digest`

**Dry-run:**

- `dry_run=true` **must not** perform the network call.
- Returns `data.summary.request_plan` with method, URL (incl. query), and
  redacted headers.

**4xx vs 5xx behavior:**

- 4xx validation errors: keep error details **inline** (bounded by
  `inline_output_kb`), do **not** artifactize.
- 5xx after retries: `error.code="ERUNTIME"` with helpful `data.hint` and any
  `request-id` header when available.

---

## Built-in Auth Strategies

`http/openapi` has built-in auth strategies that map standard security schemes
onto headers or query parameters (see `internal/openapi/auth`).

Common schemes:

- **HTTP Bearer** – `Authorization: Bearer <token>`
- **API Key** – header or query param
- **HTTP Basic** – `Authorization: Basic base64(user:pass)`
- **OAuth2 Client Credentials** – token exchange followed by Bearer auth

Secrets generally come from:

- Files under `/run/secrets/<name>`
- Env vars (`AGENTCTL_*`, or scheme-specific names)

**Rules:**

- Never log secret values; redact as `"***"`.
- Prefer secrets mounted as files or env vars over inlining in config.

---

## Pagination Strategies

The skill auto-detects or accepts explicit pagination strategies (see
`internal/openapi/pagination`):

- **Link headers** – `Link: <url>; rel="next"` (GitHub-style)
- **Cursor fields** – body fields like `next` or `next_page_token`
- **Offset/limit** – classic pagination (`page`, `per_page`, etc.)

Callers can override strategy and parameters, for example:

```bash
agentctl run http/openapi \
  --spec=memory:github \
  --operationId=listReposForUser \
  --params='{"path":{"username":"octocat"},"query":{"per_page":100}}' \
  --paging.strategy=link \
  --paging.max_pages=10
```

---

## Plugin Architecture (Auth & Pagination)

For APIs that need custom auth or paging behavior, `http/openapi` supports
**out-of-process plugins**. Plugins run as separate binaries (exec or WASI) and
communicate via JSON envelopes on stdin/stdout.

Canonical spec: `docs/spec/plugin_protocol.md`.

- **Auth plugin command:** `plugin/auth`
  - Input `data` includes the HTTP request (method, URL, headers, body) and
    spec context.
  - Output returns adjusted headers and optional signed body.
- **Pagination plugin command:** `plugin/pagination`
  - Input includes the last HTTP response and pagination limits.
  - Output returns whether to continue and how to build the next request.

Plugins are discovered via search paths (e.g. `AGENTCTL_OPENAPI_PLUGIN_PATH`) or
installed under `~/.agentctl/plugins`.

---

## Example Plugin Skeletons (Go)

The following examples show the **shape** of typical plugins. They mirror the
(now-simplified) code examples that used to live directly in `AGENTS.md`.

> These are illustrative; prefer the real types and helpers in
> `docs/spec/plugin_protocol.md` and `internal/openapi/plugin` when
> implementing production plugins.

### Auth Plugin (pseudo-code)

```go
package main

func main() {
    env, err := ReadEnvelope(os.Stdin) // plugin/auth request
    if err != nil {
        writeErr("EENVELOPE", err)
        return
    }

    var req AuthRequest
    _ = json.Unmarshal(mustJSON(env.Data), &req)

    headers := req.Headers
    token := os.Getenv("PLUGIN_BEARER_TOKEN")
    if token != "" {
        headers["Authorization"] = "Bearer " + token
    }

    out := Envelope{
        Version: 1,
        Status:  "ok",
        Command: "plugin/auth",
        Data:    map[string]any{"headers": headers},
        Meta:    Meta{TS: time.Now().UTC().Format(time.RFC3339), Source: "run"},
        Error:   Error{},
    }
    _ = json.NewEncoder(os.Stdout).Encode(out)
}
```

### Pagination Plugin (pseudo-code)

```go
package main

func main() {
    env, err := ReadEnvelope(os.Stdin)
    if err != nil {
        writeErr("EENVELOPE", err)
        return
    }

    var in PageInput
    _ = json.Unmarshal(mustJSON(env.Data), &in)

    var body map[string]any
    _ = json.Unmarshal(in.LastResponse.Body, &body)
    if nxt, ok := body["next"].(string); ok && nxt != "" {
        out := okEnv("plugin/pagination", map[string]any{
            "continue":   true,
            "next_cursor": nxt,
        })
        _ = json.NewEncoder(os.Stdout).Encode(out)
        return
    }

    _ = json.NewEncoder(os.Stdout).Encode(okEnv("plugin/pagination", map[string]any{
        "continue": false,
    }))
}
```

---

## Related Documents

- `AGENTS.md` – high-level rules and guardrails (envelopes, CAS, jobs,
  networking, filesystem safety).
- `docs/spec/core_profile_v1.md` – Core Profile spec; see §3 (OpenAPI
  integration) and §4 (artifacts).
- `docs/spec/openapi_skill.md` – detailed `http/openapi` skill behavior.
- `docs/spec/plugin_protocol.md` – plugin handshake and envelope protocol.
- `openapi_implementation_summary.md`, `openapi_plugin_guide.md` – narrative
  docs about the OpenAPI implementation strategy.
