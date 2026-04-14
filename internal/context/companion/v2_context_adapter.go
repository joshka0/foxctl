package companion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/runtime/contextbuilder"
)

func newCompanionContextBuilder(memory *ConversationMemory) *contextbuilder.Builder {
	if memory == nil {
		return nil
	}
	reader := companionTurnReader{memory: memory}
	builder := contextbuilder.New(reader)
	builder.SetCompanionProvider(companionLayerProvider{memory: memory})
	if jidoProvider, err := newJidoCompanionProviderFromEnv(); err == nil && jidoProvider != nil {
		builder.SetCompanionProvider(jidoProvider)
	}
	return builder
}

func newJidoCompanionProviderFromEnv() (contextbuilder.CompanionProvider, error) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("AGENTCTL_COMPANION_CONTEXT_PROVIDER")), "jido") {
		return nil, nil
	}

	client, err := v2jido.NewJSONRPCClient(v2jido.JSONRPCClientConfig{
		SocketPath: strings.TrimSpace(os.Getenv("AGENTCTL_JIDO_SOCKET")),
		RPCPath:    strings.TrimSpace(os.Getenv("AGENTCTL_JIDO_RPC_PATH")),
		Timeout:    parseMillisEnv("AGENTCTL_JIDO_RPC_TIMEOUT_MS", 10*time.Second),
	})
	if err != nil {
		return nil, fmt.Errorf("configure jido json-rpc client for companion provider: %w", err)
	}

	agentID := strings.TrimSpace(os.Getenv("AGENTCTL_JIDO_COMPANION_AGENT_ID"))
	if agentID == "" {
		agentID = "companion:bridge"
	}
	strict := strings.EqualFold(strings.TrimSpace(os.Getenv("AGENTCTL_JIDO_COMPANION_STRICT")), "true")

	return v2jido.NewCompanionProvider(v2jido.CompanionProviderConfig{
		Client:       client,
		AgentID:      agentID,
		SignalSource: strings.TrimSpace(os.Getenv("AGENTCTL_JIDO_SIGNAL_SOURCE")),
		Timeout:      parseMillisEnv("AGENTCTL_JIDO_COMPANION_TIMEOUT_MS", 10*time.Second),
		Strict:       strict,
	})
}

func parseMillisEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || ms <= 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

type companionTurnReader struct {
	memory *ConversationMemory
}

func (r companionTurnReader) GetTurn(ctx context.Context, turnID string) (run.TurnRecord, error) {
	if r.memory == nil {
		return run.TurnRecord{}, run.ErrTurnNotFound
	}
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return run.TurnRecord{}, run.ErrTurnNotFound
	}

	var turn ConversationTurn
	var toolCallsJSON sql.NullString
	err := r.memory.db.QueryRowContext(ctx, `
		SELECT id, conversation_id, role, content, token_count, created_at, tool_calls
		FROM companion_turns
		WHERE id = $1
		LIMIT 1
	`, turnID).Scan(
		&turn.ID,
		&turn.ConversationID,
		&turn.Role,
		&turn.Content,
		&turn.TokenCount,
		&turn.CreatedAt,
		&toolCallsJSON,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return run.TurnRecord{}, run.ErrTurnNotFound
		}
		return run.TurnRecord{}, fmt.Errorf("get companion turn: %w", err)
	}
	if toolCallsJSON.Valid && strings.TrimSpace(toolCallsJSON.String) != "" {
		turn.ToolCalls = json.RawMessage(toolCallsJSON.String)
	}
	return companionTurnToRunTurn(turn, 0), nil
}

func (r companionTurnReader) ListTurns(ctx context.Context, sessionID string, opts run.TurnListOptions) ([]run.TurnRecord, error) {
	if r.memory == nil {
		return nil, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil
	}

	rows, err := r.memory.db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, token_count, created_at, tool_calls
		FROM companion_turns
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list companion turns: %w", err)
	}
	defer rows.Close()

	turns := make([]ConversationTurn, 0, 128)
	for rows.Next() {
		var turn ConversationTurn
		var toolCallsJSON sql.NullString
		if scanErr := rows.Scan(
			&turn.ID,
			&turn.ConversationID,
			&turn.Role,
			&turn.Content,
			&turn.TokenCount,
			&turn.CreatedAt,
			&toolCallsJSON,
		); scanErr != nil {
			continue
		}
		if toolCallsJSON.Valid && strings.TrimSpace(toolCallsJSON.String) != "" {
			turn.ToolCalls = json.RawMessage(toolCallsJSON.String)
		}
		if !opts.Since.IsZero() && turn.CreatedAt.Before(opts.Since) {
			continue
		}
		if !opts.Until.IsZero() && turn.CreatedAt.After(opts.Until) {
			continue
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate companion turns: %w", err)
	}

	records := make([]run.TurnRecord, 0, len(turns))
	for idx, turn := range turns {
		records = append(records, companionTurnToRunTurn(turn, idx+1))
	}
	if !opts.Asc {
		for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
			records[i], records[j] = records[j], records[i]
		}
	}
	if opts.Limit > 0 && len(records) > opts.Limit {
		records = records[:opts.Limit]
	}
	return records, nil
}

func companionTurnToRunTurn(turn ConversationTurn, turnIndex int) run.TurnRecord {
	record := run.TurnRecord{
		ID:        strings.TrimSpace(turn.ID),
		SessionID: strings.TrimSpace(turn.ConversationID),
		TurnIndex: turnIndex,
		CreatedAt: turn.CreatedAt.UTC(),
		UpdatedAt: turn.CreatedAt.UTC(),
	}

	content := strings.TrimSpace(turn.Content)
	role := strings.ToLower(strings.TrimSpace(turn.Role))
	switch role {
	case "user":
		record.Prompt = content
	case "assistant":
		record.FinalOutput = run.MessageRef{
			ID:   fmt.Sprintf("%s:final", record.ID),
			Role: role,
			Text: content,
		}
	default:
		record.FinalOutput = run.MessageRef{
			ID:   fmt.Sprintf("%s:message", record.ID),
			Role: role,
			Text: content,
		}
	}

	if calls := parseCompanionToolCalls(turn.ToolCalls, record.ID); len(calls) > 0 {
		record.Iterations = []run.IterationRecord{
			{
				TurnID:         record.ID,
				IterationIndex: 0,
				Message: run.MessageRef{
					ID:   fmt.Sprintf("%s:iter:0", record.ID),
					Role: role,
					Text: content,
				},
				ToolCalls: calls,
			},
		}
	}

	return record
}

func parseCompanionToolCalls(raw json.RawMessage, turnID string) []run.ToolCallRecord {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	var details []ToolCallDetail
	if err := json.Unmarshal(raw, &details); err != nil {
		return nil
	}

	out := make([]run.ToolCallRecord, 0, len(details))
	for idx, detail := range details {
		name := strings.TrimSpace(detail.Name)
		if name == "" {
			continue
		}
		callID := strings.TrimSpace(detail.ID)
		if callID == "" {
			callID = fmt.Sprintf("%s:tool:%d", turnID, idx)
		}
		out = append(out, run.ToolCallRecord{
			CallID:         callID,
			IterationIndex: 0,
			Name:           name,
			ArgsJSON:       append(json.RawMessage(nil), detail.Arguments...),
			Status:         "ok",
			ResultRef: run.ArtifactRef{
				Kind: "inline",
				Text: strings.TrimSpace(detail.Output),
			},
		})
	}
	return out
}

type companionLayerProvider struct {
	memory *ConversationMemory
}

func (p companionLayerProvider) GetLayeredContext(ctx context.Context, sessionID string, _ contextbuilder.CompanionRequest) (contextbuilder.CompanionLayeredContext, error) {
	if p.memory == nil {
		return contextbuilder.CompanionLayeredContext{}, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return contextbuilder.CompanionLayeredContext{}, nil
	}

	hybridCtx, err := p.memory.GetHybridContext(ctx, sessionID, "")
	if err != nil {
		return contextbuilder.CompanionLayeredContext{}, err
	}
	sections := parseHybridContextSections(hybridCtx)
	l2 := joinNonEmptySections(
		sections["hard_state"],
		sections["assumptions"],
		sections["episodes"],
	)
	l1 := joinNonEmptySections(sections["evidence"])
	l0 := joinNonEmptySections(sections["recent_turns"])

	turns, err := p.memory.getRecentTurns(ctx, sessionID, 24)
	if err != nil {
		return contextbuilder.CompanionLayeredContext{}, err
	}
	refs := make([]string, 0, len(turns))
	for _, turn := range turns {
		id := strings.TrimSpace(turn.ID)
		if id == "" {
			continue
		}
		refs = append(refs, "turn/"+id)
	}
	sort.Strings(refs)

	return contextbuilder.CompanionLayeredContext{
		L2:   l2,
		L1:   l1,
		L0:   l0,
		Refs: dedupeStrings(refs),
		Meta: map[string]any{
			"suppress_temporal_samples": true,
			"suppress_l0_temporal":      strings.TrimSpace(l0) != "",
			"has_canonical_hard_state":  strings.TrimSpace(sections["hard_state"]) != "" && strings.TrimSpace(sections["hard_state"]) != "{}",
		},
	}, nil
}

func parseHybridContextSections(raw string) map[string]string {
	out := map[string]string{
		"hard_state":   "",
		"assumptions":  "",
		"episodes":     "",
		"evidence":     "",
		"recent_turns": "",
	}
	if strings.TrimSpace(raw) == "" {
		return out
	}

	var current string
	buffers := map[string]*strings.Builder{
		"hard_state":   {},
		"assumptions":  {},
		"episodes":     {},
		"evidence":     {},
		"recent_turns": {},
	}

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "=== HARD STATE"):
			current = "hard_state"
			continue
		case strings.HasPrefix(trimmed, "=== ACTIVE ASSUMPTIONS"):
			current = "assumptions"
			continue
		case strings.HasPrefix(trimmed, "=== EPISODE CONTEXT"):
			current = "episodes"
			continue
		case strings.HasPrefix(trimmed, "=== EVIDENCE"):
			current = "evidence"
			continue
		case strings.HasPrefix(trimmed, "=== RECENT TURNS"):
			current = "recent_turns"
			continue
		}
		if current == "" {
			continue
		}
		buffers[current].WriteString(line)
		buffers[current].WriteString("\n")
	}

	for key, buffer := range buffers {
		out[key] = strings.TrimSpace(buffer.String())
	}

	return out
}

func joinNonEmptySections(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "\n\n")
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
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
