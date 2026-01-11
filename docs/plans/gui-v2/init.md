Perfect — with `internal/domain/skill/*` + `skillout/skillmain` + `sessions` in hand, we can make the “overseer console” **real**: tool schemas derived from `skill.yaml`, tool names mapped safely for OpenAI-compatible providers, and tool execution routed through the daemon (or fallback).

Below is **concrete scaffolding + a PR-by-PR plan** you can hand to Codex/Claude.

---

# Critical design decisions (based on your code)

## 1) Tool names cannot contain `/`

OpenAI-compatible “function tool” names generally **can’t** include `/`. Your skills are named like `code/symbols`.

✅ Solution: **normalize tool names** (use the same normalization you already use in `skill.normalizeSkillName`):

* Tool name: `code_symbols`
* Maps to skill name: `code/symbols`

We keep a registry:

* `tool_name -> manifest.Metadata.Name`

## 2) JSON Schema comes from `skill.Manifest.Signature.Parameters`

You already have a strongly typed parameter tree:

* `type`, `required`, `enum`, `default`, `items`, `properties`

✅ We will generate a standard JSON schema:

```json
{
  "type": "object",
  "properties": { ... },
  "required": [ ... ],
  "additionalProperties": false
}
```

## 3) Tool results + CAS exposure in console mode

If CAS expose policy is `"off"`, the LLM may not see digests/hints for big outputs.

✅ For the console process, set:

* `AGENTCTL_CAS_EXPOSE=hint` (env override already supported in `config.finalizeConfig`)
* Hook wrappers can set `AGENTCTL_CAS_EXPOSE=off` to keep hook output quiet.

## 4) Skill execution should be daemon-fast, but safe

Best path:

* `daemon.Client.Run(skillName, argsJSON, workspace, ephemeral=true)`

Fallback path (daemon not running):

* resolve skill via `daemon.SkillResolver` or `skill.Resolver + loadSkillDir`
* run via `execution.NewRunnerExecutor().Execute(...)` (ephemeral style)
* (optional) wrap with runservice if you want job persistence for console turns later

---

# New scaffolding (Go)

## New packages

```
internal/skills/
  registry.go        // discover skills, dedupe, build tooldefs, mapping tool<->skill
  schema.go          // Parameter -> JSONSchema conversion
  names.go           // NormalizeToolName + Reverse mapping helpers

internal/web/
  (as previously proposed)
  handlers/
    skills.go         // GET /api/skills, GET /api/tools
    sessions.go       // (optional for GUI SessionsPage)
    console.go        // console session CRUD + ask/cancel
  services/
    console_service.go
    tools_service.go  // uses internal/skills registry
  sse/
    hub.go
    handler.go

internal/consoleapp/
  runner.go          // “ask loop” that runs LLM engine + tool runner + streams events
  stream.go          // adapters to publish console.Payload events
```

---

# Key code skeletons (Codex-ready)

## A) Skill tool-name normalization

```go
// internal/skills/names.go
package skills

import "strings"

// ToolName converts skill name (code/symbols) to tool-safe name (code_symbols).
func ToolName(skillName string) string {
	n := strings.ReplaceAll(skillName, "/", "_")
	n = strings.ReplaceAll(n, "-", "_")
	return n
}

// SkillName attempts to reverse tool name back to skill name.
// We can only do this reliably via the registry mapping.
```

> Don’t try to reverse purely by string rules — use the registry map.

---

## B) Convert `skill.Parameter` → JSON Schema

```go
// internal/skills/schema.go
package skills

import (
	"encoding/json"

	"github.com/jkatigb/agentctl/internal/domain/skill"
)

func ParametersToJSONSchema(params []skill.Parameter) (json.RawMessage, error) {
	props := map[string]any{}
	required := []string{}

	for _, p := range params {
		props[p.Name] = parameterToSchema(p)
		if p.Required {
			required = append(required, p.Name)
		}
	}

	root := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		root["required"] = required
	}

	b, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func parameterToSchema(p skill.Parameter) map[string]any {
	s := map[string]any{}

	// Type mapping
	switch p.Type {
	case "string":
		s["type"] = "string"
	case "boolean", "bool":
		s["type"] = "boolean"
	case "integer", "int":
		s["type"] = "integer"
	case "number", "float":
		s["type"] = "number"
	case "array":
		s["type"] = "array"
		if p.Items != nil {
			s["items"] = parameterToSchema(*p.Items)
		} else {
			s["items"] = map[string]any{"type": "string"}
		}
	case "object":
		s["type"] = "object"
		props := map[string]any{}
		req := []string{}
		for name, child := range p.Properties {
			props[name] = parameterToSchema(child)
			if child.Required {
				req = append(req, name)
			}
		}
		s["properties"] = props
		s["additionalProperties"] = false
		if len(req) > 0 {
			s["required"] = req
		}
	default:
		// Fallback
		s["type"] = "string"
	}

	if p.Description != "" {
		s["description"] = p.Description
	}
	if len(p.Enum) > 0 {
		s["enum"] = p.Enum
	}
	if p.Default != nil {
		s["default"] = p.Default
	}

	return s
}
```

---

## C) Skill discovery + tool registry

Use `skill.Resolver.SearchPaths()` but **discover recursively** with `skill.Discover(path)` (because skills can be nested or underscored).

```go
// internal/skills/registry.go
package skills

import (
	"fmt"
	"sort"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/engine"
)

type ToolSpec struct {
	ToolName  string
	SkillName string
	Manifest  skill.Manifest
}

type Registry struct {
	toolsByName map[string]ToolSpec // tool_name -> spec
}

func BuildRegistry(r *skill.Resolver) (*Registry, error) {
	searchPaths := r.SearchPaths()
	all := []skill.Manifest{}
	seenBySkillName := map[string]bool{}

	for _, root := range searchPaths {
		manifests, err := skill.Discover(root)
		if err != nil {
			continue
		}
		for _, m := range manifests {
			if m.Metadata.Name == "" {
				continue
			}
			if seenBySkillName[m.Metadata.Name] {
				continue
			}
			seenBySkillName[m.Metadata.Name] = true
			all = append(all, m)
		}
	}

	tools := map[string]ToolSpec{}
	for _, m := range all {
		tname := ToolName(m.Metadata.Name)
		// collision safety
		if _, exists := tools[tname]; exists {
			return nil, fmt.Errorf("tool name collision: %s", tname)
		}
		tools[tname] = ToolSpec{
			ToolName:  tname,
			SkillName: m.Metadata.Name,
			Manifest:  m,
		}
	}

	return &Registry{toolsByName: tools}, nil
}

func (reg *Registry) ListToolDefs() ([]engine.ToolDef, error) {
	names := make([]string, 0, len(reg.toolsByName))
	for n := range reg.toolsByName {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]engine.ToolDef, 0, len(names))
	for _, toolName := range names {
		spec := reg.toolsByName[toolName]

		schema, err := ParametersToJSONSchema(spec.Manifest.Signature.Parameters)
		if err != nil {
			return nil, err
		}

		desc := buildToolDescription(spec.Manifest)

		out = append(out, engine.ToolDef{
			Name:        toolName,
			Description: desc,
			Parameters:  schema,
		})
	}

	return out, nil
}

func (reg *Registry) ResolveTool(toolName string) (ToolSpec, bool) {
	spec, ok := reg.toolsByName[toolName]
	return spec, ok
}

func buildToolDescription(m skill.Manifest) string {
	// Keep concise but include examples if present.
	desc := m.Metadata.Description
	if desc == "" {
		desc = m.Signature.Command
	}
	if m.Signature.Help != nil && m.Signature.Help.Short != "" {
		desc += "\n\n" + m.Signature.Help.Short
	}
	// Include a couple workflow examples
	if m.Signature.Help != nil && len(m.Signature.Help.Workflows) > 0 {
		desc += "\n\nExamples:"
		for i, wf := range m.Signature.Help.Workflows {
			if i >= 2 {
				break
			}
			desc += "\n- " + wf.ID + ": " + wf.Description
		}
	}
	return desc
}
```

---

## D) ToolExecutor that executes skills

This plugs into your existing `engine.ToolRunner`.

```go
// internal/consoleapp/skill_tool_executor.go
package consoleapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jkatigb/agentctl/internal/daemon"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/execution"
	"github.com/jkatigb/agentctl/internal/skills"
)

type SkillToolExecutor struct {
	Reg      *skills.Registry
	Daemon   *daemon.Client
	Resolver *daemon.SkillResolver // fallback resolution
	Workspace string
}

func (e *SkillToolExecutor) List() []engine.ToolDef {
	// Optional; caller can use registry.ListToolDefs
	return nil
}

func (e *SkillToolExecutor) Execute(ctx context.Context, toolName string, args json.RawMessage) (string, error) {
	spec, ok := e.Reg.ResolveTool(toolName)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}

	// Optional: merge defaults + validate required/enum
	normalizedArgs, err := mergeDefaultsAndValidate(spec.Manifest, args)
	if err != nil {
		return "", err
	}

	// Prefer daemon
	if e.Daemon != nil && e.Daemon.IsRunning() {
		res, err := e.Daemon.Run(spec.SkillName, normalizedArgs, e.Workspace, true /*ephemeral*/)
		if err != nil {
			return "", err
		}
		return string(res.Output), nil
	}

	// Fallback: run skill directly (ephemeral-ish) if daemon not running
	handle, err := e.Resolver.Resolve(spec.SkillName)
	if err != nil {
		return "", err
	}

	exec := execution.NewRunnerExecutor()
	// Ensure CAS expose in console mode if desired
	extraEnv := []string{
		"AGENTCTL_WORKSPACE=" + e.Workspace,
	}
	if os.Getenv("AGENTCTL_CAS_EXPOSE") != "" {
		extraEnv = append(extraEnv, "AGENTCTL_CAS_EXPOSE="+os.Getenv("AGENTCTL_CAS_EXPOSE"))
	}

	r, err := exec.Execute(ctx, execution.ExecuteOptions{
		Manifest:     handle.Manifest,
		ArtifactPath: handle.ArtifactPath,
		Input:        normalizedArgs,
		ExtraEnv:     extraEnv,
	})
	if err != nil {
		// runner returns stderr + stdout via Result too
		if r != nil && len(r.Stdout) > 0 {
			return string(r.Stdout), nil
		}
		return "", err
	}
	return string(r.Stdout), nil
}

func mergeDefaultsAndValidate(m skill.Manifest, args json.RawMessage) ([]byte, error) {
	base := map[string]any{}

	// defaults
	for _, p := range m.Signature.Parameters {
		if p.Default != nil {
			base[p.Name] = p.Default
		}
	}

	// overlay args
	if len(args) > 0 {
		var in map[string]any
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid tool args json: %w", err)
		}
		for k, v := range in {
			base[k] = v
		}
	}

	// validate required + enum
	missing := []string{}
	for _, p := range m.Signature.Parameters {
		_, ok := base[p.Name]
		if p.Required && !ok {
			missing = append(missing, p.Name)
		}
		if ok && len(p.Enum) > 0 {
			s := fmt.Sprintf("%v", base[p.Name])
			valid := false
			for _, ev := range p.Enum {
				if s == ev {
					valid = true
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("%s: %q must be one of %v (got %q)", m.Metadata.Name, p.Name, p.Enum, s)
			}
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s: missing required params: %v", m.Metadata.Name, missing)
	}

	return json.Marshal(base)
}
```

> This keeps behavior consistent with your manifest defaults, but still lets skills validate deeper.

---

# Console API (overseer) — concrete plan

## Endpoints

* `POST /api/console/sessions`

  * request: `{ workspace, actor_id?, model?, provider?, tools_allow? }`
  * response: `{ console_id }`
* `GET /api/console/sessions?workspace=`
* `POST /api/console/sessions/:id/ask`

  * request: `{ content }`
  * response: `{ correlation_id }`
* `POST /api/console/sessions/:id/cancel`

  * request: `{ correlation_id }`
* `GET /api/console/sessions/:id/events` (SSE)

  * streams `internal/domain/console.Payload` (ask/event/reply/cmd)

## Execution loop

In `internal/consoleapp/runner.go`:

1. Load registry tooldefs (or subset based on tools_allow)
2. Create engine:

   * `engine.NewLLMChatEngine(engine.DefaultLLMChatConfig() + provider/model from request/env)`
3. Create tool executor:

   * `SkillToolExecutor{...}`
4. Create tool runner:

   * `engine.NewToolRunner(executor, dispatcher, ToolRunnerConfig{Workspace, SessionID, ActorID})`
5. Run a single turn:

   * emit `console.event` “thinking”
   * run engine `Run(...)`
   * emit tool_call + tool_result events **as they happen**
6. Emit final `console.reply`

### Streaming assistant tokens

Your current `LLMChatEngine` is **non-streaming** (it blocks until full response).

**PR needed:** add streaming support:

* either a new engine `LLMChatStreamEngine`
* or extend `LLMChatEngine` with `Stream: true` and parse SSE `data:` lines.

This is a dedicated PR (details below).

---

# Sessions integration (optional but very worth it)

You already have a full sessions DB (`internal/storage/sessions`) and your GUI has SessionsPage.

✅ Best approach:

* When a console session is created:

  * create a `sessions.Session{Status: running, AgentID: "agentctl", WorkspacePath: ...}`
  * store its session_id on the console session row
* Each `ask` saves a `sessions.SessionTurn`:

  * user message turn
  * assistant response turn
  * tool calls can be recorded in `ToolCalls` or `SessionChunk` if desired
* On stop/cancel:

  * `sessions.Store.SetStatus(id, ok/error/canceled)`

This makes your “agentctl Studio” console show up in existing Sessions tooling.

---

# PR-by-PR implementation plan (Codex-ready)

## PR1 — `/api/skills` + tool schema generation

**Files**

* `internal/skills/{names.go,schema.go,registry.go}`
* `internal/web/handlers/skills.go`

**Endpoints**

* `GET /api/skills` → list manifests (name/version/desc/tags/help/workflows)
* `GET /api/tools` → list `engine.ToolDef` (normalized tool names + schema)

**Acceptance**

* curl shows tool list + JSON schema per tool

---

## PR2 — Tool execution via daemon + fallback

**Files**

* `internal/consoleapp/skill_tool_executor.go`

**Acceptance**

* calling `SkillToolExecutor.Execute(ctx,"code_symbols",{"path":"."})` returns envelope JSON

---

## PR3 — Console sessions + SSE

**Files**

* `internal/web/handlers/console.go`
* `internal/web/sse/*` (hub + SSE handler)
* `internal/consoleapp/{runner.go,stream.go}`

**Acceptance**

* UI or curl can:

  * create console session
  * ask
  * listen on SSE and see:

    * `event` chunks (tool call/result)
    * final reply

---

## PR4 — Streaming LLM output (real-time tokens + toolcall deltas)

**Modify**

* `internal/engine/llmchat_engine.go`

**Plan**

* Add `Stream bool` + `OnDelta func(StreamDelta)` to config (only used by console)
* Implement `callLLMStream` using `"stream": true`
* Parse lines:

  * `data: {...}`
  * stop on `data: [DONE]`
* Accumulate:

  * assistant content deltas
  * tool call deltas (function name + arguments)
* Emit deltas to SSE as `console.event` (partial=true)

**Acceptance**

* In console UI, assistant text appears progressively (not only at end)

---

## PR5 — Sessions store integration (optional but recommended)

**Files**

* `internal/web/handlers/sessions.go` (if not already present)
* `internal/consoleapp/runner.go` to call sessions store

**Acceptance**

* Console sessions appear in SessionsPage

---

## PR6 — GUI “Console” page

**Add route**

* `/console`

**UX**

* left: console sessions
* center: transcript timeline (user/assistant/tool)
* right: tool inspector (args/result + CAS links)

**Acceptance**

* fully usable chat + tool calling

---

# One more thing: “skills the agent has” with examples (your earlier ask)

Because manifests include:

* `metadata.description`
* `signature.help.workflows[].example_input`

✅ You get 2 ways to surface this:

1. **UI-only**: `/api/skills` drives a Skills page with examples.
2. **LLM hinting**: in the console system prompt, include a short “tools cheat-sheet”:

   * tool name
   * 1-liner description
   * 1 example JSON (from workflow.example_input when available)

Do **not** stuff all tools into system prompt; instead:

* include a short list of “core” tools
* add a “Use /api/skills to browse all tools” style hint
* or dynamically inject only the allowed toolset for the selected agent profile (explorer/reviewer/implementer)



<context>
Awesome — with **jobs + CAS + board/agents/mailbox stores** visible, we can now write a *very concrete* scaffolding + phased plan that Codex/Claude can execute without guessing.

Below is a “copy/paste” implementation plan + a proposed directory scaffold.

---

# What we now know (so the backend can be “real”, not mocked)

## Jobs (internal/storage/jobs)

* Metadata is in **SQLite**: `cfg.Paths.Jobs/jobs.db` (via `jobs/persist`)
* Per-job files are in **job dir**: `cfg.Paths.Jobs/<jobID>/`

  * `input.json`
  * `result.json` (and `job.ResultPath` points to it)
  * `stderr.log`
  * `progress.ndjson`
  * `workspace` (optional)
  * `artifacts.json` written by `runservice` (`{"digests":[...]}`)

Canonical states: `queued | running | ok | error | canceled`.

## CAS (internal/storage/cas)

* File-backed store supports:

  * `Put`, `Head`, `Get`, `List`, `Pin/Unpin`, `GC`
* Digest format enforced: `sha256:<64hex>`
* Metadata lives at `cas/sha256/<hex>.json` and includes `Kind`, `Tags`, `Size`, `CreatedAt`, etc.

✅ This is enough to implement **Jobs + CAS endpoints** for your GUI immediately.

---

# Concrete scaffolding: “agentctl Studio” (Go backend + React GUI)

## Goal

Replace the Bun/Express server with a **Go API + SSE** backend that:

1. Powers the existing dashboard pages (jobs/tasks/agents/blackboard/mailbox/sqlite/search…)
2. Adds a **native chat console** (“alternative to Claude Code/opencode”) that streams:

   * assistant tokens
   * tool calls + tool results
   * job progress (optional)
3. Uses your existing engine/tool-runner + storage primitives.

---

# Proposed new Go backend entrypoint

Create a new command:

```
cmd/agentctl_web/
  main.go
```

This server will:

* Listen on `:8090` (matches your Vite proxy already)
* Serve JSON REST under `/api/*`
* Serve SSE under `/api/events` (global invalidation feed)
* Later: serve `/api/console/*` for interactive overseer chat

---

# Backend package layout (scaffold)

```
internal/web/
  server.go                // Server struct, wiring, Start()
  router.go                // http routes
  middleware/
    errors.go              // JSON error envelope responses
    request_id.go          // correlation_id/trace_id
    workspace.go           // workspace selection cookie/header
  sse/
    hub.go                 // pubsub + client management
    handler.go             // /api/events SSE endpoint
    events.go              // event structs + helpers
  handlers/
    jobs.go                // /api/jobs, /api/jobs/:id, progress
    cas.go                 // /api/cas/...
    board.go               // /api/mailbox (BoardMessages), /api/reservations
    blackboard.go          // /api/blackboard...
    agents.go              // /api/agents...
    health.go              // /api/health
  services/
    jobs_service.go        // thin wrapper around storage/jobs
    cas_service.go
    board_service.go
    agents_service.go
```

> Later phases add `handlers/console.go` + `services/console_service.go` for the chat/overseer loop.

---

# API surface (v1)

## 0) Health

* `GET /api/health` → `{ ok: true, version, ... }`

## 1) Jobs

* `GET /api/jobs?state=&limit=` → `{ jobs: JobSummary[] }`
* `GET /api/jobs/:id` → `JobDetail`
* Optional but recommended:

  * `GET /api/jobs/:id/progress?follow=1` → SSE stream OR NDJSON passthrough
  * `GET /api/jobs/:id/result` → raw bytes of `result.json`
  * `GET /api/jobs/:id/stderr` → `stderr.log`

### Mapping to your TS types

Your GUI currently has some *old labels* (`completed/pending/failed/cancelled`). Your **storage is canonical** (`ok/queued/error/canceled`).

**Recommendation (clean v2 alignment):**

* Update GUI to use canonical states:

  * queued, running, ok, error, canceled
    …and change JobsPage filters/colors accordingly.

## 2) CAS

Use a URL shape that avoids needing to URL-encode `sha256:...`:

* `GET /api/cas/sha256/:hex/meta` → CAS metadata
* `GET /api/cas/sha256/:hex/raw` → stream content (Content-Type = meta.Kind)
* (Optional) `GET /api/cas/sha256/:hex` → friendly JSON: `{ meta, preview?: string }`

## 3) Mailbox + Reservations (BoardStore)

Your “MailboxPage” uses `MailboxMessage` (subject/body/kind/priority/status) which matches **board_messages**, not low-level mailbox.

* `GET /api/mailbox?workspace_id=&actor_id=&limit=&only_unread=` → `{ messages: BoardMessage[] }`
* `POST /api/mailbox/mark_surfaced` → marks message IDs surfaced
* `POST /api/mailbox/mark_read`
* `POST /api/mailbox/ack`

Reservations:

* `GET /api/reservations?workspace_id=` → `{ reservations: FileReservation[] }`
* `POST /api/reservations/reserve`
* `POST /api/reservations/release`

## 4) Blackboard

* `GET /api/blackboard?ns=&topic=&limit=` → `{ records: BlackboardRecord[] }`
* `POST /api/blackboard/post`
* `POST /api/blackboard/claim`
* `POST /api/blackboard/release`

## 5) Agents

* `GET /api/agents?limit=` → `{ agents: Agent[] }`
* `POST /api/agents/:id/start_daemon` → reuse existing daemon start machinery (you already have “start daemon” in GUI)

## 6) Global SSE (for UI invalidation)

* `GET /api/events` (SSE)

Events are *not* a full data stream. It’s just:

* `event: invalidate`
* `data: {"keys":["jobs","tasks"]}`

Your current `useSSE()` can simply invalidate the matching React Query keys.

---

# Phase plan (PR-sized chunks you can hand to Codex)

## PR1 — Go web server skeleton + SSE hub

**Deliverables**

* `cmd/agentctl_web/main.go` starts server
* `internal/web/router.go` mounts:

  * `/api/health`
  * `/api/events`
* `internal/web/sse/*`: hub with:

  * `Publish(topic string, payload any)`
  * fanout to clients
* CORS enabled for local dev, or rely on same-origin in prod.

**Acceptance**

* `curl localhost:8090/api/health`
* `curl -N localhost:8090/api/events` connects and stays open

---

## PR2 — Jobs API (backed by internal/storage/jobs)

**Implement**

* `GET /api/jobs`: `jobs.Open(ctx, cfg.Paths.Jobs)`, `store.List(ctx, limit)`
* `GET /api/jobs/:id`: `store.Get`, `store.Result`, read artifacts.json + stderr.log

**Notes**

* parse `job.Command`:

  * if `strings.HasPrefix("skill:")` => `skill = strings.TrimPrefix(...)`
  * category = prefix before `/` (e.g. `code`, `fs`, `hooks`)
  * type = `"skill"` else `"job"`
* `JobDetail.result_data`: parse JSON if possible, else raw string
* `JobDetail.artifacts`: read `cfg.Paths.Jobs/<id>/artifacts.json` if present

**Acceptance**

* Jobs page populates from Go backend
* Job detail page shows result + stderr + artifacts

---

## PR3 — CAS API (backed by internal/storage/cas)

**Implement**

* `GET /api/cas/sha256/:hex/meta`

  * `casStore := cas.NewStore(cfg.Paths.CAS)` (matches how runservice writes)
  * `Head(ctx, "sha256:"+hex)`
* `GET /api/cas/sha256/:hex/raw`

  * `Get(ctx, digest)` returning `(ReadCloser, meta)`
  * stream to response
  * set headers:

    * `Content-Type: meta.Kind`
    * `Content-Length: meta.Size` if available

**Acceptance**

* You can click an artifact digest in UI and fetch bytes

---

## PR4 — Board “Mailbox” + Reservations endpoints

Use `blackboard.OpenBoardStore(ctx, cfg.Storage.Root)` (note: board uses `root/board.db`)

**Implement**

* endpoints listed above
* ensure workspace_id required (or default from cookie)

**Acceptance**

* MailboxPage + Reservations page can be “real” (no Bun)

---

## PR5 — Replace Bun server usage in packages/gui

**Implement**

* Delete or stop using `packages/gui/server/*`
* Update `packages/gui/package.json` scripts:

  * `dev:all` → `concurrently "go run ./cmd/agentctl_web" "vite"`
* Update GUI to use `@agentctl/data` client everywhere:

  * remove `packages/gui/src/api/client.ts` + `packages/gui/src/types/*` duplication (or keep temporarily but migrate)
* Fix Jobs state labels to canonical:

  * queued, running, ok, error, canceled

**Acceptance**

* `bun run dev:all` (or `pnpm dev:all`) brings up UI + Go backend

---

# PR6 — Native “Overseer Console” (the Claude/opencode alternative)

This is the “real” product step: interactive chat + streamed tool calls.

### REST

* `POST /api/console/sessions`

  * creates a `console_sessions` row via `internal/storage/console`
* `GET /api/console/sessions?workspace=`
* `POST /api/console/sessions/:id/ask`

  * starts a turn and returns `{correlation_id}`
* `POST /api/console/sessions/:id/cancel`

### Streaming

* `GET /api/console/sessions/:id/events` (SSE)

  * streams `internal/domain/console.Payload` JSON

### Execution loop (core)

* On `ask`, start goroutine:

  1. Build EngineInput from stored transcript + user message
  2. Choose engine:

     * `engine.NewLLMChatEngine(...)` (OpenAI-compatible) OR `DSPyAdapter`
  3. Provide tool defs based on allowed skills
  4. For each streamed chunk/tool call:

     * publish SSE `PayloadTypeEvent` with `Metadata.Tool`, `Partial=true`
  5. On final:

     * publish `PayloadTypeReply`

### Tool execution

Implement a `ToolExecutor` that calls skills through your daemon (fast path) or runservice (direct):

* **Recommended**: daemon client path

  * `internal/daemon.Client.Run(skill, input, workspace, ephemeral=true)`
* `ToolDef.Parameters` from skill manifests (signature → JSON schema)

### Acceptance

* UI page “Console” lets you chat with “overseer”
* You see:

  * tokens streaming
  * tool calls
  * tool results
* “Cancel” stops the turn

---

# UI additions to make this feel like “Claude Code but agentctl-native”

Add a new route/page:

* `/console` (or `/chat`)

Key UI components:

* Left: session list (ConsoleSession)
* Middle: transcript timeline (assistant/user + tool call cards)
* Right: inspector (selected tool call details + raw args/result + CAS links)

---

# What I still might need (only for later phases)

You can keep coding PR1–PR5 with what you gave me. For PR6 (console/chat), I’d ideally also want:

1. **Skill manifest loader helpers** (if you already have a place that turns `skill.yaml` into ToolDef schema)
2. **Hook dispatcher wiring** (so pre/post tool use hooks work in console mode)
3. **Sessions store** (if you want console transcripts to unify with `SessionsPage` instead of being console-only)

So if/when you’re ready for PR6, send:

* `internal/storage/sessions/*`
* any existing “web server” code if present (`cmd/agentctl_web/*` or similar)
* any helpers around skills registry / manifest parsing (maybe in `internal/domain/skill` or `internal/adapters/skillslib`)

---

# Quick sanity note: Jobs state mismatch in current GUI

* `internal/storage/jobs/types.State` is: `queued/running/ok/error/canceled`
* Your `JobsPage` currently assumes: `pending/completed/failed/cancelled`

That’s a **known mismatch** we should fix as part of the v2 alignment in PR5.

---

Yes — to implement **Phase 2 (Jobs + CAS)** cleanly (and match your existing GUI types), I’ll need to see a few **storage** files so we don’t guess the on-disk layout or the `jobs.Store` API.

## Files I’d want before “coding for real”

### A) Jobs storage (most important)

Please share the key entrypoints for the jobs store so we can implement:

* `GET /api/jobs`
* `GET /api/jobs/:id`
* `GET /api/jobs/:id/result` (optional but useful)
* `GET /api/jobs/:id/stderr` (optional but useful)

Files:

* `internal/storage/jobs/*.go` (especially whatever defines `Open`, `Job` struct, and list/get/result APIs)

  * `internal/storage/jobs/types/*` (or wherever `jobs.Job` is defined)
  * `internal/storage/jobs/persist/*` (file layout / serialization)
  * `internal/storage/jobs/fsutil/*` (path conventions for result/stderr)
  * `internal/storage/jobs/executor/*` (only if it defines where stderr/stdout live)

I can work with a smaller subset if you send:

* `internal/storage/jobs/open.go` (or `jobs.go`)
* the `jobs.Job` struct definition file
* the file that defines `FindOrPrepareSkillJob`, `Get`, `Result`, `WaitForCompletion`, and any `List*` method(s)

(We saw these method names used in `runservice/jobs.go`, so the store definitely has them — we just need exact signatures.)

---

### B) CAS storage

To implement:

* `GET /api/cas/:digest/meta`
* `GET /api/cas/:digest/raw` (stream bytes)
* optionally `GET /api/cas/:digest` returning “best effort inline preview”

Files:

* `internal/storage/cas/*.go` (whatever defines `NewStore`, plus “read/get” APIs and object metadata type)

We already see `Put`, `Pin`, `Unpin` used — we need the “read” side.

---

### C) Existing web API expectations (optional but helpful)

Your GUI types mention `cmd/agentctl_web/templates/types.go` (or older web server code). If you want the new Go server to be drop-in compatible, share:

* any existing API handler code for jobs/cas:

  * `cmd/agentctl_viewer/**` (if it serves `/api/...`)
  * or any older `cmd/agentctl_web/**` if it exists in-repo
* any TS API client methods that already expect job/cas shapes (looks like `packages/data/src/client.ts` already has jobs endpoints; if CAS exists there, include those parts)

If you don’t share these, we’ll still match **the TS interfaces** you showed (`JobSummary`, `JobDetail`) and keep the endpoint shapes consistent.

---

## What I can implement immediately (even before those files)

Even without the internals, we can lock the **HTTP contract** and then fill in the storage calls:

### Jobs API contract (matches your GUI)

* `GET /api/jobs?state=&limit=&workspace=` → `{ jobs: JobSummary[] }`
* `GET /api/jobs/:id` → `JobDetail`

  * `result_data`: parsed envelope JSON (or raw string)
  * `artifacts`: read from `jobs/<id>/artifacts.json` (you already write this)
  * `stderr`: from job store or `stderr.txt` file (depending on layout)

### CAS API contract

* `GET /api/cas/:digest/meta` → `{ digest, size, media_type, tags, created_at? }`
* `GET /api/cas/:digest/raw` → streams content with correct `Content-Type`
* optional: `GET /api/cas/:digest?preview=1` → returns a small preview for text content

This lets the GUI:

* show artifacts list
* click artifact → open CAS viewer modal (or download raw)

---

## Minimal “Phase 2” implementation plan (what Codex will execute)

1. **Implement `api/jobs.go`**

* open store: `store := jobs.Open(ctx, cfg.Paths.Jobs)`
* parse query params `state`, `limit`
* list jobs (preferred: store has list/search; fallback: scan job dirs)
* map to `JobSummary`:

  * `category = strings.Split(command,"/")[0]`
  * `skill = strings.Split(command,"/")[1]` (best-effort)
  * `type = "skill"` for skill jobs
* sort by `created_at desc`

2. **Implement `api/job_detail.go`**

* read job metadata (`store.Get`)
* read envelope result (`store.Result`)
* parse envelope JSON into `result_data`
* read artifacts:

  * `cfg.Paths.Jobs/<jobID>/artifacts.json` contains `{digests:[...]}` (from `runservice`)
* read stderr:

  * either `store.Stderr(jobID)` if exists
  * or read file path from jobs persist conventions

3. **Implement `api/cas.go`**

* validate digest: must start with `sha256:`
* `casStore := cas.NewStore(cfg.Paths.CAS)`
* implement:

  * `meta`: object stat
  * `raw`: stream reader → response

4. **GUI small enhancements**

* JobDetailPage: artifacts become clickable → open CAS modal (fetch `/api/cas/:digest/meta`, then `/raw`)
* Add a basic CAS page or modal component (optional)

---

## Send me these file contents and I’ll produce an exact patch plan (no guessing)

If you paste **just these**, it’s enough to generate precise code:

1. `internal/storage/jobs`:

* the file that defines `type Job struct { ... }`
* the file that defines `func Open(...)` and store methods (`Get`, `Result`, any `List*`)

2. `internal/storage/cas`:

* the store type + `NewStore`
* the “read” method(s): `Get/Read/Open` equivalents

---

Below is a **concrete scaffolding + implementation plan** you can hand to Codex/Claude. It’s designed to:

* Keep your existing **agentctl internal architecture** (runservice/daemon/engine/protocol/console payloads)
* Add a new **Go HTTP+WS server** (`cmd/agentctl_web`)
* Extend your existing **React GUI** (`packages/gui`) with a **Console** page that streams events over WebSocket
* Start minimal (skills run + basic console), then iterate into full “alternative to Claude Code/OpenCode”

---

# 1) Target Architecture (MVP → Full)

## MVP (2–4 days)

* Go server: REST + WebSocket
* REST:

  * `GET /api/status`
  * `POST /api/skills/run` (ephemeral/job)
  * `GET /api/jobs` + `GET /api/jobs/:id` (basic)
  * `GET /api/cas/:digest` (basic)
* WS:

  * `/ws/console/:consoleID` (stream `console.Payload` ask/event/reply/cmd)
* React:

  * Add Console page with live stream + cancel
  * Add Vite proxy for `/ws`

## Full “Claude Code alternative” (next)

* Agent profiles (explorer/reviewer/implementer) with skill/tool allowlists
* Tool call loop with **tool events** (tool_call/tool_result) streamed in real time
* Persist conversation transcripts + trajectories in agentctl storage
* Bidirectional todo sync (agentctl ↔ provider files) behind a security gate

---

# 2) File/Folder Scaffolding

Create a new command + internal web module:

```
cmd/agentctl_web/
  main.go

internal/web/
  server.go
  options.go

internal/web/api/
  helpers.go
  status.go
  skills.go
  jobs.go
  cas.go

internal/web/consolews/
  hub.go
  session.go

internal/web/tools/
  registry.go
  executor.go
  profiles.go
```

React additions:

```
packages/gui/src/pages/ConsolePage.tsx
packages/gui/src/api/console.ts
packages/gui/src/hooks/useConsoleSocket.ts
packages/gui/src/pages/index.ts   (export ConsolePage)
packages/gui/src/App.tsx          (route)
packages/gui/src/components/layout/Sidebar.tsx  (nav item)
packages/gui/vite.config.ts       (proxy /ws)
```

---

# 3) Go Server Scaffolding (copy/paste)

## 3.1 `cmd/agentctl_web/main.go`

```go
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/logging"
	"github.com/jkatigb/agentctl/internal/web"
)

func main() {
	var (
		addr    string
		uiDir   string
		devCORS bool
	)
	flag.StringVar(&addr, "addr", "127.0.0.1:8090", "HTTP listen address")
	flag.StringVar(&uiDir, "ui-dir", "", "Path to built UI (e.g., ./packages/gui/dist). Optional.")
	flag.BoolVar(&devCORS, "dev-cors", true, "Enable permissive CORS for local dev")
	flag.Parse()

	ctx := context.Background()

	// Load env files (global + project)
	config.LoadDotEnv()

	cfg, err := config.LoadCached(ctx)
	if err != nil {
		panic(err)
	}

	logger := logging.New(logging.Config{Level: logging.LevelInfo, Format: logging.FormatText})
	zerolog.SetGlobalLevel(logger.GetLevel())

	srv, err := web.NewServer(web.Options{
		Addr:    addr,
		UIDir:   uiDir,
		DevCORS: devCORS,
	}, cfg, logger)
	if err != nil {
		panic(err)
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info().Str("addr", addr).Msg("agentctl_web listening")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error().Err(err).Msg("http server failed")
		}
	}()

	// Graceful shutdown
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch

	ctxShutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctxShutdown)
}
```

> Note: `logging.New(...)` returns a `zerolog.Logger`; if your local signature differs, adjust. The idea is to use your existing logging package.

---

## 3.2 `internal/web/options.go`

```go
package web

type Options struct {
	Addr    string
	UIDir   string // optional: serve static UI build
	DevCORS bool
}
```

---

## 3.3 `internal/web/server.go`

```go
package web

import (
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/daemon"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/api"
	"github.com/jkatigb/agentctl/internal/web/consolews"
	"github.com/jkatigb/agentctl/internal/web/tools"
)

type Server struct {
	opts Options
	cfg  config.Config
	log  zerolog.Logger

	skillResolver *daemon.SkillResolver
	consoleHub    *consolews.Hub
	toolRegistry  *tools.Registry
}

func NewServer(opts Options, cfg config.Config, log zerolog.Logger) (*Server, error) {
	resolver := daemon.NewSkillResolver(cfg)

	toolRegistry := tools.NewRegistry(cfg, resolver, tools.DefaultProfiles())

	s := &Server{
		opts:          opts,
		cfg:           cfg,
		log:           log,
		skillResolver: resolver,
		consoleHub:    consolews.NewHub(log, toolRegistry),
		toolRegistry:  toolRegistry,
	}

	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// --- API ---
	mux.HandleFunc("/api/status", api.StatusHandler(s.cfg, s.log))
	mux.HandleFunc("/api/skills/run", api.RunSkillHandler(s.cfg, s.log, s.skillResolver))
	// TODO (phase 2): list skills
	mux.HandleFunc("/api/skills", api.ListSkillsHandler(s.cfg, s.log, s.skillResolver))

	// TODO (phase 2): jobs + cas
	mux.HandleFunc("/api/jobs", api.JobsHandler(s.cfg, s.log))
	mux.HandleFunc("/api/jobs/", api.JobDetailHandler(s.cfg, s.log)) // /api/jobs/:id
	mux.HandleFunc("/api/cas/", api.CASHandler(s.cfg, s.log))        // /api/cas/:digest

	// --- WebSocket console ---
	mux.HandleFunc("/ws/console/", s.consoleHub.ServeWS) // /ws/console/:consoleID

	// --- Static UI (optional) ---
	if s.opts.UIDir != "" {
		fs := http.FileServer(http.Dir(s.opts.UIDir))
		mux.Handle("/", fs)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("agentctl_web running. In dev, run Vite at :5173.\n"))
		})
	}

	h := withCORS(s.opts.DevCORS, mux)
	h = withRequestLogging(s.log, h)
	return h
}

func withCORS(dev bool, next http.Handler) http.Handler {
	if !dev {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// permissive for local dev; tighten later
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withRequestLogging(log zerolog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug().Str("method", r.Method).Str("path", r.URL.Path).Msg("http request")
		next.ServeHTTP(w, r)
	})
}
```

---

# 4) REST Handlers (scaffold)

## 4.1 `internal/web/api/helpers.go`

```go
package api

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, out any) error {
	return json.NewDecoder(r.Body).Decode(out)
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
```

---

## 4.2 `internal/web/api/status.go`

```go
package api

import (
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func StatusHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"ok":        true,
			"ts":        time.Now().UTC().Format(time.RFC3339),
			"home":      cfg.Home,
			"skills":    cfg.Paths.Skills,
			"jobs":      cfg.Paths.Jobs,
			"cas":       cfg.Paths.CAS,
			"obs":       cfg.Paths.Observability,
			"driver":    cfg.Database.Driver,
			"vector_on": cfg.Database.Vector.Enabled,
		})
	}
}
```

---

## 4.3 `internal/web/api/skills.go`

This one is important: it uses **runservice** so you get CAS limiting + artifacts + proper envelopes.

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/daemon"
	"github.com/jkatigb/agentctl/internal/runservice"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
)

type RunSkillRequest struct {
	Skill     string          `json:"skill"`
	Input     json.RawMessage `json:"input,omitempty"`
	Workspace string          `json:"workspace,omitempty"`
	Mode      string          `json:"mode,omitempty"` // "ephemeral" | "job"
	NoCAS     bool            `json:"no_cas,omitempty"`
}

type RunSkillResponse struct {
	JobID    string          `json:"job_id,omitempty"`
	Envelope json.RawMessage `json:"envelope"`
	Stderr   string          `json:"stderr,omitempty"`
}

func RunSkillHandler(cfg config.Config, log zerolog.Logger, resolver *daemon.SkillResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, 405, "method not allowed")
			return
		}

		var req RunSkillRequest
		if err := readJSON(r, &req); err != nil {
			httpError(w, 400, "invalid JSON")
			return
		}
		req.Skill = strings.TrimSpace(req.Skill)
		if req.Skill == "" {
			httpError(w, 400, "skill is required")
			return
		}

		ws := req.Workspace
		if ws == "" {
			ws = workspace.Detect("")
		}

		mode := strings.ToLower(strings.TrimSpace(req.Mode))
		if mode == "" {
			mode = "ephemeral"
		}

		handle, err := resolver.Resolve(req.Skill)
		if err != nil {
			httpError(w, 404, err.Error())
			return
		}

		// Convert daemon.SkillHandle -> runservice.SkillHandle (same fields, different pkg)
		rsHandle := runservice.SkillHandle{
			Manifest:     handle.Manifest,
			ManifestPath: handle.ManifestPath,
			ArtifactPath: handle.ArtifactPath,
		}

		// Capture stdout/stderr
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		corr := ulid.Make().String()
		w.Header().Set("X-Agentctl-Correlation-Id", corr)

		opts := runservice.RunOptions{
			SkillName:     req.Skill,
			CLICommand:    "agentctl_web",
			CorrelationID: corr,
			Input:         req.Input,
			Workspace:     ws,
			Ephemeral:     mode == "ephemeral",
			NoCAS:         req.NoCAS,
			Timeout:       2 * time.Minute,
		}
		if err := opts.Validate(); err != nil {
			httpError(w, 400, err.Error())
			return
		}

		exec := runservice.NewExecutor(context.Background(), cfg, rsHandle, &stdout, &stderr, opts)
		defer exec.Close()

		if opts.Ephemeral {
			if err := exec.ExecuteEphemeral(req.Input); err != nil {
				httpError(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, RunSkillResponse{
				Envelope: stdout.Bytes(),
				Stderr:   stderr.String(),
			})
			return
		}

		// Job mode: PrepareJob + ExecuteSync
		job, dup, err := exec.PrepareJob(req.Input)
		if err != nil {
			httpError(w, 500, err.Error())
			return
		}
		if dup {
			// simplest: just surface duplicate by executing HandleDuplicate (writes envelope to stdout)
			if err := exec.HandleDuplicate(job); err != nil {
				httpError(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, RunSkillResponse{
				JobID:    job.ID,
				Envelope: stdout.Bytes(),
				Stderr:   stderr.String(),
			})
			return
		}
		if err := exec.ExecuteSync(job); err != nil {
			httpError(w, 500, err.Error())
			return
		}

		writeJSON(w, 200, RunSkillResponse{
			JobID:    job.ID,
			Envelope: stdout.Bytes(),
			Stderr:   stderr.String(),
		})
	}
}

// Minimal stub: fill in later (phase 2)
func ListSkillsHandler(cfg config.Config, log zerolog.Logger, resolver *daemon.SkillResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"skills": []any{},
			"note":   "TODO: enumerate skill directories and load manifests",
		})
	}
}
```

> This already gives the UI a way to run **any** skill with either ephemeral or job persistence.

---

## 4.4 Jobs + CAS handlers (stubs for phase 2)

You can wire these to `internal/storage/jobs` + `internal/storage/cas` next. For scaffolding:

```go
package api

import (
	"net/http"

	"github.com/rs/zerolog"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func JobsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"jobs": []any{},
			"note": "TODO: use internal/storage/jobs store",
		})
	}
}

func JobDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"note": "TODO: parse job id from /api/jobs/:id",
		})
	}
}

func CASHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"note": "TODO: parse digest from /api/cas/:digest and read from CAS",
		})
	}
}
```

---

# 5) WebSocket Console Scaffolding

Use `nhooyr.io/websocket` (recommended). Add dependency:

```bash
go get nhooyr.io/websocket
```

## 5.1 `internal/web/consolews/hub.go`

```go
package consolews

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	domainconsole "github.com/jkatigb/agentctl/internal/domain/console"
	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/web/tools"
)

type Hub struct {
	log zerolog.Logger

	mu       sync.Mutex
	sessions map[string]*Session

	registry *tools.Registry
}

func NewHub(log zerolog.Logger, registry *tools.Registry) *Hub {
	return &Hub{
		log:      log,
		sessions: make(map[string]*Session),
		registry: registry,
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	// /ws/console/:consoleID
	consoleID := strings.TrimPrefix(r.URL.Path, "/ws/console/")
	consoleID = strings.Trim(consoleID, "/")
	if consoleID == "" {
		http.Error(w, "missing consoleID", 400)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// in dev you can allow all origins; tighten later
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	actorID := r.URL.Query().Get("actor_id")
	if actorID == "" {
		actorID = "actor:system:overseer"
	}
	profile := r.URL.Query().Get("profile")
	if profile == "" {
		profile = "implementer"
	}
	workspace := r.URL.Query().Get("workspace")
	sessionID := r.URL.Query().Get("session_id")

	sess := h.getOrCreateSession(consoleID, actorID, profile, workspace, sessionID)

	// main read loop
	ctx := r.Context()
	for {
		var p domainconsole.Payload
		if err := wsjson.Read(ctx, conn, &p); err != nil {
			return
		}
		switch p.Type {
		case domainconsole.PayloadTypeAsk:
			sess.HandleAsk(ctx, conn, p)
		case domainconsole.PayloadTypeCmd:
			sess.HandleCmd(ctx, conn, p)
		default:
			_ = wsjson.Write(ctx, conn, domainconsole.NewEventPayload(
				actorID, consoleID, p.CorrelationID, "unknown payload type", true,
			))
		}
	}
}

func (h *Hub) getOrCreateSession(consoleID, actorID, profile, workspace, sessionID string) *Session {
	h.mu.Lock()
	defer h.mu.Unlock()

	if s, ok := h.sessions[consoleID]; ok {
		return s
	}

	s := NewSession(h.log, h.registry, consoleID, actorID, profile, workspace, sessionID)
	h.sessions[consoleID] = s
	return s
}

func writePayload(ctx context.Context, conn *websocket.Conn, payload domainconsole.Payload) {
	_ = wsjson.Write(ctx, conn, payload)
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
```

---

## 5.2 `internal/web/consolews/session.go`

This is a **working non-token-streaming** console. It streams:

* “queued”
* tool_call + tool_result events (emitted after engine completes, for MVP)
* final reply
* cancel support

```go
package consolews

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	consoletrack "github.com/jkatigb/agentctl/internal/console"
	domainconsole "github.com/jkatigb/agentctl/internal/domain/console"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/web/tools"
)

type Session struct {
	log zerolog.Logger

	consoleID string
	actorID   string
	workspace string
	sessionID string
	profile   string

	mu       sync.Mutex
	inflight map[string]context.CancelFunc

	track *consoletrack.CorrelationTracker

	// conversation memory for this console session
	msgs []engine.Message

	// engine + tools
	eng   *engine.LLMChatEngine
	tools []engine.ToolDef
	exec  *tools.SkillToolExecutor
}

func NewSession(
	log zerolog.Logger,
	reg *tools.Registry,
	consoleID, actorID, profile, workspace, sessionID string,
) *Session {
	// Build tool executor from profile allowlist
	exec := reg.ExecutorForProfile(profile, workspace, sessionID, actorID)
	toolDefs := exec.List()

	// Build LLM engine (OpenAI-compatible)
	chat, _ := engine.NewLLMChatEngine(engine.LLMChatConfig{
		// provider + api key autodetected (OPENROUTER_API_KEY, GROQ_API_KEY, OPENAI_API_KEY)
		MaxIterations: 50,
		Timeout:       120 * time.Second,
		Temperature:   0.0,
		MaxTokens:     8192,
		// HookDispatcher: (wire later)
		Logger: log,
	})

	// ToolRunner (hooks dispatcher can be wired later)
	runner := engine.NewToolRunner(exec, nil, engine.ToolRunnerConfig{
		Workspace: workspace,
		SessionID: sessionID,
		ActorID:   actorID,
	})
	chat.SetToolRunner(runner)

	return &Session{
		log:       log,
		consoleID: consoleID,
		actorID:   actorID,
		workspace: workspace,
		sessionID: sessionID,
		profile:   profile,
		inflight:  make(map[string]context.CancelFunc),
		track:     consoletrack.NewCorrelationTracker(1),
		msgs:      []engine.Message{},
		eng:       chat,
		tools:     toolDefs,
		exec:      exec,
	}
}

func (s *Session) HandleAsk(parent context.Context, conn *websocket.Conn, p domainconsole.Payload) {
	s.mu.Lock()
	defer s.mu.Unlock()

	corr := p.CorrelationID
	if corr == "" {
		corr = ulid.Make().String()
	}

	// enforce 1 in-flight by default
	if _, err := s.track.NewCorrelation(s.consoleID, s.actorID, p.Content); err != nil {
		_ = wsjson.Write(parent, conn, domainconsole.NewEventPayload(s.actorID, s.consoleID, corr,
			"busy: another request is in flight", false))
		return
	}

	ctx, cancel := context.WithCancel(parent)
	s.inflight[corr] = cancel

	// quick ack
	_ = wsjson.Write(parent, conn, domainconsole.NewEventPayload(s.actorID, s.consoleID, corr, "queued", true))

	go s.runAsk(ctx, conn, corr, p.Content)
}

func (s *Session) HandleCmd(ctx context.Context, conn *websocket.Conn, p domainconsole.Payload) {
	if p.Cmd == nil || p.Cmd.Name == "" {
		_ = wsjson.Write(ctx, conn, domainconsole.NewEventPayload(s.actorID, s.consoleID, p.CorrelationID, "missing cmd", false))
		return
	}
	if p.Cmd.Name == domainconsole.CommandCancel {
		s.mu.Lock()
		cancel, ok := s.inflight[p.Cmd.CorrelationID]
		s.mu.Unlock()
		if ok && cancel != nil {
			cancel()
			_ = wsjson.Write(ctx, conn, domainconsole.NewEventPayload(s.actorID, s.consoleID, p.Cmd.CorrelationID, "cancel requested", true))
		}
	}
}

func (s *Session) runAsk(ctx context.Context, conn *websocket.Conn, corrID, prompt string) {
	defer func() {
		s.mu.Lock()
		if cancel := s.inflight[corrID]; cancel != nil {
			delete(s.inflight, corrID)
		}
		s.track.Complete(corrID)
		s.mu.Unlock()
	}()

	// Build engine input
	s.mu.Lock()
	history := append([]engine.Message{}, s.msgs...)
	s.mu.Unlock()

	history = append(history, engine.NewUserMessage(prompt))

	in := engine.EngineInput{
		Messages:      history,
		Tools:         s.tools,
		SystemPrompt:  tools.SystemPromptForProfile(s.profile),
		Workspace:     s.workspace,
		SessionID:     s.sessionID,
		ActorID:       s.actorID,
		TurnID:        corrID,
		MaxTokens:     8192,
		Temperature:   0.0,
	}

	out, err := s.eng.Run(ctx, in)
	if err != nil {
		_ = wsjson.Write(context.Background(), conn, domainconsole.NewReplyPayload(s.actorID, s.consoleID, corrID,
			fmt.Sprintf("engine error: %v", err)))
		return
	}

	// Emit tool call/result events (MVP: after completion)
	for _, tc := range out.ToolCalls {
		_ = wsjson.Write(context.Background(), conn, domainconsole.Payload{
			Type:          domainconsole.PayloadTypeEvent,
			ActorID:       s.actorID,
			ConsoleID:     s.consoleID,
			CorrelationID: corrID,
			Content:       fmt.Sprintf("tool_call: %s %s", tc.Name, previewJSON(tc.Arguments)),
			Metadata: &domainconsole.Metadata{
				Partial: true,
				Tool:    tc.Name,
				MIME:    "text/plain",
			},
		})
	}
	for _, tr := range out.ToolResults {
		_ = wsjson.Write(context.Background(), conn, domainconsole.Payload{
			Type:          domainconsole.PayloadTypeEvent,
			ActorID:       s.actorID,
			ConsoleID:     s.consoleID,
			CorrelationID: corrID,
			Content:       fmt.Sprintf("tool_result: %s", previewText(tr.Content, 400)),
			Metadata: &domainconsole.Metadata{
				Partial:   true,
				MIME:      "text/plain",
				CASDigest: tr.CASDigest,
			},
		})
	}

	// Reply
	reply := out.AssistantText
	if out.StopReason == engine.StopReasonCancelled {
		reply = "cancelled"
	}
	_ = wsjson.Write(context.Background(), conn, domainconsole.NewReplyPayload(s.actorID, s.consoleID, corrID, reply))

	// Persist minimal conversation memory for follow-ups
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, engine.NewUserMessage(prompt))
	if len(out.ToolCalls) > 0 {
		s.msgs = append(s.msgs, engine.NewAssistantToolCallMessage(out.ToolCalls))
		for _, tr := range out.ToolResults {
			s.msgs = append(s.msgs, engine.NewToolResultMessage(tr.ToolCallID, "tool", tr.Content, tr.IsError))
		}
	}
	s.msgs = append(s.msgs, engine.NewAssistantMessage(out.AssistantText))
}

func previewJSON(b json.RawMessage) string {
	if len(b) == 0 {
		return "{}"
	}
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}
	return string(b)
}

func previewText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
```

---

# 6) Tools Layer (Profiles + Skill Executor)

## 6.1 `internal/web/tools/profiles.go`

```go
package tools

import "strings"

type Profile struct {
	Name        string
	System      string
	SkillsAllow []string
}

func DefaultProfiles() []Profile {
	return []Profile{
		{
			Name: "explorer",
			System: strings.TrimSpace(`
You are Explorer. You only explore and summarize. Do not propose edits unless asked.
Prefer search/symbols/semantic tools. Keep outputs concise and actionable.
`),
			SkillsAllow: []string{
				"fs/read", "fs/find", "fs/tree",
				"text/ripgrep", "text/grep",
				"code/symbols", "code/semantic_search", "code/context_ripgrep",
				"session/recall",
			},
		},
		{
			Name: "reviewer",
			System: strings.TrimSpace(`
You are Reviewer. You review changes and suggest improvements. Do not perform edits.
Focus: correctness, security, complexity, tests.
`),
			SkillsAllow: []string{
				"fs/read", "text/ripgrep",
				"code/diff", "code/complexity", "code/security", "code/symbols",
				"code/semantic_search",
			},
		},
		{
			Name: "implementer",
			System: strings.TrimSpace(`
You are Implementer. You can make changes via agentctl skills. Prefer smart edits.
Use tasks/todos when relevant. Keep changes minimal and verified.
`),
			SkillsAllow: []string{
				"fs/read", "fs/write", "fs/apply_edit",
				"text/ripgrep", "text/replace",
				"code/smart_write", "code/symbols", "code/semantic_search", "code/context_ripgrep",
				"todo/manage", "plan/sync",
			},
		},
	}
}

func SystemPromptForProfile(name string) string {
	for _, p := range DefaultProfiles() {
		if p.Name == name {
			return p.System
		}
	}
	// fallback
	return DefaultProfiles()[2].System
}
```

---

## 6.2 `internal/web/tools/registry.go`

```go
package tools

import (
	"github.com/jkatigb/agentctl/internal/daemon"
	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/rs/zerolog"
)

type Registry struct {
	cfg      config.Config
	resolver *daemon.SkillResolver
	profiles map[string]Profile

	log zerolog.Logger
}

func NewRegistry(cfg config.Config, resolver *daemon.SkillResolver, profiles []Profile) *Registry {
	m := make(map[string]Profile, len(profiles))
	for _, p := range profiles {
		m[p.Name] = p
	}
	return &Registry{cfg: cfg, resolver: resolver, profiles: m}
}

func (r *Registry) ExecutorForProfile(profileName, workspace, sessionID, actorID string) *SkillToolExecutor {
	p, ok := r.profiles[profileName]
	if !ok {
		p = r.profiles["implementer"]
	}
	return NewSkillToolExecutor(r.cfg, r.resolver, p, workspace, sessionID, actorID)
}

type SkillToolExecutor struct {
	cfg      config.Config
	resolver *daemon.SkillResolver

	profile   Profile
	workspace string
	sessionID string
	actorID   string

	toolMap map[string]string // toolName -> skillName
	tools   []engine.ToolDef
}

func NewSkillToolExecutor(cfg config.Config, resolver *daemon.SkillResolver, prof Profile, workspace, sessionID, actorID string) *SkillToolExecutor {
	ex := &SkillToolExecutor{
		cfg:       cfg,
		resolver:  resolver,
		profile:   prof,
		workspace: workspace,
		sessionID: sessionID,
		actorID:   actorID,
		toolMap:   map[string]string{},
		tools:     []engine.ToolDef{},
	}
	ex.buildTools()
	return ex
}

// List implements engine.ToolExecutor (via wrapper below)
func (e *SkillToolExecutor) List() []engine.ToolDef { return e.tools }
```

---

## 6.3 `internal/web/tools/executor.go`

```go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/jkatigb/agentctl/internal/daemon"
	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/runservice"
)

func (e *SkillToolExecutor) buildTools() {
	// Create a tool per allowed skill.
	for _, skillName := range e.profile.SkillsAllow {
		handle, err := e.resolver.Resolve(skillName)
		if err != nil {
			continue
		}

		toolName := normalizeToolName(skillName)
		e.toolMap[toolName] = skillName

		// Keep schema simple in MVP. Later: convert manifest.signature.parameters to JSON Schema.
		var params json.RawMessage = []byte(`{"type":"object","properties":{}}`)

		e.tools = append(e.tools, engine.ToolDef{
			Name:        toolName,
			Description: handle.Manifest.Metadata.Description,
			Parameters:  params,
		})
	}
}

// Execute implements engine.ToolExecutor
func (e *SkillToolExecutor) Execute(ctx context.Context, toolName string, args json.RawMessage) (string, error) {
	skillName, ok := e.toolMap[toolName]
	if !ok {
		return "", fmt.Errorf("tool not allowed: %s", toolName)
	}

	handle, err := e.resolver.Resolve(skillName)
	if err != nil {
		return "", err
	}

	rsHandle := runservice.SkillHandle{
		Manifest:     handle.Manifest,
		ManifestPath: handle.ManifestPath,
		ArtifactPath: handle.ArtifactPath,
	}

	// Execute tools ephemerally by default (fast, CAS truncation helps)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	corr := ulid.Make().String()
	opts := runservice.RunOptions{
		SkillName:     skillName,
		CLICommand:    "agentctl_web_tool",
		CorrelationID: corr,
		Input:         args,
		Workspace:     e.workspace,
		Ephemeral:     true,
		NoCAS:         false,
		Timeout:       2 * time.Minute,
	}

	if err := opts.Validate(); err != nil {
		return "", err
	}

	exec := runservice.NewExecutor(ctx, e.cfg, rsHandle, &stdout, &stderr, opts)
	defer exec.Close()

	if err := exec.ExecuteEphemeral(args); err != nil {
		return "", err
	}

	// Return envelope JSON (stdout) as tool result content.
	// If you want, you can return only `.data` later, but raw envelope is fine.
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		out = "{}"
	}
	return out, nil
}

func normalizeToolName(skill string) string {
	// LLM tool names should be simple (no slashes).
	// This matches your normalize strategy.
	s := strings.ReplaceAll(skill, "/", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}
```

---

# 7) React Scaffolding (Console page)

## 7.1 Update Vite proxy for websockets: `packages/gui/vite.config.ts`

Add:

```ts
proxy: {
  "/api": { target: "http://localhost:8090", changeOrigin: true },
  "/ws":  { target: "ws://localhost:8090", ws: true },
},
```

---

## 7.2 `packages/gui/src/hooks/useConsoleSocket.ts`

```ts
import { useEffect, useMemo, useRef, useState } from "react";

export type ConsolePayloadType = "ask" | "reply" | "event" | "cmd";

export interface ConsolePayload {
  type: ConsolePayloadType;
  actor_id: string;
  console_id: string;
  correlation_id: string;
  content: string;
  metadata?: {
    mime?: string;
    partial?: boolean;
    tool?: string;
    cas_digest?: string;
    error?: string;
  };
  cmd?: { name: string; correlation_id?: string };
}

export function useConsoleSocket(params: {
  consoleId: string;
  actorId: string;
  profile: string;
  workspace?: string;
  sessionId?: string;
}) {
  const { consoleId, actorId, profile, workspace, sessionId } = params;
  const [connected, setConnected] = useState(false);
  const [events, setEvents] = useState<ConsolePayload[]>([]);
  const wsRef = useRef<WebSocket | null>(null);

  const url = useMemo(() => {
    const qs = new URLSearchParams({
      actor_id: actorId,
      profile,
      ...(workspace ? { workspace } : {}),
      ...(sessionId ? { session_id: sessionId } : {}),
    });
    const base =
      import.meta.env.DEV
        ? `ws://localhost:8090/ws/console/${consoleId}`
        : `ws://${location.host}/ws/console/${consoleId}`;
    return `${base}?${qs.toString()}`;
  }, [actorId, consoleId, profile, workspace, sessionId]);

  useEffect(() => {
    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);
    ws.onerror = () => setConnected(false);

    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data) as ConsolePayload;
        setEvents((prev) => [...prev, msg]);
      } catch {
        // ignore
      }
    };

    return () => ws.close();
  }, [url]);

  function send(payload: ConsolePayload) {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify(payload));
  }

  function ask(text: string, correlationId: string) {
    send({
      type: "ask",
      actor_id: actorId,
      console_id: consoleId,
      correlation_id: correlationId,
      content: text,
    });
  }

  function cancel(correlationId: string) {
    send({
      type: "cmd",
      actor_id: actorId,
      console_id: consoleId,
      correlation_id: correlationId,
      content: "",
      cmd: { name: "cancel", correlation_id: correlationId },
    });
  }

  return { connected, events, ask, cancel, clear: () => setEvents([]) };
}
```

---

## 7.3 `packages/gui/src/pages/ConsolePage.tsx`

```tsx
import { useMemo, useState } from "react";
import { useConsoleSocket } from "@/hooks/useConsoleSocket";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

function ulidLike(): string {
  // good enough for client-side correlation IDs in MVP
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

export function ConsolePage() {
  const [consoleId] = useState(() => ulidLike());
  const [actorId, setActorId] = useState("actor:system:overseer");
  const [profile, setProfile] = useState<"explorer" | "reviewer" | "implementer">("implementer");
  const [prompt, setPrompt] = useState("");

  const { connected, events, ask, cancel, clear } = useConsoleSocket({
    consoleId,
    actorId,
    profile,
  });

  const lastCorrelation = useMemo(() => {
    // last ask correlation id
    for (let i = events.length - 1; i >= 0; i--) {
      if (events[i].type === "ask") return events[i].correlation_id;
    }
    return "";
  }, [events]);

  const onSend = () => {
    if (!prompt.trim()) return;
    const corr = ulidLike();
    ask(prompt.trim(), corr);
    setPrompt("");
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Console</h1>
        <div className="flex items-center gap-2">
          <Badge variant={connected ? "success" : "destructive"}>
            {connected ? "connected" : "disconnected"}
          </Badge>
          <Badge variant="outline">{profile}</Badge>
          <Badge variant="secondary">{consoleId}</Badge>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Session</CardTitle>
        </CardHeader>
        <CardContent className="flex gap-2 items-center flex-wrap">
          <Input value={actorId} onChange={(e) => setActorId(e.target.value)} className="w-80" />
          <select
            value={profile}
            onChange={(e) => setProfile(e.target.value as any)}
            className="border rounded px-2 py-1"
          >
            <option value="explorer">explorer</option>
            <option value="reviewer">reviewer</option>
            <option value="implementer">implementer</option>
          </select>
          <Button variant="outline" onClick={clear}>Clear</Button>
          <Button variant="destructive" disabled={!lastCorrelation} onClick={() => cancel(lastCorrelation)}>
            Cancel last
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Send</CardTitle>
        </CardHeader>
        <CardContent className="flex gap-2">
          <Input
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="Ask something..."
            onKeyDown={(e) => e.key === "Enter" && onSend()}
          />
          <Button onClick={onSend}>Send</Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Stream</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2 max-h-[60vh] overflow-auto">
          {events.map((e, idx) => (
            <div key={idx} className="border rounded p-2 text-sm">
              <div className="flex items-center gap-2 mb-1">
                <Badge variant="outline">{e.type}</Badge>
                {e.metadata?.tool && <Badge variant="secondary">{e.metadata.tool}</Badge>}
                <span className="text-xs text-muted-foreground font-mono">{e.correlation_id}</span>
              </div>
              <pre className="whitespace-pre-wrap font-mono text-xs">{e.content}</pre>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  );
}
```

---

## 7.4 Wire route + nav

`packages/gui/src/pages/index.ts`:

```ts
export { ConsolePage } from "./ConsolePage";
```

`packages/gui/src/App.tsx` add:

```tsx
import { ConsolePage } from "@/pages";
// ...
<Route path="console" element={<ConsolePage />} />
```

`packages/gui/src/components/layout/Sidebar.tsx` add nav item:

```ts
{ name: "Console", href: "/console", icon: MessageSquare },
```

---

# 8) Clear Implementation Plan (hand to Codex/Claude)

## Phase 0 — Bootstrap server (0.5 day)

**Goal:** `go run ./cmd/agentctl_web` starts, `/api/status` works.

Tasks:

* Add `cmd/agentctl_web/main.go`
* Add `internal/web/{options.go,server.go}`
* Add `/api/status` handler

Acceptance:

* `curl http://127.0.0.1:8090/api/status` returns JSON with paths/config.

---

## Phase 1 — Skill runner endpoint (1 day)

**Goal:** UI can run skills and show envelopes.

Tasks:

* Implement `POST /api/skills/run` (done above)
* Ensure job mode works: `mode:"job"` persists + returns job_id
* Ensure ephemeral works: returns envelope inline

Acceptance:

* `curl -XPOST /api/skills/run` runs `fs/read` etc and returns valid envelope JSON
* Large outputs get CAS truncated via runservice (verify `meta.cas_digest` appears)

---

## Phase 2 — Jobs + CAS endpoints (1–2 days)

**Goal:** GUI pages for Jobs and CAS can be backed by Go server.

Tasks:

* Implement:

  * `GET /api/jobs?limit=&state=`
  * `GET /api/jobs/:id`
  * `GET /api/cas/:digest` (read CAS object)
* Reuse existing storage packages:

  * `internal/storage/jobs`
  * `internal/storage/cas`

Acceptance:

* Existing JobsPage + JobDetailPage can be switched to Go backend and load data.

---

## Phase 3 — WebSocket Console MVP (1–2 days)

**Goal:** Console page streams ask/event/reply and supports cancel.

Tasks:

* Add `internal/web/consolews/*` + `internal/web/tools/*`
* Implement `/ws/console/:consoleID`
* Engine: `engine.LLMChatEngine` with `SkillToolExecutor` tool allowlist
* Cancel: maintain correlation → cancel func map

Acceptance:

* Open Console page → connected
* Sending prompt yields “queued” + tool events + reply
* Cancel last stops a long tool call

---

## Phase 4 — Real streaming tool events (2–4 days)

**Goal:** show tool_call/tool_result *as they happen*, not only after final answer.

Tasks:

* Implement a small wrapper around `LLMChatEngine` loop or implement `StreamingLLMChatEngine` in `internal/engine`:

  * callback hooks per iteration
  * callback before/after each tool call
* In WS session, emit those callbacks as `console.event`

Acceptance:

* Tool call appears immediately when LLM requests it
* Tool result appears right after execution
* UI feels “live” even without token streaming

---

## Phase 5 — Agent profiles (explorer/reviewer/implementer) + UI selection (1 day)

**Goal:** profile selection changes allowed tool set + system prompt.

Tasks:

* Use `tools.DefaultProfiles()` allowlists
* Add `profile` query param support (already scaffolded)
* Show current profile in UI

Acceptance:

* “explorer” cannot call write/edit skills
* “implementer” can call smart_write/fs_write

---

## Phase 6 — Observability integration (1–2 days)

**Goal:** wide events for HTTP requests + console turns + tool exec.

Tasks:

* On each `/api/skills/run` and each WS ask:

  * ensure trace id (`observability.EnsureTraceID`)
  * emit wide event on completion (`observability.Emit(...)`)
* Add `/api/obs/tail` (optional) that tails NDJSON

Acceptance:

* events appear in `$AGENTCTL_OBS_DIR/events/wide_events.ndjson`
* correlation_id matches WS correlation_id + job_id meta when applicable

---

# 9) Recommended “Codex Prompt” for execution

Paste this as the instruction to Codex/Claude:

```text
Implement agentctl_web scaffolding:

- Add cmd/agentctl_web with Go HTTP server at 127.0.0.1:8090.
- Add internal/web server with routes:
  - GET /api/status
  - POST /api/skills/run (ephemeral/job) using internal/runservice (CAS limiting + artifacts)
  - GET /api/skills (can be stub)
  - WS /ws/console/:consoleID implementing domain console payloads ask/event/reply/cmd

- Add internal/web/tools:
  - profiles: explorer/reviewer/implementer allowlists
  - registry: build tool defs from allowlisted skills
  - executor: implements engine.ToolExecutor by running allowlisted skills ephemerally via runservice

- Add internal/web/consolews:
  - hub: accept websocket connections and route payloads
  - session: per console session, correlation tracking, cancel, run engine.LLMChatEngine with tool runner

- Update packages/gui:
  - add ConsolePage with websocket client to /ws/console/:consoleID
  - add Vite ws proxy for /ws
  - add sidebar link /console and route

Acceptance:
- curl /api/status works
- curl POST /api/skills/run runs a skill
- Console UI connects and can ask questions, see events, and cancel
```

---

Yep — the code you pasted is already very aligned with a **Go backend + React UI** architecture. You basically have:

* A **fast local execution substrate** (skills + daemon + runservice)
* A **conversation/agent substrate** (engine + tool runner + console correlation)
* A **standard wire format** (protocol/envelope + domain/console payloads)

Below is a concrete way to stitch those together into a web app without fighting the existing design.

---

## How I’d slice the system

### 1) Keep Go as the “control plane”

Your Go backend should own:

* Starting/communicating with the daemon (or embedding it)
* Running skills (ephemeral or persisted)
* Managing console sessions (ask/reply/event/cmd)
* Streaming progress/events to the UI
* Reading artifacts/CAS/job results for display

React should be a thin client: render state, call APIs, subscribe to streams.

---

## Backend options (choose one)

### Option A (recommended): **One Go process that exposes HTTP + WS**

* `cmd/agentctl_web` (or similar) runs:

  * HTTP API (REST-ish)
  * WebSocket for console streaming
  * Skill execution using `runservice` (persisted) and/or daemon for fast paths

This gives you the best UX because you can do **true streaming** and consistent metadata (job_id, correlation_id, cas hints, etc.).

### Option B: **Thin HTTP server that proxies to the daemon over Unix socket**

* Web server uses `internal/daemon.Client` (`status`, `run`, `warm`, `shutdown`)
* Fast to implement, minimal refactor
* Downsides:

  * Current daemon `run` is “single response” (no streaming)
  * `RunParams.Ephemeral` exists but isn’t used in `handleRun`
  * No job persistence / artifact pinning / CAS output limiting (unless you add it)

If the UI is primarily “run a skill and show the result”, B works.
If you want “chat-like agent console with tool-call traces + cancel”, do A.

---

## What you already have that maps perfectly to a web UI

### A) Console message model (UI streaming backbone)

`internal/domain/console/types.go` is basically ready-made for WebSockets:

* `ask` → user submits prompt
* `event` → streaming chunks/progress/tool updates
* `reply` → final answer
* `cmd` → cancel/pause etc.

And you have correlation primitives:

* `CorrelationTracker` enforces backpressure (defaults to 1 in-flight), and tracks streamed content.

**This is exactly what you want for a browser console.**

---

### B) Execution / agent engine layer

You have a clean abstraction:

* `engine.AgentEngine` (`Run(ctx, EngineInput) -> EngineOutput`)
* Implementations:

  * `LLMChatEngine` (OpenAI-compatible tool calling loop)
  * `DSPyAdapter` (dspy-go agent)

This maps to a UI “agent console” that can show:

* assistant text
* tool calls
* tool results
* stop reason

Even if you don’t stream token-by-token yet, you can still stream:

* “calling tool X”
* “tool result received”
* “iteration N”

---

### C) Skill execution layer (jobs + CAS + artifacts)

`internal/runservice` is your “run skills with all the correct side effects” layer:

* output limiting to CAS (`enforceOutputLimit`)
* artifact pinning (`handleArtifacts` + `adapters/artifacts`)
* job persistence (`jobs.go`)
* trajectory capture metadata (`trajectory_meta.go`)
* consistent envelope annotation (`protocol.AnnotateRunBytes` + correlation/job metadata)

This is what you want powering the “Runs / Jobs / Artifacts” pages in React.

---

## Suggested API shape (simple + UI-friendly)

### REST endpoints

These are straightforward wrappers around existing packages:

#### Daemon / server status

* `GET /api/status`

  * return daemon status + app status + warm workspaces

If you’re embedding daemon logic, reuse `handleStatus()` shape from `internal/daemon/service.go`.

#### Skills

* `GET /api/skills`

  * list available skills (you can enumerate skill directories using the resolver search paths)
* `POST /api/skills/run`

  * body: `{ skill, input, workspace, mode }`
  * `mode = "ephemeral" | "job"`

If `mode="job"`, route through `runservice` so the UI can fetch:

* job_id
* correlated output
* CAS references / artifacts

#### Jobs

* `GET /api/jobs?limit=…`
* `GET /api/jobs/:id`
* `GET /api/jobs/:id/result`
* `GET /api/jobs/:id/artifacts`

These will come from the job store (`internal/storage/jobs`) + `runservice/artifacts.go` + CAS.

#### CAS

* `GET /api/cas/:digest`

  * return metadata + optionally content (or paginate)
  * (You can also add `GET /api/cas/:digest/raw`)

---

## WebSocket console (the most valuable feature)

### `WS /ws/console/:consoleID?actor_id=...`

* Client sends JSON `console.Payload` (your domain type)
* Server sends JSON `console.Payload`

**Message flow example:**

1. UI → server:

```json
{ "type":"ask", "actor_id":"actor:system:overseer", "console_id":"01J...", "correlation_id":"01J...", "content":"run tests" }
```

2. server → UI (progress events):

```json
{ "type":"event", "actor_id":"...", "console_id":"...", "correlation_id":"...", "content":"Starting…", "metadata":{"partial":true} }
```

3. server → UI (tool call events):

```json
{ "type":"event", "metadata":{"tool":"fs.read_file","partial":true}, "content":"Calling fs.read_file …" }
```

4. server → UI (final reply):

```json
{ "type":"reply", "content":"Done. Tests passed." }
```

5. UI cancel:

```json
{ "type":"cmd", "cmd":{"name":"cancel","correlation_id":"01J..."} }
```

### Implementation detail (backend)

* Keep:

  * `CorrelationTracker` (for max-in-flight + accumulated streamed content)
  * a `map[correlationID]context.CancelFunc` to actually cancel runs
* On `ask`:

  * create correlation (or validate)
  * spawn goroutine:

    * run engine / skill
    * send `event` messages along the way
    * send `reply` at end
* On `cmd:cancel`:

  * call cancel func
  * mark correlation cancelled (`ct.Cancel`)
  * send reply with “cancelled”

This matches your current types and semantics cleanly.

---

## React UI layout that fits your backend

I’d do TypeScript React with:

* **TanStack Query** for REST (`/api/status`, `/api/jobs`, etc.)
* **WebSocket hook** for console (subscribe to events)

### Pages

1. **Dashboard**

   * daemon/server status
   * warm workspaces
   * recent jobs
2. **Skills**

   * list skills, run a skill with JSON input
3. **Jobs**

   * job list + detail page
   * display envelope, artifacts, CAS hint
4. **Console**

   * chat-style UI
   * show tool calls/results as expandable sections
   * cancel button tied to correlation_id
5. **CAS Browser**

   * view stored payloads (especially truncated outputs)

---

## Important code-level observations (things to tighten before wiring UI)

These aren’t “blockers”, but they’ll matter once a UI is constantly connected.

### 1) `RunParams.Ephemeral` is currently unused in the daemon

* `daemon.Client.Run(... ephemeral bool)` sends it
* `daemon.Service.handleRun` ignores it

If you keep the daemon as the execution backend, decide what ephemeral means:

* **Ephemeral true** → run fast, no job store, minimal side effects
* **Ephemeral false** → use `runservice` (jobs, artifacts, CAS limiting, trajectories)

Right now it always behaves like “ephemeral-ish”.

### 2) Daemon `shutdown` handling doesn’t fully stop acceptLoop cleanly

In `handleConnection` → `"shutdown"`:

* it closes `shutdownCh`
* but does **not** set `isShutdown` or close listener there

If the process doesn’t exit immediately after `Run()` returns, acceptLoop can keep accepting.
If you embed daemon + web server in one process, you *definitely* want shutdown to close the listener and flip `isShutdown`.

### 3) `LLMChatEngine` tool runner error is ignored

In `LLMChatEngine.Run`:

```go
result, _ = e.toolRunner.Execute(ctx, toolCall)
```

`ToolRunner.Execute` already turns many errors into ToolResult content, so it’s often fine — but for debugging a UI, you’ll want to:

* at least log the returned error
* and/or attach an “internal_error” field in tool result content

### 4) For web UI, prefer `runservice` output shaping

Your UI will love that `runservice`:

* enforces output size
* stores large output in CAS
* pins artifacts
* annotates metadata (workspace, skill_version, job_id, correlation_id)

The daemon `handleRun` currently just returns stdout (or an error envelope) without those extra guarantees.

---

## Minimal “first milestone” plan (fastest path to a working app)

1. **Add Go HTTP server**

   * `GET /api/status` (daemon status + config summary)
   * `GET /api/jobs` (if you wire job store)
2. **Add WS console**

   * accepts `console.Payload`
   * responds with mock `event` + `reply` first (to prove streaming)
3. **Wire skill execution**

   * implement `POST /api/skills/run` using `runservice` (job mode)
   * show result envelope in React
4. **Wire agent console**

   * use `engine.LLMChatEngine` behind the WS ask/reply flow
   * stream “iteration/tool” events even if you don’t stream tokens yet

That gets you a useful UI quickly without needing deep refactors.

---

## If you want, I can turn this into concrete scaffolding

If you tell me where you want the web server to live (new `cmd/agentctl_web` vs extending an existing one like `packages/gui/server`), I can outline:

* Go router + handler skeletons
* WS handler using `console.Payload`
* A small React app structure (pages + API client + WS hook)
* The exact JSON shapes to reuse `protocol/envelope` consistently

But even without that, the big picture is: **your domain types already match a browser console**, and `runservice` is the right execution backend for anything you want to “see later” in the UI (jobs/artifacts/CAS).

</context>
