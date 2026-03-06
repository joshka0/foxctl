package contextbuilder

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

var (
	// ErrMissingReader indicates the builder was configured without turn source.
	ErrMissingReader = errors.New("v2 contextbuilder: missing turn reader")
	// ErrMissingEpisodeReader indicates episode refs are unavailable without an episode source.
	ErrMissingEpisodeReader = errors.New("v2 contextbuilder: missing episode reader")
	// ErrMessageNotFound indicates a requested message ID is not present in the turn.
	ErrMessageNotFound = errors.New("v2 contextbuilder: message not found")
	// ErrMissingSessionID indicates temporal requests missing session identity.
	ErrMissingSessionID = errors.New("v2 contextbuilder: missing session id")
	// ErrTemporalUnsupported indicates the configured reader cannot list turns.
	ErrTemporalUnsupported = errors.New("v2 contextbuilder: temporal retrieval unsupported by turn reader")
	// ErrInvalidTemporalView indicates an unsupported temporal view.
	ErrInvalidTemporalView = errors.New("v2 contextbuilder: invalid temporal view")
)

// Request is one context assembly request.
type Request struct {
	Ref string
}

// Bundle is assembled context from one reference.
type Bundle struct {
	Ref     string         `json:"ref"`
	TurnID  string         `json:"turn_id"`
	Kind    RefKind        `json:"kind"`
	Content string         `json:"content"`
	Meta    map[string]any `json:"meta,omitempty"`
}

// TemporalView controls the requested pyramid level.
type TemporalView string

const (
	ViewHours  TemporalView = "hours"
	ViewDays   TemporalView = "days"
	ViewWeeks  TemporalView = "weeks"
	ViewMonths TemporalView = "months"
)

// TemporalRequest is one hierarchical context assembly request.
type TemporalRequest struct {
	SessionID string       `json:"session_id"`
	View      TemporalView `json:"view"`
	Limit     int          `json:"limit,omitempty"`
	Since     time.Time    `json:"since,omitempty"`
	Until     time.Time    `json:"until,omitempty"`
}

// TemporalBucket is one grouped context unit in a temporal view.
type TemporalBucket struct {
	Key            string       `json:"key"`
	View           TemporalView `json:"view"`
	StartAt        time.Time    `json:"start_at"`
	EndAt          time.Time    `json:"end_at"`
	TurnCount      int          `json:"turn_count"`
	ToolCalls      int          `json:"tool_calls"`
	Summary        string       `json:"summary,omitempty"`
	Refs           []string     `json:"refs,omitempty"`
	ExpandableRefs []string     `json:"expandable_refs,omitempty"`
}

// TemporalBundle is a hierarchical context response with drill-down metadata.
type TemporalBundle struct {
	SessionID       string           `json:"session_id"`
	View            TemporalView     `json:"view"`
	Content         string           `json:"content"`
	Buckets         []TemporalBucket `json:"buckets,omitempty"`
	ExpandableDates []string         `json:"expandable_dates,omitempty"`
	ExpandableRefs  []string         `json:"expandable_refs,omitempty"`
	Meta            map[string]any   `json:"meta,omitempty"`
}

// Builder resolves turn references to context bundles.
type Builder struct {
	reader           run.TurnReader
	episodeReader    run.EpisodeReader
	narrativeReader  run.NarrativeReader
	companion        CompanionProvider
	artifactSearcher run.ArtifactSemanticRetriever
	now              func() time.Time

	artifactSearchTotal        atomic.Int64
	artifactSearchVector       atomic.Int64
	artifactSearchFallback     atomic.Int64
	artifactSearchDisabled     atomic.Int64
	artifactSearchError        atomic.Int64
	artifactSearchTotalHits    atomic.Int64
	artifactSearchVectorHits   atomic.Int64
	artifactSearchFallbackHits atomic.Int64
	artifactSearchCapEnabled   atomic.Int64
	artifactSearchCapDisabled  atomic.Int64
	artifactSearchCapUnknown   atomic.Int64

	artifactVectorLatencyLE10MS    atomic.Int64
	artifactVectorLatencyLE50MS    atomic.Int64
	artifactVectorLatencyLE100MS   atomic.Int64
	artifactVectorLatencyGT100MS   atomic.Int64
	artifactFallbackLatencyLE10MS  atomic.Int64
	artifactFallbackLatencyLE50MS  atomic.Int64
	artifactFallbackLatencyLE100MS atomic.Int64
	artifactFallbackLatencyGT100MS atomic.Int64

	artifactVectorHitsZero       atomic.Int64
	artifactVectorHitsOneTo3     atomic.Int64
	artifactVectorHitsFourTo10   atomic.Int64
	artifactVectorHitsGT10       atomic.Int64
	artifactFallbackHitsZero     atomic.Int64
	artifactFallbackHitsOneTo3   atomic.Int64
	artifactFallbackHitsFourTo10 atomic.Int64
	artifactFallbackHitsGT10     atomic.Int64
}

// ArtifactSearchStats is a point-in-time snapshot of semantic retrieval counters.
type ArtifactSearchStats struct {
	TotalCalls    int64 `json:"total_calls"`
	VectorCalls   int64 `json:"vector_calls"`
	FallbackCalls int64 `json:"fallback_calls"`
	DisabledCalls int64 `json:"disabled_calls"`
	ErrorCalls    int64 `json:"error_calls"`

	TotalHits    int64 `json:"total_hits"`
	VectorHits   int64 `json:"vector_hits"`
	FallbackHits int64 `json:"fallback_hits"`

	VectorCapabilityEnabledCalls  int64 `json:"vector_capability_enabled_calls"`
	VectorCapabilityDisabledCalls int64 `json:"vector_capability_disabled_calls"`
	VectorCapabilityUnknownCalls  int64 `json:"vector_capability_unknown_calls"`

	VectorLatencyLE10MS    int64 `json:"vector_latency_le_10ms"`
	VectorLatencyLE50MS    int64 `json:"vector_latency_le_50ms"`
	VectorLatencyLE100MS   int64 `json:"vector_latency_le_100ms"`
	VectorLatencyGT100MS   int64 `json:"vector_latency_gt_100ms"`
	FallbackLatencyLE10MS  int64 `json:"fallback_latency_le_10ms"`
	FallbackLatencyLE50MS  int64 `json:"fallback_latency_le_50ms"`
	FallbackLatencyLE100MS int64 `json:"fallback_latency_le_100ms"`
	FallbackLatencyGT100MS int64 `json:"fallback_latency_gt_100ms"`

	VectorHitBucketZero       int64 `json:"vector_hit_bucket_zero"`
	VectorHitBucketOneTo3     int64 `json:"vector_hit_bucket_one_to_three"`
	VectorHitBucketFourTo10   int64 `json:"vector_hit_bucket_four_to_ten"`
	VectorHitBucketGT10       int64 `json:"vector_hit_bucket_gt_ten"`
	FallbackHitBucketZero     int64 `json:"fallback_hit_bucket_zero"`
	FallbackHitBucketOneTo3   int64 `json:"fallback_hit_bucket_one_to_three"`
	FallbackHitBucketFourTo10 int64 `json:"fallback_hit_bucket_four_to_ten"`
	FallbackHitBucketGT10     int64 `json:"fallback_hit_bucket_gt_ten"`
}

// New creates a context builder.
func New(reader run.TurnReader) *Builder {
	builder := &Builder{
		reader: reader,
		now:    func() time.Time { return time.Now().UTC() },
	}
	if episodeReader, ok := reader.(run.EpisodeReader); ok {
		builder.episodeReader = episodeReader
	}
	if narrativeReader, ok := reader.(run.NarrativeReader); ok {
		builder.narrativeReader = narrativeReader
	}
	return builder
}

// SetEpisodeReader configures optional episode retrieval support.
func (b *Builder) SetEpisodeReader(reader run.EpisodeReader) {
	if b == nil {
		return
	}
	b.episodeReader = reader
}

// SetNarrativeReader configures optional narrative retrieval support.
func (b *Builder) SetNarrativeReader(reader run.NarrativeReader) {
	if b == nil {
		return
	}
	b.narrativeReader = reader
}

// SetNow configures an optional clock for deterministic tests.
func (b *Builder) SetNow(now func() time.Time) {
	if b == nil || now == nil {
		return
	}
	b.now = now
}

// ArtifactStats returns semantic retrieval usage counters.
func (b *Builder) ArtifactStats() ArtifactSearchStats {
	if b == nil {
		return ArtifactSearchStats{}
	}
	return ArtifactSearchStats{
		TotalCalls:                    b.artifactSearchTotal.Load(),
		VectorCalls:                   b.artifactSearchVector.Load(),
		FallbackCalls:                 b.artifactSearchFallback.Load(),
		DisabledCalls:                 b.artifactSearchDisabled.Load(),
		ErrorCalls:                    b.artifactSearchError.Load(),
		TotalHits:                     b.artifactSearchTotalHits.Load(),
		VectorHits:                    b.artifactSearchVectorHits.Load(),
		FallbackHits:                  b.artifactSearchFallbackHits.Load(),
		VectorCapabilityEnabledCalls:  b.artifactSearchCapEnabled.Load(),
		VectorCapabilityDisabledCalls: b.artifactSearchCapDisabled.Load(),
		VectorCapabilityUnknownCalls:  b.artifactSearchCapUnknown.Load(),
		VectorLatencyLE10MS:           b.artifactVectorLatencyLE10MS.Load(),
		VectorLatencyLE50MS:           b.artifactVectorLatencyLE50MS.Load(),
		VectorLatencyLE100MS:          b.artifactVectorLatencyLE100MS.Load(),
		VectorLatencyGT100MS:          b.artifactVectorLatencyGT100MS.Load(),
		FallbackLatencyLE10MS:         b.artifactFallbackLatencyLE10MS.Load(),
		FallbackLatencyLE50MS:         b.artifactFallbackLatencyLE50MS.Load(),
		FallbackLatencyLE100MS:        b.artifactFallbackLatencyLE100MS.Load(),
		FallbackLatencyGT100MS:        b.artifactFallbackLatencyGT100MS.Load(),
		VectorHitBucketZero:           b.artifactVectorHitsZero.Load(),
		VectorHitBucketOneTo3:         b.artifactVectorHitsOneTo3.Load(),
		VectorHitBucketFourTo10:       b.artifactVectorHitsFourTo10.Load(),
		VectorHitBucketGT10:           b.artifactVectorHitsGT10.Load(),
		FallbackHitBucketZero:         b.artifactFallbackHitsZero.Load(),
		FallbackHitBucketOneTo3:       b.artifactFallbackHitsOneTo3.Load(),
		FallbackHitBucketFourTo10:     b.artifactFallbackHitsFourTo10.Load(),
		FallbackHitBucketGT10:         b.artifactFallbackHitsGT10.Load(),
	}
}

func (b *Builder) recordArtifactSearch(path run.ArtifactSearchPath, capability run.ArtifactVectorCapability, hits int, duration time.Duration) {
	if b == nil {
		return
	}

	b.artifactSearchTotal.Add(1)
	if hits > 0 {
		b.artifactSearchTotalHits.Add(int64(hits))
	}
	switch capability {
	case run.ArtifactVectorCapabilityEnabled:
		b.artifactSearchCapEnabled.Add(1)
	case run.ArtifactVectorCapabilityDisabled:
		b.artifactSearchCapDisabled.Add(1)
	default:
		b.artifactSearchCapUnknown.Add(1)
	}

	switch path {
	case run.ArtifactSearchPathVector:
		b.artifactSearchVector.Add(1)
		if hits > 0 {
			b.artifactSearchVectorHits.Add(int64(hits))
		}
		recordLatencyBucket(duration,
			&b.artifactVectorLatencyLE10MS,
			&b.artifactVectorLatencyLE50MS,
			&b.artifactVectorLatencyLE100MS,
			&b.artifactVectorLatencyGT100MS,
		)
		recordHitBucket(hits,
			&b.artifactVectorHitsZero,
			&b.artifactVectorHitsOneTo3,
			&b.artifactVectorHitsFourTo10,
			&b.artifactVectorHitsGT10,
		)
	case run.ArtifactSearchPathFallback:
		b.artifactSearchFallback.Add(1)
		if hits > 0 {
			b.artifactSearchFallbackHits.Add(int64(hits))
		}
		recordLatencyBucket(duration,
			&b.artifactFallbackLatencyLE10MS,
			&b.artifactFallbackLatencyLE50MS,
			&b.artifactFallbackLatencyLE100MS,
			&b.artifactFallbackLatencyGT100MS,
		)
		recordHitBucket(hits,
			&b.artifactFallbackHitsZero,
			&b.artifactFallbackHitsOneTo3,
			&b.artifactFallbackHitsFourTo10,
			&b.artifactFallbackHitsGT10,
		)
	case run.ArtifactSearchPathError:
		b.artifactSearchError.Add(1)
	default:
		b.artifactSearchDisabled.Add(1)
	}
}

func recordLatencyBucket(
	duration time.Duration,
	le10ms *atomic.Int64,
	le50ms *atomic.Int64,
	le100ms *atomic.Int64,
	gt100ms *atomic.Int64,
) {
	switch {
	case duration <= 10*time.Millisecond:
		le10ms.Add(1)
	case duration <= 50*time.Millisecond:
		le50ms.Add(1)
	case duration <= 100*time.Millisecond:
		le100ms.Add(1)
	default:
		gt100ms.Add(1)
	}
}

func recordHitBucket(
	hits int,
	zero *atomic.Int64,
	oneTo3 *atomic.Int64,
	fourTo10 *atomic.Int64,
	gt10 *atomic.Int64,
) {
	switch {
	case hits <= 0:
		zero.Add(1)
	case hits <= 3:
		oneTo3.Add(1)
	case hits <= 10:
		fourTo10.Add(1)
	default:
		gt10.Add(1)
	}
}

// Build resolves one reference into deterministic context content.
func (b *Builder) Build(ctx context.Context, req Request) (Bundle, error) {
	if b == nil || b.reader == nil {
		return Bundle{}, ErrMissingReader
	}
	parsed, err := ParseRef(req.Ref)
	if err != nil {
		return Bundle{}, err
	}

	if parsed.Kind == RefEpisode {
		if b.episodeReader == nil {
			return Bundle{}, ErrMissingEpisodeReader
		}
		episode, err := b.episodeReader.GetEpisode(ctx, parsed.EpisodeID)
		if err != nil {
			return Bundle{}, err
		}
		content, meta, err := b.resolveEpisode(parsed, episode.Clone())
		if err != nil {
			return Bundle{}, err
		}
		return Bundle{
			Ref:     parsed.Raw,
			TurnID:  episode.StartTurnID,
			Kind:    parsed.Kind,
			Content: content,
			Meta:    meta,
		}, nil
	}

	turn, err := b.reader.GetTurn(ctx, parsed.TurnID)
	if err != nil {
		return Bundle{}, err
	}
	turn = turn.Clone()

	content, meta, err := b.resolve(parsed, turn)
	if err != nil {
		return Bundle{}, err
	}

	return Bundle{
		Ref:     parsed.Raw,
		TurnID:  parsed.TurnID,
		Kind:    parsed.Kind,
		Content: content,
		Meta:    meta,
	}, nil
}

func (b *Builder) resolveEpisode(parsed Ref, episode run.EpisodeRecord) (string, map[string]any, error) {
	if parsed.Kind != RefEpisode {
		return "", nil, ErrInvalidRef
	}
	return renderEpisode(episode), map[string]any{
		"episode_id":       episode.ID,
		"episode_version":  episode.EpisodeVersion,
		"boundary_key":     episode.BoundaryKey,
		"start_turn_id":    episode.StartTurnID,
		"end_turn_id":      episode.EndTurnID,
		"start_turn_index": episode.StartTurnIndex,
		"end_turn_index":   episode.EndTurnIndex,
		"is_landmark":      episode.IsLandmark,
		"anchor_ref_count": len(episode.AnchorRefs),
	}, nil
}

// BuildTemporal assembles coarse context for one temporal view with drill-down refs.
func (b *Builder) BuildTemporal(ctx context.Context, req TemporalRequest) (TemporalBundle, error) {
	if b == nil || b.reader == nil {
		return TemporalBundle{}, ErrMissingReader
	}

	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return TemporalBundle{}, ErrMissingSessionID
	}

	view, err := normalizeView(req.View)
	if err != nil {
		return TemporalBundle{}, err
	}

	timelineReader, ok := b.reader.(run.TurnTimelineReader)
	if !ok {
		return TemporalBundle{}, ErrTemporalUnsupported
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultTemporalLimit(view)
	}

	turns, err := timelineReader.ListTurns(ctx, sessionID, run.TurnListOptions{
		Limit: limit * 8,
		Since: req.Since,
		Until: req.Until,
		Asc:   true,
	})
	if err != nil {
		return TemporalBundle{}, err
	}
	if len(turns) == 0 {
		return TemporalBundle{
			SessionID: sessionID,
			View:      view,
			Content:   fmt.Sprintf("Temporal view: %s\nNo turns found.", view),
			Meta: map[string]any{
				"bucket_count": 0,
				"turn_count":   0,
				"view":         string(view),
			},
		}, nil
	}

	sort.SliceStable(turns, func(i, j int) bool {
		left := turns[i].CreatedAt.UTC()
		right := turns[j].CreatedAt.UTC()
		if left.Equal(right) {
			return turns[i].ID < turns[j].ID
		}
		return left.Before(right)
	})

	buckets := groupTemporalBuckets(view, turns)
	if len(buckets) > limit {
		buckets = buckets[:limit]
	}

	expandableDates := make([]string, 0, 32)
	expandableRefs := make([]string, 0, 64)
	dateSet := make(map[string]struct{})
	refSet := make(map[string]struct{})
	for _, bucket := range buckets {
		for _, date := range bucketDates(bucket) {
			if addUnique(dateSet, strings.TrimSpace(date)) {
				expandableDates = append(expandableDates, date)
			}
		}
		for _, ref := range bucket.ExpandableRefs {
			if addUnique(refSet, strings.TrimSpace(ref)) {
				expandableRefs = append(expandableRefs, ref)
			}
		}
		for _, ref := range bucket.Refs {
			if addUnique(refSet, strings.TrimSpace(ref)) {
				expandableRefs = append(expandableRefs, ref)
			}
		}
	}

	content := renderTemporalContent(view, buckets)
	return TemporalBundle{
		SessionID:       sessionID,
		View:            view,
		Content:         content,
		Buckets:         buckets,
		ExpandableDates: expandableDates,
		ExpandableRefs:  expandableRefs,
		Meta: map[string]any{
			"view":         string(view),
			"bucket_count": len(buckets),
			"turn_count":   len(turns),
			"limit":        limit,
		},
	}, nil
}

func (b *Builder) resolve(parsed Ref, turn run.TurnRecord) (string, map[string]any, error) {
	switch parsed.Kind {
	case RefWholeTurn:
		return renderWholeTurn(turn), map[string]any{
			"iterations": len(turn.Iterations),
			"tool_calls": countToolCalls(turn),
		}, nil
	case RefIteration:
		iter, ok := findIteration(turn, parsed.IterationIndex)
		if !ok {
			return "", nil, run.ErrTurnNotFound
		}
		return renderIteration(iter), map[string]any{
			"iteration_index": iter.IterationIndex,
			"tool_calls":      len(iter.ToolCalls),
		}, nil
	case RefToolCall:
		iter, ok := findIteration(turn, parsed.IterationIndex)
		if !ok {
			return "", nil, run.ErrTurnNotFound
		}
		call, ok := findToolCall(iter, parsed.ToolCallID)
		if !ok {
			return "", nil, run.ErrTurnNotFound
		}
		return renderToolCall(call), map[string]any{
			"iteration_index": iter.IterationIndex,
			"tool_call_id":    call.CallID,
		}, nil
	case RefSlice:
		msg, ok := findMessage(turn, parsed.MessageID)
		if !ok {
			return "", nil, ErrMessageNotFound
		}
		slice, err := sliceMessage(msg.Text, parsed.Start, parsed.End)
		if err != nil {
			return "", nil, err
		}
		return slice, map[string]any{
			"message_id": parsed.MessageID,
			"start":      parsed.Start,
			"end":        parsed.End,
		}, nil
	default:
		return "", nil, ErrInvalidRef
	}
}

type temporalAgg struct {
	Key          string
	View         TemporalView
	StartAt      time.Time
	EndAt        time.Time
	TurnCount    int
	ToolCalls    int
	CommandCount map[string]int
	TurnRefs     []string
	SamplePrompt string
	SampleFinal  string
	DayKeys      []string
	WeekKeys     []string
	HourKeys     []string
}

func normalizeView(view TemporalView) (TemporalView, error) {
	switch TemporalView(strings.ToLower(strings.TrimSpace(string(view)))) {
	case ViewHours:
		return ViewHours, nil
	case ViewDays:
		return ViewDays, nil
	case ViewWeeks:
		return ViewWeeks, nil
	case ViewMonths:
		return ViewMonths, nil
	default:
		return "", ErrInvalidTemporalView
	}
}

func defaultTemporalLimit(view TemporalView) int {
	switch view {
	case ViewHours:
		return 24
	case ViewDays:
		return 14
	case ViewWeeks:
		return 12
	case ViewMonths:
		return 6
	default:
		return 12
	}
}

func groupTemporalBuckets(view TemporalView, turns []run.TurnRecord) []TemporalBucket {
	byKey := make(map[string]*temporalAgg, len(turns))
	order := make([]string, 0, len(turns))
	for _, turn := range turns {
		ts := turn.CreatedAt.UTC()
		key := temporalKey(view, ts)
		if strings.TrimSpace(key) == "" {
			continue
		}

		agg, ok := byKey[key]
		if !ok {
			agg = &temporalAgg{
				Key:          key,
				View:         view,
				StartAt:      ts,
				EndAt:        ts,
				CommandCount: make(map[string]int),
			}
			byKey[key] = agg
			order = append(order, key)
		}

		if ts.Before(agg.StartAt) {
			agg.StartAt = ts
		}
		if ts.After(agg.EndAt) {
			agg.EndAt = ts
			agg.SampleFinal = strings.TrimSpace(turn.FinalOutput.Text)
		}
		if agg.SamplePrompt == "" {
			agg.SamplePrompt = strings.TrimSpace(turn.Prompt)
		}
		agg.TurnCount++
		agg.ToolCalls += countToolCalls(turn)

		if cmd := strings.TrimSpace(turn.Command); cmd != "" {
			agg.CommandCount[cmd]++
		}
		if len(agg.TurnRefs) < 5 {
			agg.TurnRefs = append(agg.TurnRefs, "turn/"+turn.ID)
		}
		agg.DayKeys = appendKeyUnique(agg.DayKeys, turn.CreatedAt.UTC().Format("2006-01-02"))
		weekYear, weekNum := turn.CreatedAt.UTC().ISOWeek()
		agg.WeekKeys = appendKeyUnique(agg.WeekKeys, fmt.Sprintf("%04d-W%02d", weekYear, weekNum))
		agg.HourKeys = appendKeyUnique(agg.HourKeys, turn.CreatedAt.UTC().Format("2006-01-02T15"))
	}

	sort.SliceStable(order, func(i, j int) bool {
		left := byKey[order[i]]
		right := byKey[order[j]]
		if left.StartAt.Equal(right.StartAt) {
			return left.Key > right.Key
		}
		return left.StartAt.After(right.StartAt)
	})

	out := make([]TemporalBucket, 0, len(order))
	for _, key := range order {
		agg := byKey[key]
		bucket := TemporalBucket{
			Key:       agg.Key,
			View:      agg.View,
			StartAt:   agg.StartAt,
			EndAt:     agg.EndAt,
			TurnCount: agg.TurnCount,
			ToolCalls: agg.ToolCalls,
			Summary:   buildTemporalSummary(agg),
			Refs:      append([]string(nil), agg.TurnRefs...),
		}

		switch view {
		case ViewMonths:
			for _, weekKey := range agg.WeekKeys {
				bucket.ExpandableRefs = append(bucket.ExpandableRefs, "week:"+weekKey)
			}
			for _, dayKey := range agg.DayKeys {
				bucket.ExpandableRefs = append(bucket.ExpandableRefs, "day:"+dayKey)
			}
		case ViewWeeks:
			for _, dayKey := range agg.DayKeys {
				bucket.ExpandableRefs = append(bucket.ExpandableRefs, "day:"+dayKey)
			}
		case ViewDays:
			for _, hourKey := range agg.HourKeys {
				bucket.ExpandableRefs = append(bucket.ExpandableRefs, "hour:"+hourKey)
			}
		case ViewHours:
			// Leaf bucket in the temporal pyramid; use turn refs as drill targets.
			bucket.ExpandableRefs = append(bucket.ExpandableRefs, bucket.Refs...)
		}
		out = append(out, bucket)
	}
	return out
}

func temporalKey(view TemporalView, ts time.Time) string {
	ts = ts.UTC()
	switch view {
	case ViewHours:
		return ts.Format("2006-01-02T15")
	case ViewDays:
		return ts.Format("2006-01-02")
	case ViewWeeks:
		year, week := ts.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	case ViewMonths:
		return ts.Format("2006-01")
	default:
		return ""
	}
}

func appendKeyUnique(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func buildTemporalSummary(agg *temporalAgg) string {
	if agg == nil {
		return ""
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("%d turns", agg.TurnCount))
	parts = append(parts, fmt.Sprintf("%d tool calls", agg.ToolCalls))

	cmds := summarizeCommands(agg.CommandCount)
	if cmds != "" {
		parts = append(parts, "commands "+cmds)
	}

	last := strings.TrimSpace(agg.SampleFinal)
	if last == "" {
		last = strings.TrimSpace(agg.SamplePrompt)
	}
	if last != "" {
		parts = append(parts, fmt.Sprintf("sample \"%s\"", truncateRunes(last, 96)))
	}

	return strings.Join(parts, "; ")
}

func summarizeCommands(commandCount map[string]int) string {
	if len(commandCount) == 0 {
		return ""
	}

	type pair struct {
		Name  string
		Count int
	}
	items := make([]pair, 0, len(commandCount))
	for name, count := range commandCount {
		if strings.TrimSpace(name) == "" || count <= 0 {
			continue
		}
		items = append(items, pair{Name: name, Count: count})
	}
	if len(items) == 0 {
		return ""
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Name < items[j].Name
		}
		return items[i].Count > items[j].Count
	})

	rendered := make([]string, 0, len(items))
	for _, item := range items {
		rendered = append(rendered, fmt.Sprintf("%s(%d)", item.Name, item.Count))
	}
	return strings.Join(rendered, ", ")
}

func renderTemporalContent(view TemporalView, buckets []TemporalBucket) string {
	var sb strings.Builder
	sb.WriteString("Temporal view: ")
	sb.WriteString(string(view))
	if len(buckets) == 0 {
		sb.WriteString("\nNo turns found.")
		return sb.String()
	}
	for _, bucket := range buckets {
		sb.WriteString("\n- ")
		sb.WriteString(bucket.Key)
		sb.WriteString(": ")
		sb.WriteString(fmt.Sprintf("%d turns, %d tool calls", bucket.TurnCount, bucket.ToolCalls))
		if bucket.Summary != "" {
			sb.WriteString("\n  summary: ")
			sb.WriteString(bucket.Summary)
		}
		if len(bucket.ExpandableRefs) > 0 {
			sb.WriteString("\n  drill: ")
			sb.WriteString(strings.Join(bucket.ExpandableRefs, ", "))
		}
		if len(bucket.Refs) > 0 {
			sb.WriteString("\n  refs: ")
			sb.WriteString(strings.Join(bucket.Refs, ", "))
		}
	}
	return strings.TrimSpace(sb.String())
}

func bucketDates(bucket TemporalBucket) []string {
	out := make([]string, 0, len(bucket.ExpandableRefs)+1)
	if bucket.View == ViewDays {
		out = append(out, bucket.Key)
	}
	for _, ref := range bucket.ExpandableRefs {
		if strings.HasPrefix(ref, "day:") {
			out = append(out, strings.TrimPrefix(ref, "day:"))
		}
	}
	return out
}

func addUnique(set map[string]struct{}, value string) bool {
	if set == nil {
		return false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if _, exists := set[value]; exists {
		return false
	}
	set[value] = struct{}{}
	return true
}

func truncateRunes(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "..."
}

func renderWholeTurn(turn run.TurnRecord) string {
	var sb strings.Builder
	if strings.TrimSpace(turn.Prompt) != "" {
		sb.WriteString("Prompt: ")
		sb.WriteString(strings.TrimSpace(turn.Prompt))
		sb.WriteString("\n")
	}
	for _, iter := range turn.Iterations {
		sb.WriteString(renderIteration(iter))
		sb.WriteString("\n")
	}
	if strings.TrimSpace(turn.FinalOutput.Text) != "" {
		sb.WriteString("Final: ")
		sb.WriteString(strings.TrimSpace(turn.FinalOutput.Text))
	}
	return strings.TrimSpace(sb.String())
}

func renderIteration(iter run.IterationRecord) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Iteration %d", iter.IterationIndex))
	if strings.TrimSpace(iter.Message.Text) != "" {
		sb.WriteString(": ")
		sb.WriteString(strings.TrimSpace(iter.Message.Text))
	}
	for _, call := range iter.ToolCalls {
		sb.WriteString("\n")
		sb.WriteString(renderToolCall(call))
	}
	return sb.String()
}

func renderToolCall(call run.ToolCallRecord) string {
	text := strings.TrimSpace(call.ResultRef.Text)
	if text == "" {
		text = "<empty>"
	}
	return fmt.Sprintf("Tool %s (%s): %s", call.Name, call.CallID, text)
}

func renderEpisode(episode run.EpisodeRecord) string {
	var sb strings.Builder
	sb.WriteString("Episode")
	if topic := strings.TrimSpace(episode.Topic); topic != "" {
		sb.WriteString(": ")
		sb.WriteString(topic)
	}
	sb.WriteString("\n")
	if summary := strings.TrimSpace(episode.Summary); summary != "" {
		sb.WriteString("Summary: ")
		sb.WriteString(summary)
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("Turns: %d-%d (%s -> %s)\n",
		episode.StartTurnIndex,
		episode.EndTurnIndex,
		strings.TrimSpace(episode.StartTurnID),
		strings.TrimSpace(episode.EndTurnID)))
	sb.WriteString(fmt.Sprintf("Landmark: %t | Salience: %.2f",
		episode.IsLandmark,
		episode.SalienceScore))
	if len(episode.AnchorRefs) > 0 {
		sb.WriteString("\nAnchors: ")
		sb.WriteString(strings.Join(episode.AnchorRefs, ", "))
	}
	return strings.TrimSpace(sb.String())
}

func countToolCalls(turn run.TurnRecord) int {
	var total int
	for _, iter := range turn.Iterations {
		total += len(iter.ToolCalls)
	}
	return total
}

func findIteration(turn run.TurnRecord, index int) (run.IterationRecord, bool) {
	for _, iter := range turn.Iterations {
		if iter.IterationIndex == index {
			return iter, true
		}
	}
	return run.IterationRecord{}, false
}

func findToolCall(iter run.IterationRecord, callID string) (run.ToolCallRecord, bool) {
	for _, call := range iter.ToolCalls {
		if call.CallID == callID {
			return call, true
		}
	}
	return run.ToolCallRecord{}, false
}

func findMessage(turn run.TurnRecord, msgID string) (run.MessageRef, bool) {
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return run.MessageRef{}, false
	}

	if turn.FinalOutput.ID == msgID {
		return turn.FinalOutput, true
	}
	for _, iter := range turn.Iterations {
		if iter.Message.ID == msgID {
			return iter.Message, true
		}
		for _, call := range iter.ToolCalls {
			if call.ResultRef.ID == msgID {
				return run.MessageRef{
					ID:   call.ResultRef.ID,
					Role: "tool",
					Text: call.ResultRef.Text,
				}, true
			}
		}
	}
	return run.MessageRef{}, false
}

func sliceMessage(text string, start, end int) (string, error) {
	if start < 0 || end < start {
		return "", ErrInvalidSlice
	}
	runes := []rune(text)
	if start > len(runes) {
		return "", ErrInvalidSlice
	}
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[start:end]), nil
}
