# Skill Chaining Example (fs/ls → text/grep)

Once skills are built (`make skills-build`), you can run them directly through the CLI. Because `agentctl skills run` reads JSON input from `--input` or an input file/pipe, you can chain skills together using standard shell tools.

Example: list a directory, pick a file from the listing, then search for a pattern within that file—all without leaving JSON land.

```bash
# Build the skills first
make skills-build

# List files (fs/ls) and pick the first regular file via jq,
# then feed that path into text/grep to search for TODO comments.
agentctl skills run fs/ls --input '{"path":"."}' \
  | jq '{path: .data.preview[0].path, pattern: "TODO"}' \
  | agentctl skills run text/grep --input-file -
```

Because `agentctl skills run` defaults to reading JSON from stdin when `--input-file -` is set, any tool that emits JSON (such as `jq`) can transform one skill's envelope into the next skill's input. This makes skill chaining straightforward without special plumbing.
