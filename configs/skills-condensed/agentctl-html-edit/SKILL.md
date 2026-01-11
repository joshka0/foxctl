---
name: agentctl HTML Edit
description: Precise DOM-aware HTML editing using CSS selectors for targeted modifications.
---

# HTML Edit

Edit HTML precisely using CSS selectors. DOM-aware for reliable modifications.

## Operations

```bash
# Query elements (read-only)
agentctl run html/edit --input '{"path": "index.html", "operations": [{"type": "select", "selector": ".feature-card"}]}'

# Insert content (before/after/prepend/append)
agentctl run html/edit --input '{"path": "index.html", "operations": [{"type": "insert", "selector": "#content", "position": "append", "html": "<section>New</section>"}]}'

# Replace element or inner HTML
agentctl run html/edit --input '{"path": "index.html", "operations": [{"type": "replace", "selector": ".hero h1", "html": "<h1>New Title</h1>", "inner": false}]}'

# Update/remove attributes (null to remove)
agentctl run html/edit --input '{"path": "index.html", "operations": [{"type": "update_attr", "selector": "a[href^=\"http\"]", "attributes": {"target": "_blank", "onclick": null}}]}'

# Delete elements
agentctl run html/edit --input '{"path": "index.html", "operations": [{"type": "delete", "selector": ".deprecated"}]}'

# Wrap elements
agentctl run html/edit --input '{"path": "index.html", "operations": [{"type": "wrap", "selector": "img", "html": "<figure></figure>"}]}'

# Unwrap (remove wrapper, keep children)
agentctl run html/edit --input '{"path": "index.html", "operations": [{"type": "unwrap", "selector": ".wrapper"}]}'

# Structure - DOM tree outline (read-only, ideal for large files)
agentctl run html/edit --input '{"path": "index.html", "operations": [{"type": "structure"}]}'
# Options: selector (start point), max_depth (default: 10)

# Extract - Get full HTML of matched elements (read-only)
agentctl run html/edit --input '{"path": "index.html", "operations": [{"type": "extract", "selector": ".hero"}]}'
# Options: include_parent (N levels), max_length (chars), limit (default: 10)
```

## Options

- `dry_run: true` - Preview changes without writing
- `nth: 1` - Target nth match only (1-indexed)
- `limit: 5` - Max elements to affect

## Multiple Operations

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "dry_run": true,
  "operations": [
    {"type": "delete", "selector": ".deprecated"},
    {"type": "update_attr", "selector": "img", "attributes": {"loading": "lazy"}}
  ]
}'
```

Full docs: `~/.agentctl/share/configs/skills/agentctl-html-edit/Skill.md`
