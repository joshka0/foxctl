package contextbuilder

import (
	"context"
	"fmt"
	"sort"
	"strings"
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

// LayeredRequest asks for L2 -> L1 -> L0 context assembly.
type LayeredRequest struct {
	SessionID   string `json:"session_id"`
	MaxChars    int    `json:"max_chars,omitempty"`
	MonthsLimit int    `json:"months_limit,omitempty"`
	DaysLimit   int    `json:"days_limit,omitempty"`
	HoursLimit  int    `json:"hours_limit,omitempty"`
}

// LayeredBundle is deterministic layered context with turn/slice refs.
type LayeredBundle struct {
	SessionID string            `json:"session_id"`
	Content   string            `json:"content"`
	Layers    map[string]string `json:"layers,omitempty"`

	Refs      []string `json:"refs,omitempty"`
	TurnRefs  []string `json:"turn_refs,omitempty"`
	SliceRefs []string `json:"slice_refs,omitempty"`

	Meta map[string]any `json:"meta,omitempty"`
}

// SetCompanionProvider configures optional companion-memory integration.
func (b *Builder) SetCompanionProvider(provider CompanionProvider) {
	if b == nil {
		return
	}
	b.companion = provider
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

	content := renderLayeredContent(l2Section, l1Section, l0Section)
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
		append(append(l1Temporal.ExpandableRefs, l0Temporal.ExpandableRefs...), sliceRefs...)...,
	))

	return LayeredBundle{
		SessionID: sessionID,
		Content:   content,
		Layers: map[string]string{
			"L2": l2Section,
			"L1": l1Section,
			"L0": l0Section,
		},
		Refs:      refs,
		TurnRefs:  turnRefs,
		SliceRefs: sliceRefs,
		Meta: map[string]any{
			"session_id":          sessionID,
			"has_companion":       b.companion != nil,
			"l2_bucket_count":     len(l2Temporal.Buckets),
			"l1_bucket_count":     len(l1Temporal.Buckets),
			"l0_bucket_count":     len(l0Temporal.Buckets),
			"expandable_date_cnt": len(uniqueStrings(append(l1Temporal.ExpandableDates, l2Temporal.ExpandableDates...))),
		},
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

func renderLayeredContent(l2, l1, l0 string) string {
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
	return strings.Join(sections, "\n\n")
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
