package tools

import (
	"context"
	"fmt"
	"time"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
)

// registerMailTools registers mailbox/communication tools.
func (r *Registry) registerMailTools() error {
	// mail.send - send a message to another actor
	sendTool := dstools.NewFuncTool(
		"mail.send",
		"Send a message to another actor (agent, overseer, or human) via the mailbox.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"recipient": {
					Type:        "string",
					Description: "Recipient actor ID (e.g., 'overseer', 'agent:coder-1', 'team:backend')",
					Required:    true,
				},
				"subject": {
					Type:        "string",
					Description: "Message subject",
					Required:    true,
				},
				"body": {
					Type:        "string",
					Description: "Message body",
					Required:    true,
				},
				"kind": {
					Type:        "string",
					Description: "Message kind: instruction, info, alert, review_request, status_update",
				},
				"priority": {
					Type:        "integer",
					Description: "Priority level (1=low, 2=normal, 3=high, 4=urgent)",
				},
				"ack_required": {
					Type:        "boolean",
					Description: "Whether acknowledgment is required",
				},
			},
		},
		r.wrapWithTelemetry("mail.send", r.mailSend),
	)
	if err := r.tools.Register(sendTool); err != nil {
		return fmt.Errorf("register mail.send: %w", err)
	}

	// mail.inbox - check inbox for messages
	inboxTool := dstools.NewFuncTool(
		"mail.inbox",
		"Check inbox for messages addressed to this agent.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"unread_only": {
					Type:        "boolean",
					Description: "Only return unread messages (default true)",
				},
				"kind": {
					Type:        "string",
					Description: "Filter by message kind",
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum number of messages to return (default 20)",
				},
			},
		},
		r.wrapWithTelemetry("mail.inbox", r.mailInbox),
	)
	if err := r.tools.Register(inboxTool); err != nil {
		return fmt.Errorf("register mail.inbox: %w", err)
	}

	// mail.ack - acknowledge a message
	ackTool := dstools.NewFuncTool(
		"mail.ack",
		"Acknowledge receipt of a message.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"message_id": {
					Type:        "string",
					Description: "ID of the message to acknowledge",
					Required:    true,
				},
				"response": {
					Type:        "string",
					Description: "Optional response message",
				},
			},
		},
		r.wrapWithTelemetry("mail.ack", r.mailAck),
	)
	if err := r.tools.Register(ackTool); err != nil {
		return fmt.Errorf("register mail.ack: %w", err)
	}

	return nil
}

// MailMessage represents a message in the mailbox.
type MailMessage struct {
	ID          string    `json:"id"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	Subject     string    `json:"subject"`
	Body        string    `json:"body"`
	Kind        string    `json:"kind"`
	Priority    int       `json:"priority"`
	AckRequired bool      `json:"ack_required"`
	Read        bool      `json:"read"`
	Timestamp   time.Time `json:"timestamp"`
}

// mailSend implements the mail.send tool.
// Note: This is a stub - in production it would use the agentctl mailbox.
func (r *Registry) mailSend(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	recipient, ok := args["recipient"].(string)
	if !ok || recipient == "" {
		return errorResult("recipient is required"), nil
	}

	subject, ok := args["subject"].(string)
	if !ok || subject == "" {
		return errorResult("subject is required"), nil
	}

	body, ok := args["body"].(string)
	if !ok || body == "" {
		return errorResult("body is required"), nil
	}

	kind := "info"
	if k, ok := args["kind"].(string); ok {
		kind = k
	}

	priority := 2
	if p, ok := args["priority"].(float64); ok {
		priority = int(p)
	}

	ackRequired := false
	if a, ok := args["ack_required"].(bool); ok {
		ackRequired = a
	}

	// Stub implementation - would send via agentctl mailbox
	message := MailMessage{
		ID:          fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		From:        r.config.ActorID,
		To:          recipient,
		Subject:     subject,
		Body:        body,
		Kind:        kind,
		Priority:    priority,
		AckRequired: ackRequired,
		Read:        false,
		Timestamp:   time.Now(),
	}

	return successResult(map[string]any{
		"message_id": message.ID,
		"sent_to":    recipient,
		"success":    true,
		"note":       "Stub implementation - connect to agentctl mailbox for real delivery",
	}), nil
}

// mailInbox implements the mail.inbox tool.
// Note: This is a stub - in production it would read from the agentctl mailbox.
func (r *Registry) mailInbox(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	unreadOnly := true
	if u, ok := args["unread_only"].(bool); ok {
		unreadOnly = u
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	_ = unreadOnly
	_ = limit

	// Stub implementation - returns placeholder data
	messages := []MailMessage{
		{
			ID:          "msg_example_001",
			From:        "overseer",
			To:          r.config.ActorID,
			Subject:     "Welcome",
			Body:        "Welcome to the agent system. This is a placeholder message.",
			Kind:        "info",
			Priority:    2,
			AckRequired: false,
			Read:        false,
			Timestamp:   time.Now().Add(-1 * time.Hour),
		},
	}

	return successResult(map[string]any{
		"messages": messages,
		"count":    len(messages),
		"note":     "Stub implementation - connect to agentctl mailbox for real messages",
	}), nil
}

// mailAck implements the mail.ack tool.
// Note: This is a stub - in production it would update the agentctl mailbox.
func (r *Registry) mailAck(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	messageID, ok := args["message_id"].(string)
	if !ok || messageID == "" {
		return errorResult("message_id is required"), nil
	}

	response := ""
	if r, ok := args["response"].(string); ok {
		response = r
	}

	_ = response

	return successResult(map[string]any{
		"message_id":   messageID,
		"acknowledged": true,
		"success":      true,
		"note":         "Stub implementation - connect to agentctl mailbox for real acknowledgment",
	}), nil
}
