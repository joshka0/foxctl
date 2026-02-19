package contextbuilder

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

const semanticEventQueueCapacity = 128

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
	QueryEmbedding []float32 `json:"query_embedding,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	ArtifactTypes  []string  `json:"artifact_types,omitempty"`
	Limit          int       `json:"limit,omitempty"`
	MinSimilarity  float64   `json:"min_similarity,omitempty"`
}

// LayeredRequest asks for L2 -> L1 -> L0 context assembly.
type LayeredRequest struct {
	SessionID   string                 `json:"session_id"`
	MaxChars    int                    `json:"max_chars,omitempty"`
	MonthsLimit int                    `json:"months_limit,omitempty"`
	DaysLimit   int                    `json:"days_limit,omitempty"`
	HoursLimit  int                    `json:"hours_limit,omitempty"`
	Semantic    *ArtifactSemanticQuery `json:"semantic,omitempty"`
}

// LayeredBundle is deterministic layered context with turn/slice refs.
type LayeredBundle struct {
	SessionID string            `json:"session_id"`
	Content   string            `json:"content"`
	Layers    map[string]string `json:"layers,omitempty"`

	Refs         []string `json:"refs,omitempty"`
	TurnRefs     []string `json:"turn_refs,omitempty"`
	SliceRefs    []string `json:"slice_refs,omitempty"`
	ArtifactRefs []string `json:"artifact_refs,omitempty"`

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

	companionCtx := CompanionLayeredContext{}
	if b.companion != nil {
		companionCtx, err = b.companion.GetLayeredContext(ctx, sessionID, CompanionRequest{
			MaxChars: req.MaxChars,
		})
		if err != nil {
			return LayeredBundle{}, err
		}
	}

	l2Section := joinSections(
		strings.TrimSpace(companionCtx.L2),
		strings.TrimSpace(renderTemporalLayer(l2Temporal)),
	)
	l1Section := joinSections(
		strings.TrimSpace(companionCtx.L1),
		strings.TrimSpace(renderTemporalLayer(l1Temporal)),
	)
	l0Section := joinSections(
		strings.TrimSpace(companionCtx.L0),
		strings.TrimSpace(renderTemporalLayer(l0Temporal)),
	)

	semanticRefs, semanticSection, semanticPath, semanticErr := b.resolveSemanticArtifacts(ctx, sessionID, req.Semantic)
	content := renderLayeredContent(l2Section, l1Section, l0Section, semanticSection)
	if req.MaxChars > 0 {
		content = truncateRunes(content, req.MaxChars)
	}

	turnRefs := collectTurnRefs(l0Temporal, l1Temporal)
	sliceRefs := b.deriveSliceRefs(ctx, turnRefs, 5)

	refs := uniqueStrings(append(
		append(
			append([]string(nil), companionCtx.Refs...),
			l2Temporal.ExpandableRefs...,
		),
		append(append(append(l1Temporal.ExpandableRefs, l0Temporal.ExpandableRefs...), sliceRefs...), semanticRefs...)...,
	))

	meta := map[string]any{
		"session_id":           sessionID,
		"has_companion":        b.companion != nil,
		"l2_bucket_count":      len(l2Temporal.Buckets),
		"l1_bucket_count":      len(l1Temporal.Buckets),
		"l0_bucket_count":      len(l0Temporal.Buckets),
		"expandable_date_cnt":  len(uniqueStrings(append(l1Temporal.ExpandableDates, l2Temporal.ExpandableDates...))),
		"artifact_search_path": string(semanticPath),
		"artifact_hit_count":   len(semanticRefs),
	}
	if semanticErr != "" {
		meta["artifact_search_error"] = semanticErr
	}

	return LayeredBundle{
		SessionID: sessionID,
		Content:   content,
		Layers: map[string]string{
			"L2": l2Section,
			"L1": l1Section,
			"L0": l0Section,
		},
		Refs:         refs,
		TurnRefs:     turnRefs,
		SliceRefs:    sliceRefs,
		ArtifactRefs: semanticRefs,
		Meta:         meta,
	}, nil
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

func renderTemporalLayer(bundle TemporalBundle) string {
	if len(bundle.Buckets) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, bucket := range bundle.Buckets {
		sb.WriteString("- ")
		sb.WriteString(bucket.Key)
		sb.WriteString(": ")
		sb.WriteString(fmt.Sprintf("%d turns, %d tool calls", bucket.TurnCount, bucket.ToolCalls))
		if strings.TrimSpace(bucket.Summary) != "" {
			sb.WriteString(" (")
			sb.WriteString(strings.TrimSpace(bucket.Summary))
			sb.WriteString(")")
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func renderLayeredContent(l2, l1, l0, semantic string) string {
	var sections []string
	if strings.TrimSpace(l2) != "" {
		sections = append(sections, "## L2 History\n"+strings.TrimSpace(l2))
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

func (b *Builder) resolveSemanticArtifacts(
	ctx context.Context,
	defaultSessionID string,
	query *ArtifactSemanticQuery,
) (refs []string, section string, path run.ArtifactSearchPath, errText string) {
	path = run.ArtifactSearchPathDisabled
	if query == nil || len(query.QueryEmbedding) == 0 {
		return nil, "", path, ""
	}

	startedAt := time.Now()
	sessionID := strings.TrimSpace(query.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(defaultSessionID)
	}

	finalize := func(refs []string, section string, path run.ArtifactSearchPath, errText string) ([]string, string, run.ArtifactSearchPath, string) {
		if b != nil {
			b.recordArtifactSearch(path, len(refs))
			b.emitSemanticArtifactSearchEvent(ctx, sessionID, query, path, len(refs), errText, time.Since(startedAt))
		}
		return refs, section, path, errText
	}
	if b == nil || b.artifactSearcher == nil {
		return finalize(nil, "", path, "")
	}

	searchResult, err := b.artifactSearcher.SearchArtifactsByEmbedding(ctx, query.QueryEmbedding, run.ArtifactSearchOptions{
		SessionID:     sessionID,
		ArtifactTypes: append([]string(nil), query.ArtifactTypes...),
		Limit:         query.Limit,
		MinSimilarity: query.MinSimilarity,
	})
	if err != nil {
		return finalize(nil, "", run.ArtifactSearchPathError, strings.TrimSpace(err.Error()))
	}

	path = searchResult.SearchPath
	if strings.TrimSpace(string(path)) == "" {
		path = run.ArtifactSearchPathVector
	}

	hits := normalizeSemanticHits(searchResult.Hits, query.MinSimilarity, query.Limit)
	if len(hits) == 0 {
		return finalize(nil, "", path, "")
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
	return finalize(refs, strings.Join(lines, "\n"), path, "")
}

func (b *Builder) emitSemanticArtifactSearchEvent(
	ctx context.Context,
	sessionID string,
	query *ArtifactSemanticQuery,
	path run.ArtifactSearchPath,
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
		WithData("hit_count", hitCount).
		WithData("session_id", sessionID).
		WithData("query_dims", len(query.QueryEmbedding)).
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
