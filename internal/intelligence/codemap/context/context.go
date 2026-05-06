package context

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/buildinfo"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/execution/runner"
	"github.com/joshka0/foxctl/internal/storage/graph"
	"github.com/rs/zerolog"
)

// Context holds all gathered context for codemap generation.
type Context struct {
	Query    string           `json:"query"`
	Graph    *GraphContext    `json:"graph,omitempty"`
	Symbols  *SymbolContext   `json:"symbols,omitempty"`
	Semantic *SemanticContext `json:"semantic,omitempty"`
	Patterns *PatternContext  `json:"patterns,omitempty"`
}

// GraphContext contains graph-derived context.
type GraphContext struct {
	// Nodes matching search terms
	Nodes []graph.Node `json:"nodes"`
	// Edges between matched nodes
	Edges []graph.Edge `json:"edges"`
	// Top nodes by PageRank score
	TopByPageRank []graph.Node `json:"top_by_pagerank"`
	// Shortest paths between key nodes
	Paths [][]string `json:"paths,omitempty"`
}

// SymbolContext contains symbol-derived context.
type SymbolContext struct {
	// Symbols grouped by file
	SymbolsByFile map[string][]Symbol `json:"symbols_by_file"`
	// Imports/dependencies by file
	ImportsByFile map[string][]string `json:"imports_by_file"`
	// Common imports across files
	SharedImports []string `json:"shared_imports"`
	// Exported API symbols
	ExportedAPIs []Symbol `json:"exported_apis"`
}

// SemanticContext contains semantic search results to seed codemap exploration.
type SemanticContext struct {
	Results []SemanticResult `json:"results"`
}

// SemanticResult represents a semantic search hit.
type SemanticResult struct {
	Source     string  `json:"source"`
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Path       string  `json:"path,omitempty"`
	Line       int     `json:"line,omitempty"`
	Snippet    string  `json:"snippet,omitempty"`
	Summary    string  `json:"summary,omitempty"`
	Similarity float64 `json:"similarity"`
	Rank       int     `json:"rank"`
}

// Symbol represents a code symbol.
type Symbol struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Signature string `json:"signature"`
	Exported  bool   `json:"exported"`
	Doc       string `json:"doc,omitempty"`
}

// PatternContext contains pattern-search context.
type PatternContext struct {
	// Matches grouped by search term
	MatchesByTerm map[string][]Block `json:"matches_by_term"`
	// Cross-references between matches
	CrossReferences []CrossRef `json:"cross_references"`
}

// Block represents a code block containing pattern matches.
type Block struct {
	File       string `json:"file"`
	Language   string `json:"language"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	HeaderLine string `json:"header_line,omitempty"`
	SymbolName string `json:"symbol_name,omitempty"`
	SymbolKind string `json:"symbol_kind,omitempty"`
	Source     string `json:"source"`
	MatchLines []int  `json:"match_lines"`
	MatchCount int    `json:"match_count"`
}

// CrossRef represents a cross-reference between code locations.
type CrossRef struct {
	FromFile string `json:"from_file"`
	FromLine int    `json:"from_line"`
	ToFile   string `json:"to_file"`
	ToLine   int    `json:"to_line"`
	Kind     string `json:"kind"` // "call", "import", "reference"
}

// Gatherer collects context from multiple sources.
type Gatherer struct {
	graphStore    graph.Store
	skillResolver *skill.Resolver
	workspace     string
	logger        zerolog.Logger
}

// GathererOption configures the Gatherer.
type GathererOption func(*Gatherer)

// WithGraphStore sets the graph store for the gatherer.
func WithGraphStore(store graph.Store) GathererOption {
	return func(g *Gatherer) {
		g.graphStore = store
	}
}

// WithSkillResolver sets the skill resolver for the gatherer.
func WithSkillResolver(resolver *skill.Resolver) GathererOption {
	return func(g *Gatherer) {
		g.skillResolver = resolver
	}
}

// WithLogger sets the logger for the gatherer.
func WithLogger(logger zerolog.Logger) GathererOption {
	return func(g *Gatherer) {
		g.logger = logger
	}
}

// WithWorkspace sets the workspace path for the gatherer.
func WithWorkspace(workspace string) GathererOption {
	return func(g *Gatherer) {
		g.workspace = workspace
	}
}

// NewGatherer creates a new context gatherer.
// NewGatherer creates a context gatherer with optional dependencies.
//
// Index:
//
//	Purpose: Initialize a codemap context gatherer
//	Keywords: codemap_context, gatherer, graph_store, skill_resolver, workspace
//	Related: Gatherer.GatherAll
//	Flow: apply options → return gatherer
//	Resources: graph store, skill resolver
//	Events: none
//	OutputFields: Gatherer
//
// [[protocol:codemap-context-gatherer-init]]
func NewGatherer(opts ...GathererOption) *Gatherer {
	g := &Gatherer{
		skillResolver: skill.NewResolver(),
		logger:        zerolog.New(os.Stderr),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// GatherAll gathers context from all sources in parallel.
// GatherAll collects graph, symbol, semantic, and pattern context in parallel.
//
// Index:
//
//	Purpose: Gather codemap context from multiple sources
//	Keywords: codemap_context, graph, symbols, semantic_search, patterns, errgroup
//	Related: gatherGraphContext, gatherSymbolContext, gatherSemanticContext, gatherPatternContext
//	Flow: extract terms → run graph/symbol/semantic/pattern gatherers → merge context
//	Resources: graph store, skill resolver, LLM provider
//	Events: codemap-context-gathered
//	OutputFields: Context
//
// [[protocol:codemap-context-gather-all]]
// [[invariant:partial-context-on-gather-failure]]
func (g *Gatherer) GatherAll(ctx context.Context, query, workspace string) (*Context, error) {
	if workspace == "" {
		workspace = g.workspace
	}

	// Extract search terms from query
	terms := ExtractTerms(query)
	if len(terms) == 0 {
		terms = []string{query}
	}

	result := &Context{
		Query: query,
	}

	// Use errgroup for parallel execution with shared context
	eg, egCtx := errgroup.WithContext(ctx)

	// Gather graph context
	eg.Go(func() error {
		graphCtx, err := g.gatherGraphContext(egCtx, workspace, terms)
		if err != nil {
			// Log but don't fail - graph context is optional
			return nil
		}
		result.Graph = graphCtx
		return nil
	})

	// Gather symbol context
	eg.Go(func() error {
		symbolCtx, err := g.gatherSymbolContext(egCtx, workspace, terms)
		if err != nil {
			// Log but don't fail - symbol context is optional
			return nil
		}
		result.Symbols = symbolCtx
		return nil
	})

	// Gather semantic search context
	eg.Go(func() error {
		semanticCtx, err := g.gatherSemanticContext(egCtx, workspace, query)
		if err != nil {
			// Log but don't fail - semantic context is optional
			g.logger.Warn().Err(err).Msg("gather semantic context failed")
			return nil
		}
		result.Semantic = semanticCtx
		return nil
	})

	// Gather pattern context
	eg.Go(func() error {
		patternCtx, err := g.gatherPatternContext(egCtx, workspace, terms)
		if err != nil {
			// Log but don't fail - pattern context is optional
			g.logger.Warn().Err(err).Msg("gather pattern context failed")
			return nil
		}
		result.Patterns = patternCtx
		return nil
	})

	// Wait for all gatherers
	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("gather context: %w", err)
	}

	return result, nil
}

// gatherGraphContext gathers context from the graph store.
func (g *Gatherer) gatherGraphContext(ctx context.Context, workspace string, terms []string) (*GraphContext, error) {
	if g.graphStore == nil {
		return nil, nil
	}

	result := &GraphContext{
		Nodes:         make([]graph.Node, 0),
		Edges:         make([]graph.Edge, 0),
		TopByPageRank: make([]graph.Node, 0),
	}

	// Search for nodes matching terms
	nodeIDs := make(map[string]bool)
	for _, term := range terms {
		nodes, err := g.graphStore.SearchNodes(ctx, workspace, term, 20)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			if !nodeIDs[node.NodeID] {
				nodeIDs[node.NodeID] = true
				result.Nodes = append(result.Nodes, node)
			}
		}
	}

	// Get edges between matched nodes
	if len(nodeIDs) > 0 {
		ids := make([]string, 0, len(nodeIDs))
		for id := range nodeIDs {
			ids = append(ids, id)
		}
		edges, err := g.graphStore.GetEdgesBetween(ctx, workspace, ids)
		if err == nil {
			result.Edges = edges
		}
	}

	// Get top nodes by PageRank
	topNodes, err := g.graphStore.TopNodes(ctx, graph.TopNodesOptions{
		Workspace: workspace,
		Limit:     10,
	})
	if err == nil {
		result.TopByPageRank = topNodes
	}

	// Find paths between first two important nodes if we have enough
	if len(result.Nodes) >= 2 {
		paths, err := g.graphStore.FindShortestPath(ctx, workspace, result.Nodes[0].NodeID, result.Nodes[1].NodeID, 5)
		if err == nil && len(paths) > 0 {
			result.Paths = paths
		}
	}

	return result, nil
}

// gatherSymbolContext gathers context from the code/symbols skill.
func (g *Gatherer) gatherSymbolContext(ctx context.Context, workspace string, terms []string) (*SymbolContext, error) {
	if g.skillResolver == nil {
		return nil, nil
	}

	result := &SymbolContext{
		SymbolsByFile: make(map[string][]Symbol),
		ImportsByFile: make(map[string][]string),
		SharedImports: make([]string, 0),
		ExportedAPIs:  make([]Symbol, 0),
	}

	// Resolve and run code/symbols skill
	handle, err := g.skillResolver.Resolve("code/symbols")
	if err != nil {
		return nil, fmt.Errorf("resolve code/symbols: %w", err)
	}

	manifest, artifactPath, err := skill.LoadManifestAndArtifact(handle.ManifestPath, skill.ArtifactOptions{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: workspace,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve code/symbols artifact: %w", err)
	}

	// Build input for symbols skill
	input := map[string]any{
		"path":            workspace,
		"symbol_type":     "all",
		"include_private": false,
		"include_docs":    true,
		"max_results":     200,
	}

	inputBytes, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	// Execute skill
	stdout, _, err := runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        inputBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("run code/symbols: %w", err)
	}

	var payload struct {
		Preview []Symbol `json:"preview"`
	}
	if err := protocol.DecodeEnvelopeInto(stdout, &payload); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	for _, sym := range payload.Preview {
		// Group by file
		result.SymbolsByFile[sym.File] = append(result.SymbolsByFile[sym.File], sym)

		// Collect exported APIs
		if sym.Exported {
			result.ExportedAPIs = append(result.ExportedAPIs, sym)
		}
	}

	return result, nil
}

// gatherSemanticContext gathers semantic search results from code/semantic_search.
func (g *Gatherer) gatherSemanticContext(ctx context.Context, workspace, query string) (*SemanticContext, error) {
	if g.skillResolver == nil || query == "" {
		return nil, nil
	}

	handle, err := g.skillResolver.Resolve("code/semantic_search")
	if err != nil {
		return nil, fmt.Errorf("resolve code/semantic_search: %w", err)
	}

	manifest, artifactPath, err := skill.LoadManifestAndArtifact(handle.ManifestPath, skill.ArtifactOptions{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: workspace,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve code/semantic_search artifact: %w", err)
	}

	input := map[string]any{
		"query":           query,
		"scope":           []string{"symbols", "codemaps"},
		"workspace":       workspace,
		"limit":           15,
		"min_similarity":  0.2,
		"include_context": false,
	}

	inputBytes, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	stdout, _, err := runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        inputBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("run code/semantic_search: %w", err)
	}

	var payload struct {
		Results []SemanticResult `json:"results"`
	}
	if err := protocol.DecodeEnvelopeInto(stdout, &payload); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if len(payload.Results) == 0 {
		return nil, nil
	}

	return &SemanticContext{Results: payload.Results}, nil
}

// gatherPatternContext gathers context from the code/context_ripgrep skill.
func (g *Gatherer) gatherPatternContext(ctx context.Context, workspace string, terms []string) (*PatternContext, error) {
	if g.skillResolver == nil {
		return nil, nil
	}

	result := &PatternContext{
		MatchesByTerm:   make(map[string][]Block),
		CrossReferences: make([]CrossRef, 0),
	}

	// Resolve code/context_ripgrep skill
	handle, err := g.skillResolver.Resolve("code/context_ripgrep")
	if err != nil {
		return nil, fmt.Errorf("resolve code/context_ripgrep: %w", err)
	}

	manifest, artifactPath, err := skill.LoadManifestAndArtifact(handle.ManifestPath, skill.ArtifactOptions{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: workspace,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve code/context_ripgrep artifact: %w", err)
	}

	// Search for each term
	for _, term := range terms {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		// Build input for ripgrep skill
		input := map[string]any{
			"path":             workspace,
			"pattern":          term,
			"case_insensitive": true,
			"max_matches":      50,
			"max_blocks":       10,
			"max_block_lines":  100,
		}

		inputBytes, err := json.Marshal(input)
		if err != nil {
			continue
		}

		// Execute skill
		stdout, _, err := runner.RunWithOptions(ctx, runner.RunOptions{
			Manifest:     manifest,
			ArtifactPath: artifactPath,
			Input:        inputBytes,
		})
		if err != nil {
			continue
		}

		var payload struct {
			Preview []Block `json:"preview"`
		}
		if err := protocol.DecodeEnvelopeInto(stdout, &payload); err != nil {
			continue
		}

		blocks := payload.Preview
		for i := range blocks {
			if blocks[i].MatchLines == nil {
				blocks[i].MatchLines = []int{}
			}
		}

		if len(blocks) > 0 {
			result.MatchesByTerm[term] = blocks
		}
	}

	// Build cross-references from pattern matches
	result.CrossReferences = buildCrossReferences(result.MatchesByTerm)

	return result, nil
}

// ExtractTerms extracts search terms from a natural language query.
func ExtractTerms(query string) []string {
	// Lowercase and split
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}

	// Stop words to filter out
	stopWords := map[string]bool{
		"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
		"be": true, "by": true, "can": true, "do": true, "does": true, "for": true,
		"from": true, "has": true, "have": true, "how": true, "i": true, "in": true,
		"is": true, "it": true, "its": true, "of": true, "on": true, "or": true,
		"that": true, "the": true, "this": true, "to": true, "was": true, "what": true,
		"when": true, "where": true, "which": true, "who": true, "why": true, "will": true,
		"with": true,
	}

	var terms []string
	seen := make(map[string]bool)

	// Split on whitespace and punctuation
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		term := current.String()
		current.Reset()

		// Filter out short terms and stop words
		if len(term) < 3 {
			return
		}
		if stopWords[term] {
			return
		}
		if seen[term] {
			return
		}

		seen[term] = true
		terms = append(terms, term)
	}

	for _, r := range query {
		if isWordChar(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	return terms
}

// isWordChar returns true if the rune is part of a word.
func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_'
}

// buildCrossReferences analyzes pattern matches to find cross-references.
func buildCrossReferences(matchesByTerm map[string][]Block) []CrossRef {
	var refs []CrossRef

	// Collect all files with matches
	fileBlocks := make(map[string][]Block)
	for _, blocks := range matchesByTerm {
		for _, block := range blocks {
			fileBlocks[block.File] = append(fileBlocks[block.File], block)
		}
	}

	// Find references between files
	// Collect and sort file keys for deterministic output ordering
	files := make([]string, 0, len(fileBlocks))
	for f := range fileBlocks {
		files = append(files, f)
	}
	sort.Strings(files)

	// Simple heuristic: if the same symbol name appears in multiple files,
	// create a cross-reference
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			blocksA := fileBlocks[files[i]]
			blocksB := fileBlocks[files[j]]

			for _, a := range blocksA {
				for _, b := range blocksB {
					// If same symbol name in different files, create reference
					if a.SymbolName != "" && a.SymbolName == b.SymbolName {
						refs = append(refs, CrossRef{
							FromFile: a.File,
							FromLine: a.StartLine,
							ToFile:   b.File,
							ToLine:   b.StartLine,
							Kind:     "reference",
						})
					}
				}
			}
		}
	}

	return refs
}

// Helper functions for safe type assertions
