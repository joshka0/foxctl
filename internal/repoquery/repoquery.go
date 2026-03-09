package repoquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
)

const (
	defaultSearchLimit      = 20
	defaultExpandDepth      = 1
	defaultExpandBudget     = 50
	defaultExpandPerNodeCap = 50
	defaultDAGDepth         = 2
	defaultDAGBudget        = 80
	defaultDAGPerNodeCap    = 20
	defaultDAGK             = 1
)

// SearchRequest captures a typed repo-index search request.
type SearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

// ParseSearchRequest parses and validates a search request from JSON.
func ParseSearchRequest(raw json.RawMessage) (SearchRequest, error) {
	var req SearchRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return SearchRequest{}, err
	}
	return NewSearchRequest(req.Query, req.Limit)
}

// NewSearchRequest builds and validates a search request.
func NewSearchRequest(query string, limit int) (SearchRequest, error) {
	req := SearchRequest{
		Query: strings.TrimSpace(query),
		Limit: limit,
	}
	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}
	if req.Query == "" {
		return SearchRequest{}, errors.New("query is required")
	}
	return req, nil
}

// ExpandRequest captures a typed repo-index expand request.
type ExpandRequest struct {
	Seeds         []string
	EdgeTypes     []repoindex.EdgeType
	EdgeTypeNames []string
	Direction     repoindex.Direction
	Depth         int
	Budget        int
	PerNodeCap    int
}

type expandRequestInput struct {
	Seeds      []string `json:"seeds"`
	EdgeTypes  []string `json:"edge_types,omitempty"`
	Direction  string   `json:"direction,omitempty"`
	Depth      int      `json:"depth,omitempty"`
	Budget     int      `json:"budget,omitempty"`
	PerNodeCap int      `json:"per_node_cap,omitempty"`
}

// ParseExpandRequest parses and validates an expand request from JSON.
func ParseExpandRequest(raw json.RawMessage) (ExpandRequest, error) {
	var input expandRequestInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return ExpandRequest{}, err
	}
	return NewExpandRequest(input.Seeds, input.EdgeTypes, input.Direction, input.Depth, input.Budget, input.PerNodeCap)
}

// NewExpandRequest builds and validates an expand request.
func NewExpandRequest(seeds, edgeTypes []string, direction string, depth, budget, perNodeCap int) (ExpandRequest, error) {
	if len(seeds) == 0 {
		return ExpandRequest{}, errors.New("seeds are required")
	}

	parsedDirection, err := ParseDirection(direction)
	if err != nil {
		return ExpandRequest{}, err
	}

	normalizedEdgeTypes := NormalizeEdgeTypes(edgeTypes)
	parsedEdgeTypes, err := ParseEdgeTypes(normalizedEdgeTypes)
	if err != nil {
		return ExpandRequest{}, err
	}

	if depth <= 0 {
		depth = defaultExpandDepth
	}
	if budget <= 0 {
		budget = defaultExpandBudget
	}
	if perNodeCap <= 0 {
		perNodeCap = defaultExpandPerNodeCap
	}

	return ExpandRequest{
		Seeds:         append([]string(nil), seeds...),
		EdgeTypes:     parsedEdgeTypes,
		EdgeTypeNames: normalizedEdgeTypes,
		Direction:     parsedDirection,
		Depth:         depth,
		Budget:        budget,
		PerNodeCap:    perNodeCap,
	}, nil
}

// OpenRequest captures a typed repo-index open request.
type OpenRequest struct {
	ID string
}

type openRequestInput struct {
	ID string `json:"id"`
}

// ParseOpenRequest parses and validates an open request from JSON.
func ParseOpenRequest(raw json.RawMessage) (OpenRequest, error) {
	var input openRequestInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return OpenRequest{}, err
	}
	return NewOpenRequest(input.ID)
}

// NewOpenRequest builds and validates an open request.
func NewOpenRequest(id string) (OpenRequest, error) {
	if strings.TrimSpace(id) == "" {
		return OpenRequest{}, errors.New("id is required")
	}
	return OpenRequest{ID: id}, nil
}

// DAGGrepRequest captures a typed repo-index DAG request.
type DAGGrepRequest struct {
	Query          string
	Mode           string
	K              int
	NodeKinds      []repoindex.NodeKind
	EdgeTypes      []repoindex.EdgeType
	Direction      repoindex.Direction
	Depth          int
	Budget         int
	PerNodeCap     int
	IncludeAnchors bool
	Render         string
}

type dagGrepRequestInput struct {
	Query          string   `json:"query"`
	Mode           string   `json:"mode,omitempty"`
	K              int      `json:"k,omitempty"`
	NodeKinds      []string `json:"node_kinds,omitempty"`
	EdgeSets       []string `json:"edge_sets,omitempty"`
	EdgeTypes      []string `json:"edge_types,omitempty"`
	Direction      string   `json:"direction,omitempty"`
	Depth          int      `json:"depth,omitempty"`
	Budget         int      `json:"budget,omitempty"`
	PerNodeCap     int      `json:"per_node_cap,omitempty"`
	IncludeAnchors *bool    `json:"include_anchors,omitempty"`
	Render         string   `json:"render,omitempty"`
}

// ParseDAGGrepRequest parses and validates a DAG request from JSON.
func ParseDAGGrepRequest(raw json.RawMessage) (DAGGrepRequest, error) {
	var input dagGrepRequestInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return DAGGrepRequest{}, err
	}

	return NewDAGGrepRequest(
		input.Query,
		input.Mode,
		input.K,
		input.NodeKinds,
		input.EdgeSets,
		input.EdgeTypes,
		input.Direction,
		input.Depth,
		input.Budget,
		input.PerNodeCap,
		input.IncludeAnchors,
		input.Render,
	)
}

// NewDAGGrepRequest builds and validates a DAG request.
func NewDAGGrepRequest(query, mode string, k int, nodeKinds, edgeSets, edgeTypes []string, direction string, depth, budget, perNodeCap int, includeAnchors *bool, render string) (DAGGrepRequest, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return DAGGrepRequest{}, errors.New("query is required")
	}

	parsedDirection, err := ParseDirection(direction)
	if err != nil {
		return DAGGrepRequest{}, err
	}

	nodeKindValues, err := ParseNodeKinds(nodeKinds)
	if err != nil {
		return DAGGrepRequest{}, err
	}

	edgeTypeValues, err := MergeEdgeTypes(edgeSets, edgeTypes)
	if err != nil {
		return DAGGrepRequest{}, err
	}

	if k <= 0 {
		k = defaultDAGK
	}
	if depth <= 0 {
		depth = defaultDAGDepth
	}
	if budget <= 0 {
		budget = defaultDAGBudget
	}
	if perNodeCap <= 0 {
		perNodeCap = defaultDAGPerNodeCap
	}

	anchors := true
	if includeAnchors != nil {
		anchors = *includeAnchors
	}

	return DAGGrepRequest{
		Query:          query,
		Mode:           mode,
		K:              k,
		NodeKinds:      nodeKindValues,
		EdgeTypes:      edgeTypeValues,
		Direction:      parsedDirection,
		Depth:          depth,
		Budget:         budget,
		PerNodeCap:     perNodeCap,
		IncludeAnchors: anchors,
		Render:         strings.TrimSpace(render),
	}, nil
}

// QueryService is a shared typed query adapter over repoindex.QueryEngine.
type QueryService struct {
	Engine *repoindex.QueryEngine
}

// NewQueryService creates a query service for shared repo-index requests.
func NewQueryService(engine *repoindex.QueryEngine) *QueryService {
	return &QueryService{Engine: engine}
}

// Search executes a typed search request.
func (s *QueryService) Search(ctx context.Context, req SearchRequest) ([]repoindex.Node, error) {
	if s == nil || s.Engine == nil {
		return nil, errors.New("repo query service is not configured")
	}
	if strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("query is required")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	return s.Engine.Search(ctx, req.Query, limit)
}

// SearchWithProjection executes a typed search request and projects anchors for results.
func (s *QueryService) SearchWithProjection(ctx context.Context, req SearchRequest) (SearchOutput, error) {
	if s == nil || s.Engine == nil {
		return SearchOutput{}, errors.New("repo query service is not configured")
	}
	if strings.TrimSpace(req.Query) == "" {
		return SearchOutput{}, errors.New("query is required")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	scored, err := s.Engine.SearchScored(ctx, req.Query, limit)
	if err == nil {
		nodes := make([]repoindex.Node, 0, len(scored))
		scores := make(map[string]float64, len(scored))
		for _, item := range scored {
			nodes = append(nodes, item.Node)
			if strings.TrimSpace(item.Node.ID) != "" {
				scores[item.Node.ID] = item.Score
			}
		}
		return SearchOutput{Nodes: nodes, Anchors: ProjectAnchors(nodes, scores)}, nil
	}

	results, err := s.Search(ctx, req)
	if err != nil {
		return SearchOutput{}, err
	}
	return SearchOutput{Nodes: results, Anchors: ProjectAnchors(results, nil)}, nil
}

// Expand executes a typed expand request.
func (s *QueryService) Expand(ctx context.Context, req ExpandRequest) (repoindex.ExpandResult, error) {
	if s == nil || s.Engine == nil {
		return repoindex.ExpandResult{}, errors.New("repo query service is not configured")
	}
	if len(req.Seeds) == 0 {
		return repoindex.ExpandResult{}, errors.New("seeds are required")
	}
	if req.Direction == "" {
		req.Direction = repoindex.DirOut
	}
	if req.Depth <= 0 {
		req.Depth = defaultExpandDepth
	}
	if req.Budget <= 0 {
		req.Budget = defaultExpandBudget
	}
	if req.PerNodeCap <= 0 {
		req.PerNodeCap = defaultExpandPerNodeCap
	}

	return s.Engine.Expand(ctx, req.Seeds, repoindex.ExpandOptions{
		Direction:  req.Direction,
		EdgeTypes:  req.EdgeTypes,
		Depth:      req.Depth,
		Budget:     req.Budget,
		PerNodeCap: req.PerNodeCap,
	})
}

// ExpandWithProjection executes a typed expand request and projects anchors for nodes.
func (s *QueryService) ExpandWithProjection(ctx context.Context, req ExpandRequest) (ExpandOutput, error) {
	result, err := s.Expand(ctx, req)
	if err != nil {
		return ExpandOutput{}, err
	}
	return ExpandOutput{Result: result, Anchors: ProjectAnchors(result.Nodes, nil)}, nil
}

// Open executes a typed open request.
func (s *QueryService) Open(ctx context.Context, req OpenRequest) (repoindex.Node, error) {
	if s == nil || s.Engine == nil {
		return repoindex.Node{}, errors.New("repo query service is not configured")
	}
	if strings.TrimSpace(req.ID) == "" {
		return repoindex.Node{}, errors.New("id is required")
	}
	return s.Engine.Open(ctx, req.ID)
}

// OpenWithProjection executes a typed open request and returns an optional projected anchor.
func (s *QueryService) OpenWithProjection(ctx context.Context, req OpenRequest) (OpenOutput, error) {
	node, err := s.Open(ctx, req)
	if err != nil {
		return OpenOutput{}, err
	}
	anchor, ok := AnchorFromNode(node, 1)
	if !ok {
		return OpenOutput{Node: node}, nil
	}
	return OpenOutput{Node: node, Anchor: &anchor}, nil
}

// DAGGrep executes a typed DAG request.
func (s *QueryService) DAGGrep(ctx context.Context, req DAGGrepRequest) (repoindex.DAGGrepResult, error) {
	if s == nil || s.Engine == nil {
		return repoindex.DAGGrepResult{}, errors.New("repo query service is not configured")
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return repoindex.DAGGrepResult{}, errors.New("query is required")
	}
	mode := strings.TrimSpace(req.Mode)
	direction := req.Direction
	if direction == "" {
		direction = repoindex.DirOut
	}

	r := repoindex.DAGGrepRequest{
		Query:          query,
		Mode:           mode,
		K:              req.K,
		NodeKinds:      req.NodeKinds,
		EdgeTypes:      req.EdgeTypes,
		Direction:      direction,
		Depth:          req.Depth,
		Budget:         req.Budget,
		PerNodeCap:     req.PerNodeCap,
		IncludeAnchors: req.IncludeAnchors,
	}
	if r.K <= 0 {
		r.K = defaultDAGK
	}
	if r.Depth <= 0 {
		r.Depth = defaultDAGDepth
	}
	if r.Budget <= 0 {
		r.Budget = defaultDAGBudget
	}
	if r.PerNodeCap <= 0 {
		r.PerNodeCap = defaultDAGPerNodeCap
	}

	return s.Engine.DAGGrep(ctx, r)
}

// DAGGrepWithProjection executes a typed DAG request and projects anchors for graph nodes.
func (s *QueryService) DAGGrepWithProjection(ctx context.Context, req DAGGrepRequest) (DAGOutput, error) {
	result, err := s.DAGGrep(ctx, req)
	if err != nil {
		return DAGOutput{}, err
	}

	scores := make(map[string]float64, len(result.Seeds))
	for _, seed := range result.Seeds {
		if strings.TrimSpace(seed.Node.ID) == "" {
			continue
		}
		scores[seed.Node.ID] = seed.Score
	}

	output := DAGOutput{Result: result}
	output.Anchors = ProjectAnchors(result.Graph.Nodes, scores)
	if rendered := RenderDAG(result, req.Render); rendered != "" {
		output.Rendered = map[string]string{strings.TrimSpace(req.Render): rendered}
	}

	return output, nil
}

// ParseDirection parses and validates traversal direction values.
func ParseDirection(value string) (repoindex.Direction, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	direction := repoindex.DirOut
	if trimmed != "" {
		direction = repoindex.Direction(trimmed)
	}
	if direction != repoindex.DirOut && direction != repoindex.DirIn {
		return "", fmt.Errorf("invalid direction: %s", value)
	}
	return direction, nil
}

// NormalizeEdgeTypes flattens comma-delimited values and trims whitespace.
func NormalizeEdgeTypes(values []string) []string {
	var result []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			result = append(result, part)
		}
	}
	return result
}

// ParseEdgeTypes converts string edge types to typed repoindex values.
func ParseEdgeTypes(values []string) ([]repoindex.EdgeType, error) {
	if len(values) == 0 {
		return nil, nil
	}

	allowed := map[string]repoindex.EdgeType{
		string(repoindex.EdgeContains):        repoindex.EdgeContains,
		string(repoindex.EdgeImports):         repoindex.EdgeImports,
		string(repoindex.EdgeRefersTo):        repoindex.EdgeRefersTo,
		string(repoindex.EdgeCalls):           repoindex.EdgeCalls,
		string(repoindex.EdgeImplements):      repoindex.EdgeImplements,
		string(repoindex.EdgeEmbeds):          repoindex.EdgeEmbeds,
		string(repoindex.EdgeTests):           repoindex.EdgeTests,
		string(repoindex.EdgeHasKeyword):      repoindex.EdgeHasKeyword,
		string(repoindex.EdgeHasOutputField):  repoindex.EdgeHasOutputField,
		string(repoindex.EdgeTouchesResource): repoindex.EdgeTouchesResource,
		string(repoindex.EdgeEmitsEvent):      repoindex.EdgeEmitsEvent,
		string(repoindex.EdgeDocRelated):      repoindex.EdgeDocRelated,
		string(repoindex.EdgeDocFlow):         repoindex.EdgeDocFlow,
	}

	var parsed []repoindex.EdgeType
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		upper := strings.ToUpper(trimmed)
		edgeType, ok := allowed[upper]
		if !ok {
			return nil, fmt.Errorf("unknown edge type: %s", upper)
		}
		parsed = append(parsed, edgeType)
	}

	return parsed, nil
}

// ParseNodeKinds converts string node kinds to typed repoindex values.
func ParseNodeKinds(values []string) ([]repoindex.NodeKind, error) {
	if len(values) == 0 {
		return nil, nil
	}

	kinds := make([]repoindex.NodeKind, 0, len(values))
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		switch trimmed {
		case "symbol":
			kinds = append(kinds, repoindex.NodeSymbol)
		case "file":
			kinds = append(kinds, repoindex.NodeFile)
		case "package":
			kinds = append(kinds, repoindex.NodePackage)
		case "concept":
			kinds = append(kinds, repoindex.NodeConcept)
		default:
			return nil, fmt.Errorf("unknown node kind: %s", value)
		}
	}

	return kinds, nil
}

func parseEdgeSet(value string) ([]repoindex.EdgeType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "structural":
		return append([]repoindex.EdgeType(nil), repoindex.EdgeSetStructural...), nil
	case "doc":
		return append([]repoindex.EdgeType(nil), repoindex.EdgeSetDoc...), nil
	case "all":
		all := append([]repoindex.EdgeType(nil), repoindex.EdgeSetStructural...)
		all = append(all, repoindex.EdgeSetDoc...)
		return all, nil
	default:
		if value == "" {
			return nil, nil
		}
		return nil, fmt.Errorf("unknown edge set: %s", value)
	}
}

// MergeEdgeTypes merges edge sets and explicit edge types with fallback defaults.
func MergeEdgeTypes(edgeSets, edgeTypes []string) ([]repoindex.EdgeType, error) {
	var merged []repoindex.EdgeType

	for _, edgeSet := range edgeSets {
		setTypes, err := parseEdgeSet(edgeSet)
		if err != nil {
			return nil, err
		}
		merged = append(merged, setTypes...)
	}

	parsedEdgeTypes, err := ParseEdgeTypes(NormalizeEdgeTypes(edgeTypes))
	if err != nil {
		return nil, err
	}
	merged = append(merged, parsedEdgeTypes...)

	if len(merged) == 0 {
		return repoindex.EdgeSetStructural, nil
	}

	return uniqueEdgeTypes(merged), nil
}

func uniqueEdgeTypes(values []repoindex.EdgeType) []repoindex.EdgeType {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[repoindex.EdgeType]struct{}, len(values))
	out := make([]repoindex.EdgeType, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// EdgeTypeValues converts typed edge values to their wire labels.
func EdgeTypeValues(values []repoindex.EdgeType) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, edgeType := range values {
		out = append(out, string(edgeType))
	}
	return out
}

// RenderDAG builds optional render output for DAG queries.
func RenderDAG(result repoindex.DAGGrepResult, mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "tree":
		return renderDAGTree(result)
	case "mermaid":
		return renderDAGMermaid(result)
	default:
		return ""
	}
}

func renderDAGTree(result repoindex.DAGGrepResult) string {
	if len(result.Graph.Nodes) == 0 {
		return ""
	}

	nodeLabels := make(map[string]string, len(result.Graph.Nodes))
	for _, node := range result.Graph.Nodes {
		nodeLabels[node.ID] = repoindexNodeLabel(node)
	}

	layerBuckets := make(map[int][]string)
	maxLayer := 0
	for id, layer := range result.DAG.Layers {
		layerBuckets[layer] = append(layerBuckets[layer], id)
		if layer > maxLayer {
			maxLayer = layer
		}
	}

	var b strings.Builder
	for layer := 0; layer <= maxLayer; layer++ {
		ids := layerBuckets[layer]
		if len(ids) == 0 {
			continue
		}
		sort.Strings(ids)
		b.WriteString(fmt.Sprintf("Layer %d:\n", layer))
		for _, id := range ids {
			label := nodeLabels[id]
			if label == "" {
				label = id
			}
			b.WriteString("  - ")
			b.WriteString(label)
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func renderDAGMermaid(result repoindex.DAGGrepResult) string {
	if len(result.DAG.Edges) == 0 {
		return ""
	}

	nodeLabels := make(map[string]string, len(result.Graph.Nodes))
	for _, node := range result.Graph.Nodes {
		nodeLabels[node.ID] = repoindexNodeLabel(node)
	}

	var b strings.Builder
	b.WriteString("graph TD\n")
	edges := append([]repoindex.Edge(nil), result.DAG.Edges...)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Src == edges[j].Src {
			if edges[i].Dst == edges[j].Dst {
				return edges[i].Type < edges[j].Type
			}
			return edges[i].Dst < edges[j].Dst
		}
		return edges[i].Src < edges[j].Src
	})
	for _, edge := range edges {
		src := edge.Src
		dst := edge.Dst
		srcLabel := nodeLabels[src]
		if srcLabel == "" {
			srcLabel = src
		}
		dstLabel := nodeLabels[dst]
		if dstLabel == "" {
			dstLabel = dst
		}
		b.WriteString(fmt.Sprintf("  \"%s\"[\"%s\"] --> \"%s\"[\"%s\"]\n",
			escapeMermaidID(src), escapeMermaidLabel(srcLabel),
			escapeMermaidID(dst), escapeMermaidLabel(dstLabel),
		))
	}

	return strings.TrimSpace(b.String())
}

func repoindexNodeLabel(node repoindex.Node) string {
	switch node.Kind {
	case repoindex.NodeSymbol:
		if node.Name != "" && node.File != "" {
			return fmt.Sprintf("%s (%s)", node.Name, node.File)
		}
		if node.Name != "" {
			return node.Name
		}
	case repoindex.NodeFile:
		if node.File != "" {
			return node.File
		}
	case repoindex.NodePackage:
		if node.Pkg != "" {
			return node.Pkg
		}
	case repoindex.NodeConcept:
		if node.Name != "" {
			return node.Name
		}
	}
	if node.ID != "" {
		return node.ID
	}
	return "unknown"
}

func escapeMermaidID(value string) string {
	return strings.ReplaceAll(value, "\"", "'")
}

func escapeMermaidLabel(value string) string {
	value = strings.ReplaceAll(value, "\"", "'")
	return strings.ReplaceAll(value, "\n", " ")
}
