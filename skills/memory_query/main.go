// Package main implements the memory/query skill for canonical memory-record access.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/mathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage"
	contextstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

const (
	DefaultLimit                  = 10
	DefaultMinSimilarity          = 0.3
	DefaultStrongLifecycleScore   = 0.9
	DefaultTimeout                = 5 * time.Second
	defaultLifecyclePolicy        = "active_default_strong_candidate_stale"
	explicitLifecycleFilterPolicy = "explicit_lifecycle_states"
)

// Input defines the skill input parameters for memory query with filtering and search options.
type Input struct {
	Query           string  `json:"query,omitempty"`
	File            string  `json:"file,omitempty"`
	Kinds           string  `json:"kinds,omitempty"`
	LifecycleStates string  `json:"lifecycle_states,omitempty"`
	Workspace       string  `json:"workspace,omitempty"`
	SessionID       string  `json:"session_id,omitempty"`
	Limit           int     `json:"limit,omitempty"`
	Offset          int     `json:"offset,omitempty"`
	MinSimilarity   float64 `json:"min_similarity,omitempty"`
	IncludeContent  bool    `json:"include_content,omitempty"`
}

// Output defines canonical memory-record results, pagination, and query statistics.
type Output struct {
	Records    []memorycore.Record `json:"records"`
	Pagination Pagination          `json:"pagination"`
	Stats      QueryStats          `json:"stats"`
}

// Pagination provides pagination metadata for memory query results with total count and navigation.
type Pagination struct {
	Total   int  `json:"total"`
	Offset  int  `json:"offset"`
	Limit   int  `json:"limit"`
	HasMore bool `json:"has_more"`
}

// QueryStats provides query statistics with performance metrics and filter information.
type QueryStats struct {
	TotalFound          int            `json:"total_found"`
	Filtered            int            `json:"filtered"`
	SearchMethod        string         `json:"search_method"`
	LatencyMS           int            `json:"latency_ms"`
	KindsFilter         string         `json:"kinds_filter,omitempty"`
	LifecycleFilter     string         `json:"lifecycle_filter,omitempty"`
	LifecyclePolicy     string         `json:"lifecycle_policy,omitempty"`
	FileFilter          string         `json:"file_filter,omitempty"`
	SessionIDFilter     string         `json:"session_id_filter,omitempty"`
	Hint                string         `json:"hint,omitempty"`
	SourceCounts        map[string]int `json:"source_counts,omitempty"`
	UnavailableSources  []string       `json:"unavailable_sources,omitempty"`
	SuppressedLifecycle int            `json:"suppressed_by_lifecycle,omitempty"`
}

type recordCandidate struct {
	Record memorycore.Record
	Score  float64
}

// main is the skill entry point for memory/query with canonical record access.
func main() {
	skillmain.Main("memory/query", skillmain.Chain(run,
		skillmain.WithTimeout[Input](DefaultTimeout),
		skillmain.WithRecover[Input](),
	))
}

// run orchestrates memory query operations with validation, normalization, and result formatting.
//
// Index:
//   Purpose: Query memory records with filtering, search, and pagination capabilities
//   Keywords: memory/query, memory_search, vector_search, filtering, pagination
//   Related: normalizeInput, query, searchWithEmbeddings, isFileAssociated, extractFileFromEntry
//   Flow: validate input → normalize parameters → execute query → format results → emit output
//   Resources: memory store (SQLite); embedding service
//   Events: memory-queried
//   OutputFields: records, pagination, stats
//
// [[domain:memory-query]]
// [[protocol:vector-search]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate: at least one search criteria must be provided
	if in.Query == "" && in.File == "" && in.Kinds == "" && in.LifecycleStates == "" {
		return skillerr.Arg("at least one of query, file, kinds, or lifecycle_states must be provided", skillerr.WithHint("Provide query text, a file filter, canonical memory kinds, or explicit lifecycle states."))
	}

	// Apply defaults
	normalizeInput(&in, rc)

	start := time.Now()
	out, err := query(ctx, rc, &in)
	emitMemoryQueryTelemetry(ctx, rc, &in, out, err, time.Since(start))
	if err != nil {
		return err
	}

	return skillout.Emit(rc, "memory/query", out)
}

func emitMemoryQueryTelemetry(ctx context.Context, rc *skillmain.RunContext, in *Input, out *Output, err error, duration time.Duration) {
	workspace := in.Workspace
	sessionID := strings.TrimSpace(in.SessionID)
	agentID := ""
	if rc != nil {
		if workspace == "" {
			workspace = rc.Workspace
		}
		if sessionID == "" {
			sessionID = rc.SessionID
		}
		agentID = rc.AgentID
	}

	builder := observability.NewEvent(observability.OpMemoryQuery).
		WithComponent(observability.ComponentSkill).
		WithCommand("memory/query").
		WithWorkspace(workspace).
		WithSession(sessionID, agentID).
		EnrichFromContext(ctx).
		EnrichFromEnv().
		WithData("always_sample", true).
		WithData("query_present", strings.TrimSpace(in.Query) != "").
		WithData("query_chars", len(strings.TrimSpace(in.Query))).
		WithData("file_filter_present", strings.TrimSpace(in.File) != "").
		WithData("kinds_filter_present", strings.TrimSpace(in.Kinds) != "").
		WithData("lifecycle_filter_present", strings.TrimSpace(in.LifecycleStates) != "").
		WithData("session_filter_present", strings.TrimSpace(in.SessionID) != "").
		WithData("include_content", in.IncludeContent).
		WithData("limit", in.Limit).
		WithData("offset", in.Offset).
		WithData("min_similarity", in.MinSimilarity)
	if out != nil {
		builder.WithData("records_returned", len(out.Records)).
			WithData("total_found", out.Stats.TotalFound).
			WithData("filtered", out.Stats.Filtered).
			WithData("suppressed_by_lifecycle", out.Stats.SuppressedLifecycle).
			WithData("search_method", out.Stats.SearchMethod).
			WithData("lifecycle_policy", out.Stats.LifecyclePolicy).
			WithData("source_counts", out.Stats.SourceCounts).
			WithData("unavailable_sources", len(out.Stats.UnavailableSources)).
			WithData("record_kind_counts", recordKindCounts(out.Records)).
			WithData("record_lifecycle_counts", recordLifecycleCounts(out.Records)).
			WithData("has_more", out.Pagination.HasMore)
	}
	if err != nil {
		observability.Emit(ctx, builder.Error(err, duration))
		return
	}
	observability.Emit(ctx, builder.Success(duration))
}

// normalizeInput applies default values and validation limits to input parameters with bounds checking.
func normalizeInput(in *Input, rc *skillmain.RunContext) {
	in.Limit = mathutil.DefaultPositiveInt(in.Limit, DefaultLimit)
	if in.Limit > 100 {
		in.Limit = 100
	}
	if in.Offset < 0 {
		in.Offset = 0
	}
	in.MinSimilarity = mathutil.DefaultPositiveFloat(in.MinSimilarity, DefaultMinSimilarity)
	if in.Workspace == "" {
		in.Workspace = rc.Workspace
	}
	in.Workspace = workspace.CanonicalID(in.Workspace)
}

// query executes memory search with filtering, vector search fallback, and result pagination.
func query(ctx context.Context, rc *skillmain.RunContext, in *Input) (*Output, error) {
	start := time.Now()
	out := &Output{
		Records: []memorycore.Record{},
		Pagination: Pagination{
			Offset: in.Offset,
			Limit:  in.Limit,
		},
		Stats: QueryStats{
			KindsFilter:     in.Kinds,
			LifecycleFilter: in.LifecycleStates,
			FileFilter:      in.File,
			SessionIDFilter: in.SessionID,
			LifecyclePolicy: defaultLifecyclePolicy,
		},
	}

	kindFilters, err := memorycore.ParseKinds(in.Kinds)
	if err != nil {
		return nil, skillerr.Arg(err.Error(), skillerr.WithHint("Use canonical memory kinds such as semantic_fact, decision, procedural_skill, policy_rule, episodic_trace, reflection, eval_result, or adapter_example."))
	}
	lifecycleFilters, err := memorycore.ParseLifecycleStates(in.LifecycleStates)
	if err != nil {
		return nil, skillerr.Arg(err.Error(), skillerr.WithHint("Use lifecycle states candidate, active, stale, archived, deprecated, or quarantined."))
	}
	if len(lifecycleFilters) > 0 {
		out.Stats.LifecyclePolicy = explicitLifecycleFilterPolicy
	}

	workspacePath := in.Workspace

	memStore, err := rc.Stores.Memory(ctx)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store", err)
	}

	namedRecords, totalNamed, suppressedNamed, searchMethod, searchHint, err := queryNamedMemoryRecords(ctx, memStore, rc, workspacePath, in, kindFilters, lifecycleFilters)
	if err != nil {
		return nil, err
	}
	out.Stats.SearchMethod = searchMethod
	if searchHint != "" {
		out.Stats.Hint = searchHint
	}

	claimRecords, totalClaims, suppressedClaims, err := queryContextClaimRecords(ctx, rc, workspacePath, in, kindFilters, lifecycleFilters)
	if err != nil {
		out.Stats.UnavailableSources = append(out.Stats.UnavailableSources, "context_claim")
		out.Stats.Hint = appendHint(out.Stats.Hint, fmt.Sprintf("context claims unavailable: %v", err))
	}

	candidates := append(namedRecords, claimRecords...)
	sortRecordCandidates(candidates)

	out.Stats.TotalFound = totalNamed + totalClaims
	out.Stats.Filtered = len(candidates)
	out.Stats.SuppressedLifecycle = suppressedNamed + suppressedClaims
	out.Stats.SourceCounts = sourceCounts(candidates)
	out.Pagination.Total = len(candidates)
	out.Pagination.HasMore = in.Offset+in.Limit < len(candidates)

	endIdx := in.Offset + in.Limit
	if endIdx > len(candidates) {
		endIdx = len(candidates)
	}
	if in.Offset < len(candidates) {
		candidates = candidates[in.Offset:endIdx]
	} else {
		candidates = nil
	}

	for _, candidate := range candidates {
		out.Records = append(out.Records, candidate.Record)
	}

	out.Stats.LatencyMS = int(time.Since(start).Milliseconds())

	if len(out.Records) == 0 && out.Stats.Hint == "" {
		if in.Query != "" {
			out.Stats.Hint = "no matching memory records found; try a broader query or different kinds"
		} else {
			out.Stats.Hint = "no memory records match the filters; check kinds and file path"
		}
	}

	return out, nil
}

func queryNamedMemoryRecords(ctx context.Context, memStore *memory.Store, rc *skillmain.RunContext, workspacePath string, in *Input, kindFilters []memorycore.Kind, lifecycleFilters []memorycore.LifecycleState) ([]recordCandidate, int, int, string, string, error) {
	scoredEntries, searchMethod, hint, err := namedMemoryScoredEntries(ctx, memStore, rc, workspacePath, in)
	if err != nil {
		return nil, 0, 0, searchMethod, hint, err
	}

	filtered := make([]recordCandidate, 0, len(scoredEntries))
	suppressed := 0
	for _, scored := range scoredEntries {
		entry := scored.Entry
		if skipInternalMemoryEntry(entry) {
			continue
		}
		if !memorycore.KindAllowed(memorycore.KindForNamedType(entry.Type), kindFilters) {
			continue
		}
		if strings.TrimSpace(in.SessionID) != "" && entry.SessionID != strings.TrimSpace(in.SessionID) {
			continue
		}
		if in.File != "" && !isFileAssociated(entry, in.File) {
			continue
		}
		if in.Query != "" && scored.Score < in.MinSimilarity {
			continue
		}
		record := memorycore.RecordFromNamedEntry(entry, memorycore.NamedEntryOptions{
			Score:          scored.Score,
			Summary:        skillout.TruncateString(entry.Summary, 500),
			FileRefs:       fileRefsFromEntry(entry),
			IncludeContent: in.IncludeContent,
		})
		if !recordAllowedByLifecycle(record, scored.Score, in, lifecycleFilters) {
			suppressed++
			continue
		}
		filtered = append(filtered, recordCandidate{Record: record, Score: scored.Score})
	}
	return filtered, len(scoredEntries), suppressed, searchMethod, hint, nil
}

func namedMemoryScoredEntries(ctx context.Context, memStore *memory.Store, rc *skillmain.RunContext, workspacePath string, in *Input) ([]storage.ScoredEntry, string, string, error) {
	if in.Query != "" {
		scoredEntries, err := searchWithEmbeddings(ctx, memStore, rc.Config, workspacePath, in, skillmain.EmbeddingGuard(rc))
		if err == nil {
			return scoredEntries, "vector", "", nil
		}
		scoredEntries, fallbackErr := memStore.Search(ctx, workspacePath, in.Query, in.Limit*3)
		if fallbackErr != nil {
			return nil, "bm25", "", skillerr.WrapIO("search memory records", fallbackErr)
		}
		return scoredEntries, "bm25", fmt.Sprintf("vector search failed: %v; using BM25", err), nil
	}

	limit := in.Limit * 3
	if in.File == "" && (strings.TrimSpace(in.SessionID) != "" || strings.TrimSpace(in.Kinds) != "" || strings.TrimSpace(in.LifecycleStates) != "") {
		limit = 1000
	}
	entries, err := memStore.List(ctx, workspacePath, limit)
	if err != nil {
		return nil, "filter", "", skillerr.WrapIO("list memory records", err)
	}
	scoredEntries := make([]storage.ScoredEntry, 0, len(entries))
	for _, entry := range entries {
		scoredEntries = append(scoredEntries, storage.ScoredEntry{
			Entry: entry,
			Score: 1.0,
		})
	}
	return scoredEntries, "filter", "", nil
}

func queryContextClaimRecords(ctx context.Context, rc *skillmain.RunContext, workspacePath string, in *Input, kindFilters []memorycore.Kind, lifecycleFilters []memorycore.LifecycleState) ([]recordCandidate, int, int, error) {
	store, err := contextstore.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return nil, 0, 0, skillerr.WrapIO("open context claim store", err)
	}
	defer store.Close()

	claims, err := store.ListClaims(ctx, contextengine.ClaimFilter{
		WorkspaceID: workspacePath,
		Limit:       1000,
	})
	if err != nil {
		return nil, 0, 0, skillerr.WrapIO("list context claims", err)
	}

	filtered := make([]recordCandidate, 0, len(claims))
	suppressed := 0
	for _, claim := range claims {
		kind := memorycore.KindForClaimType(claim.ClaimType)
		if !memorycore.KindAllowed(kind, kindFilters) {
			continue
		}
		if strings.TrimSpace(in.SessionID) != "" && claim.Scope.SessionID != strings.TrimSpace(in.SessionID) {
			continue
		}
		if in.File != "" && !isClaimFileAssociated(claim, in.File) {
			continue
		}
		score := 1.0
		if in.Query != "" {
			score = scoreContextClaim(claim, in.Query)
			if score <= 0 {
				continue
			}
		}
		record := memorycore.RecordFromContextClaim(claim, memorycore.ContextClaimOptions{
			Score:          score,
			Summary:        skillout.TruncateString(claim.Summary, 500),
			IncludeContent: in.IncludeContent,
		})
		if !recordAllowedByLifecycle(record, score, in, lifecycleFilters) {
			suppressed++
			continue
		}
		filtered = append(filtered, recordCandidate{Record: record, Score: score})
	}
	return filtered, len(claims), suppressed, nil
}

func recordAllowedByLifecycle(record memorycore.Record, score float64, in *Input, lifecycleFilters []memorycore.LifecycleState) bool {
	if len(lifecycleFilters) > 0 {
		return memorycore.LifecycleStateAllowed(record.Lifecycle.State, lifecycleFilters)
	}
	switch record.Lifecycle.State {
	case memorycore.LifecycleStateActive:
		return true
	case memorycore.LifecycleStateCandidate, memorycore.LifecycleStateStale:
		return strings.TrimSpace(in.Query) != "" && score >= DefaultStrongLifecycleScore
	default:
		return false
	}
}

func scoreContextClaim(claim contextengine.MemoryClaim, query string) float64 {
	terms := normalizedQueryTerms(query)
	if len(terms) == 0 {
		return 0
	}
	body := strings.ToLower(strings.Join([]string{
		claim.ID,
		claim.ClaimType,
		claim.Summary,
		claim.Reason,
		claim.BlastRadius,
		claim.Scope.Path,
		claim.Scope.TaskID,
		claim.Scope.SessionID,
	}, " "))
	matches := 0
	for _, term := range terms {
		if strings.Contains(body, term) {
			matches++
		}
	}
	if matches == 0 {
		return 0
	}
	return 0.55 + 0.45*(float64(matches)/float64(len(terms)))
}

func normalizedQueryTerms(query string) []string {
	parts := strings.Fields(strings.ToLower(query))
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, ".,:;()[]{}\"'")
		if len(part) < 2 {
			continue
		}
		terms = append(terms, part)
	}
	return terms
}

func isClaimFileAssociated(claim contextengine.MemoryClaim, filePath string) bool {
	filePath = normalizePath(filePath)
	for _, candidate := range claimFileRefs(claim) {
		candidate = normalizePath(candidate)
		if strings.Contains(strings.ToLower(candidate), strings.ToLower(filePath)) ||
			strings.Contains(strings.ToLower(filePath), strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func claimFileRefs(claim contextengine.MemoryClaim) []string {
	refs := []string{claim.Scope.Path}
	for _, ref := range claim.Scope.Refs {
		if ref.Type == contextengine.RefTypePath {
			refs = append(refs, ref.Ref)
		}
	}
	for _, ref := range claim.SourceRefs {
		if ref.Type == contextengine.RefTypePath {
			refs = append(refs, ref.Ref)
		}
	}
	return refs
}

func sortRecordCandidates(candidates []recordCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Record.SourceLane != candidates[j].Record.SourceLane {
			return candidates[i].Record.SourceLane < candidates[j].Record.SourceLane
		}
		return candidates[i].Record.ID < candidates[j].Record.ID
	})
}

func sourceCounts(candidates []recordCandidate) map[string]int {
	if len(candidates) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, candidate := range candidates {
		counts[string(candidate.Record.SourceLane)]++
	}
	return counts
}

func recordKindCounts(records []memorycore.Record) map[string]int {
	if len(records) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, record := range records {
		counts[string(record.Kind)]++
	}
	return counts
}

func recordLifecycleCounts(records []memorycore.Record) map[string]int {
	if len(records) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, record := range records {
		counts[string(record.Lifecycle.State)]++
	}
	return counts
}

func appendHint(existing, next string) string {
	if strings.TrimSpace(existing) == "" {
		return next
	}
	if strings.TrimSpace(next) == "" {
		return existing
	}
	return existing + "; " + next
}

// searchWithEmbeddings performs vector similarity search using embeddings with query enrichment.
func searchWithEmbeddings(ctx context.Context, memStore *memory.Store, cfg config.Config, workspacePath string, in *Input, embedOpts ...semantic.EmbedderOption) ([]storage.ScoredEntry, error) {
	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeMemory, cfg, embedOpts...)
	if err != nil {
		return nil, skillerr.WrapRuntime("create embedder", err)
	}

	// Enrich query with temporal terms for better matching.
	enrichedQuery := semantic.EnrichQuery(in.Query)
	result, err := embedder.Embed(ctx, enrichedQuery)
	if err != nil {
		return nil, skillerr.WrapRuntime("generate query embedding", err)
	}

	results, err := memStore.SearchSimilar(ctx, workspacePath, result.Vec, in.Limit*3)
	if err != nil {
		return nil, skillerr.WrapIO("vector search", err)
	}

	return results, nil
}

func skipInternalMemoryEntry(entry storage.NamedEntry) bool {
	return entry.Type == "symbol" || entry.Type == "code_symbol"
}

// isFileAssociated checks if a memory record is associated with a specific file path.
func isFileAssociated(entry storage.NamedEntry, filePath string) bool {
	filePath = normalizePath(filePath)

	if strings.Contains(strings.ToLower(entry.Name), strings.ToLower(filePath)) {
		return true
	}

	if strings.Contains(strings.ToLower(entry.Summary), strings.ToLower(filePath)) {
		return true
	}

	if entry.Result != nil {
		var data map[string]any
		if json.Unmarshal(entry.Result, &data) == nil {
			for _, key := range []string{"file", "path", "file_path", "filePath"} {
				if val, ok := data[key].(string); ok {
					if strings.Contains(strings.ToLower(val), strings.ToLower(filePath)) {
						return true
					}
				}
			}
		}
	}

	if strings.HasPrefix(strings.ToLower(filePath), strings.ToLower(normalizePath(entry.Name))) {
		return true
	}

	return false
}

// extractFileFromEntry extracts file path information from a stored memory row.
func extractFileFromEntry(entry storage.NamedEntry) string {
	if strings.HasPrefix(entry.Name, "edit:") {
		parts := strings.SplitN(entry.Name, ":", 3)
		if len(parts) >= 2 {
			return parts[1]
		}
	}

	if entry.Result != nil {
		var data map[string]any
		if json.Unmarshal(entry.Result, &data) == nil {
			for _, key := range []string{"file", "path", "file_path", "filePath"} {
				if val, ok := data[key].(string); ok && val != "" {
					return val
				}
			}
		}
	}

	return ""
}

func fileRefsFromEntry(entry storage.NamedEntry) []string {
	file := extractFileFromEntry(entry)
	if file == "" {
		return nil
	}
	return []string{file}
}

// normalizePath normalizes file paths by removing common prefixes for consistent comparison.
func normalizePath(p string) string {
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	return p
}
