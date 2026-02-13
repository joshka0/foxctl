package tools

import (
	"context"
	"fmt"
	"time"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	errspkg "github.com/jkatigb/agentctl/internal/platform/errors"
	tooling "github.com/jkatigb/agentctl/internal/tooling"
)

// registerMailTools registers mailbox/communication tools.
func (r *Registry) registerMailTools() error {
	// mail.send - send a message to another actor
	sendTool := tooling.NewFuncTool(
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
	inboxTool := tooling.NewFuncTool(
		"mail.inbox",
		"Check inbox for messages addressed to this agent.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"unread_only": {
					Type:        "boolean",
					Description: "Only return unread messages (default true)",
				},
				"unsurfaced_only": {
					Type:        "boolean",
					Description: "Only return messages that have never been surfaced into context (default false)",
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
	ackTool := tooling.NewFuncTool(
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

	// mail.reserve - reserve files
	reserveTool := tooling.NewFuncTool(
		"mail.reserve",
		"Reserve files for exclusive or shared access.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"paths": {
					Type:        "array",
					Description: "List of file paths to reserve",
					Required:    true,
				},
				"mode": {
					Type:        "string",
					Description: "Reservation mode: exclusive, shared (default exclusive)",
				},
				"ttl_seconds": {
					Type:        "integer",
					Description: "Time-to-live in seconds (default 600)",
				},
				"reason": {
					Type:        "string",
					Description: "Reason for reservation (defaults to active task title)",
				},
			},
		},
		r.wrapWithTelemetry("mail.reserve", r.mailReserve),
	)
	if err := r.tools.Register(reserveTool); err != nil {
		return fmt.Errorf("register mail.reserve: %w", err)
	}

	// mail.release - release reservations
	releaseTool := tooling.NewFuncTool(
		"mail.release",
		"Release file reservations.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"paths": {
					Type:        "array",
					Description: "List of file paths to release (optional if reservation_ids provided)",
				},
				"reservation_ids": {
					Type:        "array",
					Description: "List of reservation IDs to release (optional if paths provided)",
				},
			},
		},
		r.wrapWithTelemetry("mail.release", r.mailRelease),
	)
	if err := r.tools.Register(releaseTool); err != nil {
		return fmt.Errorf("register mail.release: %w", err)
	}

	return nil
}

// mailSend implements the mail.send tool.
func (r *Registry) mailSend(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openBoardStore == nil {
		return errorResult("board store not configured"), nil
	}

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

	kind := agent.BoardMessageKindInfo
	if k, ok := args["kind"].(string); ok && k != "" {
		kind = agent.BoardMessageKind(k)
	}

	priority := agent.DefaultPriority
	if p, ok := args["priority"].(float64); ok {
		priority = int(p)
	}

	ackRequired := false
	if a, ok := args["ack_required"].(bool); ok {
		ackRequired = a
	}

	store, err := r.openBoardStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open board store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close board store") }()

	msg := &agent.BoardMessage{
		WorkspaceID: r.config.WorkspaceID,
		TaskID:      r.config.TaskID,
		Stream:      agent.DefaultStream,
		Sender:      r.config.ActorID,
		Recipient:   recipient,
		Kind:        kind,
		Priority:    priority,
		AckRequired: ackRequired,
		Subject:     subject,
		Body:        body,
	}

	if err := store.SendMessage(ctx, msg); err != nil {
		return errorResult(fmt.Sprintf("send message: %v", err)), nil
	}

	return successResult(map[string]any{
		"message_id": msg.ID,
		"sent_to":    recipient,
		"success":    true,
	}), nil
}

// mailInbox implements the mail.inbox tool.
func (r *Registry) mailInbox(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openBoardStore == nil {
		return errorResult("board store not configured"), nil
	}

	unreadOnly := true
	if u, ok := args["unread_only"].(bool); ok {
		unreadOnly = u
	}

	unsurfacedOnly := false
	if us, ok := args["unsurfaced_only"].(bool); ok {
		unsurfacedOnly = us
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	store, err := r.openBoardStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open board store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close board store") }()

	filter := agent.InboxFilter{
		WorkspaceID:    r.config.WorkspaceID,
		ActorID:        r.config.ActorID,
		OnlyUnread:     unreadOnly,
		OnlyUnsurfaced: unsurfacedOnly,
		Limit:          limit,
	}
	// Note: We could filter by kind if InboxFilter supported it, but it doesn't currently.
	// We'll rely on client-side filtering if needed, or update filter spec.

	messages, err := store.Inbox(ctx, filter)
	if err != nil {
		return errorResult(fmt.Sprintf("fetch inbox: %v", err)), nil
	}

	return successResult(map[string]any{
		"messages": messages,
		"count":    len(messages),
	}), nil
}

// mailAck implements the mail.ack tool.
func (r *Registry) mailAck(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openBoardStore == nil {
		return errorResult("board store not configured"), nil
	}

	messageID, ok := args["message_id"].(string)
	if !ok || messageID == "" {
		return errorResult("message_id is required"), nil
	}

	store, err := r.openBoardStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open board store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close board store") }()

	count, err := store.AckMessages(ctx, r.config.WorkspaceID, r.config.ActorID, []string{messageID})
	if err != nil {
		return errorResult(fmt.Sprintf("ack message: %v", err)), nil
	}

	return successResult(map[string]any{
		"message_id":   messageID,
		"acknowledged": true,
		"count":        count,
		"success":      true,
	}), nil
}

// mailReserve implements the mail.reserve tool.
func (r *Registry) mailReserve(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openBoardStore == nil {
		return errorResult("board store not configured"), nil
	}

	pathsRaw, ok := args["paths"].([]any)
	if !ok {
		return errorResult("paths must be an array"), nil
	}
	var paths []string
	for _, p := range pathsRaw {
		if ps, ok := p.(string); ok {
			resolved, err := r.resolvePath(ps)
			if err != nil {
				return errorResult(fmt.Sprintf("invalid path %q: %v", ps, err)), nil
			}
			paths = append(paths, resolved)
		}
	}
	if len(paths) == 0 {
		return errorResult("paths cannot be empty"), nil
	}

	mode := agent.ReservationModeExclusive
	if m, ok := args["mode"].(string); ok && m != "" {
		mode = agent.ReservationMode(m)
	}

	ttlSeconds := 600
	if t, ok := args["ttl_seconds"].(float64); ok && t > 0 {
		ttlSeconds = int(t)
	}

	reason := ""
	if s, ok := args["reason"].(string); ok {
		reason = s
	}

	store, err := r.openBoardStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open board store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close board store") }()

	// Check conflicts
	conflicts, err := store.CheckConflicts(ctx, r.config.WorkspaceID, paths, r.config.ActorID, mode)
	if err != nil {
		return errorResult(fmt.Sprintf("check conflicts: %v", err)), nil
	}
	if len(conflicts) > 0 {
		return successResult(map[string]any{
			"success":   false,
			"conflicts": conflicts,
			"error":     "reservation conflict",
		}), nil
	}

	expiresAt := time.Now().UTC().Add(time.Duration(ttlSeconds) * time.Second)
	var reserved []string

	for _, p := range paths {
		res := &agent.FileReservation{
			WorkspaceID: r.config.WorkspaceID,
			TaskID:      r.config.TaskID,
			Path:        p,
			Holder:      r.config.ActorID,
			Mode:        mode,
			Reason:      reason,
			ExpiresAt:   expiresAt,
		}
		if err := store.Reserve(ctx, res); err != nil {
			return errorResult(fmt.Sprintf("reserve %q: %v", p, err)), nil
		}
		reserved = append(reserved, p)
	}

	return successResult(map[string]any{
		"reserved":   reserved,
		"expires_at": expiresAt,
		"success":    true,
	}), nil
}

// mailRelease implements the mail.release tool.
func (r *Registry) mailRelease(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openBoardStore == nil {
		return errorResult("board store not configured"), nil
	}

	var paths []string
	if pRaw, ok := args["paths"].([]any); ok {
		for _, p := range pRaw {
			if ps, ok := p.(string); ok {
				resolved, err := r.resolvePath(ps)
				if err != nil {
					return errorResult(fmt.Sprintf("invalid path %q: %v", ps, err)), nil
				}
				paths = append(paths, resolved)
			}
		}
	}

	var reservationIDs []string
	if idsRaw, ok := args["reservation_ids"].([]any); ok {
		for _, id := range idsRaw {
			if ids, ok := id.(string); ok {
				reservationIDs = append(reservationIDs, ids)
			}
		}
	}

	if len(paths) == 0 && len(reservationIDs) == 0 {
		return errorResult("paths or reservation_ids required"), nil
	}

	store, err := r.openBoardStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open board store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close board store") }()

	totalReleased := 0

	if len(paths) > 0 {
		count, err := store.Release(ctx, r.config.WorkspaceID, r.config.ActorID, paths)
		if err != nil {
			return errorResult(fmt.Sprintf("release by paths: %v", err)), nil
		}
		totalReleased += count
	}

	if len(reservationIDs) > 0 {
		count, err := store.ReleaseByID(ctx, reservationIDs)
		if err != nil {
			return errorResult(fmt.Sprintf("release by ids: %v", err)), nil
		}
		totalReleased += count
	}

	return successResult(map[string]any{
		"released_count": totalReleased,
		"success":        true,
	}), nil
}
