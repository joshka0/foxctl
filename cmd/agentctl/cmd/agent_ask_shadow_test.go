package cmd

import (
	"errors"
	"testing"
	"time"

	v2ask "github.com/jkatigb/agentctl/internal/v2/core/ask"
)

func TestEvaluateAskShadowParity_Match(t *testing.T) {
	t.Parallel()

	in := askShadowInput{
		AskID:          "ask-1",
		AgentID:        "agent-1",
		Namespace:      "agent:1",
		FromNS:         "cli:1",
		Kind:           "context",
		Question:       "what did you find?",
		ConversationID: "conv-1",
		Timeout:        30 * time.Second,
		V1MessageID:    "msg-1",
	}

	match, reason := evaluateAskShadowParity(in, v2ask.Response{
		AskID:     "ask-1",
		MessageID: "msg-1",
	}, v2ask.Message{
		AskID:          "ask-1",
		FromNS:         "cli:1",
		ToNS:           "agent:1",
		Kind:           "context",
		Question:       "what did you find?",
		ConversationID: "conv-1",
		TTLMS:          int64((30 * time.Second).Milliseconds()),
	}, nil)
	if !match {
		t.Fatalf("expected match=true, got false reason=%q", reason)
	}
}

func TestEvaluateAskShadowParity_ShadowError(t *testing.T) {
	t.Parallel()

	match, reason := evaluateAskShadowParity(askShadowInput{AskID: "ask-1"}, v2ask.Response{}, v2ask.Message{}, errors.New("shadow failed"))
	if match {
		t.Fatal("expected match=false when shadow fails")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason when shadow fails")
	}
}

func TestEvaluateAskShadowParity_FieldMismatch(t *testing.T) {
	t.Parallel()

	in := askShadowInput{
		AskID:          "ask-1",
		Namespace:      "agent:1",
		FromNS:         "cli:1",
		Kind:           "context",
		Question:       "Q",
		ConversationID: "conv-1",
		Timeout:        30 * time.Second,
		V1MessageID:    "msg-1",
	}
	match, reason := evaluateAskShadowParity(in, v2ask.Response{
		AskID:     "ask-2",
		MessageID: "msg-1",
	}, v2ask.Message{
		AskID:          "ask-2",
		FromNS:         "cli:1",
		ToNS:           "agent:1",
		Kind:           "context",
		Question:       "Q",
		ConversationID: "conv-2",
		TTLMS:          int64((10 * time.Second).Milliseconds()),
	}, nil)
	if match {
		t.Fatalf("expected mismatch, got match=true reason=%q", reason)
	}
	if reason == "" {
		t.Fatal("expected mismatch reason")
	}
}
