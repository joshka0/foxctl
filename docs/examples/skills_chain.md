# Skill Chaining Example (fs/ls → text/grep)

Once skills are built (`make skills-build`), you can run them directly through the CLI. Because `foxctl skills run` reads JSON input from `--input` or an input file/pipe, you can chain skills together using standard shell tools.

## WASI quickstart

The CLI now auto-installs the bundled `wasi/echo` sample the first time you run it, so you can verify WASI execution with a single command:

```bash
foxctl run wasi/echo
```

The skill emits a deterministic envelope without needing network or filesystem access, making it a safe starting point for new WASI-based workflows.

Example: list a directory, pick a file from the listing, then search for a pattern within that file—all without leaving JSON land.

```bash
# Build the skills first
make skills-build

# List files (fs/ls) and pick the first regular file via jq,
# then feed that path into text/grep to search for TODO comments.
foxctl skills run fs/ls --input '{"path":"."}' \
  | jq '{path: .data.preview[0].path, pattern: "TODO"}' \
  | foxctl skills run text/grep --input-file -
```

Because `foxctl skills run` defaults to reading JSON from stdin when `--input-file -` is set, any tool that emits JSON (such as `jq`) can transform one skill's envelope into the next skill's input. This makes skill chaining straightforward without special plumbing.

## Deep-dive with `fs/read`

When you need a deterministic `cat` replacement, slot the new `fs/read` skill between discovery and search:

```bash
foxctl skills run fs/ls --input '{"path":"."}' \
  | jq '{path: .data.preview[0].path}' \
  | foxctl skills run fs/read --input-file - \
  | jq '{path: .data.path, pattern: "needle"}' \
  | foxctl skills run text/grep --input-file -
```

`fs/read` streams the full file into CAS (so large files stay out of band) while returning a UTF-8 preview inline. The digest it emits can be fed into `foxctl cas get` or attached to higher-level envelopes when you need downstream reproducibility. When you do not need the intermediate `jq` plumbing, call it directly as `foxctl fs read path/to/file` and let the CLI craft the JSON envelope for you.

## TODO capture with `todo/manage`

You can log follow-up work straight from a bash pipeline by piping structured JSON into the new todo skill (or simply call `foxctl todo add` for flags-first ergonomics):

```bash
cat <<'JSON' \
  | foxctl skills run todo/manage --input-file -
{
  "operation": "add",
  "add": {
    "title": "Refactor fs/read docs",
    "description": "Clarify CAS usage in README",
    "depends_on": [],
    "parent_id": ""
  }
}
JSON
```

Later, complete the task with contextual notes (no backticks allowed, so both the CLI wrapper and skill keep envelopes safe):

```bash
foxctl skills run todo/manage --input '{
  "operation":"complete",
  "complete":{"id":"01H...", "notes":"Documented CAS workflow", "gotchas":"Remember to update CLI help too"}
}'
```
