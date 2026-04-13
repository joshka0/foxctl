# Phase 7: Tool + GUI Integration

> **Goal:** Expose repo graph capabilities through agent skills and provide visual navigation in the GUI.

## Overview

This phase bridges the repo graph infrastructure (Phase 4-5) to end users through:
1. Agent-facing skills for programmatic graph access
2. GUI panels for visual exploration and navigation

**UI behavior:**
- Cache search results per session
- Show expansion trail (why nodes were included)
- Edge-type toggles and weight threshold controls
- Open-in-file viewer integration

## Dependencies

- **Phase 4** (Repo graph) - Graph store and query engine must exist
- **Phase 5** (Comment edges) - Soft edges from `Index:` blocks enhance navigation

## PRs

---

### PR 7.1: Skill/Tool Surface for Repo Browsing

**Summary:**  
Create three skills that expose repo graph functionality to agents and the overseer workflow. These skills provide FTS search, graph expansion, and node detail retrieval.

**Files Touched:**
- `skills/repo_index_search/main.go` - FTS search skill
- `skills/repo_index_search/skill.yaml` - Skill manifest
- `skills/repo_index_expand/main.go` - Graph expansion skill
- `skills/repo_index_expand/skill.yaml` - Skill manifest
- `skills/repo_index_open/main.go` - Node detail skill
- `skills/repo_index_open/skill.yaml` - Skill manifest
- `internal/adapters/skillslib/repoindex/client.go` - Shared client for skills

**Implementation Details:**

#### repo_index/search

```go
// skills/repo_index_search/main.go

type Input struct {
    Query     string   `json:"query"`               // FTS query
    Workspace string   `json:"workspace,omitempty"` // defaults to cwd
    Limit     int      `json:"limit,omitempty"`     // default 20
    NodeTypes []string `json:"node_types,omitempty"` // file, package, func, type, etc.
}

type Output struct {
    Results []SearchResult `json:"results"`
    Query   string         `json:"query"`
    Total   int            `json:"total"`
}

type SearchResult struct {
    NodeID      string   `json:"node_id"`      // unique node identifier
    Kind        string   `json:"kind"`         // file, package, func, type
    Name        string   `json:"name"`         // symbol/file name
    Path        string   `json:"path"`         // file path
    Line        int      `json:"line"`         // start line (0 for files)
    Summary     string   `json:"summary"`      // doc summary or snippet
    Score       float64  `json:"score"`        // FTS relevance score
    FanIn       int      `json:"fan_in"`       // incoming edges count
    FanOut      int      `json:"fan_out"`      // outgoing edges count
}
```

**Usage:**
```bash
agentctl run repo_index/search --input '{"query": "authentication handler"}'
agentctl run repo_index/search --input '{"query": "Store", "node_types": ["type"], "limit": 10}'
```

#### repo_index/expand

```go
// skills/repo_index_expand/main.go

type Input struct {
    Seed      string   `json:"seed"`                // node ID to expand from
    Workspace string   `json:"workspace,omitempty"`
    EdgeTypes []string `json:"edge_types,omitempty"` // CALLS, REFERS_TO, CONTAINS, IMPORTS, SOFT
    Direction string   `json:"direction,omitempty"`  // out, in, both (default: both)
    Depth     int      `json:"depth,omitempty"`      // expansion depth (default: 1)
    Limit     int      `json:"limit,omitempty"`      // max nodes returned (default: 50)
}

type Output struct {
    Seed   NodeInfo      `json:"seed"`
    Edges  []EdgeInfo    `json:"edges"`
    Nodes  []NodeInfo    `json:"nodes"`
    Trail  []TrailStep   `json:"trail"`  // expansion path for "why included"
}

type NodeInfo struct {
    NodeID  string `json:"node_id"`
    Kind    string `json:"kind"`
    Name    string `json:"name"`
    Path    string `json:"path"`
    Line    int    `json:"line"`
    Summary string `json:"summary"`
}

type EdgeInfo struct {
    From      string `json:"from"`       // source node ID
    To        string `json:"to"`         // target node ID
    EdgeType  string `json:"edge_type"`  // CALLS, REFERS_TO, etc.
    Weight    int    `json:"weight"`     // edge weight (call count, etc.)
}

type TrailStep struct {
    NodeID   string `json:"node_id"`
    Reason   string `json:"reason"`    // "seed", "CALLS from X", "SOFT via Index"
    Depth    int    `json:"depth"`
}
```

**Usage:**
```bash
# Expand from a specific node
agentctl run repo_index/expand --input '{"seed": "func:internal/storage.Store.Put"}'

# Follow only call edges, 2 levels deep
agentctl run repo_index/expand --input '{
  "seed": "func:internal/intelligence/retrieval.Search",
  "edge_types": ["CALLS"],
  "depth": 2,
  "direction": "out"
}'

# Include soft edges from Index: blocks
agentctl run repo_index/expand --input '{
  "seed": "type:internal/indexing/repoindex/store.Store",
  "edge_types": ["CALLS", "SOFT"]
}'
```

#### repo_index/open

```go
// skills/repo_index_open/main.go

type Input struct {
    NodeID    string `json:"node_id"`
    Workspace string `json:"workspace,omitempty"`
}

type Output struct {
    Node       NodeDetail     `json:"node"`
    CodeSpan   CodeSpan       `json:"code_span"`
    Statistics NodeStatistics `json:"statistics"`
}

type NodeDetail struct {
    NodeID     string            `json:"node_id"`
    Kind       string            `json:"kind"`
    Name       string            `json:"name"`
    Path       string            `json:"path"`
    Line       int               `json:"line"`
    EndLine    int               `json:"end_line"`
    Signature  string            `json:"signature,omitempty"`  // for funcs
    Doc        string            `json:"doc"`                  // full GoDoc
    IndexBlock *IndexBlock       `json:"index_block,omitempty"`
    Metadata   map[string]string `json:"metadata,omitempty"`
}

type CodeSpan struct {
    Content   string `json:"content"`     // raw source code
    StartLine int    `json:"start_line"`
    EndLine   int    `json:"end_line"`
    Language  string `json:"language"`    // go, typescript, etc.
}

type IndexBlock struct {
    Purpose  string   `json:"purpose"`
    Keywords []string `json:"keywords"`
    Related  []string `json:"related"`
}

type NodeStatistics struct {
    FanIn        int            `json:"fan_in"`
    FanOut       int            `json:"fan_out"`
    EdgesByType  map[string]int `json:"edges_by_type"`
    Callers      []string       `json:"callers"`       // top 5 node IDs
    Callees      []string       `json:"callees"`       // top 5 node IDs
}
```

**Usage:**
```bash
agentctl run repo_index/open --input '{"node_id": "func:internal/storage.Store.Put"}'
```

#### Shared Client

```go
// internal/adapters/skillslib/repoindex/client.go

// Client wraps repo index store for skill usage
type Client struct {
    store *store.Store
}

// NewClient opens the repo index for a workspace
func NewClient(ctx context.Context, workspace string) (*Client, error)

// Search performs FTS query
func (c *Client) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)

// Expand traverses graph from seed node
func (c *Client) Expand(ctx context.Context, seed string, opts ExpandOptions) (*ExpansionResult, error)

// GetNode retrieves full node details
func (c *Client) GetNode(ctx context.Context, nodeID string) (*NodeDetail, error)
```

**Skill Manifests:**

```yaml
# skills/repo_index_search/skill.yaml
name: repo_index/search
version: 1.0.0
description: Search the repository graph index using full-text search
entry: bin
keywords:
  - repo_index/search
  - code search
  - symbol search
  - graph search
input_schema:
  type: object
  properties:
    query:
      type: string
      description: Full-text search query
    workspace:
      type: string
      description: Workspace path (defaults to current directory)
    limit:
      type: integer
      description: Maximum results to return
    node_types:
      type: array
      items:
        type: string
      description: Filter by node types (file, package, func, type)
  required:
    - query
```

**Agent/Overseer Integration:**

The skills are automatically available to agents. For overseer workflows:

```yaml
# configs/workflows/code-navigation.yaml
name: code-navigation
steps:
  - skill: repo_index/search
    input:
      query: "{{user_query}}"
      limit: 10
  - skill: repo_index/expand
    input:
      seed: "{{steps[0].results[0].node_id}}"
      depth: 2
  - skill: repo_index/open
    input:
      node_id: "{{steps[1].nodes[0].node_id}}"
```

**Testing Strategy:**
- Unit tests for each skill with mock store
- Integration tests with real graph DB
- Test edge type filtering
- Test depth limiting
- Test trail generation for expansion
- Benchmark query performance

**Acceptance Criteria:**
- [ ] `repo_index/search` returns ranked results with scores
- [ ] `repo_index/expand` respects edge type and direction filters
- [ ] `repo_index/expand` includes "trail" explaining why each node included
- [ ] `repo_index/open` returns full node details including code span
- [ ] All skills handle missing workspace/graph gracefully
- [ ] Skills work in agent context via `agentctl run`
- [ ] Response times under 100ms for typical queries
- [ ] Skill manifests properly declare input schemas

---

### PR 7.2: GUI Panels for Repo Graph Navigation

**Summary:**  
Add React components to the GUI agent for visual repo graph exploration. Users can search, select nodes, view details, and navigate relationships.

**Files Touched:**
- `packages/gui-agent/src/components/repoindex/SearchPanel.tsx` - Search interface
- `packages/gui-agent/src/components/repoindex/NodeDetail.tsx` - Node detail view
- `packages/gui-agent/src/components/repoindex/ExpansionTrail.tsx` - Navigation trail
- `packages/gui-agent/src/components/repoindex/CodeSpanViewer.tsx` - Source code display
- `packages/gui-agent/src/components/repoindex/GraphView.tsx` - Visual graph (optional)
- `packages/gui-agent/src/components/repoindex/index.ts` - Component exports
- `packages/gui-agent/src/stores/repoIndexStore.ts` - Zustand store for state
- `packages/gui-agent/src/api/repoindex.ts` - API client for skills

**Implementation Details:**

#### SearchPanel.tsx

```tsx
// packages/gui-agent/src/components/repoindex/SearchPanel.tsx

interface SearchPanelProps {
  onNodeSelect: (nodeId: string) => void;
}

export function SearchPanel({ onNodeSelect }: SearchPanelProps) {
  const [query, setQuery] = useState('');
  const [nodeTypes, setNodeTypes] = useState<string[]>([]);
  const { results, isLoading, search } = useRepoIndexStore();

  const handleSearch = async () => {
    await search(query, { nodeTypes, limit: 20 });
  };

  return (
    <div className="search-panel">
      <div className="search-input-group">
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
          placeholder="Search symbols, files, packages..."
        />
        <button onClick={handleSearch} disabled={isLoading}>
          {isLoading ? 'Searching...' : 'Search'}
        </button>
      </div>
      
      <div className="node-type-filters">
        {['file', 'package', 'func', 'type', 'const'].map(type => (
          <label key={type}>
            <input
              type="checkbox"
              checked={nodeTypes.includes(type)}
              onChange={(e) => {
                if (e.target.checked) {
                  setNodeTypes([...nodeTypes, type]);
                } else {
                  setNodeTypes(nodeTypes.filter(t => t !== type));
                }
              }}
            />
            {type}
          </label>
        ))}
      </div>

      <div className="search-results">
        {results.map(result => (
          <div
            key={result.nodeId}
            className="search-result"
            onClick={() => onNodeSelect(result.nodeId)}
          >
            <span className="result-kind">{result.kind}</span>
            <span className="result-name">{result.name}</span>
            <span className="result-path">{result.path}:{result.line}</span>
            <span className="result-score">{result.score.toFixed(2)}</span>
            <div className="result-summary">{result.summary}</div>
            <div className="result-stats">
              <span>Fan-in: {result.fanIn}</span>
              <span>Fan-out: {result.fanOut}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
```

#### NodeDetail.tsx

```tsx
// packages/gui-agent/src/components/repoindex/NodeDetail.tsx

interface NodeDetailProps {
  nodeId: string;
  onNavigate: (nodeId: string) => void;
}

export function NodeDetail({ nodeId, onNavigate }: NodeDetailProps) {
  const { selectedNode, loadNode, expand, expandedEdges } = useRepoIndexStore();

  useEffect(() => {
    loadNode(nodeId);
  }, [nodeId, loadNode]);

  if (!selectedNode) return <div>Loading...</div>;

  return (
    <div className="node-detail">
      <header>
        <span className="node-kind">{selectedNode.kind}</span>
        <h2>{selectedNode.name}</h2>
        <p className="node-path">{selectedNode.path}:{selectedNode.line}</p>
      </header>

      {selectedNode.signature && (
        <pre className="node-signature">{selectedNode.signature}</pre>
      )}

      <section className="node-doc">
        <h3>Documentation</h3>
        <div className="doc-content">{selectedNode.doc}</div>
      </section>

      {selectedNode.indexBlock && (
        <section className="index-block">
          <h3>Index</h3>
          <dl>
            <dt>Purpose</dt>
            <dd>{selectedNode.indexBlock.purpose}</dd>
            <dt>Keywords</dt>
            <dd>{selectedNode.indexBlock.keywords.join(', ')}</dd>
            <dt>Related</dt>
            <dd>
              {selectedNode.indexBlock.related.map(ref => (
                <button key={ref} onClick={() => onNavigate(ref)}>
                  {ref}
                </button>
              ))}
            </dd>
          </dl>
        </section>
      )}

      <section className="node-statistics">
        <h3>Statistics</h3>
        <div className="stats-grid">
          <div>Fan-in: {selectedNode.statistics.fanIn}</div>
          <div>Fan-out: {selectedNode.statistics.fanOut}</div>
        </div>
        <h4>Edges by Type</h4>
        <ul>
          {Object.entries(selectedNode.statistics.edgesByType).map(([type, count]) => (
            <li key={type}>{type}: {count}</li>
          ))}
        </ul>
      </section>

      <section className="related-nodes">
        <h3>Related Nodes</h3>
        <div className="edge-type-tabs">
          {['CALLS', 'REFERS_TO', 'CONTAINS', 'IMPORTS', 'SOFT'].map(edgeType => (
            <button
              key={edgeType}
              onClick={() => expand(nodeId, { edgeTypes: [edgeType] })}
            >
              {edgeType}
            </button>
          ))}
        </div>
        <div className="expanded-nodes">
          {expandedEdges.map(edge => (
            <div
              key={edge.to}
              className="related-node"
              onClick={() => onNavigate(edge.to)}
            >
              <span className="edge-type">{edge.edgeType}</span>
              <span className="node-name">{edge.toName}</span>
            </div>
          ))}
        </div>
      </section>

      <CodeSpanViewer codeSpan={selectedNode.codeSpan} />
    </div>
  );
}
```

#### ExpansionTrail.tsx

```tsx
// packages/gui-agent/src/components/repoindex/ExpansionTrail.tsx

interface ExpansionTrailProps {
  trail: TrailStep[];
  onNavigate: (nodeId: string) => void;
}

export function ExpansionTrail({ trail, onNavigate }: ExpansionTrailProps) {
  return (
    <nav className="expansion-trail" aria-label="Navigation trail">
      <h4>How you got here</h4>
      <ol>
        {trail.map((step, index) => (
          <li key={step.nodeId} className={`depth-${step.depth}`}>
            <button onClick={() => onNavigate(step.nodeId)}>
              {step.nodeName}
            </button>
            {index < trail.length - 1 && (
              <span className="trail-reason">{trail[index + 1].reason}</span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}
```

#### CodeSpanViewer.tsx

```tsx
// packages/gui-agent/src/components/repoindex/CodeSpanViewer.tsx

interface CodeSpanViewerProps {
  codeSpan: CodeSpan;
}

export function CodeSpanViewer({ codeSpan }: CodeSpanViewerProps) {
  return (
    <section className="code-span-viewer">
      <header>
        <h3>Source Code</h3>
        <span className="line-range">
          Lines {codeSpan.startLine}-{codeSpan.endLine}
        </span>
      </header>
      <pre className={`language-${codeSpan.language}`}>
        <code>
          {codeSpan.content.split('\n').map((line, i) => (
            <div key={i} className="code-line">
              <span className="line-number">
                {codeSpan.startLine + i}
              </span>
              <span className="line-content">{line}</span>
            </div>
          ))}
        </code>
      </pre>
    </section>
  );
}
```

#### repoIndexStore.ts

```tsx
// packages/gui-agent/src/stores/repoIndexStore.ts

import { create } from 'zustand';
import { searchRepoIndex, expandNode, getNode } from '../api/repoindex';

interface RepoIndexState {
  // Search state
  results: SearchResult[];
  isLoading: boolean;
  
  // Selected node state
  selectedNode: NodeDetail | null;
  
  // Expansion state
  expandedEdges: EdgeInfo[];
  trail: TrailStep[];
  
  // Actions
  search: (query: string, opts?: SearchOptions) => Promise<void>;
  loadNode: (nodeId: string) => Promise<void>;
  expand: (nodeId: string, opts?: ExpandOptions) => Promise<void>;
  clearTrail: () => void;
  pushTrail: (step: TrailStep) => void;
}

export const useRepoIndexStore = create<RepoIndexState>((set, get) => ({
  results: [],
  isLoading: false,
  selectedNode: null,
  expandedEdges: [],
  trail: [],

  search: async (query, opts) => {
    set({ isLoading: true });
    try {
      const response = await searchRepoIndex(query, opts);
      set({ results: response.results, isLoading: false });
    } catch (error) {
      set({ isLoading: false });
      throw error;
    }
  },

  loadNode: async (nodeId) => {
    const response = await getNode(nodeId);
    set({ selectedNode: response.node });
    
    // Update trail
    const { trail } = get();
    const existingIndex = trail.findIndex(s => s.nodeId === nodeId);
    if (existingIndex >= 0) {
      // Navigating back - trim trail
      set({ trail: trail.slice(0, existingIndex + 1) });
    } else {
      // New node - append to trail
      set({
        trail: [...trail, {
          nodeId,
          nodeName: response.node.name,
          reason: trail.length === 0 ? 'search' : 'navigation',
          depth: trail.length,
        }]
      });
    }
  },

  expand: async (nodeId, opts) => {
    const response = await expandNode(nodeId, opts);
    set({ expandedEdges: response.edges });
  },

  clearTrail: () => set({ trail: [] }),
  pushTrail: (step) => set((state) => ({ trail: [...state.trail, step] })),
}));
```

#### API Client

```tsx
// packages/gui-agent/src/api/repoindex.ts

import { apiClient } from './client';

export interface SearchOptions {
  nodeTypes?: string[];
  limit?: number;
}

export interface ExpandOptions {
  edgeTypes?: string[];
  direction?: 'out' | 'in' | 'both';
  depth?: number;
}

export async function searchRepoIndex(query: string, opts?: SearchOptions) {
  return apiClient.runSkill('repo_index/search', {
    query,
    ...opts,
  });
}

export async function expandNode(nodeId: string, opts?: ExpandOptions) {
  return apiClient.runSkill('repo_index/expand', {
    seed: nodeId,
    ...opts,
  });
}

export async function getNode(nodeId: string) {
  return apiClient.runSkill('repo_index/open', {
    node_id: nodeId,
  });
}
```

**Layout Integration:**

The repo index panel can be integrated into the main layout as a new view or sidebar:

```tsx
// In App.tsx or layout component
<RepoIndexPanel 
  initialQuery={searchQuery}
  onCodeNavigate={(path, line) => openEditor(path, line)}
/>
```

**Testing Strategy:**
- Unit tests for each component with mock store
- Integration tests for store actions
- E2E tests for search -> select -> expand flow
- Test trail management (forward navigation, back navigation)
- Test edge type filtering in UI
- Verify code span viewer syntax highlighting

**Acceptance Criteria:**
- [ ] SearchPanel allows FTS queries with node type filters
- [ ] Search results show name, path, summary, fan-in/fan-out
- [ ] Clicking result loads NodeDetail view
- [ ] NodeDetail shows full doc, signature, Index block
- [ ] Related nodes panel with edge type tabs
- [ ] Clicking related node navigates and updates trail
- [ ] ExpansionTrail shows "why included" path
- [ ] Trail supports back-navigation by clicking previous items
- [ ] CodeSpanViewer displays source with line numbers
- [ ] Loading states shown during API calls
- [ ] Error states handled gracefully

---

## Future Enhancements

Not in scope for Phase 7 but potential follow-ups:

1. **GraphView component** - Force-directed graph visualization
2. **Breadth-first exploration** - "Show me everything 2 hops from here"
3. **Diff view** - Compare two versions of a node
4. **Bookmarks** - Save interesting nodes for later
5. **Export** - Export subgraph as Mermaid/DOT

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Skill response time | < 100ms for typical queries |
| GUI time-to-first-result | < 500ms |
| Navigation depth | Average 3+ hops per session |
| Trail accuracy | 100% - every node explains why included |
