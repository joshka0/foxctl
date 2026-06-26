# code/semantic_search

Semantic code search over the workspace. Returns ranked file paths and
symbol matches based on embedding similarity and lexical matching.

## Usage

```bash
foxctl skills run code/semantic_search --query "authentication flow" --format tree
```

## Parameters

- `query` (string, required): The search query
- `format` (string): Output format — "tree" or "flat" (default: "flat")
- `workspace` (string): Workspace path to search
