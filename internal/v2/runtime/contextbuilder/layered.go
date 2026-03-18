package contextbuilder

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

const semanticEventQueueCapacity = 128
const (
	defaultL2BudgetPct       = 20
	defaultL1BudgetPct       = 25
	defaultL0BudgetPct       = 45
	defaultSemanticBudgetPct = 10
	narrativeMaxAge          = 30 * time.Minute
	layeredEventMaxRefs      = 24
)

type semanticEventJob struct {
	ctx   context.Context
	event *observability.WideEvent
}

var (
	semanticEventQueueOnce sync.Once
	semanticEventQueue     = make(chan semanticEventJob, semanticEventQueueCapacity)
)

// CompanionRequest controls companion-memory context fetches.
type CompanionRequest struct {
	MaxChars int `json:"max_chars,omitempty"`
}

// CompanionLayeredContext is L2/L1/L0 companion context plus stable refs.
type CompanionLayeredContext struct {
	L2   string         `json:"l2,omitempty"`
	L1   string         `json:"l1,omitempty"`
	L0   string         `json:"l0,omitempty"`
	Refs []string       `json:"refs,omitempty"`
	Meta map[string]any `json:"meta,omitempty"`
}

// CompanionProvider returns layered companion context for a conversation/session.
type CompanionProvider interface {
	GetLayeredContext(ctx context.Context, sessionID string, req CompanionRequest) (CompanionLayeredContext, error)
}

// ArtifactSemanticQuery controls optional semantic artifact retrieval.
type ArtifactSemanticQuery struct {
	QueryEmbedding []float32          `json:"query_embedding,omitempty"`
	SessionID      string             `json:"session_id,omitempty"`
	ArtifactTypes  []string           `json:"artifact_types,omitempty"`
	Limit          int                `json:"limit,omitempty"`
	MinSimilarity  float64            `json:"min_similarity,omitempty"`
	Working        run.WorkingContext `json:"working_context,omitempty"`
}

// LayerBudget configures per-layer and total character budgets for layered
// context assembly.
type LayerBudget struct {
	TotalChars    int `json:"total_chars,omitempty"`
	L2Chars       int `json:"l2_chars,omitempty"`
	L1Chars       int `json:"l1_chars,omitempty"`
	L0Chars       int `json:"l0_chars,omitempty"`
	SemanticChars int `json:"semantic_chars,omitempty"`
}

// LayeredRequest asks for L2 -> L1 -> L0 context assembly.
type LayeredRequest struct {
	SessionID   string                 `json:"session_id"`
	MaxChars    int                    `json:"max_chars,omitempty"`
	MonthsLimit int                    `json:"months_limit,omitempty"`
	DaysLimit   int                    `json:"days_limit,omitempty"`
	HoursLimit  int                    `json:"hours_limit,omitempty"`
	Budget      *LayerBudget           `json:"budget,omitempty"`
	Semantic    *ArtifactSemanticQuery `json:"semantic,omitempty"`
}

// LayeredBundle is deterministic layered context with turn/slice refs.
type LayeredBundle struct {
	SessionID string            `json:"session_id"`
	Content   string            `json:"content"`
	Layers    map[string]string `json:"layers,omitempty"`

	Refs          []string `json:"refs,omitempty"`
	TurnRefs      []string `json:"turn_refs,omitempty"`
	SliceRefs     []string `json:"slice_refs,omitempty"`
	EpisodeRefs   []string `json:"episode_refs,omitempty"`
	NarrativeRefs []string `json:"narrative_refs,omitempty"`
	ArtifactRefs  []string `json:"artifact_refs,omitempty"`

	Meta map[string]any `json:"meta,omitempty"`
}

// SetCompanionProvider configures optional companion-memory integration.
func (b *Builder) SetCompanionProvider(provider CompanionProvider) {
	if b == nil {
		return
	}
	b.companion = provider
}

// SetArtifactRetriever configures optional semantic artifact retrieval.
func (b *Builder) SetArtifactRetriever(retriever run.ArtifactSemanticRetriever) {
	if b == nil {
		return
	}
	b.artifactSearcher = retriever
}

// BuildLayered assembles deterministic layered context (L2 -> L1 -> L0).
func (b *Builder) BuildLayered(ctx context.Context, req LayeredRequest) (LayeredBundle, error) {
	if b == nil || b.reader == nil {
		return LayeredBundle{}, ErrMissingReader
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return LayeredBundle{}, ErrMissingSessionID
	}

	monthsLimit := req.MonthsLimit
	if monthsLimit <= 0 {
		monthsLimit = 2
	}
	daysLimit := req.DaysLimit
	if daysLimit <= 0 {
		daysLimit = 7
	}
	hoursLimit := req.HoursLimit
	if hoursLimit <= 0 {
		hoursLimit = 12
	}

	l2Temporal, err := b.BuildTemporal(ctx, TemporalRequest{
		SessionID: sessionID,
		View:      ViewMonths,
		Limit:     monthsLimit,
	})
	if err != nil {
		return LayeredBundle{}, err
	}
	l1Temporal, err := b.BuildTemporal(ctx, TemporalRequest{
		SessionID: sessionID,
		View:      ViewDays,
		Limit:     daysLimit,
	})
	if err != nil {
		return LayeredBundle{}, err
	}
	l0Temporal, err := b.BuildTemporal(ctx, TemporalRequest{
		SessionID: sessionID,
		View:      ViewHours,
		Limit:     hoursLimit,
	})
	if err != nil {
		return LayeredBundle{}, err
	}
	episodeSince, episodeUntil := temporalBounds(l2Temporal.Buckets)
	episodeRefs, episodeSection, episodeCount, episodeLandmarkCount := b.resolveEpisodeLayer(
		ctx,
		sessionID,
		monthsLimit,
		episodeSince,
		episodeUntil,
	)
	narrativeRefs, narrativeSection, narrativePresent, narrativeVersion, narrativeClaimCount, narrativeStale, narrativeAgeSeconds, narrativeMaxAgeSeconds, narrativeErr := b.resolveNarrative(
		ctx,
		sessionID,
	)

	companionCtx := CompanionLayeredContext{}
	hasSemanticBudget := req.Semantic != nil && len(req.Semantic.QueryEmbedding) > 0 && b.artifactSearcher != nil
	budget := resolveLayerBudget(req, hasSemanticBudget)
	if b.companion != nil {
		companionCtx, err = b.companion.GetLayeredContext(ctx, sessionID, CompanionRequest{
			MaxChars: budget.TotalChars,
		})
		if err != nil {
			return LayeredBundle{}, err
		}
	}
	suppressTemporalSamples := companionMetaBool(companionCtx.Meta, "suppress_temporal_samples")
	suppressL0Temporal := companionMetaBool(companionCtx.Meta, "suppress_l0_temporal")

	l2Section := joinSections(
		strings.TrimSpace(companionCtx.L2),
		strings.TrimSpace(episodeSection),
		strings.TrimSpace(renderTemporalLayerWithOptions(l2Temporal, !suppressTemporalSamples)),
	)
	l1Section := joinSections(
		strings.TrimSpace(companionCtx.L1),
		strings.TrimSpace(renderTemporalLayerWithOptions(l1Temporal, !suppressTemporalSamples)),
	)
	l0TemporalSection := ""
	if !(suppressL0Temporal && strings.TrimSpace(companionCtx.L0) != "") {
		l0TemporalSection = strings.TrimSpace(renderTemporalLayerWithOptions(l0Temporal, !suppressTemporalSamples))
	}
	l0Section := joinSections(
		strings.TrimSpace(companionCtx.L0),
		l0TemporalSection,
	)

	semanticRefs, semanticSection, semanticPath, semanticCapability, semanticApplied, semanticFallbackLevel, semanticEligibleCount, semanticErr := b.resolveSemanticArtifacts(ctx, sessionID, req.Semantic)
	if budget.TotalChars > 0 {
		if budget.L2Chars > 0 {
			l2Section = truncateRunes(l2Section, budget.L2Chars)
		} else {
			l2Section = ""
		}
		if budget.L1Chars > 0 {
			l1Section = truncateRunes(l1Section, budget.L1Chars)
		} else {
			l1Section = ""
		}
		if budget.L0Chars > 0 {
			l0Section = truncateRunes(l0Section, budget.L0Chars)
		} else {
			l0Section = ""
		}
		if strings.TrimSpace(semanticSection) != "" {
			if budget.SemanticChars > 0 {
				semanticSection = truncateRunes(semanticSection, budget.SemanticChars)
			} else {
				semanticSection = ""
			}
		}
	}

	content := renderLayeredContent(l2Section, narrativeSection, l1Section, l0Section, semanticSection)
	if budget.TotalChars > 0 {
		content = truncateRunes(content, budget.TotalChars)
	}

	turnRefs := collectTurnRefs(l0Temporal, l1Temporal)
	sliceRefs := b.deriveSliceRefs(ctx, turnRefs, 5)

	refs := uniqueStrings(append(
		append(
			append([]string(nil), companionCtx.Refs...),
			l2Temporal.ExpandableRefs...,
		),
		append(append(append(append(append(l1Temporal.ExpandableRefs, l0Temporal.ExpandableRefs...), sliceRefs...), episodeRefs...), narrativeRefs...), semanticRefs...)...,
	))

	meta := map[string]any{
		"session_id":                     sessionID,
		"has_companion":                  b.companion != nil,
		"l2_bucket_count":                len(l2Temporal.Buckets),
		"l1_bucket_count":                len(l1Temporal.Buckets),
		"l0_bucket_count":                len(l0Temporal.Buckets),
		"episode_count":                  episodeCount,
		"episode_landmark_count":         episodeLandmarkCount,
		"narrative_present":              narrativePresent,
		"narrative_version":              narrativeVersion,
		"narrative_claim_count":          narrativeClaimCount,
		"narrative_ref_count":            len(narrativeRefs),
		"narrative_stale":                narrativeStale,
		"narrative_age_seconds":          narrativeAgeSeconds,
		"narrative_max_age_seconds":      narrativeMaxAgeSeconds,
		"expandable_date_cnt":            len(uniqueStrings(append(l1Temporal.ExpandableDates, l2Temporal.ExpandableDates...))),
		"artifact_search_path":           string(semanticPath),
		"artifact_vector_capability":     string(semanticCapability),
		"artifact_hit_count":             len(semanticRefs),
		"artifact_hit_bucket":            semanticHitBucket(len(semanticRefs)),
		"working_context_applied":        semanticApplied,
		"working_context_fallback_level": semanticFallbackLevel,
		"working_context_eligible_count": semanticEligibleCount,
		"budget_total_chars":             budget.TotalChars,
		"budget_l2_chars":                budget.L2Chars,
		"budget_l1_chars":                budget.L1Chars,
		"budget_l0_chars":                budget.L0Chars,
		"budget_semantic_chars":          budget.SemanticChars,
	}
	if semanticErr != "" {
		meta["artifact_search_error"] = semanticErr
	}
	if strings.TrimSpace(narrativeErr) != "" {
		meta["narrative_error"] = strings.TrimSpace(narrativeErr)
	}

	bundle := LayeredBundle{
		SessionID: sessionID,
		Content:   content,
		Layers: map[string]string{
			"L2": l2Section,
			"L1": l1Section,
			"L0": l0Section,
		},
		Refs:          refs,
		TurnRefs:      turnRefs,
		SliceRefs:     sliceRefs,
		EpisodeRefs:   episodeRefs,
		NarrativeRefs: narrativeRefs,
		ArtifactRefs:  semanticRefs,
		Meta:          meta,
	}
	b.emitLayeredBundleEvent(ctx, bundle)
	return bundle, nil
}

func collectTurnRefs(temporal ...TemporalBundle) []string {
	var refs []string
	for _, bundle := range temporal {
		for _, bucket := range bundle.Buckets {
			refs = append(refs, bucket.Refs...)
		}
	}
	return uniqueStrings(refs)
}

func renderTemporalLayerWithOptions(bundle TemporalBundle, includeSample bool) string {
	if len(bundle.Buckets) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, bucket := range bundle.Buckets {
		sb.WriteString("- ")
		sb.WriteString(bucket.Key)
		sb.WriteString(": ")
		sb.WriteString(fmt.Sprintf("%d turns, %d tool calls", bucket.TurnCount, bucket.ToolCalls))
		summary := strings.TrimSpace(bucket.Summary)
		if !includeSample {
			summary = stripTemporalSample(summary)
		}
		if strings.TrimSpace(summary) != "" {
			sb.WriteString(" (")
			sb.WriteString(strings.TrimSpace(summary))
			sb.WriteString(")")
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func companionMetaBool(meta map[string]any, key string) bool {
	if len(meta) == 0 {
		return false
	}
	raw, ok := meta[key]
	if !ok {
		return false
	}
	value, ok := raw.(bool)
	return ok && value
}

func stripTemporalSample(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	idx := strings.Index(summary, `; sample "`)
	if idx >= 0 {
		return strings.TrimSpace(summary[:idx])
	}
	return summary
}

func renderLayeredContent(l2, narrative, l1, l0, semantic string) string {
	var sections []string
	if strings.TrimSpace(l2) != "" {
		sections = append(sections, "## L2 History\n"+strings.TrimSpace(l2))
	}
	if strings.TrimSpace(narrative) != "" {
		sections = append(sections, "## Narrative\n"+strings.TrimSpace(narrative))
	}
	if strings.TrimSpace(l1) != "" {
		sections = append(sections, "## L1 Recent\n"+strings.TrimSpace(l1))
	}
	if strings.TrimSpace(l0) != "" {
		sections = append(sections, "## L0 Vivid\n"+strings.TrimSpace(l0))
	}
	if strings.TrimSpace(semantic) != "" {
		sections = append(sections, "## Semantic Artifacts\n"+strings.TrimSpace(semantic))
	}
	return strings.Join(sections, "\n\n")
}

func resolveLayerBudget(req LayeredRequest, hasSemantic bool) LayerBudget {
	total := req.MaxChars
	if req.Budget != nil && req.Budget.TotalChars > 0 {
		total = req.Budget.TotalChars
	}
	if total <= 0 {
		return LayerBudget{}
	}

	l2 := percentOf(total, defaultL2BudgetPct)
	l1 := percentOf(total, defaultL1BudgetPct)
	l0 := percentOf(total, defaultL0BudgetPct)
	semantic := percentOf(total, defaultSemanticBudgetPct)
	if !hasSemantic {
		l0 += semantic
		semantic = 0
	}

	if req.Budget != nil {
		if req.Budget.L2Chars > 0 {
			l2 = req.Budget.L2Chars
		}
		if req.Budget.L1Chars > 0 {
			l1 = req.Budget.L1Chars
		}
		if req.Budget.L0Chars > 0 {
			l0 = req.Budget.L0Chars
		}
		if req.Budget.SemanticChars > 0 {
			semantic = req.Budget.SemanticChars
		}
	}

	remaining := total
	l2 = clampToRemaining(l2, &remaining)
	l1 = clampToRemaining(l1, &remaining)
	l0 = clampToRemaining(l0, &remaining)
	semantic = clampToRemaining(semantic, &remaining)
	l0 += remaining

	return LayerBudget{
		TotalChars:    total,
		L2Chars:       l2,
		L1Chars:       l1,
		L0Chars:       l0,
		SemanticChars: semantic,
	}
}

func percentOf(total int, pct int) int {
	if total <= 0 || pct <= 0 {
		return 0
	}
	return (total * pct) / 100
}

func clampToRemaining(value int, remaining *int) int {
	if remaining == nil || *remaining <= 0 || value <= 0 {
		return 0
	}
	if value > *remaining {
		value = *remaining
	}
	*remaining -= value
	return value
}

func (b *Builder) resolveSemanticArtifacts(
	ctx context.Context,
	defaultSessionID string,
	query *ArtifactSemanticQuery,
) (
	refs []string,
	section string,
	path run.ArtifactSearchPath,
	capability run.ArtifactVectorCapability,
	workingApplied bool,
	workingFallbackLevel int,
	workingEligibleCount int,
	errText string,
) {
	path = run.ArtifactSearchPathDisabled
	capability = run.ArtifactVectorCapabilityUnknown
	if query == nil || len(query.QueryEmbedding) == 0 {
		return nil, "", path, capability, false, 0, 0, ""
	}

	startedAt := time.Now()
	sessionID := strings.TrimSpace(query.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(defaultSessionID)
	}

	finalize := func(
		refs []string,
		section string,
		path run.ArtifactSearchPath,
		capability run.ArtifactVectorCapability,
		workingApplied bool,
		workingFallbackLevel int,
		workingEligibleCount int,
		errText string,
	) ([]string, string, run.ArtifactSearchPath, run.ArtifactVectorCapability, bool, int, int, string) {
		if b != nil {
			elapsed := time.Since(startedAt)
			b.recordArtifactSearch(path, capability, len(refs), elapsed)
			b.emitSemanticArtifactSearchEvent(
				ctx,
				sessionID,
				query,
				path,
				capability,
				workingApplied,
				workingFallbackLevel,
				workingEligibleCount,
				len(refs),
				errText,
				elapsed,
			)
		}
		return refs, section, path, capability, workingApplied, workingFallbackLevel, workingEligibleCount, errText
	}
	if b == nil || b.artifactSearcher == nil {
		return finalize(nil, "", path, run.ArtifactVectorCapabilityDisabled, false, 0, 0, "")
	}

	searchResult, err := b.artifactSearcher.SearchArtifactsByEmbedding(ctx, query.QueryEmbedding, run.ArtifactSearchOptions{
		SessionID:     sessionID,
		ArtifactTypes: append([]string(nil), query.ArtifactTypes...),
		Limit:         query.Limit,
		MinSimilarity: query.MinSimilarity,
		Working:       query.Working,
	})
	if err != nil {
		return finalize(nil, "", run.ArtifactSearchPathError, capability, false, 0, 0, strings.TrimSpace(err.Error()))
	}

	path = searchResult.SearchPath
	if strings.TrimSpace(string(path)) == "" {
		path = run.ArtifactSearchPathVector
	}
	capability = searchResult.VectorCapability
	if strings.TrimSpace(string(capability)) == "" {
		capability = run.ArtifactVectorCapabilityUnknown
	}

	hits := normalizeSemanticHits(searchResult.Hits, query.MinSimilarity, query.Limit)
	if len(hits) == 0 {
		return finalize(
			nil,
			"",
			path,
			capability,
			searchResult.WorkingApplied,
			searchResult.FallbackLevel,
			searchResult.EligibleCount,
			"",
		)
	}

	lines := make([]string, 0, len(hits))
	refs = make([]string, 0, len(hits))
	for _, hit := range hits {
		ref := semanticRef(hit)
		if ref == "" {
			continue
		}
		refs = append(refs, ref)

		line := fmt.Sprintf("- %s (sim=%.3f", ref, hit.Similarity)
		if strings.TrimSpace(hit.ArtifactType) != "" {
			line += ", type=" + strings.TrimSpace(hit.ArtifactType)
		}
		line += ")"
		if summary := strings.TrimSpace(hit.Summary); summary != "" {
			line += ": " + truncateRunes(summary, 140)
		}
		lines = append(lines, line)
	}

	refs = uniqueStrings(refs)
	return finalize(
		refs,
		strings.Join(lines, "\n"),
		path,
		capability,
		searchResult.WorkingApplied,
		searchResult.FallbackLevel,
		searchResult.EligibleCount,
		"",
	)
}

func (b *Builder) emitSemanticArtifactSearchEvent(
	ctx context.Context,
	sessionID string,
	query *ArtifactSemanticQuery,
	path run.ArtifactSearchPath,
	capability run.ArtifactVectorCapability,
	workingApplied bool,
	workingFallbackLevel int,
	workingEligibleCount int,
	hitCount int,
	errText string,
	duration time.Duration,
) {
	if b == nil || query == nil || len(query.QueryEmbedding) == 0 {
		return
	}

	event := observability.NewEvent(observability.OpContextSemanticArtifactSearch).
		WithComponent(observability.ComponentContextBuilder).
		WithCommand("contextbuilder.layered").
		WithSession(sessionID, "").
		WithData("search_path", string(path)).
		WithData("vector_capability", string(capability)).
		WithData("hit_count", hitCount).
		WithData("latency_bucket", semanticLatencyBucket(duration)).
		WithData("hit_bucket", semanticHitBucket(hitCount)).
		WithData("session_id", sessionID).
		WithData("query_dims", len(query.QueryEmbedding)).
		WithData("working_context_applied", workingApplied).
		WithData("working_context_fallback_level", workingFallbackLevel).
		WithData("working_context_eligible_count", workingEligibleCount).
		EnrichFromContext(ctx)

	if parentID := strings.TrimSpace(observability.SpanIDFromContext(ctx)); parentID != "" {
		event.WithParentID(parentID)
	}

	if len(query.ArtifactTypes) > 0 {
		artifactTypes := append([]string(nil), query.ArtifactTypes...)
		sort.Strings(artifactTypes)
		event.WithData("artifact_types", artifactTypes)
	}
	if query.MinSimilarity > 0 {
		event.WithData("min_similarity", query.MinSimilarity)
	}
	if query.Limit > 0 {
		event.WithData("limit", query.Limit)
	}

	if strings.TrimSpace(errText) != "" || path == run.ArtifactSearchPathError {
		emitWideEventAsync(ctx, event.ErrorWithDetails(
			"semantic_retrieval",
			"ESEMANTIC_RETRIEVAL",
			strings.TrimSpace(errText),
			true,
			duration))
		return
	}
	emitWideEventAsync(ctx, event.Success(duration))
}

func semanticLatencyBucket(duration time.Duration) string {
	switch {
	case duration <= 10*time.Millisecond:
		return "le_10ms"
	case duration <= 50*time.Millisecond:
		return "le_50ms"
	case duration <= 100*time.Millisecond:
		return "le_100ms"
	default:
		return "gt_100ms"
	}
}

func semanticHitBucket(hitCount int) string {
	switch {
	case hitCount <= 0:
		return "zero"
	case hitCount <= 3:
		return "one_to_three"
	case hitCount <= 10:
		return "four_to_ten"
	default:
		return "gt_ten"
	}
}

func emitWideEventAsync(ctx context.Context, event *observability.WideEvent) {
	if event == nil {
		return
	}
	semanticEventQueueOnce.Do(func() {
		go func() {
			for job := range semanticEventQueue {
				if job.event == nil {
					continue
				}
				emitCtx := job.ctx
				if emitCtx == nil {
					emitCtx = context.Background()
				}
				observability.Emit(emitCtx, job.event)
			}
		}()
	})

	select {
	case semanticEventQueue <- semanticEventJob{
		ctx:   ctx,
		event: event,
	}:
	default:
		// Preserve non-blocking behavior under burst load by dropping low-priority telemetry.
	}
}

func (b *Builder) emitLayeredBundleEvent(ctx context.Context, bundle LayeredBundle) {
	sessionID := strings.TrimSpace(bundle.SessionID)
	if sessionID == "" {
		return
	}

	event := observability.NewEvent(observability.OpContextLayeredBundle).
		WithComponent(observability.ComponentContextBuilder).
		WithCommand("contextbuilder.layered").
		WithSession(sessionID, "").
		WithData("session_id", sessionID).
		WithData("ref_count", len(bundle.Refs)).
		WithData("turn_ref_count", len(bundle.TurnRefs)).
		WithData("slice_ref_count", len(bundle.SliceRefs)).
		WithData("episode_ref_count", len(bundle.EpisodeRefs)).
		WithData("narrative_ref_count", len(bundle.NarrativeRefs)).
		WithData("artifact_ref_count", len(bundle.ArtifactRefs)).
		WithData("refs", clampRefList(bundle.Refs, layeredEventMaxRefs)).
		WithData("turn_refs", clampRefList(bundle.TurnRefs, layeredEventMaxRefs)).
		WithData("slice_refs", clampRefList(bundle.SliceRefs, layeredEventMaxRefs)).
		WithData("episode_refs", clampRefList(bundle.EpisodeRefs, layeredEventMaxRefs)).
		WithData("narrative_refs", clampRefList(bundle.NarrativeRefs, layeredEventMaxRefs)).
		WithData("artifact_refs", clampRefList(bundle.ArtifactRefs, layeredEventMaxRefs)).
		EnrichFromContext(ctx)

	if parentID := strings.TrimSpace(observability.SpanIDFromContext(ctx)); parentID != "" {
		event.WithParentID(parentID)
	}
	if bundle.Meta != nil {
		if v, ok := bundle.Meta["artifact_search_path"]; ok {
			event.WithData("artifact_search_path", v)
		}
		if v, ok := bundle.Meta["artifact_vector_capability"]; ok {
			event.WithData("artifact_vector_capability", v)
		}
		if v, ok := bundle.Meta["artifact_hit_count"]; ok {
			event.WithData("artifact_hit_count", v)
		}
		if v, ok := bundle.Meta["narrative_stale"]; ok {
			event.WithData("narrative_stale", v)
		}
		if v, ok := bundle.Meta["working_context_applied"]; ok {
			event.WithData("working_context_applied", v)
		}
	}
	emitWideEventAsync(ctx, event.Success(0))
}

func normalizeSemanticHits(hits []run.ScoredArtifact, minSimilarity float64, limit int) []run.ScoredArtifact {
	filtered := make([]run.ScoredArtifact, 0, len(hits))
	for _, hit := range hits {
		if hit.Similarity < minSimilarity {
			continue
		}
		filtered = append(filtered, hit.Clone())
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Similarity == filtered[j].Similarity {
			return semanticRef(filtered[i]) < semanticRef(filtered[j])
		}
		return filtered[i].Similarity > filtered[j].Similarity
	})

	out := make([]run.ScoredArtifact, 0, len(filtered))
	seen := make(map[string]struct{}, len(filtered))
	for _, hit := range filtered {
		ref := semanticRef(hit)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, hit)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func semanticRef(hit run.ScoredArtifact) string {
	ref := strings.TrimSpace(hit.Ref)
	if ref != "" {
		return ref
	}
	turnID := strings.TrimSpace(hit.TurnID)
	artifactType := strings.TrimSpace(strings.ToLower(hit.ArtifactType))
	artifactVersion := strings.TrimSpace(hit.ArtifactVersion)
	if turnID == "" || artifactType == "" || artifactVersion == "" {
		return ""
	}
	return fmt.Sprintf("turn/%s/artifact/%s/%s", turnID, artifactType, artifactVersion)
}

func clampRefList(refs []string, max int) []string {
	refs = uniqueStrings(refs)
	if max <= 0 || len(refs) <= max {
		return append([]string(nil), refs...)
	}
	out := make([]string, max)
	copy(out, refs[:max])
	return out
}

func joinSections(parts ...string) string {
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "\n\n")
}

func (b *Builder) resolveNarrative(
	ctx context.Context,
	sessionID string,
) (
	refs []string,
	section string,
	present bool,
	version string,
	claimCount int,
	stale bool,
	ageSeconds int64,
	maxAgeSeconds int64,
	errText string,
) {
	maxAgeSeconds = int64(narrativeMaxAge.Seconds())
	if b == nil || b.narrativeReader == nil {
		return nil, "", false, "", 0, false, 0, maxAgeSeconds, ""
	}
	narrative, err := b.narrativeReader.GetNarrative(ctx, sessionID, "v1")
	if errors.Is(err, run.ErrNarrativeNotFound) {
		return nil, "", false, "", 0, false, 0, maxAgeSeconds, ""
	}
	if err != nil {
		return nil, "", false, "", 0, false, 0, maxAgeSeconds, strings.TrimSpace(err.Error())
	}

	narrative = narrative.Clone()
	present = true
	version = strings.TrimSpace(narrative.ArtifactVersion)
	claimCount = len(narrative.Claims)
	now := b.now().UTC()
	if !narrative.UpdatedAt.IsZero() {
		age := now.Sub(narrative.UpdatedAt.UTC())
		if age < 0 {
			age = 0
		}
		ageSeconds = int64(age.Seconds())
		stale = age >= narrativeMaxAge
	}

	lines := make([]string, 0, 1+len(narrative.Claims))
	if summary := strings.TrimSpace(narrative.Summary); summary != "" {
		lines = append(lines, "- "+truncateRunes(summary, 220))
	}
	for _, claim := range narrative.Claims {
		text := strings.TrimSpace(claim.Text)
		if text == "" {
			continue
		}
		claimRefs := uniqueStrings(claim.AnchorRefs)
		line := "- " + truncateRunes(text, 180)
		if len(claimRefs) > 0 {
			line += " [" + strings.Join(claimRefs, ", ") + "]"
		}
		lines = append(lines, line)
	}

	if ref := strings.TrimSpace(narrative.Ref); ref != "" {
		refs = append(refs, ref)
	}
	refs = append(refs, uniqueStrings(narrative.AnchorRefs)...)
	refs = uniqueStrings(refs)
	if len(lines) == 0 {
		return refs, "", present, version, claimCount, stale, ageSeconds, maxAgeSeconds, ""
	}
	return refs, strings.Join(lines, "\n"), present, version, claimCount, stale, ageSeconds, maxAgeSeconds, ""
}

func (b *Builder) resolveEpisodeLayer(
	ctx context.Context,
	sessionID string,
	limit int,
	since time.Time,
	until time.Time,
) (refs []string, section string, count int, landmarkCount int) {
	if b == nil || b.episodeReader == nil {
		return nil, "", 0, 0
	}
	if limit <= 0 {
		limit = 6
	}
	episodes, err := b.episodeReader.ListEpisodes(ctx, sessionID, run.EpisodeListOptions{
		Limit: limit,
		Asc:   false,
		Since: since,
		Until: until,
	})
	if err != nil {
		event := observability.NewEvent(observability.OpContextLayeredBundle).
			WithComponent(observability.ComponentContextBuilder).
			WithCommand("contextbuilder.layered").
			WithSession(sessionID, "").
			WithData("session_id", sessionID).
			WithData("phase", "episode_layer").
			WithData("limit", limit).
			EnrichFromContext(ctx)
		if !since.IsZero() {
			event.WithData("since", since.UTC().Format(time.RFC3339))
		}
		if !until.IsZero() {
			event.WithData("until", until.UTC().Format(time.RFC3339))
		}
		emitWideEventAsync(ctx, event.ErrorWithDetails(
			"episode_layer",
			"EEPISODE_LAYER",
			strings.TrimSpace(err.Error()),
			true,
			0))
		return nil, "", 0, 0
	}
	if len(episodes) == 0 {
		return nil, "", 0, 0
	}

	lines := make([]string, 0, len(episodes))
	refs = make([]string, 0, len(episodes)*2)
	for _, episode := range episodes {
		episode = episode.Clone()
		episodeID := strings.TrimSpace(episode.ID)
		if episodeID == "" {
			continue
		}
		if episode.IsLandmark {
			landmarkCount++
		}
		refs = append(refs, "episode/"+episodeID)
		refs = append(refs, episode.AnchorRefs...)

		label := strings.TrimSpace(episode.Topic)
		if label == "" {
			label = episode.BoundaryKey
		}
		line := fmt.Sprintf("- %s [%d-%d]", label, episode.StartTurnIndex, episode.EndTurnIndex)
		if episode.IsLandmark {
			line += " [landmark]"
		}
		if summary := strings.TrimSpace(episode.Summary); summary != "" {
			line += ": " + truncateRunes(summary, 140)
		}
		lines = append(lines, line)
	}

	refs = uniqueStrings(refs)
	count = len(lines)
	if count == 0 {
		return refs, "", 0, 0
	}
	return refs, strings.Join(lines, "\n"), count, landmarkCount
}

func temporalBounds(buckets []TemporalBucket) (since time.Time, until time.Time) {
	if len(buckets) == 0 {
		return time.Time{}, time.Time{}
	}
	for _, bucket := range buckets {
		if bucket.StartAt.IsZero() && bucket.EndAt.IsZero() {
			continue
		}
		if since.IsZero() || (!bucket.StartAt.IsZero() && bucket.StartAt.Before(since)) {
			since = bucket.StartAt
		}
		if until.IsZero() || (!bucket.EndAt.IsZero() && bucket.EndAt.After(until)) {
			until = bucket.EndAt
		}
	}
	return since, until
}

func uniqueStrings(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (b *Builder) deriveSliceRefs(ctx context.Context, turnRefs []string, maxRefs int) []string {
	if b == nil || b.reader == nil || maxRefs <= 0 {
		return nil
	}

	type rankedTurn struct {
		ID        string
		CreatedAt int64
	}
	ranked := make([]rankedTurn, 0, len(turnRefs))
	for _, ref := range turnRefs {
		parsed, err := ParseRef(ref)
		if err != nil || parsed.Kind != RefWholeTurn {
			continue
		}
		turn, err := b.reader.GetTurn(ctx, parsed.TurnID)
		if err != nil {
			continue
		}
		ranked = append(ranked, rankedTurn{
			ID:        turn.ID,
			CreatedAt: turn.CreatedAt.UTC().UnixNano(),
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].CreatedAt == ranked[j].CreatedAt {
			return ranked[i].ID < ranked[j].ID
		}
		return ranked[i].CreatedAt > ranked[j].CreatedAt
	})

	out := make([]string, 0, maxRefs)
	for _, item := range ranked {
		if len(out) >= maxRefs {
			break
		}
		turn, err := b.reader.GetTurn(ctx, item.ID)
		if err != nil {
			continue
		}
		msgID := strings.TrimSpace(turn.FinalOutput.ID)
		if msgID == "" {
			continue
		}
		text := strings.TrimSpace(turn.FinalOutput.Text)
		if text == "" {
			continue
		}
		end := len([]rune(text))
		if end > 240 {
			end = 240
		}
		if end <= 0 {
			continue
		}
		out = append(out, fmt.Sprintf("turn/%s#msg:%s:0-%d", turn.ID, msgID, end))
	}

	return uniqueStrings(out)
}
