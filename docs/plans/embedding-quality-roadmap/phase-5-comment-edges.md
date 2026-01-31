# Phase 5: Comment-driven Expansion

> **Status**: Planning
> **Dependencies**: Phase 4 (Repo Graph Index v1)
> **Estimated PRs**: 4
> **Priority**: Medium - Enables semantic navigation beyond hard edges

## Overview

Phase 5 extends the repo graph with "soft edges" derived from structured documentation comments. By parsing `Index:` blocks in GoDoc/JSDoc, we create concept nodes and weighted edges that enable semantic navigation.

**Goals:**
- Parse structured `Index:` blocks from doc comments
- Create concept nodes for keywords, output fields, resources, events
- Generate weighted "soft edges" linking symbols to concepts
- Enable query-aware expansion that activates relevant edges

**Non-Goals (this phase):**
- Automatic comment generation (future)
- Cross-repository concept linking
- GUI visualization (Phase 7)

---

## Index Block Format

Before diving into PRs, here's the `Index:` block format we'll parse:

```go
// ProcessTransaction handles payment processing.
//
// Index:
//   Purpose: Validate and execute payment transactions
//   Flow: ValidateCard → ChargeProcessor → RecordTransaction
//   SideEffects: Writes to payments table, emits payment.completed event
//   Observability: payment.processing, payment.completed, payment.failed
//   Related: RefundTransaction, PaymentValidator, CardProcessor
//   Keywords: payment, billing, charge, stripe
func ProcessTransaction(ctx context.Context, req PaymentRequest) (*PaymentResult, error)
```

Field lines may be indented or use list bullets (e.g., `// - Purpose: ...`). Indentation is not required because GoDoc strips it.

**Supported fields:**
| Field | Description | Edge Type Created |
|-------|-------------|-------------------|
| `Purpose` | One-line description | Stored in node summary |
| `Flow` | Call sequence (→ separated) | `DOC_FLOW` edges |
| `SideEffects` | External effects | `TOUCHES_RESOURCE` edges |
| `Observability` | Event names | `EMITS_EVENT` edges |
| `Related` | Related symbols | `DOC_RELATED` edges |
| `Keywords` | Search terms | `HAS_KEYWORD` edges |

**Multi-line fields:** v1 expects single-line values. v2 may allow bullet continuation under a field (e.g., `SideEffects:` followed by indented `-` lines).

### Guardrails (to prevent concept explosion)

- Cap per symbol: Keywords (<=12), Events (<=8), Resources (<=8)
- Normalize tokens (lowercase, trim, dedupe)
- Reserve prefixes (e.g., `kw:`, `event:`, `res:`, `field:`)
- Malformed blocks are ignored (never crash)

### Soft Edge Policy

- Hard edges are always included
- Soft edges only activate when query tokens match concept nodes
- Soft edge weights < 1.0; default traversal threshold 0.6-0.7

---

## PR 5.1: Parse Index Blocks into Structured Metadata

### Summary

Create a parser that extracts `Index:` blocks from doc comments and produces structured metadata for storage in `nodes.meta_json`.

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/indexing/repoindex/parser/index_block.go` | Create | Index block parser |
| `internal/indexing/repoindex/parser/types.go` | Create | Parsed metadata types |
| `internal/indexing/repoindex/parser/parser_test.go` | Create | Parser tests with fixtures |

### Implementation Details

**Types (types.go):**
```go
package parser

// IndexMeta represents parsed Index: block content
type IndexMeta struct {
    Purpose       string   `json:"purpose,omitempty"`
    Flow          []string `json:"flow,omitempty"`           // Symbol names in call order
    SideEffects   []string `json:"side_effects,omitempty"`   // Resource descriptions
    Observability []string `json:"observability,omitempty"`  // Event names
    Related       []string `json:"related,omitempty"`        // Symbol names
    Keywords      []string `json:"keywords,omitempty"`       // Search terms
    Raw           string   `json:"raw,omitempty"`            // Original Index: block
}

// Resource represents a parsed side effect
type Resource struct {
    Type string // "db", "file", "network", "event", "cache"
    Name string // Table name, file path, URL, event name
}

// ParsedDoc contains both raw doc and extracted metadata
type ParsedDoc struct {
    Summary   string     // First line/paragraph
    Doc       string     // Full doc without Index: block
    IndexMeta *IndexMeta // Parsed Index: block (nil if absent)
}
```

**Parser (index_block.go):**
```go
package parser

import (
    "regexp"
    "strings"
)

var (
    indexLineRe = regexp.MustCompile(`^\s*Index:\s*(.*)$`)
    fieldRe     = regexp.MustCompile(`^\s*(?:[-*]\s*)?(\w+):\s*(.+)\s*$`)
)

// ParseDoc extracts structured metadata from a doc comment.
// Single-line `Index: term1, term2` is treated as Keywords-only.
func ParseDoc(doc string) ParsedDoc {
    result := ParsedDoc{
        Doc: doc,
    }

    // Extract first sentence/paragraph as summary
    result.Summary = extractSummary(doc)

    lines := strings.Split(doc, "\n")
    indexLine := -1
    inline := ""

    // Find Index: line (ast.CommentGroup.Text() removes indentation)
    for i, line := range lines {
        if match := indexLineRe.FindStringSubmatch(line); match != nil {
            indexLine = i
            inline = strings.TrimSpace(match[1])
            break
        }
    }
    if indexLine == -1 {
        return result
    }

    // Keywords-only shorthand when content is on the same line
    if inline != "" {
        result.Doc = strings.TrimSpace(strings.Join(append(lines[:indexLine], lines[indexLine+1:]...), "\n"))
        result.IndexMeta = &IndexMeta{
            Raw:      lines[indexLine],
            Keywords: parseCommaList(inline),
        }
        return result
    }

    // Structured block: consume following non-empty lines until a blank line
    end := indexLine + 1
    for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
        end++
    }

    blockLines := lines[indexLine+1 : end]
    result.Doc = strings.TrimSpace(strings.Join(append(lines[:indexLine], lines[end:]...), "\n"))

    meta := &IndexMeta{
        Raw: strings.Join(lines[indexLine:end], "\n"),
    }

    for _, line := range blockLines {
        fieldMatch := fieldRe.FindStringSubmatch(line)
        if fieldMatch == nil {
            continue
        }

        field := fieldMatch[1]
        value := strings.TrimSpace(fieldMatch[2])

        switch field {
        case "Purpose":
            meta.Purpose = value
        case "Flow":
            meta.Flow = parseArrowList(value)
        case "SideEffects":
            meta.SideEffects = parseCommaList(value)
        case "Observability":
            meta.Observability = parseCommaList(value)
        case "Related":
            meta.Related = parseCommaList(value)
        case "Keywords":
            meta.Keywords = parseCommaList(value)
        }
    }

    result.IndexMeta = meta
    return result
}

// parseArrowList splits "A → B → C" into ["A", "B", "C"]
func parseArrowList(s string) []string {
    parts := strings.Split(s, "→")
    result := make([]string, 0, len(parts))
    for _, p := range parts {
        if t := strings.TrimSpace(p); t != "" {
            result = append(result, t)
        }
    }
    return result
}

// parseCommaList splits "a, b, c" into ["a", "b", "c"]
func parseCommaList(s string) []string {
    parts := strings.Split(s, ",")
    result := make([]string, 0, len(parts))
    for _, p := range parts {
        if t := strings.TrimSpace(p); t != "" {
            result = append(result, t)
        }
    }
    return result
}

// extractSummary gets the first sentence or paragraph
func extractSummary(doc string) string {
    // First line up to period, or first paragraph
    lines := strings.SplitN(doc, "\n\n", 2)
    if len(lines) == 0 {
        return ""
    }
    
    first := strings.TrimSpace(lines[0])
    if idx := strings.Index(first, "."); idx > 0 && idx < 100 {
        return first[:idx+1]
    }
    
    if len(first) > 150 {
        return first[:147] + "..."
    }
    return first
}
```

**Integration with builder:**
```go
// In builder/nodes.go

func extractSymbolNode(pkg *packages.Package, decl ast.Node, filename string) Node {
    doc := extractDocComment(decl)
    parsed := parser.ParseDoc(doc)
    
    node := Node{
        // ... existing fields ...
        Doc:     parsed.Doc,
        Summary: parsed.Summary,
    }
    
    // Store Index meta as JSON
    if parsed.IndexMeta != nil {
        metaJSON, _ := json.Marshal(parsed.IndexMeta)
        node.MetaJSON = string(metaJSON)
    }
    
    return node
}
```

### Testing Strategy

1. **Parser unit tests**:
   - Doc with no Index: block → nil IndexMeta
   - Doc with full Index: block → all fields parsed
   - Arrow list parsing (Flow)
   - Comma list parsing (Keywords, Related)
   - Summary extraction edge cases

2. **Fixture-based tests**:
   - `testdata/doc_simple.txt` → expected output
   - `testdata/doc_complex.txt` → expected output
   - `testdata/doc_malformed.txt` → graceful handling

### Acceptance Criteria

- [ ] Parser extracts all Index: block fields
- [ ] Summary extracted from first sentence/paragraph
- [ ] Doc field contains original without Index: block
- [ ] Malformed Index: blocks handled gracefully
- [ ] MetaJSON stored in node during build
- [ ] All tests pass including edge cases

---

## PR 5.2: Create Concept Nodes + Comment Edges

### Summary

Generate concept nodes from parsed metadata and create weighted edges linking symbols to concepts. This enables semantic navigation like "show me all code that emits payment events".

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/indexing/repoindex/builder/concepts.go` | Create | Concept node creation |
| `internal/indexing/repoindex/builder/comment_edges.go` | Create | Comment-derived edge generation |
| `internal/indexing/repoindex/types.go` | Modify | Add concept-related constants |
| `internal/indexing/repoindex/builder/concepts_test.go` | Create | Tests |

### Schema Additions

New edge types in `types.go`:
```go
const (
    // ... existing edge types ...
    
    // Comment-derived edges (soft edges, weight < 1.0)
    EdgeHasKeyword     EdgeType = "HAS_KEYWORD"      // symbol → <repo_key>::kw:token
    EdgeHasOutputField EdgeType = "HAS_OUTPUT_FIELD" // symbol → <repo_key>::field:name
    EdgeTouchesResource EdgeType = "TOUCHES_RESOURCE" // symbol → <repo_key>::res:type:name
    EdgeEmitsEvent     EdgeType = "EMITS_EVENT"       // symbol → <repo_key>::event:name
    EdgeDocRelated     EdgeType = "DOC_RELATED"       // symbol → symbol (from Related:)
    EdgeDocFlow        EdgeType = "DOC_FLOW"          // symbol → symbol (from Flow:)
)

// Concept node prefixes (repo-key namespacing added at ID creation)
const (
    ConceptKeyword  = "kw:"     // <repo_key>::kw:payment
    ConceptField    = "field:"  // <repo_key>::field:user_id
    ConceptResource = "res:"    // <repo_key>::res:db:payments, res:cache:sessions
    ConceptEvent    = "event:"  // <repo_key>::event:payment.completed
)
```

### Implementation Details

**Concept creation (concepts.go):**
```go
package builder

// ConceptNode creates a concept node with the given prefix and name
func ConceptNode(repoKey, prefix, name string) Node {
    id := repoKey + "::" + prefix + normalizeConceptName(name)
    return Node{
        ID:   id,
        Kind: NodeConcept,
        Name: name,
    }
}

// normalizeConceptName lowercases and removes special characters
func normalizeConceptName(name string) string {
    name = strings.ToLower(name)
    name = strings.ReplaceAll(name, " ", "_")
    // Keep alphanumeric, underscore, dot, colon
    return regexp.MustCompile(`[^a-z0-9_.:]+`).ReplaceAllString(name, "")
}

// extractConceptNodes extracts all concept nodes from IndexMeta
func extractConceptNodes(repoKey string, meta *parser.IndexMeta) []Node {
    var nodes []Node
    seen := make(map[string]bool)
    
    add := func(n Node) {
        if !seen[n.ID] {
            seen[n.ID] = true
            nodes = append(nodes, n)
        }
    }
    
    // Keywords → <repo_key>::kw:token
    for _, kw := range meta.Keywords {
        add(ConceptNode(repoKey, ConceptKeyword, kw))
    }
    
    // Observability → <repo_key>::event:name
    for _, ev := range meta.Observability {
        add(ConceptNode(repoKey, ConceptEvent, ev))
    }
    
    // SideEffects → <repo_key>::res:type:name
    for _, se := range meta.SideEffects {
        res := parseResourceString(se)
        add(ConceptNode(repoKey, ConceptResource, res.Type+":"+res.Name))
    }
    
    return nodes
}

// parseResourceString extracts resource type and name from descriptions like:
// "Writes to payments table" → {Type: "db", Name: "payments"}
// "Emits payment.completed event" → {Type: "event", Name: "payment.completed"}
func parseResourceString(s string) Resource {
    s = strings.ToLower(s)
    
    // Patterns for common resources
    patterns := []struct {
        re   *regexp.Regexp
        typ  string
        name int // capture group for name
    }{
        {regexp.MustCompile(`writes?\s+to\s+(\w+)\s+table`), "db", 1},
        {regexp.MustCompile(`reads?\s+from\s+(\w+)\s+table`), "db", 1},
        {regexp.MustCompile(`emits?\s+([\w.]+)\s+event`), "event", 1},
        {regexp.MustCompile(`calls?\s+([\w.]+)\s+api`), "network", 1},
        {regexp.MustCompile(`caches?\s+in\s+(\w+)`), "cache", 1},
        {regexp.MustCompile(`writes?\s+to\s+([\w/]+)`), "file", 1},
    }
    
    for _, p := range patterns {
        if m := p.re.FindStringSubmatch(s); m != nil {
            return Resource{Type: p.typ, Name: m[p.name]}
        }
    }
    
    // Fallback: use whole string as generic resource
    return Resource{Type: "unknown", Name: normalizeConceptName(s)}
}
```

**Comment edge creation (comment_edges.go):**
```go
package builder

const (
    WeightHard = 1.0   // Code-derived edges (CALLS, CONTAINS, etc.)
    WeightSoft = 0.7   // Comment-derived, unverified
    WeightDoc  = 0.5   // Doc mentions without verification
)

// extractCommentEdges creates edges from IndexMeta
func extractCommentEdges(repoKey, symbolID string, meta *parser.IndexMeta, resolver SymbolResolver) []Edge {
    var edges []Edge
    
    // Keywords → HAS_KEYWORD
    for _, kw := range meta.Keywords {
        edges = append(edges, Edge{
            Src:    symbolID,
            Dst:    repoKey + "::" + ConceptKeyword + normalizeConceptName(kw),
            Type:   EdgeHasKeyword,
            Weight: WeightSoft,
        })
    }
    
    // Observability → EMITS_EVENT
    for _, ev := range meta.Observability {
        edges = append(edges, Edge{
            Src:    symbolID,
            Dst:    repoKey + "::" + ConceptEvent + normalizeConceptName(ev),
            Type:   EdgeEmitsEvent,
            Weight: WeightSoft,
        })
    }
    
    // SideEffects → TOUCHES_RESOURCE
    for _, se := range meta.SideEffects {
        res := parseResourceString(se)
        edges = append(edges, Edge{
            Src:    symbolID,
            Dst:    repoKey + "::" + ConceptResource + res.Type + ":" + normalizeConceptName(res.Name),
            Type:   EdgeTouchesResource,
            Weight: WeightSoft,
        })
    }
    
    // Related → DOC_RELATED (try to resolve to real symbol)
    for _, rel := range meta.Related {
        dst := resolver.Resolve(rel)
        if dst == "" {
            // Couldn't resolve, link to concept node
            dst = repoKey + "::" + ConceptKeyword + normalizeConceptName(rel)
        }
        edges = append(edges, Edge{
            Src:    symbolID,
            Dst:    dst,
            Type:   EdgeDocRelated,
            Weight: WeightDoc,
        })
    }
    
    // Flow → DOC_FLOW (ordered sequence)
    for i := 0; i < len(meta.Flow)-1; i++ {
        srcName := meta.Flow[i]
        dstName := meta.Flow[i+1]
        
        srcID := resolver.Resolve(srcName)
        dstID := resolver.Resolve(dstName)
        
        if srcID != "" && dstID != "" {
            edges = append(edges, Edge{
                Src:    srcID,
                Dst:    dstID,
                Type:   EdgeDocFlow,
                Weight: WeightDoc,
                Meta:   json.RawMessage(fmt.Sprintf(`{"flow_index":%d}`, i)),
            })
        }
    }
    
    return edges
}

// SymbolResolver attempts to resolve symbol names to node IDs
type SymbolResolver interface {
    Resolve(name string) string // Returns "" if not found
}

// PackageSymbolResolver resolves symbols within a package
type PackageSymbolResolver struct {
    pkg       string
    symbols   map[string]string // name → ID
    globalIdx map[string]string // fallback: name → ID across all packages
}

func (r *PackageSymbolResolver) Resolve(name string) string {
    // Try package-local first
    if id, ok := r.symbols[name]; ok {
        return id
    }
    // Try global
    if id, ok := r.globalIdx[name]; ok {
        return id
    }
    return ""
}
```

### Testing Strategy

1. **Concept node tests**:
   - Keywords create `kw:` nodes
   - Events create `event:` nodes
   - Side effects parsed into `res:` nodes
   - Normalization handles edge cases

2. **Edge creation tests**:
   - All edge types created with correct weights
   - Symbol resolution finds package-local symbols
   - Unresolved Related creates concept node fallback
   - Flow creates ordered edges

3. **Integration test**:
   - Build package with documented symbols
   - Verify concept nodes in database
   - Verify comment edges link correctly

### Acceptance Criteria

- [ ] Concept nodes created for keywords, events, resources
- [ ] HAS_KEYWORD edges link symbols to keyword concepts
- [ ] EMITS_EVENT edges link symbols to event concepts
- [ ] TOUCHES_RESOURCE edges link to resource concepts
- [ ] DOC_RELATED edges attempt symbol resolution
- [ ] DOC_FLOW edges maintain order from Flow: field
- [ ] All comment edges have weight < 1.0
- [ ] Concept node IDs are normalized consistently

---

## PR 5.3: Query-aware Expansion Using Comment Edges

### Summary

Enhance the expand algorithm to conditionally include comment edges based on query tokens. This enables semantic queries like "find code related to payment events" to traverse EMITS_EVENT edges.

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/indexing/repoindex/query/smart_expand.go` | Create | Query-aware expansion |
| `internal/indexing/repoindex/query/trail.go` | Create | Expansion trail formatting |
| `internal/indexing/repoindex/query/smart_expand_test.go` | Create | Tests |

### Implementation Details

**Smart expand (smart_expand.go):**
```go
package query

// SmartExpandOptions extends ExpandOptions with query context
type SmartExpandOptions struct {
    ExpandOptions
    
    Query           string   // Natural language query
    QueryTokens     []string // Extracted tokens (auto-generated if Query set)
    IncludeComments bool     // Always include comment edges (default false)
    WeightThreshold float64  // Min weight to traverse (default 0.5)
}

// SmartExpand performs query-aware graph expansion
func (e *Engine) SmartExpand(ctx context.Context, seeds []string, opts SmartExpandOptions) (SmartExpandResult, error) {
    // Extract tokens from query if not provided
    if opts.Query != "" && len(opts.QueryTokens) == 0 {
        opts.QueryTokens = tokenizeQuery(opts.Query)
    }
    
    // Build set of relevant concept IDs from query
    relevantConcepts := e.findRelevantConcepts(ctx, opts.QueryTokens)
    
    // Determine which edge types to include
    edgeTypes := opts.EdgeTypes
    if len(edgeTypes) == 0 {
        // Default: hard edges only
        edgeTypes = []EdgeType{EdgeContains, EdgeImports, EdgeCalls, EdgeRefersTo}
    }
    
    // Add comment edges if query matches concepts
    if opts.IncludeComments || len(relevantConcepts) > 0 {
        edgeTypes = append(edgeTypes,
            EdgeHasKeyword,
            EdgeEmitsEvent,
            EdgeTouchesResource,
            EdgeDocRelated,
            EdgeDocFlow,
        )
    }
    
    // Run expansion with edge filtering
    result, err := e.expandWithFilter(ctx, seeds, edgeTypes, opts, relevantConcepts)
    if err != nil {
        return SmartExpandResult{}, err
    }
    
    return SmartExpandResult{
        Nodes:       result.Nodes,
        Edges:       result.Edges,
        Trail:       result.Trail,
        QueryTokens: opts.QueryTokens,
        Concepts:    relevantConcepts,
    }, nil
}

// findRelevantConcepts searches for concept nodes matching query tokens
func (e *Engine) findRelevantConcepts(ctx context.Context, tokens []string) map[string]bool {
    concepts := make(map[string]bool)
    repoKey := e.store.RepoKey()

    for _, token := range tokens {
        // Check for keyword concepts
        kwID := repoKey + "::" + ConceptKeyword + normalizeConceptName(token)
        if _, err := e.store.GetNode(ctx, kwID); err == nil {
            concepts[kwID] = true
        }

        // Check for event concepts
        eventID := repoKey + "::" + ConceptEvent + normalizeConceptName(token)
        if _, err := e.store.GetNode(ctx, eventID); err == nil {
            concepts[eventID] = true
        }

        // Check for resource concepts (partial match)
        resourceNodes, _ := e.store.SearchFTS(ctx, "res:*"+token+"*", SearchOptions{
            Kinds: []NodeKind{NodeConcept},
            Limit: 10,
        })
        for _, n := range resourceNodes {
            concepts[n.ID] = true
        }
    }
    
    return concepts
}

// expandWithFilter runs BFS with edge weight and concept filtering
func (e *Engine) expandWithFilter(
    ctx context.Context,
    seeds []string,
    edgeTypes []EdgeType,
    opts SmartExpandOptions,
    relevantConcepts map[string]bool,
) (ExpandResult, error) {
    visited := make(map[string]bool)
    var result ExpandResult
    trail := NewTrail()
    repoKey := e.store.RepoKey()

    queue := seeds
    for depth := 0; depth < opts.Depth && len(queue) > 0 && len(result.Nodes) < opts.Budget; depth++ {
        var nextQueue []string
        
        for _, nodeID := range queue {
            if visited[nodeID] {
                continue
            }
            visited[nodeID] = true
            
            node, err := e.store.GetNode(ctx, nodeID)
            if err != nil {
                continue
            }
            result.Nodes = append(result.Nodes, node)
            
            edges, _ := e.store.GetEdges(ctx, nodeID, EdgeOptions{
                Types:     edgeTypes,
                Direction: opts.Direction,
            })
            
            for _, edge := range edges {
                // Filter by weight threshold
                if edge.Weight < opts.WeightThreshold {
                    continue
                }
                
                // For comment edges to concepts, only follow if concept is relevant
                if isCommentEdge(edge.Type) && isConcept(repoKey, edge.Dst) {
                    if !opts.IncludeComments && !relevantConcepts[edge.Dst] {
                        continue
                    }
                }
                
                result.Edges = append(result.Edges, edge)
                
                target := edge.Dst
                if opts.Direction == DirIn {
                    target = edge.Src
                }
                
                if !visited[target] {
                    nextQueue = append(nextQueue, target)
                    trail.Add(nodeID, edge.Type, target, edge.Weight)
                }
            }
        }
        
        queue = nextQueue
    }
    
    result.Trail = trail.Format()
    return result, nil
}

// tokenizeQuery extracts searchable tokens from a query
func tokenizeQuery(query string) []string {
    // Remove common words, lowercase, split
    query = strings.ToLower(query)
    words := strings.Fields(query)
    
    stopWords := map[string]bool{
        "the": true, "a": true, "an": true, "is": true, "are": true,
        "find": true, "show": true, "get": true, "all": true,
        "code": true, "that": true, "which": true, "where": true,
    }
    
    var tokens []string
    for _, w := range words {
        w = regexp.MustCompile(`[^a-z0-9_.]`).ReplaceAllString(w, "")
        if w != "" && !stopWords[w] {
            tokens = append(tokens, w)
        }
    }
    
    return tokens
}

func isCommentEdge(t EdgeType) bool {
    switch t {
    case EdgeHasKeyword, EdgeEmitsEvent, EdgeTouchesResource, EdgeDocRelated, EdgeDocFlow:
        return true
    }
    return false
}

func isConcept(repoKey, id string) bool {
    id = strings.TrimPrefix(id, repoKey+"::")
    return strings.HasPrefix(id, "kw:") ||
        strings.HasPrefix(id, "event:") ||
        strings.HasPrefix(id, "res:") ||
        strings.HasPrefix(id, "field:")
}
```

**Trail formatting (trail.go):**
```go
package query

// Trail tracks the expansion path for debugging/visualization
type Trail struct {
    steps []TrailStep
}

type TrailStep struct {
    From   string
    Edge   EdgeType
    To     string
    Weight float64
}

func NewTrail() *Trail {
    return &Trail{}
}

func (t *Trail) Add(from string, edge EdgeType, to string, weight float64) {
    t.steps = append(t.steps, TrailStep{from, edge, to, weight})
}

// Format returns human-readable trail strings
func (t *Trail) Format() []string {
    result := make([]string, len(t.steps))
    for i, s := range t.steps {
        weightStr := ""
        if s.Weight < 1.0 {
            weightStr = fmt.Sprintf(" (%.1f)", s.Weight)
        }
        result[i] = fmt.Sprintf("%s -[%s]-> %s%s", 
            shortenID(s.From), s.Edge, shortenID(s.To), weightStr)
    }
    return result
}

// FormatJSON returns structured trail for GUI consumption
func (t *Trail) FormatJSON() []map[string]interface{} {
    result := make([]map[string]interface{}, len(t.steps))
    for i, s := range t.steps {
        result[i] = map[string]interface{}{
            "from":   s.From,
            "edge":   s.Edge,
            "to":     s.To,
            "weight": s.Weight,
        }
    }
    return result
}

// shortenID makes node IDs more readable
func shortenID(id string) string {
    // "sym:github.com/user/repo/pkg:FuncName" → "pkg:FuncName"
    parts := strings.Split(id, "/")
    if len(parts) > 1 {
        return parts[len(parts)-1]
    }
    return id
}
```

**SmartExpandResult:**
```go
type SmartExpandResult struct {
    Nodes       []Node              `json:"nodes"`
    Edges       []Edge              `json:"edges"`
    Trail       []string            `json:"trail"`
    QueryTokens []string            `json:"query_tokens,omitempty"`
    Concepts    map[string]bool     `json:"concepts,omitempty"`
}
```

### Testing Strategy

1. **Token extraction tests**:
   - "find payment handlers" → ["payment", "handlers"]
   - Stop words removed
   - Punctuation handled

2. **Concept matching tests**:
   - Query "payment" matches `kw:payment`
   - Query "payment.completed" matches `event:payment.completed`

3. **Smart expand tests**:
   - Without query: only hard edges traversed
   - With matching query: comment edges to relevant concepts included
   - Weight threshold respected
   - Trail correctly formatted

4. **Integration test**:
   - Build graph with documented symbols
   - Run smart expand with query
   - Verify concept nodes reachable

### Acceptance Criteria

- [ ] Query tokens extracted from natural language
- [ ] Concept nodes identified from query tokens
- [ ] Hard edges always traversed
- [ ] Comment edges only traversed when query matches concepts
- [ ] Weight threshold filters low-confidence edges
- [ ] Trail shows expansion path with weights
- [ ] TrailJSON provides GUI-friendly format
- [ ] IncludeComments flag forces all comment edges

---

## PR 5.4 (Optional): Validate Comment Edges and Upgrade Weights

### Summary

Validate comment-derived edges by checking if the documented behavior exists in code. Upgrade edge weights when confirmed, keep low when unverified.

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/indexing/repoindex/validation/validate_edges.go` | Create | Edge validation logic |
| `internal/indexing/repoindex/validation/patterns.go` | Create | Code pattern matchers |
| `internal/indexing/repoindex/validation/validate_edges_test.go` | Create | Tests |

### Implementation Details

**Validation types:**
```go
package validation

type ValidationResult struct {
    EdgeSrc      string
    EdgeDst      string
    EdgeType     EdgeType
    OrigWeight   float64
    NewWeight    float64
    Verified     bool
    Evidence     string // Code location that confirms edge
}

type Validator struct {
    store   *store.Store
    fset    *token.FileSet
    pkgs    map[string]*packages.Package
}

func New(store *store.Store) *Validator
```

**Event validation (validate_edges.go):**
```go
// ValidateEmitsEvent checks if symbol actually emits the claimed event
func (v *Validator) ValidateEmitsEvent(ctx context.Context, edge Edge) ValidationResult {
    result := ValidationResult{
        EdgeSrc:    edge.Src,
        EdgeDst:    edge.Dst,
        EdgeType:   edge.Type,
        OrigWeight: edge.Weight,
        NewWeight:  edge.Weight,
    }

    repoKey := v.store.RepoKey()

    // Extract event name from concept node ID
    // "<repo_key>::event:payment.completed" → "payment.completed"
    dst := strings.TrimPrefix(edge.Dst, repoKey+"::")
    eventName := strings.TrimPrefix(dst, ConceptEvent)
    
    // Get symbol node
    node, err := v.store.GetNode(ctx, edge.Src)
    if err != nil {
        return result
    }
    
    // Load and parse the source file
    content, err := os.ReadFile(node.File)
    if err != nil {
        return result
    }
    
    // Search for event emission patterns
    patterns := []string{
        fmt.Sprintf(`observability\.NewEvent\(%q\)`, eventName),
        fmt.Sprintf(`Emit\([^,]*,\s*%q`, eventName),
        fmt.Sprintf(`event\s*[:=]\s*%q`, eventName),
    }
    
    for _, pattern := range patterns {
        re := regexp.MustCompile(pattern)
        if loc := re.FindIndex(content); loc != nil {
            result.Verified = true
            result.NewWeight = WeightHard
            result.Evidence = fmt.Sprintf("%s:%d", node.File, lineNumber(content, loc[0]))
            break
        }
    }
    
    return result
}

// ValidateTouchesResource checks for actual resource access
func (v *Validator) ValidateTouchesResource(ctx context.Context, edge Edge) ValidationResult {
    result := ValidationResult{
        EdgeSrc:    edge.Src,
        EdgeDst:    edge.Dst,
        EdgeType:   edge.Type,
        OrigWeight: edge.Weight,
        NewWeight:  edge.Weight,
    }
    
    repoKey := v.store.RepoKey()

    // Parse resource from concept ID
    // "<repo_key>::res:db:payments" → {type: "db", name: "payments"}
    dst := strings.TrimPrefix(edge.Dst, repoKey+"::")
    resStr := strings.TrimPrefix(dst, ConceptResource)
    parts := strings.SplitN(resStr, ":", 2)
    if len(parts) != 2 {
        return result
    }
    resType, resName := parts[0], parts[1]
    
    node, _ := v.store.GetNode(ctx, edge.Src)
    content, _ := os.ReadFile(node.File)
    
    var patterns []string
    switch resType {
    case "db":
        patterns = []string{
            fmt.Sprintf(`(?i)FROM\s+%s`, resName),
            fmt.Sprintf(`(?i)INTO\s+%s`, resName),
            fmt.Sprintf(`(?i)UPDATE\s+%s`, resName),
            fmt.Sprintf(`Table\(%q\)`, resName),
        }
    case "cache":
        patterns = []string{
            fmt.Sprintf(`cache\.Get\([^,]*%q`, resName),
            fmt.Sprintf(`cache\.Set\([^,]*%q`, resName),
        }
    case "file":
        patterns = []string{
            fmt.Sprintf(`os\.(Open|Create|Write).*%s`, regexp.QuoteMeta(resName)),
        }
    }
    
    for _, pattern := range patterns {
        re := regexp.MustCompile(pattern)
        if loc := re.FindIndex(content); loc != nil {
            result.Verified = true
            result.NewWeight = WeightHard
            result.Evidence = fmt.Sprintf("%s:%d", node.File, lineNumber(content, loc[0]))
            break
        }
    }
    
    return result
}

// ValidateDocRelated checks if Related symbol is actually called/referenced
func (v *Validator) ValidateDocRelated(ctx context.Context, edge Edge) ValidationResult {
    // Check if there's a CALLS or REFERS_TO edge between same nodes
    existingEdges, _ := v.store.GetEdges(ctx, edge.Src, EdgeOptions{
        Types:     []EdgeType{EdgeCalls, EdgeRefersTo},
        Direction: DirOut,
    })
    
    result := ValidationResult{
        EdgeSrc:    edge.Src,
        EdgeDst:    edge.Dst,
        EdgeType:   edge.Type,
        OrigWeight: edge.Weight,
        NewWeight:  edge.Weight,
    }
    
    for _, e := range existingEdges {
        if e.Dst == edge.Dst {
            result.Verified = true
            result.NewWeight = WeightHard
            result.Evidence = "Confirmed by CALLS/REFERS_TO edge"
            break
        }
    }
    
    return result
}
```

**Batch validation:**
```go
// ValidateAll validates all comment edges and updates weights
func (v *Validator) ValidateAll(ctx context.Context) ([]ValidationResult, error) {
    var results []ValidationResult
    
    // Get all comment edges
    edges, err := v.store.GetEdgesByTypes(ctx, []EdgeType{
        EdgeHasKeyword,
        EdgeEmitsEvent,
        EdgeTouchesResource,
        EdgeDocRelated,
    })
    if err != nil {
        return nil, err
    }
    
    for _, edge := range edges {
        var result ValidationResult
        
        switch edge.Type {
        case EdgeEmitsEvent:
            result = v.ValidateEmitsEvent(ctx, edge)
        case EdgeTouchesResource:
            result = v.ValidateTouchesResource(ctx, edge)
        case EdgeDocRelated:
            result = v.ValidateDocRelated(ctx, edge)
        default:
            continue
        }
        
        // Update edge weight if changed
        if result.NewWeight != result.OrigWeight {
            edge.Weight = result.NewWeight
            v.store.UpsertEdge(ctx, edge)
        }
        
        results = append(results, result)
    }
    
    return results, nil
}
```

### Testing Strategy

1. **Pattern matching tests**:
   - Event emission patterns detected
   - Database table patterns detected
   - Cache access patterns detected

2. **Validation tests**:
   - Verified edge gets weight=1.0
   - Unverified edge keeps original weight
   - Evidence string populated correctly

3. **Integration test**:
   - Build graph with documented + actual code
   - Run validation
   - Verify weights updated appropriately

### Acceptance Criteria

- [ ] EMITS_EVENT edges validated against `observability.NewEvent` calls
- [ ] TOUCHES_RESOURCE edges validated against SQL/cache patterns
- [ ] DOC_RELATED edges validated against CALLS/REFERS_TO edges
- [ ] Verified edges upgraded to weight=1.0
- [ ] Unverified edges keep weight < 1.0
- [ ] Evidence field shows code location
- [ ] Batch validation updates store in place

---

## Integration Notes

### Edge Weight Semantics

| Weight | Meaning | Source |
|--------|---------|--------|
| 1.0 | Hard edge, code-derived or validated | AST analysis, validation |
| 0.7 | Soft edge, comment-derived unverified | Index: block parsing |
| 0.5 | Doc mention, lower confidence | Related: without resolution |

### CLI Integration

```bash
# Build with comment edges
agentctl repoindex build --workspace . --go --comments

# Smart expand with query
agentctl repoindex expand "sym:pkg:Handler" \
  --query "payment events" \
  --depth 2

# Validate comment edges
agentctl repoindex validate --workspace .
```

### Future: GUI Integration (Phase 7)

The trail output enables visualization:
- Nodes as circles, sized by edge count
- Hard edges as solid lines
- Comment edges as dashed lines, opacity by weight
- Concept nodes as smaller squares
- Query-matched concepts highlighted

---

## Success Metrics

1. **Comment coverage**: 50%+ exported symbols have Index: blocks
2. **Validation rate**: 60%+ comment edges verified to weight=1.0
3. **Query relevance**: Smart expand returns semantically related code
4. **Trail clarity**: Expansion path understandable to developers

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Low Index: block adoption | Few comment edges | Provide scaffolding tools, lint rules |
| False positive validation | Wrong weight upgrades | Conservative patterns, manual review |
| Concept explosion | Too many concept nodes | Normalize aggressively, dedupe |
| Query tokenization naive | Poor concept matching | Add synonym expansion, embeddings |

---

## Appendix: Index Block Examples

### Function with Full Index Block

```go
// ProcessPayment handles payment processing for orders.
//
// Index:
//   Purpose: Validate payment details and charge customer card
//   Flow: ValidateCard → AuthorizeCharge → CapturePayment → RecordTransaction
//   SideEffects: Writes to payments table, calls Stripe API
//   Observability: payment.processing, payment.completed, payment.failed
//   Related: RefundPayment, PaymentValidator, StripeClient
//   Keywords: payment, billing, charge, stripe, order
func ProcessPayment(ctx context.Context, order *Order, card *Card) (*Payment, error)
```

### Type with Index Block

```go
// SessionManager handles user session lifecycle.
//
// Index:
//   Purpose: Create, validate, and revoke user sessions
//   SideEffects: Writes to sessions table, caches in Redis
//   Observability: session.created, session.validated, session.revoked
//   Related: UserAuth, TokenValidator, SessionStore
//   Keywords: session, auth, login, token, security
type SessionManager struct {
    store SessionStore
    cache *redis.Client
}
```

### Package with Index Block

```go
// Package auth implements authentication and authorization.
//
// Index:
//   Purpose: Handle user authentication, session management, and access control
//   Related: user, session, permission
//   Keywords: auth, login, logout, session, token, jwt, oauth
package auth
```
