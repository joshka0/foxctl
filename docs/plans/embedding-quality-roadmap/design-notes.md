# Design Notes: Index Blocks & Symbol Documentation

> Key design decisions for the embedding quality roadmap.

---

## Two-Tier Documentation Approach

You **don't need `Index:` blocks on every symbol** for embeddings (or for repo-index expansion). Think of it as two tiers:

### Tier 1: Hub Symbols (have `Index:`)

These get the "full" doc signal:

* `Doc:` includes the `Index:` block
* Your repo graph can parse `Related/Keywords/Observability` into **soft edges**
* Great for intent-based expansion ("undo", "CAS", "workflow", etc.)

### Tier 2: Regular Symbols (only short GoDoc, or no doc)

These still embed fine - you just feed a smaller, consistent representation.

---

## Index Syntax Contract

- Structured `Index:` blocks are canonical and produce edges.
- Single-line `Index: term1, term2` is keywords-only (no Related/Flow edges).
- Keep the Index block as the last part of the doc comment.

---

## Embedding Input Safety (Secrets)

- Secret-scan embedding inputs (docs + code) before enqueue.
- Default policy should redact and emit an observability event.
- Block mode should prevent enqueue when policy requires it.

---

## Normalization Single Source

- All doc/code normalization should live in `internal/indexing/embeddingtext`.
- Avoid duplicate normalize helpers in feature-specific packages.

---

## For Embeddings: What to Do with Small Comments

In your symbol embedding text builder, always include:

1. **Identity**: kind, name, file, signature
2. **Doc** (if present): even a one-liner helps
3. **Code excerpt** (capped): so code semantics still work when doc is tiny/absent

So short comments are "nice to have", not required.

### If Doc is Missing or Too Short

Add one of these *light* fallbacks (pick one):

**Option A (simple, recommended): Include a deterministic "summary line"**

* Derived from signature + kind + file path (no LLM)
* Example: `Summary: function Login(ctx, in) error (auth/login.go)`

**Option B (context boost): Include the file summary only when doc is empty**

* `FileSummary: <1 sentence from file_summary entry>`
* **IMPORTANT**: Only do this when doc is empty, otherwise you'll cause churn (file summary changes would re-embed every symbol)

That way, helpers without doc still become searchable by the file's intent.

---

## For Repo Index Expansion: What to Do with Small Comments

Small comments should mostly act as:

* **Ranking signal** (FTS + embeddings on `nodes.doc` / `nodes.summary`)
* **NOT** as explicit edges

Only `Index:` blocks become edges (e.g., `DOC_RELATED`, `HAS_KEYWORD`) because they're structured and reliable.

If you want small comments to also influence expansion, the lightest safe addition is:

* Allow **one optional line** like `// Related: Foo, Bar` (no full Index block)
* Keep this optional and only for cases where you *really* want curated navigation without the full block

---

## Practical Rule of Thumb

| Symbol Type | Documentation Level | Result |
|-------------|---------------------|--------|
| Orchestrators / entrypoints / transaction boundaries / high fan-in/out | Full `Index:` block | Soft edges created, intent-based expansion |
| Exported symbols and important helpers | One-liner GoDoc | Embeds well, FTS searchable |
| Internal helpers | No doc acceptable | Embeddings still work via signature + code excerpt |

---

## BuildSymbolEmbeddingText: Handling Empty Docs

When doc is empty, add the file summary fallback while keeping embed-digest churn low:

```go
func BuildSymbolEmbeddingText(meta SymbolMeta, codeSnippet string, fileSummary string) string {
    var parts []string
    
    // Always include identity
    parts = append(parts, fmt.Sprintf("Kind: %s", meta.Kind))
    parts = append(parts, fmt.Sprintf("Name: %s", meta.Name))
    parts = append(parts, fmt.Sprintf("File: %s", meta.File))
    
    if meta.Signature != "" {
        parts = append(parts, fmt.Sprintf("Signature: %s", meta.Signature))
    }
    
    // Doc handling with fallback
    doc := strings.TrimSpace(meta.Doc)
    if doc != "" {
        parts = append(parts, fmt.Sprintf("Doc: %s", NormalizeDoc(doc)))
    } else if fileSummary != "" {
        // Only add file summary when doc is empty (prevents churn)
        parts = append(parts, fmt.Sprintf("FileSummary: %s", fileSummary))
    } else {
        // Deterministic fallback from signature
        parts = append(parts, fmt.Sprintf("Summary: %s %s (%s)", 
            meta.Kind, meta.Name, filepath.Base(meta.File)))
    }
    
    // Code excerpt (capped)
    if codeSnippet != "" {
        excerpt := capCodeExcerpt(codeSnippet, 500)
        parts = append(parts, fmt.Sprintf("Code:\n%s", excerpt))
    }
    
    return strings.Join(parts, "\n")
}
```

### Digest Contract (v1)

Compute `content_digest` from structured components rather than hashing the full embedding text. This avoids whitespace-sensitive churn and keeps behavior deterministic across languages.

Recommended components (symbol embeddings):

1. **docDigest** = `sha256:` of `NormalizeDoc(doc + indexBlock)`
2. **bodyDigest** = existing symbol body digest
3. **sigHash** = stable signature hash (if available)
4. **callsDigest** = `sha256:` of sorted+deduped calls (optional)
5. **fallbackDigest** = file summary digest used only when doc is empty

Then:

```
content_digest = sha256("v1|scope=symbol|model="+model+"|doc="+docDigest+"|sig="+sigHash+"|body="+bodyDigest+"|calls="+callsDigest+"|fallback="+fallbackDigest)
```

Re-embed only when any component changes. This keeps doc-only edits, code edits, and relationship edits properly isolated and stable.
