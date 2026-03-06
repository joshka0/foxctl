package jido

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/v2/core/ask"
)

type askSignalData struct {
	AskID          string `json:"ask_id"`
	RequestID      string `json:"request_id,omitempty"`
	Kind           string `json:"kind"`
	Question       string `json:"question"`
	ConversationID string `json:"conversation_id,omitempty"`
	FromNS         string `json:"from_ns"`
	ToNS           string `json:"to_ns"`
	TTLMS          int64  `json:"ttl_ms,omitempty"`
}

// AskMessageToSignal maps a canonical v2 ask message into a runtime signal.
func AskMessageToSignal(msg ask.Message, source string) (Signal, error) {
	msg.AskID = strings.TrimSpace(msg.AskID)
	msg.Question = strings.TrimSpace(msg.Question)
	msg.Kind = strings.TrimSpace(msg.Kind)
	msg.FromNS = strings.TrimSpace(msg.FromNS)
	msg.ToNS = strings.TrimSpace(msg.ToNS)
	msg.ConversationID = strings.TrimSpace(msg.ConversationID)
	msg.RequestID = strings.TrimSpace(msg.RequestID)

	if msg.AskID == "" {
		return Signal{}, fmt.Errorf("ask_id is required")
	}
	if msg.Question == "" {
		return Signal{}, fmt.Errorf("question is required")
	}
	if msg.Kind == "" {
		return Signal{}, fmt.Errorf("kind is required")
	}
	if msg.FromNS == "" {
		return Signal{}, fmt.Errorf("from_ns is required")
	}
	if msg.ToNS == "" {
		return Signal{}, fmt.Errorf("to_ns is required")
	}

	raw, err := json.Marshal(askSignalData{
		AskID:          msg.AskID,
		RequestID:      msg.RequestID,
		Kind:           msg.Kind,
		Question:       msg.Question,
		ConversationID: msg.ConversationID,
		FromNS:         msg.FromNS,
		ToNS:           msg.ToNS,
		TTLMS:          msg.TTLMS,
	})
	if err != nil {
		return Signal{}, fmt.Errorf("marshal ask signal payload: %w", err)
	}

	src := strings.TrimSpace(source)
	if src == "" {
		src = DefaultSignalSource
	}

	return Signal{
		ID:            msg.AskID,
		Type:          DefaultAskSignal,
		Source:        src,
		Subject:       "/agents/" + msg.ToNS,
		CorrelationID: msg.AskID,
		CausationID:   msg.RequestID,
		Data:          raw,
	}, nil
}

// AskMessageToSignalRequest wraps an ask message in a runtime signal request.
func AskMessageToSignalRequest(msg ask.Message, source string) (SignalRequest, error) {
	sig, err := AskMessageToSignal(msg, source)
	if err != nil {
		return SignalRequest{}, err
	}

	timeout := msg.TTLMS
	if timeout < 0 {
		timeout = 0
	}

	return SignalRequest{
		RequestID: msg.RequestID,
		AgentID:   strings.TrimSpace(msg.ToNS),
		Signal:    sig,
		Mode:      SignalModeCall,
		TimeoutMS: timeout,
	}, nil
}

// MessageIDFromSignalResponse returns a stable message id fallback chain.
func MessageIDFromSignalResponse(resp SignalResponse, fallback string) string {
	if mid := strings.TrimSpace(resp.MessageID); mid != "" {
		return mid
	}
	if sid := strings.TrimSpace(resp.SignalID); sid != "" {
		return sid
	}
	return strings.TrimSpace(fallback)
}
