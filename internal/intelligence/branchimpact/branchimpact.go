package branchimpact

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/fsutil"
)

const (
	DefaultBaseRef    = "HEAD"
	DefaultDepth      = 2
	DefaultLimit      = 50
	DefaultPerFileCap = 20
)

type Rank string

const (
	MustReview   Rank = "must_review"
	ShouldReview Rank = "should_review"
	ContextOnly  Rank = "context_only"
)

type LaneStatus string

const (
	LaneAvailable   LaneStatus = "available"
	LaneUnavailable LaneStatus = "unavailable"
)

type Input struct {
	Workspace  string `json:"workspace,omitempty"`
	BaseRef    string `json:"base_ref,omitempty"`
	HeadRef    string `json:"head_ref,omitempty"`
	Depth      int    `json:"depth,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	PerFileCap int    `json:"per_file_cap,omitempty"`
	MaxChanged int    `json:"max_changed,omitempty"`
}

type Change struct {
	Path        string `json:"path"`
	OldPath     string `json:"old_path,omitempty"`
	Status      string `json:"status"`
	Additions   int    `json:"additions,omitempty"`
	Deletions   int    `json:"deletions,omitempty"`
	IsTest      bool   `json:"is_test,omitempty"`
	IsDeleted   bool   `json:"is_deleted,omitempty"`
	Description string `json:"description,omitempty"`
}

type Candidate struct {
	Path        string   `json:"path"`
	Rank        Rank     `json:"rank"`
	Score       int      `json:"score"`
	Reasons     []string `json:"reasons"`
	Sources     []string `json:"sources"`
	Symbols     []string `json:"symbols,omitempty"`
	LineHints   []int    `json:"line_hints,omitempty"`
	Changed     bool     `json:"changed,omitempty"`
	GraphDepth  int      `json:"graph_depth,omitempty"`
	ChangeStats *Change  `json:"change,omitempty"`
}

type Lane struct {
	Name   string     `json:"name"`
	Status LaneStatus `json:"status"`
	Reason string     `json:"reason,omitempty"`
}

type Summary struct {
	ChangedFiles      int `json:"changed_files"`
	MustReviewCount   int `json:"must_review_count"`
	ShouldReviewCount int `json:"should_review_count"`
	ContextOnlyCount  int `json:"context_only_count"`
}

type Output struct {
	BaseRef    string      `json:"base_ref"`
	HeadRef    string      `json:"head_ref,omitempty"`
	Workspace  string      `json:"workspace,omitempty"`
	Summary    Summary     `json:"summary"`
	Changes    []Change    `json:"changes"`
	Candidates []Candidate `json:"candidates"`
	Lanes      []Lane      `json:"lanes"`
}

type DiffProvider interface {
	ChangedFiles(ctx context.Context, in Input) ([]Change, error)
}

type GraphProvider interface {
	BlastRadius(ctx context.Context, changes []Change, opts GraphOptions) (GraphResult, error)
}

type GraphOptions struct {
	Depth      int
	Limit      int
	PerFileCap int
}

type GraphResult struct {
	Available  bool
	Reason     string
	Candidates []GraphCandidate
}

type GraphCandidate struct {
	Path      string
	Symbol    string
	LineHint  int
	Depth     int
	EdgeTypes []string
}

type SemanticProvider interface {
	Neighbors(ctx context.Context, changes []Change, opts SemanticOptions) (SemanticResult, error)
}

type SemanticOptions struct {
	Limit      int
	PerFileCap int
}

type SemanticResult struct {
	Available  bool
	Reason     string
	Candidates []SemanticCandidate
}

type SemanticCandidate struct {
	Path       string
	Symbol     string
	LineHint   int
	Similarity float64
	Summary    string
	Source     string
}

type Providers struct {
	Diff     DiffProvider
	Graph    GraphProvider
	Semantic SemanticProvider
}

const (
	sourceGitDiff           = "git_diff"
	sourceRepoindexGraph    = "repoindex_graph"
	sourceSemanticNeighbors = "semantic_neighbors"
)

func Analyze(ctx context.Context, in Input, providers Providers) (Output, error) {
	if providers.Diff == nil {
		return Output{}, fmt.Errorf("diff provider is required")
	}
	in = normalizeInput(in)
	changes, err := providers.Diff.ChangedFiles(ctx, in)
	if err != nil {
		return Output{}, err
	}
	changes = normalizeChanges(changes)
	if in.MaxChanged > 0 && len(changes) > in.MaxChanged {
		changes = changes[:in.MaxChanged]
	}

	agg := newAggregator()
	for _, change := range changes {
		score := 100
		reason := "changed file in branch diff"
		if change.IsDeleted {
			reason = "deleted file in branch diff"
		}
		agg.add(change.Path, score, sourceGitDiff, reason, candidatePatch{changed: true, change: &change})
		if change.OldPath != "" && change.OldPath != change.Path {
			old := change
			old.Path = change.OldPath
			old.Description = "rename source"
			agg.add(change.OldPath, 90, sourceGitDiff, "rename source in branch diff", candidatePatch{changed: true, change: &old})
		}
	}

	lanes := []Lane{{Name: sourceGitDiff, Status: LaneAvailable}}
	lanes = append(lanes, collectGraphLane(ctx, changes, in, providers.Graph, agg))
	lanes = append(lanes, collectSemanticLane(ctx, changes, in, providers.Semantic, agg))

	candidates := agg.sorted(in.Limit)
	out := Output{
		BaseRef:    in.BaseRef,
		HeadRef:    in.HeadRef,
		Workspace:  in.Workspace,
		Changes:    changes,
		Candidates: candidates,
		Lanes:      lanes,
	}
	out.Summary.ChangedFiles = len(changes)
	for _, c := range candidates {
		switch c.Rank {
		case MustReview:
			out.Summary.MustReviewCount++
		case ShouldReview:
			out.Summary.ShouldReviewCount++
		case ContextOnly:
			out.Summary.ContextOnlyCount++
		}
	}
	return out, nil
}

func collectGraphLane(ctx context.Context, changes []Change, in Input, graph GraphProvider, agg *aggregator) Lane {
	if graph == nil {
		return Lane{Name: sourceRepoindexGraph, Status: LaneUnavailable, Reason: "graph provider not configured"}
	}
	graphResult, err := graph.BlastRadius(ctx, changes, GraphOptions{
		Depth:      in.Depth,
		Limit:      in.Limit,
		PerFileCap: in.PerFileCap,
	})
	if err != nil {
		return Lane{Name: sourceRepoindexGraph, Status: LaneUnavailable, Reason: err.Error()}
	}
	status := LaneUnavailable
	if graphResult.Available {
		status = LaneAvailable
	}
	lane := Lane{Name: sourceRepoindexGraph, Status: status, Reason: graphResult.Reason}
	if !graphResult.Available {
		return lane
	}
	for _, gc := range graphResult.Candidates {
		path := cleanPath(gc.Path)
		if path == "" {
			continue
		}
		score := graphScore(gc.Depth)
		reason := graphReason(gc)
		patch := candidatePatch{symbol: gc.Symbol, lineHint: gc.LineHint, graphDepth: gc.Depth}
		agg.add(path, score, sourceRepoindexGraph, reason, patch)
	}
	return lane
}

func collectSemanticLane(ctx context.Context, changes []Change, in Input, semantic SemanticProvider, agg *aggregator) Lane {
	if semantic == nil {
		return Lane{Name: sourceSemanticNeighbors, Status: LaneUnavailable, Reason: "semantic provider not configured"}
	}
	result, err := semantic.Neighbors(ctx, changes, SemanticOptions{Limit: in.Limit, PerFileCap: in.PerFileCap})
	if err != nil {
		return Lane{Name: sourceSemanticNeighbors, Status: LaneUnavailable, Reason: err.Error()}
	}
	status := LaneUnavailable
	if result.Available {
		status = LaneAvailable
	}
	lane := Lane{Name: sourceSemanticNeighbors, Status: status, Reason: result.Reason}
	if !result.Available {
		return lane
	}
	for _, candidate := range result.Candidates {
		path := cleanPath(candidate.Path)
		if path == "" {
			continue
		}
		score := semanticScore(candidate.Similarity)
		reason := semanticReason(candidate)
		patch := candidatePatch{symbol: candidate.Symbol, lineHint: candidate.LineHint}
		agg.add(path, score, sourceSemanticNeighbors, reason, patch)
	}
	return lane
}

func semanticScore(similarity float64) int {
	switch {
	case similarity >= 0.85:
		return 50
	case similarity >= 0.70:
		return 40
	case similarity >= 0.55:
		return 30
	default:
		return 20
	}
}

func semanticReason(candidate SemanticCandidate) string {
	parts := []string{"semantically near changed branch content"}
	if candidate.Similarity > 0 {
		parts = append(parts, fmt.Sprintf("similarity %.3f", candidate.Similarity))
	}
	if candidate.Source != "" {
		parts = append(parts, "via "+candidate.Source)
	}
	return strings.Join(parts, "; ")
}

func normalizeInput(in Input) Input {
	in.BaseRef = strings.TrimSpace(in.BaseRef)
	if in.BaseRef == "" {
		in.BaseRef = DefaultBaseRef
	}
	in.HeadRef = strings.TrimSpace(in.HeadRef)
	if in.Depth <= 0 {
		in.Depth = DefaultDepth
	}
	if in.Limit <= 0 {
		in.Limit = DefaultLimit
	}
	if in.PerFileCap <= 0 {
		in.PerFileCap = DefaultPerFileCap
	}
	return in
}

func normalizeChanges(changes []Change) []Change {
	out := make([]Change, 0, len(changes))
	for _, change := range changes {
		change.Path = cleanPath(change.Path)
		change.OldPath = cleanPath(change.OldPath)
		change.Status = strings.TrimSpace(change.Status)
		if change.Path == "" {
			continue
		}
		change.IsTest = fsutil.IsTestFile(filepath.Base(change.Path))
		change.IsDeleted = strings.HasPrefix(change.Status, "D")
		out = append(out, change)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Status < out[j].Status
	})
	return out
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func graphScore(depth int) int {
	switch {
	case depth <= 0:
		return 70
	case depth == 1:
		return 55
	default:
		return 30
	}
}

func graphReason(gc GraphCandidate) string {
	parts := []string{"reachable from changed file in repoindex graph"}
	if gc.Depth > 0 {
		parts = append(parts, fmt.Sprintf("depth %d", gc.Depth))
	}
	if len(gc.EdgeTypes) > 0 {
		parts = append(parts, "via "+strings.Join(uniqueSorted(gc.EdgeTypes), ","))
	}
	return strings.Join(parts, "; ")
}

type candidatePatch struct {
	changed    bool
	change     *Change
	symbol     string
	lineHint   int
	graphDepth int
}

type aggregator struct {
	byPath map[string]*Candidate
}

func newAggregator() *aggregator {
	return &aggregator{byPath: make(map[string]*Candidate)}
}

func (a *aggregator) add(path string, score int, source, reason string, patch candidatePatch) {
	path = cleanPath(path)
	if path == "" {
		return
	}
	c, ok := a.byPath[path]
	if !ok {
		c = &Candidate{Path: path, GraphDepth: patch.graphDepth}
		a.byPath[path] = c
	}
	c.Score += score
	c.Rank = rankForScore(c.Score)
	c.Changed = c.Changed || patch.changed
	if patch.change != nil {
		change := *patch.change
		c.ChangeStats = &change
	}
	if patch.graphDepth > 0 && (c.GraphDepth == 0 || patch.graphDepth < c.GraphDepth) {
		c.GraphDepth = patch.graphDepth
	}
	c.Sources = appendUnique(c.Sources, source)
	c.Reasons = appendUnique(c.Reasons, reason)
	if patch.symbol != "" {
		c.Symbols = appendUnique(c.Symbols, patch.symbol)
	}
	if patch.lineHint > 0 && !containsInt(c.LineHints, patch.lineHint) {
		c.LineHints = append(c.LineHints, patch.lineHint)
		sort.Ints(c.LineHints)
	}
}

func (a *aggregator) sorted(limit int) []Candidate {
	candidates := make([]Candidate, 0, len(a.byPath))
	for _, c := range a.byPath {
		sort.Strings(c.Reasons)
		sort.Strings(c.Sources)
		sort.Strings(c.Symbols)
		candidates = append(candidates, *c)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Rank != candidates[j].Rank {
			return rankOrder(candidates[i].Rank) < rankOrder(candidates[j].Rank)
		}
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Path < candidates[j].Path
	})
	if limit > 0 && len(candidates) > limit {
		return candidates[:limit]
	}
	return candidates
}

func rankForScore(score int) Rank {
	switch {
	case score >= 100:
		return MustReview
	case score >= 50:
		return ShouldReview
	default:
		return ContextOnly
	}
}

func rankOrder(rank Rank) int {
	switch rank {
	case MustReview:
		return 0
	case ShouldReview:
		return 1
	default:
		return 2
	}
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func containsInt(values []int, value int) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func uniqueSorted(values []string) []string {
	var out []string
	for _, value := range values {
		out = appendUnique(out, value)
	}
	sort.Strings(out)
	return out
}
