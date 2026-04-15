# Exa MCP + context/filter Workflow

This doc shows how to wire the Exa MCP server into `foxctl` and feed its
results into the `context/filter` skill.

It assumes you:

- Have `foxctl` checked out and built.
- Have valid API keys:
  - `EXA_API_KEY` for Exa MCP.
  - An LLM key for `context/filter` (e.g., `GROQ_API_KEY`, `OPENAI_API_KEY`,
    etc.).

---

## 1. Build CLI and skills

From the repo root:

```bash
make build        # builds ./foxctl
make skills-build # builds skills into dist/skills/
```

---

## 2. Install Exa MCP skills via `mcp/install`

The Exa MCP server is hosted at `https://mcp.exa.ai/mcp`.

1. Export your Exa key:

   ```bash
   export EXA_API_KEY=...     # from dashboard.exa.ai
   ```

2. Run `mcp/install` to generate local skills backed by Exa MCP:

   ```bash
   BRIDGE="$(pwd)/dist/skills/mcp_bridge/bin"

   ./foxctl run mcp/install --input '{
     "server_url": "https://mcp.exa.ai/mcp",
     "server_headers": {
       "Accept": "application/json, text/event-stream",
       "Authorization": "Bearer '"$EXA_API_KEY"'"
     },
     "output_dir": "./exa_skills",
     "bridge_path": "'"$BRIDGE"'"
   }'
   ```

   This will:
   - Connect to the Exa MCP server.
   - Discover its tools.
   - Generate one `foxctl` skill per tool under `./exa_skills/<tool-name>/`.

3. Point `foxctl` at the generated skills:

   ```bash
   export FOXCTL_SKILL_PATH="$(pwd)/exa_skills"
   ```

4. Inspect what was generated:

   ```bash
   ls exa_skills
   # e.g. search, context, ... (actual names depend on the server)
   cat exa_skills/<tool-name>/skill.yaml
   ```

---

## 3. Run an Exa skill

Pick one of the generated tools (for example, `search`; replace with the real
name you see under `exa_skills/`).

Example call:

```bash
FOXCTL_SKILL_PATH="$(pwd)/exa_skills" \
  ./foxctl run search --input '{
    "query": "foxctl CAS integrity failures",
    "num_results": 10
  }' > exa_raw.json
```

Notes:

- The exact input schema comes from the generated `skill.yaml`.
- The result will be an `foxctl` envelope; the payload will be under `.data`.

Inspect the result shape:

```bash
jq '.data' exa_raw.json
```

You can choose either to:

- Treat the entire Exa result as one big `source.text`, or
- Map individual Exa hits into `source.chunks` (recommended once you know the
  schema).

---

## 4. Simple pipeline: Exa -> `context/filter` via `source.text`

For a minimal working pipeline, convert the Exa response into plain text and
feed it into `context/filter`.

1. Flatten Exa content into text (very simple example):

   ```bash
   jq -r '.data.content[] | tostring' exa_raw.json > exa_text.txt
   ```

   Adjust the `.data...` path based on the real Exa tool output.

2. Call `context/filter` with that text and an LLM provider (example: Groq):

   ```bash
   export GROQ_API_KEY=...

   # Use jq -Rs to safely embed file contents as a JSON string
   jq -Rs '{
     prompt: "Explain how foxctl handles CAS integrity failures.",
     scope: "code",
     source: { text: . },
     budget: { target_tokens: 2000, max_chunks: 16 },
     llm: { provider: "groq", model: "llama-3.3-70b-versatile" }
   }' exa_text.txt | ./foxctl run context/filter --input-file -
   ```

3. The `context/filter` envelope will contain:
   - `data.chunks`: selected text chunks + scores + rationales.
   - `data.summary`: short natural-language summary of the selected context.
   - `data.llm_usage`: provider/model/usage metadata.

Use `data.chunks` as the retrieval context for downstream agents or tools.

---

## 5. Piping data directly into `context/filter`

`foxctl run` already supports stdin-friendly patterns:

- `--input-file -` — read **raw stdin** as the JSON input to a skill.
- `--input stdin` — read an **envelope** from stdin and pass its `.data` field
  as input.

This makes it easy to build Unix-style pipelines around `context/filter`.

### 5.1 Pipe arbitrary text into `context/filter`

If you have a command that produces raw text, you can wrap it into the
`context/filter` input JSON with `jq` and feed it via stdin:

```bash
some-command-producing-text \
  | jq -Rs '{
      prompt: "Summarize this text",
      scope: "docs",
      source: { text: . },
      budget: { target_tokens: 2000, max_chunks: 16 },
      llm: { provider: "groq", model: "llama-3.3-70b-versatile" }
    }' \
  | ./foxctl run context/filter --input-file -
```

Notes:

- `jq -Rs` reads stdin as a single raw string and escapes it for JSON.
- You can parameterize `prompt`, `scope`, and `llm` as needed.

### 5.2 Pipe one skill's envelope into `context/filter`

If an upstream skill already emits an envelope, and its `.data` is _itself_ a
valid `context/filter` input object, you can chain runs directly using
`--input stdin`:

```bash
./foxctl run some/producer --input '{ ... }' \
  | ./foxctl run context/filter --input stdin
```

In this pattern:

- The first command prints an envelope to stdout.
- `--input stdin` on the second command extracts `.data` from that envelope and
  passes it to `context/filter`.

You can use this once you have a dedicated glue skill that:

- Calls Exa MCP.
- Transforms the Exa result into the `context/filter` input schema.
- Emits that object as its own `data` payload for chaining.

---

## 6. Refinement: Exa results as `source.chunks`

Once you understand the Exa tool output structure, you can map it directly into
the `source.chunks` schema expected by `context/filter`:

```jsonc
{
  "source": {
    "chunks": [
      {
        "id": "exa-<result-id>",
        "text": "<page or snippet text>",
        "metadata": {
          "url": "<source-url>",
          "score": 0.92,
          "title": "<page title>"
        }
      }
    ]
  }
}
```

In this mode, `context/filter` will:

- Skip its own text chunking.
- Only perform LLM-based selection + summarization over the provided chunks.

You can build those chunks either in a small glue script or a dedicated skill
that:

1. Calls the Exa MCP-generated skill.
2. Transforms the response into `source.chunks`.
3. Calls `context/filter` and returns its envelope.

---

## 7. Using CAS instead of inline text

`context/filter` also accepts `source.cas_digest`. A more scalable pipeline is:

1. Persist Exa results into CAS via a helper/skill.
2. Call `context/filter` with:

   ```jsonc
   {
     "source": { "cas_digest": "sha256:..." }
   }
   ```

3. Let `context/filter` load and chunk the CAS payload internally.

See `docs/spec/context_filter.md` §10.1 for the high-level contract.

---

## 8. Troubleshooting

- **Network issues / 4xx from Exa**: Check `server_headers` (especially `Accept`
  and `Authorization`) and `EXA_API_KEY`.
- **`context/filter` LLM errors**: Ensure the appropriate LLM API key is
  exported (`GROQ_API_KEY`, `OPENAI_API_KEY`, etc.) and that `llm.model` is
  valid.
- **Debugging `context/filter`**: Set `CONTEXT_FILTER_DEBUG=1` to log LLM HTTP
  calls (to stderr) without leaking secrets.
