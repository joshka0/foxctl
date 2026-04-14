# `context/filter` Skill Spec

**Status:** Draft v0.1

---

## 1. Purpose

`context/filter` is a generic *context selection* skill:

- **Input:**
  - a natural language **prompt** (what the agent/user is trying to do)
  - one or more **sources** (raw text, pre‑chunked items, or a CAS blob)
  - an optional **budget** (approximate token/size limits)
  - an **LLM config** (provider/model/options)
- **Output:**
  - an ordered list of **relevant chunks** with scores + rationales
  - a short **summary** of what the chunks cover
  - optional CAS artifact for large outputs

It is deliberately generic: any agent or tool can call it, regardless of how the source text was produced (Exa, MCP tools, filesystem skills, memory search, etc.).

---

## 2. Command & Transport

- **Skill name:** `context/filter`
- **Distribution:** `exec`
- **I/O format:** JSON envelopes (Core Profile v1)
- **Envelopes:**
  - `version: 1`
  - `status: "ok" | "error"`
  - `command: "context/filter"`
  - `data`: request/response payloads as specified below
  - `meta`: execution metadata; may include `cas_digest` for large outputs

The skill must **only** write JSON envelopes to stdout and use stderr for logs.

---

## 3. Input Schema (`data`)

Top‑level request structure:

```jsonc
{
  "prompt": "User or agent question/instruction",      // required

  "scope": "code",                                    // optional: "auto" | "code" | "web" | "docs" | ...

  "source": {                                          // at least one field must be present
    "cas_digest": "sha256:...",                       // optional: CAS blob
    "text": "raw long text",                          // optional: unstructured text
    "chunks": [                                         // optional: pre-chunked items
      {
        "id": "doc-1",
        "text": "some text or code",
        "metadata": {
          "url": "https://...",
          "path": "internal/cas/store.go",
          "language": "go",
          "start_line": 120,
          "end_line": 175,
          "source": "exa"          // arbitrary tags
        }
      }
    ]
  },

  "budget": {                                          // optional
    "target_tokens": 2000,                             // soft cap for *returned* chunks
    "max_chunks": 16,                                  // hard cap on number of chunks
    "max_source_tokens": 16000                         // cap on text seen by the LLM
  },

  "llm": {                                             // optional (defaults apply)
    "provider": "openai",                             // "openai" | "anthropic" | "gemini" | "groq" | "openrouter"
    "model": "gpt-4.1-mini",
    "temperature": 0.1,
    "max_output_tokens": 512                           // for the LLM's JSON selection, not chunk text
  }
}
```

### 3.1 Required / optional fields

- **`prompt`** (string, required)
  - Natural language description of what context is needed.
- **`scope`** (string, optional)
  - Hints how to interpret the source (`"code"`, `"web"`, `"docs"`, etc.).
  - Used to bias prompt instructions; **no behavioral guarantees**.
- **`source`** (object, required)
  - At least one of `cas_digest`, `text`, or `chunks` must be non‑empty.
  - The skill normalizes these into internal *candidate chunks*.
- **`budget`** (object, optional)
  - `target_tokens` (int, optional, default: 2000)
  - `max_chunks` (int, optional, default: 16)
  - `max_source_tokens` (int, optional, default: 16000)
- **`llm`** (object, optional)
  - `provider` (string, default: `"openai"`)
  - `model` (string, default per provider; e.g. `"gpt-4.1-mini"`)
  - `temperature` (float, default: `0.1`)
  - `max_output_tokens` (int, default: 512)

If `llm` is omitted, the skill uses configured defaults (see §5). If both `budget` and `llm.max_output_tokens` are omitted, conservative defaults apply.

---

## 4. Output Schema (`data`)

On **success** (`status: "ok"`), the envelope `data` contains:

```jsonc
{
  "prompt": "...",                 // echoed from input
  "scope": "code",                 // echoed from input

  "chunks": [                       // ordered by relevance (best first)
    {
      "id": "doc-1#120-175",      // globally unique within this result
      "text": "…trimmed content…", // full chunk text
      "score": 0.94,                // relevance score in [0, 1]
      "metadata": {
        "source_id": "doc-1",     // back-reference to source chunk (if any)
        "url": "https://...",
        "path": "internal/cas/store.go",
        "language": "go",
        "start_line": 120,
        "end_line": 175,
        "provider": "exa"         // free-form tags
      },
      "rationale": "1–2 sentence explanation of why this chunk is relevant."
    }
  ],

  "summary": "Short overview of what these chunks collectively cover.",
  "approx_tokens": 1800,           // approximate token count for chunks[]

  "llm_usage": {                   // usage for the *selection* call
    "provider": "openai",
    "model": "gpt-4.1-mini",
    "prompt_tokens": 800,
    "completion_tokens": 350
  }
}
```

### 4.1 CAS usage for large outputs

If `chunks` is large, the skill may:

- Store the full `chunks` array in CAS as an artifact (e.g. NDJSON or JSON), and
- Inline only:
  - `summary`
  - a *small* subset of top chunks
  - artifact descriptor

Example when artifactizing:

```jsonc
{
  "summary": "…",
  "chunks": [ /* top 3 only */ ],
  "artifact": {
    "digest": "sha256:...",
    "bytes": 12345,
    "kind": "context/snippets/v1"
  }
}
```

In this case the envelope **must** also set:

```jsonc
"meta": {
  "cas_digest": "sha256:..."
}
```

The initial implementation may keep all results inline when they comfortably fit within `inline_output_kb`.

---

## 5. LLM Providers & Configuration

The skill talks directly to model APIs using HTTPS. Providers are selected via `llm.provider` and configured via environment variables / secrets:

| Provider     | `llm.provider` | Auth env var           | Base URL (default)                          |
|-------------|----------------|------------------------|---------------------------------------------|
| OpenAI      | `"openai"`     | `OPENAI_API_KEY`       | `https://api.openai.com`                    |
| Anthropic   | `"anthropic"`  | `ANTHROPIC_API_KEY`    | `https://api.anthropic.com`                 |
| Gemini      | `"gemini"`     | `GEMINI_API_KEY`       | `https://generativelanguage.googleapis.com` |
| Groq        | `"groq"`       | `GROQ_API_KEY`         | `https://api.groq.com`                      |
| OpenRouter  | `"openrouter"` | `OPENROUTER_API_KEY`   | `https://api.openrouter.ai`                 |

### 5.1 Network policy

Skill manifest capabilities **must** include:

```yaml
network: "egress"
egressAllow:
  - api.openai.com
  - api.anthropic.com
  - generativelanguage.googleapis.com
  - api.groq.com
  - api.openrouter.ai
filesystem:
  - type: workdir
pure: false
```

Secrets must never be logged or echoed into envelopes. Keys are loaded from env / `/run/secrets/*` only.

### 5.2 Provider behavior (overview)

- **OpenAI / Groq / OpenRouter**
  - Use OpenAI‑compatible `POST /v1/chat/completions`.
  - Request includes `model`, `messages`, `temperature`, and `max_tokens`.
- **Anthropic (Claude)**
  - Use `POST /v1/messages` with `x-api-key` and `anthropic-version` headers.
- **Gemini**
  - Use `POST /v1beta/models/{model}:generateContent?key=...`.

All providers must be prompted to return **strict JSON** in a shared selection schema (see §6), not free‑text.

---

## 6. Internal Selection Contract (LLM)

The skill presents the LLM with a compact view of candidate chunks and asks it to output JSON of the form:

```jsonc
{
  "chunks": [
    {
      "id": "doc-1#120-175",
      "score": 0.94,
      "rationale": "why this chunk is relevant"
    }
  ],
  "summary": "short natural language summary",
  "approx_tokens": 1800
}
```

Notes:

- The LLM **does not** need to echo full chunk text; the skill already has it locally.
- `id` must match one of the candidate IDs provided by the skill.
- The skill uses the LLM’s `id`/`score`/`rationale` and then:
  - looks up the original chunk text + metadata, and
  - enforces the `budget` constraints (trimming low‑priority chunks if needed).

If the LLM output is invalid (non‑JSON or missing fields), the skill should return an `error` envelope with an actionable message rather than guessing.

---

## 7. Source Normalization

The implementation should normalize `source` into an internal list of *candidate chunks*:

1. **Pre‑chunked (`source.chunks`)**
   - Use chunks as‑is (subject to limits) with IDs and metadata preserved.
2. **CAS (`source.cas_digest`)**
   - Load the CAS object via the standard CAS store.
   - If the payload is JSON and contains a `chunks` array, reuse it.
   - Otherwise, treat it as raw text (UTF‑8) and fall back to text chunking.
3. **Raw text (`source.text`)**
   - Split into reasonable chunks (e.g. paragraphs / code blocks) with conservative size limits.

Implementations should:

- Enforce an upper bound on the number of candidates considered (e.g. 64–128).
- Respect `budget.max_source_tokens` by:
  - truncating or sampling candidate chunks before sending them to the LLM.

---

## 8. Budget Semantics

- `target_tokens`
  - Soft cap on approximate token count of the **returned** `chunks`.
  - Implementation may approximate tokens using character length heuristics.
- `max_chunks`
  - Hard cap on number of chunks returned.
- `max_source_tokens`
  - Hard cap on approximate token count of **input** text seen by the LLM.

If the LLM selects more chunks than allowed, the skill must trim the result greedily (highest score first) until both `target_tokens` and `max_chunks` are satisfied.

---

## 9. Error Handling

Common error conditions and recommended `error.code` values:

- Invalid input (missing `prompt` or `source`): `EARG`
- Missing or invalid provider configuration / API key: `EAUTH`
- Network / HTTP failures talking to providers: `ENETWORK`
- Invalid JSON from provider (non‑parsable or missing fields): `ERUNTIME`
- CAS lookup failures (missing digest, integrity error): `ECAS`

Errors should include a short human‑readable message and, where safe, a `data.hint` explaining how to fix the problem (e.g. which env var is missing).

---

## 10. Usage Patterns

### 10.1 Exa + MCP + context/filter

1. Use `mcp/install` against Exa’s MCP server to generate an `exa/search` skill.
2. Call `exa/search` (or similar MCP‑generated skill) to get a CAS digest containing raw Exa results.
3. Call `context/filter`:

```jsonc
{
  "prompt": "Explain how foxctl handles CAS integrity failures.",
  "scope": "code",
  "source": {
    "cas_digest": "sha256:...exa-result..."
  },
  "budget": {
    "target_tokens": 2000,
    "max_chunks": 16
  },
  "llm": {
    "provider": "openai",
    "model": "gpt-4.1-mini"
  }
}
```

4. Use `data.chunks` from `context/filter` as the sole retrieval context for downstream reasoning.

### 10.2 Generic text / logs

- Provide `source.text` directly (e.g., combined logs, documentation) and let the skill chunk + select.

### 10.3 Pre‑chunked inputs

- Other skills (filesystem, memory search, Exa wrappers) can emit `source.chunks` directly in the same schema; `context/filter` then performs **only** LLM‑based selection.

---

## 11. Testing & Golden Cases

Recommended tests:

- Happy paths:
  - pre‑chunked `source.chunks` with simple budget
  - raw `source.text` chunking and selection
- Provider selection:
  - invalid `llm.provider` → `EARG` with hint
  - missing API key → `EAUTH`
- CAS paths:
  - missing digest → `ECAS`
  - non‑JSON CAS payload treated as raw text
- LLM robustness:
  - non‑JSON response → `ERUNTIME`
  - valid JSON but missing `chunks` → `ERUNTIME`

Golden outputs should focus on **envelope shape**, `chunks` ordering, and budget enforcement, not on specific model wording.
