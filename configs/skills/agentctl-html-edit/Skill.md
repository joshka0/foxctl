---
name: agentctl HTML Edit
description: Precise DOM-aware HTML editing using CSS selectors for targeted modifications.
---

# HTML Editing with agentctl

Edit HTML files precisely using CSS selectors to target specific elements. Unlike text-based search/replace, this skill understands DOM structure for reliable modifications.

## Query Elements (select)

Find elements and get info without modifying:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{"type": "select", "selector": "nav.main-nav > ul > li"}]
}'
```

Returns:

- Matched element count
- Tag names and IDs
- Class attributes
- Text content preview

## Insert Content

Insert HTML at specific positions relative to matched elements:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "dry_run": true,
  "operations": [{
    "type": "insert",
    "selector": "#content",
    "position": "append",
    "html": "<section class=\"new-section\"><h2>New Section</h2></section>"
  }]
}'
```

Positions:

- `before`: Insert as previous sibling
- `after`: Insert as next sibling
- `prepend`: Insert as first child
- `append`: Insert as last child

## Replace Content

Replace element content or the entire element:

```bash
# Replace inner HTML only
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{
    "type": "replace",
    "selector": ".hero",
    "inner": true,
    "html": "<h1>New Title</h1><p>New content</p>"
  }]
}'

# Replace entire element
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{
    "type": "replace",
    "selector": ".old-button",
    "inner": false,
    "html": "<button class=\"new-button\">Click Me</button>"
  }]
}'
```

## Update Attributes

Add, modify, or remove attributes:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{
    "type": "update_attr",
    "selector": "a[href^=\"http\"]",
    "attributes": {
      "target": "_blank",
      "rel": "noopener noreferrer",
      "onclick": null
    }
  }]
}'
```

Use `null` as the value to remove an attribute.

## Delete Elements

Remove matched elements from the DOM:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{"type": "delete", "selector": ".deprecated-banner, [data-legacy]"}]
}'
```

## Wrap Elements

Wrap matched elements with new HTML:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{
    "type": "wrap",
    "selector": "img.gallery",
    "html": "<figure class=\"image-figure\"></figure>"
  }]
}'
```

## Unwrap Elements

Remove wrapper element but keep its children:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{"type": "unwrap", "selector": ".unnecessary-wrapper"}]
}'
```

## Structure (DOM Tree Outline)

Get a tree outline of the DOM structure - ideal for understanding large HTML files without reading all the content:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{"type": "structure"}]
}'
```

Returns a tree like:

```
html
├── head
│   ├── title
│   └── meta (3)
└── body
    ├── header#main-header
    │   └── nav.main-nav
    ├── main#content
    │   ├── section.hero
    │   ├── section.features
    │   │   └── div.feature-card (4)
    │   └── section.pricing
    └── footer#footer
```

Options:

- `selector`: Start from a specific element instead of document root
- `max_depth`: Limit tree depth (default: 10)

```bash
# Get structure starting from a specific section
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{"type": "structure", "selector": "main#content", "max_depth": 3}]
}'
```

## Extract (Get Full HTML Content)

Get the full HTML content of matched elements - useful for viewing specific sections of large files:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{"type": "extract", "selector": ".hero"}]
}'
```

Returns the complete outer HTML of each matched element.

Options:

- `include_parent`: Include N parent levels for context
- `max_length`: Truncate each match to N characters (default: 10000)
- `limit`: Maximum elements to extract (default: 10)

```bash
# Extract with parent context
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{
    "type": "extract",
    "selector": ".hero h1",
    "include_parent": 2
  }]
}'
```

## Targeting Specific Matches

Use `nth` to target a specific match (1-indexed):

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{"type": "delete", "selector": "p", "nth": 1}]
}'
```

Use `limit` to cap the number of affected elements:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "operations": [{
    "type": "update_attr",
    "selector": "a",
    "limit": 5,
    "attributes": {"target": "_blank"}
  }]
}'
```

## Multiple Operations

Chain multiple operations in a single call:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "dry_run": true,
  "operations": [
    {"type": "delete", "selector": ".deprecated"},
    {"type": "update_attr", "selector": "img", "attributes": {"loading": "lazy"}},
    {"type": "insert", "selector": "head", "position": "append", "html": "<meta name=\"robots\" content=\"index\">"}
  ]
}'
```

## Dry Run Mode

Preview all changes without modifying the file:

```bash
agentctl run html/edit --input '{
  "path": "index.html",
  "dry_run": true,
  "operations": [...]
}'
```

Returns a unified diff showing exactly what would change.

## CSS Selector Examples

| Selector | Matches |
|----------|---------|
| `#header` | Element with id="header" |
| `.nav-item` | Elements with class="nav-item" |
| `div.content` | div elements with class="content" |
| `div > p` | p elements that are direct children of div |
| `a[href^="http"]` | Links with href starting with "http" |
| `input[type="text"]` | Text input elements |
| `ul li:first-child` | First li in each ul |
| `.card, .panel` | Elements with either class |

## Notes

- Uses DOM parsing for precision - output HTML will be normalized
- For exact text preservation, use `text/replace` instead
- Complex selectors like `:nth-child()`, `:not()`, `[attr*=val]` are supported
