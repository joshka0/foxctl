package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	v2ask "github.com/jkatigb/agentctl/internal/v2/core/ask"
	portconfig "github.com/jkatigb/agentctl/internal/v2/ports/config"
	v2services "github.com/jkatigb/agentctl/internal/v2/services"

	"github.com/jkatigb/agentctl/internal/observability"
)

const shadowAskTimeout = 2 * time.Second

type askShadowInput struct {
	AskID          string
	AgentID        string
	Namespace      string
	FromNS         string
	Kind           string
	Question       string
	ConversationID string
	Timeout        time.Duration
	V1MessageID    string
}

type shadowAskDispatcher struct {
	messageID string
	lastMsg   v2ask.Message
}

func (d *shadowAskDispatcher) Send(_ context.Context, msg v2ask.Message) (string, error) {
	d.lastMsg = msg
	if strings.TrimSpace(d.messageID) == "" {
		d.messageID = "shadow-msg"
	}
	return d.messageID, nil
}

func maybeRunAgentAskShadow(ctx context.Context, in askShadowInput) {
	flags, err := portconfig.ParseV2ShadowCommandsFromEnv()
	if err != nil {
		emitAskShadowEvent(ctx, in, false, "invalid shadow flag configuration", "", "", "", err, 0)
		return
	}
	if !flags.Enabled("ask") {
		return
	}

	start := time.Now()
	shadowCtx, cancel := context.WithTimeout(ctx, shadowAskTimeout)
	defer cancel()

	dispatcher := &shadowAskDispatcher{messageID: in.V1MessageID}
	svc := v2services.NewAskService(v2services.AskDependencies{
		Dispatcher: dispatcher,
		DefaultTTL: in.Timeout,
		NewID: func() string {
			return "shadow"
		},
	})

	resp, shadowErr := svc.Ask(shadowCtx, v2ask.Request{
		RequestID:      in.V1MessageID,
		AskID:          in.AskID,
		AgentID:        in.AgentID,
		Namespace:      in.Namespace,
		Kind:           in.Kind,
		Question:       in.Question,
		ConversationID: in.ConversationID,
		CallerNS:       in.FromNS,
		Timeout:        in.Timeout,
	})
	match, reason := evaluateAskShadowParity(in, resp, dispatcher.lastMsg, shadowErr)
	emitAskShadowEvent(
		ctx,
		in,
		match,
		reason,
		strings.TrimSpace(resp.MessageID),
		strings.TrimSpace(resp.AskID),
		strings.TrimSpace(dispatcher.lastMsg.ToNS),
		shadowErr,
		time.Since(start),
	)
}

func evaluateAskShadowParity(in askShadowInput, resp v2ask.Response, shadowMsg v2ask.Message, shadowErr error) (bool, string) {
	if shadowErr != nil {
		return false, "shadow ask failed"
	}

	var mismatches []string
	if strings.TrimSpace(resp.AskID) != strings.TrimSpace(in.AskID) {
		mismatches = append(mismatches, "ask_id")
	}
	if strings.TrimSpace(resp.MessageID) != strings.TrimSpace(in.V1MessageID) {
		mismatches = append(mismatches, "message_id")
	}
	if strings.TrimSpace(shadowMsg.ToNS) != strings.TrimSpace(in.Namespace) {
		mismatches = append(mismatches, "to_ns")
	}
	if strings.TrimSpace(shadowMsg.FromNS) != strings.TrimSpace(in.FromNS) {
		mismatches = append(mismatches, "from_ns")
	}
	if strings.TrimSpace(shadowMsg.Kind) != strings.TrimSpace(in.Kind) {
		mismatches = append(mismatches, "kind")
	}
	if strings.TrimSpace(shadowMsg.Question) != strings.TrimSpace(in.Question) {
		mismatches = append(mismatches, "question")
	}
	if strings.TrimSpace(shadowMsg.ConversationID) != strings.TrimSpace(in.ConversationID) {
		mismatches = append(mismatches, "conversation_id")
	}
	if shadowMsg.TTLMS != int64(in.Timeout.Milliseconds()) {
		mismatches = append(mismatches, "ttl_ms")
	}

	if len(mismatches) == 0 {
		return true, ""
	}
	return false, fmt.Sprintf("mismatch: %s", strings.Join(mismatches, ","))
}

func emitAskShadowEvent(
	ctx context.Context,
	in askShadowInput,
	match bool,
	reason string,
	shadowMessageID string,
	shadowAskID string,
	shadowNamespace string,
	shadowErr error,
	duration time.Duration,
) {
	builder := observability.NewEvent("agent.ask.shadow").
		WithComponent(observability.ComponentCLI).
		WithCommand("agent/ask").
		EnrichFromContext(ctx).
		WithDataMap(map[string]any{
			"command":          "ask",
			"match":            match,
			"reason":           strings.TrimSpace(reason),
			"agent_id":         strings.TrimSpace(in.AgentID),
			"namespace":        strings.TrimSpace(in.Namespace),
			"shadow_namespace": strings.TrimSpace(shadowNamespace),
			"ask_id":           strings.TrimSpace(in.AskID),
			"shadow_ask_id":    strings.TrimSpace(shadowAskID),
			"v1_message_id":    strings.TrimSpace(in.V1MessageID),
			"v2_message_id":    strings.TrimSpace(shadowMessageID),
			"question_hash":    observability.HashQuestion(in.Question),
		})
	if shadowErr != nil {
		observability.Emit(ctx, builder.Error(shadowErr, duration))
		return
	}
	observability.Emit(ctx, builder.Success(duration))
}
