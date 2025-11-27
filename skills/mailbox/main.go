// Package main implements the mailbox/manage skill for workspace coordination.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
)

type input struct {
	Operation   string      `json:"operation"`
	WorkspaceID string      `json:"workspace_id"`
	Send        *sendReq    `json:"send"`
	Inbox       *inboxReq   `json:"inbox"`
	Ack         *ackReq     `json:"ack"`
	Reserve     *reserveReq `json:"reserve"`
	Release     *releaseReq `json:"release"`
}

type sendReq struct {
	Sender      string `json:"sender"`
	Recipient   string `json:"recipient"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	TaskID      string `json:"task_id"`
	Stream      string `json:"stream"`
	Kind        string `json:"kind"`
	Priority    int    `json:"priority"`
	AckRequired bool   `json:"ack_required"`
}

type inboxReq struct {
	ActorID    string `json:"actor_id"`
	TaskID     string `json:"task_id"`
	Stream     string `json:"stream"`
	OnlyUnread bool   `json:"only_unread"`
	Limit      int    `json:"limit"`
}

type ackReq struct {
	ActorID    string   `json:"actor_id"`
	MessageIDs []string `json:"message_ids"`
}

type reserveReq struct {
	ActorID    string   `json:"actor_id"`
	Paths      []string `json:"paths"`
	Mode       string   `json:"mode"`
	TTLSeconds int      `json:"ttl_seconds"`
}

type releaseReq struct {
	ActorID        string   `json:"actor_id"`
	Paths          []string `json:"paths"`
	ReservationIDs []string `json:"reservation_ids"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("mailbox/manage", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("mailbox/manage", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("mailbox/manage", "EARG", fmt.Errorf("decode input: %w", err))
	}
	if err := run(ctx, rc, cfg, in); err != nil {
		fail("mailbox/manage", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, cfg config.Config, in input) error {
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open board store: %w", err)
	}
	defer func() { _ = store.Close() }()

	op := strings.ToLower(strings.TrimSpace(in.Operation))
	workspaceID := in.WorkspaceID
	if workspaceID == "" {
		workspaceID, _ = os.Getwd()
	}

	var data map[string]any

	switch op {
	case "send":
		if in.Send == nil {
			return fmt.Errorf("send payload is required")
		}
		if in.Send.Sender == "" {
			return fmt.Errorf("sender is required")
		}
		if in.Send.Recipient == "" {
			return fmt.Errorf("recipient is required")
		}
		if in.Send.Subject == "" {
			return fmt.Errorf("subject is required")
		}

		msg := agent.BoardMessage{
			WorkspaceID: workspaceID,
			TaskID:      in.Send.TaskID,
			Stream:      in.Send.Stream,
			Sender:      in.Send.Sender,
			Recipient:   in.Send.Recipient,
			Kind:        agent.BoardMessageKind(in.Send.Kind),
			Priority:    in.Send.Priority,
			AckRequired: in.Send.AckRequired,
			Subject:     in.Send.Subject,
			Body:        in.Send.Body,
		}
		if msg.Stream == "" {
			msg.Stream = agent.DefaultStream
		}
		if msg.Kind == "" {
			msg.Kind = agent.BoardMessageKindInfo
		}
		if msg.Priority == 0 {
			msg.Priority = agent.DefaultPriority
		}

		if err := store.SendMessage(ctx, msg); err != nil {
			return err
		}
		data = map[string]any{
			"message_id": msg.ID,
			"summary":    fmt.Sprintf("sent message to %s: %s", msg.Recipient, msg.Subject),
		}

	case "inbox":
		if in.Inbox == nil {
			return fmt.Errorf("inbox payload is required")
		}
		if in.Inbox.ActorID == "" {
			return fmt.Errorf("actor_id is required")
		}

		filter := agent.InboxFilter{
			WorkspaceID: workspaceID,
			ActorID:     in.Inbox.ActorID,
			TaskID:      in.Inbox.TaskID,
			Stream:      in.Inbox.Stream,
			OnlyUnread:  in.Inbox.OnlyUnread,
			Limit:       in.Inbox.Limit,
		}

		messages, err := store.Inbox(ctx, filter)
		if err != nil {
			return err
		}

		// Mark as read by default when retrieved
		if len(messages) > 0 {
			ids := make([]string, len(messages))
			for i, m := range messages {
				ids[i] = m.ID
			}
			_, _ = store.MarkRead(ctx, workspaceID, in.Inbox.ActorID, ids)
		}

		data = map[string]any{
			"messages": messages,
			"count":    len(messages),
			"summary":  fmt.Sprintf("retrieved %d messages for %s", len(messages), in.Inbox.ActorID),
		}

	case "ack":
		if in.Ack == nil {
			return fmt.Errorf("ack payload is required")
		}
		if in.Ack.ActorID == "" {
			return fmt.Errorf("actor_id is required")
		}
		if len(in.Ack.MessageIDs) == 0 {
			return fmt.Errorf("message_ids is required")
		}

		count, err := store.AckMessages(ctx, workspaceID, in.Ack.ActorID, in.Ack.MessageIDs)
		if err != nil {
			return err
		}
		data = map[string]any{
			"acked_count": count,
			"summary":     fmt.Sprintf("acknowledged %d messages", count),
		}

	case "reserve":
		if in.Reserve == nil {
			return fmt.Errorf("reserve payload is required")
		}
		if in.Reserve.ActorID == "" {
			return fmt.Errorf("actor_id is required")
		}
		if len(in.Reserve.Paths) == 0 {
			return fmt.Errorf("paths is required")
		}

		mode := agent.ReservationModeExclusive
		if in.Reserve.Mode == "shared" {
			mode = agent.ReservationModeShared
		}

		ttl := agent.DefaultReservationTTL
		if in.Reserve.TTLSeconds > 0 {
			ttl = time.Duration(in.Reserve.TTLSeconds) * time.Second
		}

		// Check for conflicts first
		conflicts, err := store.CheckConflicts(ctx, workspaceID, in.Reserve.Paths, in.Reserve.ActorID, mode)
		if err != nil {
			return err
		}

		var granted []agent.FileReservation
		var conflictPaths []string

		// Create conflict lookup
		conflictSet := make(map[string]bool)
		for _, c := range conflicts {
			conflictSet[c.Path] = true
			conflictPaths = append(conflictPaths, c.Path)
		}

		// Reserve non-conflicting paths
		now := time.Now().UTC()
		for _, p := range in.Reserve.Paths {
			if conflictSet[p] {
				continue
			}
			res := agent.FileReservation{
				WorkspaceID: workspaceID,
				Path:        p,
				Holder:      in.Reserve.ActorID,
				Mode:        mode,
				ExpiresAt:   now.Add(ttl),
				CreatedAt:   now,
			}
			if err := store.Reserve(ctx, res); err != nil {
				return err
			}
			granted = append(granted, res)
		}

		data = map[string]any{
			"granted":   granted,
			"conflicts": conflicts,
			"summary":   fmt.Sprintf("reserved %d paths, %d conflicts", len(granted), len(conflicts)),
		}

	case "release":
		if in.Release == nil {
			return fmt.Errorf("release payload is required")
		}

		var count int
		var err error

		if len(in.Release.ReservationIDs) > 0 {
			count, err = store.ReleaseByID(ctx, in.Release.ReservationIDs)
		} else if in.Release.ActorID != "" && len(in.Release.Paths) > 0 {
			count, err = store.Release(ctx, workspaceID, in.Release.ActorID, in.Release.Paths)
		} else {
			return fmt.Errorf("either reservation_ids or (actor_id + paths) is required")
		}

		if err != nil {
			return err
		}
		data = map[string]any{
			"released_count": count,
			"summary":        fmt.Sprintf("released %d reservations", count),
		}

	case "list_reservations":
		reservations, err := store.ListReservations(ctx, workspaceID)
		if err != nil {
			return err
		}
		data = map[string]any{
			"reservations": reservations,
			"count":        len(reservations),
			"summary":      fmt.Sprintf("found %d active reservations", len(reservations)),
		}

	default:
		return fmt.Errorf("unknown operation %q (expected send|inbox|ack|reserve|release|list_reservations)", op)
	}

	return rc.Emit("mailbox/manage", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit mailbox failure")
	os.Exit(1)
}
