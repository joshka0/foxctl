package jido

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/runtime/contextbuilder"
)

const defaultCompanionSignalType = "agentctl.companion.context"

// CompanionProviderConfig configures a Jido-backed companion context provider.
type CompanionProviderConfig struct {
	Client       Client
	AgentID      string
	SignalSource string
	SignalType   string
	DefaultQuery string
	Timeout      time.Duration
	Strict       bool
}

// CompanionProvider fetches layered context by signaling a Jido runtime agent.
type CompanionProvider struct {
	client       Client
	agentID      string
	signalSource string
	signalType   string
	defaultQuery string
	timeout      time.Duration
	strict       bool
}

// NewCompanionProvider builds a Jido-backed companion context provider.
func NewCompanionProvider(cfg CompanionProviderConfig) (*CompanionProvider, error) {
	if cfg.Client == nil {
		return nil, fmt.Errorf("jido companion provider requires client")
	}
	agentID := strings.TrimSpace(cfg.AgentID)
	if agentID == "" {
		return nil, fmt.Errorf("jido companion provider requires agent_id")
	}
	source := strings.TrimSpace(cfg.SignalSource)
	if source == "" {
		source = DefaultSignalSource
	}
	signalType := strings.TrimSpace(cfg.SignalType)
	if signalType == "" {
		signalType = defaultCompanionSignalType
	}
	query := strings.TrimSpace(cfg.DefaultQuery)
	if query == "" {
		query = "recent context"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &CompanionProvider{
		client:       cfg.Client,
		agentID:      agentID,
		signalSource: source,
		signalType:   signalType,
		defaultQuery: query,
		timeout:      timeout,
		strict:       cfg.Strict,
	}, nil
}

// GetLayeredContext fetches L2/L1/L0 companion layers from a Jido runtime agent.
func (p *CompanionProvider) GetLayeredContext(
	ctx context.Context,
	sessionID string,
	req contextbuilder.CompanionRequest,
) (contextbuilder.CompanionLayeredContext, error) {
	if p == nil || p.client == nil {
		return contextbuilder.CompanionLayeredContext{}, fmt.Errorf("jido companion provider is not configured")
	}

	sessionID = strings.TrimSpace(sessionID)
	payload := map[string]any{
		"query":      p.defaultQuery,
		"session_id": sessionID,
	}
	if req.MaxChars > 0 {
		payload["max_chars"] = req.MaxChars
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		if p.strict {
			return contextbuilder.CompanionLayeredContext{}, fmt.Errorf("marshal companion signal payload: %w", err)
		}
		return contextbuilder.CompanionLayeredContext{}, nil
	}

	resp, err := p.client.Signal(ctx, SignalRequest{
		AgentID: p.agentID,
		Mode:    SignalModeCall,
		Signal: Signal{
			ID:            "companion:" + sessionID,
			Type:          p.signalType,
			Source:        p.signalSource,
			CorrelationID: sessionID,
			Data:          raw,
		},
		TimeoutMS: p.timeout.Milliseconds(),
	})
	if err != nil {
		if p.strict {
			return contextbuilder.CompanionLayeredContext{}, fmt.Errorf("dispatch companion signal: %w", err)
		}
		return contextbuilder.CompanionLayeredContext{}, nil
	}

	layered, err := decodeCompanionLayeredContext(resp.Data)
	if err != nil {
		if p.strict {
			return contextbuilder.CompanionLayeredContext{}, err
		}
		return contextbuilder.CompanionLayeredContext{}, nil
	}
	return layered, nil
}

func decodeCompanionLayeredContext(raw json.RawMessage) (contextbuilder.CompanionLayeredContext, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return contextbuilder.CompanionLayeredContext{}, nil
	}

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return contextbuilder.CompanionLayeredContext{}, fmt.Errorf("decode companion signal data: %w", err)
	}

	state := mapAt(root, "state")
	if len(state) == 0 {
		return contextbuilder.CompanionLayeredContext{}, fmt.Errorf("companion state is missing")
	}
	agentctlState := mapAt(state, "agentctl")
	if len(agentctlState) == 0 {
		agentctlState = state
	}

	lastResult := mapAt(agentctlState, "last_result")
	if len(lastResult) == 0 {
		lastResult = agentctlState
	}

	contextMap := mapAt(lastResult, "companion_context")
	if len(contextMap) == 0 {
		contextMap = mapAt(lastResult, "context")
	}
	if len(contextMap) == 0 {
		return contextbuilder.CompanionLayeredContext{}, fmt.Errorf("companion context payload is missing")
	}

	out := contextbuilder.CompanionLayeredContext{
		L2: strings.TrimSpace(stringValue(contextMap["l2"])),
		L1: strings.TrimSpace(stringValue(contextMap["l1"])),
		L0: strings.TrimSpace(stringValue(contextMap["l0"])),
	}

	if refs, ok := contextMap["refs"].([]any); ok {
		for _, ref := range refs {
			text := strings.TrimSpace(stringValue(ref))
			if text == "" {
				continue
			}
			out.Refs = append(out.Refs, text)
		}
	}
	if meta, ok := contextMap["meta"].(map[string]any); ok && len(meta) > 0 {
		out.Meta = meta
	}

	return out, nil
}

var _ contextbuilder.CompanionProvider = (*CompanionProvider)(nil)
