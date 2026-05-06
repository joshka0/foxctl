// Package main implements the mailbox/manage skill for workspace coordination.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/teams"
)

const command = "mailbox/manage"

var allowedOps = []string{
	"send",
	"inbox",
	"ack",
	"mark_surfaced",
	"reserve",
	"release",
}

// input defines the skill input parameters for mailbox management operations with message passing and reservations.
type input struct {
	Operation    string           `json:"operation"`
	WorkspaceID  string           `json:"workspace_id"`
	Send         *sendReq         `json:"send"`
	Inbox        *inboxReq        `json:"inbox"`
	Ack          *ackReq          `json:"ack"`
	MarkSurfaced *markSurfacedReq `json:"mark_surfaced"`
	Reserve      *reserveReq      `json:"reserve"`
	Release      *releaseReq      `json:"release"`
}

// sendReq defines parameters for sending a message with delivery options and team broadcasting.
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

// inboxReq defines parameters for retrieving inbox messages with filtering and read status management.
type inboxReq struct {
	ActorID        string `json:"actor_id"`
	TaskID         string `json:"task_id"`
	Stream         string `json:"stream"`
	OnlyUnread     bool   `json:"only_unread"`
	OnlyUnsurfaced bool   `json:"only_unsurfaced"`
	Limit          int    `json:"limit"`
}

// ackReq defines parameters for acknowledging messages to mark them as processed.
type ackReq struct {
	ActorID    string   `json:"actor_id"`
	MessageIDs []string `json:"message_ids"`
}

// markSurfacedReq defines parameters for marking messages as surfaced to user interfaces.
type markSurfacedReq struct {
	ActorID    string   `json:"actor_id"`
	MessageIDs []string `json:"message_ids"`
}

// reserveReq defines parameters for reserving file paths with conflict detection and TTL support.
type reserveReq struct {
	ActorID    string   `json:"actor_id"`
	Paths      []string `json:"paths"`
	Mode       string   `json:"mode"`
	TTLSeconds int      `json:"ttl_seconds"`
}

// releaseReq defines parameters for releasing file reservations by ID or actor/path combination.
type releaseReq struct {
	ActorID        string   `json:"actor_id"`
	Paths          []string `json:"paths"`
	ReservationIDs []string `json:"reservation_ids"`
}

// main is the skill entry point for mailbox/manage with workspace coordination capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates mailbox management with message passing and file reservation capabilities.
//
// Index:
//
//	Purpose: Manage workspace coordination through message passing and file reservations
//	Keywords: mailbox/manage, message_passing, file_reservations, workspace_coordination, team_messaging
//	Related: blackboard.OpenBoardStore, teams.Open, agent.BoardMessage, agent.FileReservation
//	Flow: open board store → validate operation → route to handler → execute operation → emit results
//	Resources: blackboard store (SQLite); teams store
//	Events: message-sent, inbox-retrieved, reservation-created, reservation-released
//	OutputFields: message_id, messages, count, granted, conflicts, summary
//
// [[domain:mailbox-management]]
// [[protocol:workspace-coordination]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	store, err := blackboard.OpenBoardStore(ctx, rc.Config.Storage.Root)
	if err != nil {
		return fmt.Errorf("open board store: %w", err)
	}
	defer store.Close()

	op := oputil.Op(in.Operation)
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if op == "" {
		return skillerr.Arg("operation is required", skillerr.WithHint(opHint))
	}
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}
	workspaceID := workspaceutil.ResolveID(in.WorkspaceID, rc.Workspace)

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

		base := agent.BoardMessage{
			WorkspaceID: workspaceID,
			TaskID:      in.Send.TaskID,
			Stream:      in.Send.Stream,
			Sender:      in.Send.Sender,
			Recipient:   strings.TrimSpace(in.Send.Recipient),
			Kind:        agent.BoardMessageKind(in.Send.Kind),
			Priority:    in.Send.Priority,
			AckRequired: in.Send.AckRequired,
			Subject:     in.Send.Subject,
			Body:        in.Send.Body,
		}
		if base.Stream == "" {
			base.Stream = agent.DefaultStream
		}
		if base.Kind == "" {
			base.Kind = agent.BoardMessageKindInfo
		}
		if base.Priority == 0 {
			base.Priority = agent.DefaultPriority
		}

		if strings.HasPrefix(base.Recipient, "team:") {
			teamStore, err := teams.Open(ctx, rc.Config.Storage.Root)
			if err != nil {
				return fmt.Errorf("open teams store: %w", err)
			}
			defer teamStore.Close()

			if _, err := teamStore.GetTeam(ctx, workspaceID, base.Recipient); err != nil {
				if errors.Is(err, teams.ErrNotFound) {
					return fmt.Errorf("team not found: %s", base.Recipient)
				}
				return err
			}

			members, err := teamStore.ListMembers(ctx, workspaceID, base.Recipient, 1000)
			if err != nil {
				return err
			}
			if len(members) == 0 {
				return fmt.Errorf("team has no members: %s", base.Recipient)
			}

			messageIDs := make([]string, 0, len(members))
			var failedMembers []string
			for _, m := range members {
				msg := base
				msg.Recipient = m.ActorID
				if err := store.SendMessage(ctx, &msg); err != nil {
					failedMembers = append(failedMembers, m.ActorID)
					continue
				}
				messageIDs = append(messageIDs, msg.ID)
			}

			// If all deliveries failed, return an error.
			if len(messageIDs) == 0 {
				return fmt.Errorf("failed to deliver message to any team member")
			}

			data = map[string]any{
				"message_id":      messageIDs[0],
				"message_ids":     messageIDs,
				"delivered_count": len(messageIDs),
				"failed_count":    len(failedMembers),
				"failed_members":  failedMembers,
				"summary":         fmt.Sprintf("sent message to %s (%d recipients): %s", base.Recipient, len(messageIDs), base.Subject),
			}
			break
		}

		msg := base
		if err := store.SendMessage(ctx, &msg); err != nil {
			return err
		}
		data = map[string]any{
			"message_id":      msg.ID,
			"message_ids":     []string{msg.ID},
			"delivered_count": 1,
			"summary":         fmt.Sprintf("sent message to %s: %s", msg.Recipient, msg.Subject),
		}

	case "inbox":
		if in.Inbox == nil {
			return fmt.Errorf("inbox payload is required")
		}
		if in.Inbox.ActorID == "" {
			return fmt.Errorf("actor_id is required")
		}

		filter := agent.InboxFilter{
			WorkspaceID:    workspaceID,
			ActorID:        in.Inbox.ActorID,
			TaskID:         in.Inbox.TaskID,
			Stream:         in.Inbox.Stream,
			OnlyUnread:     in.Inbox.OnlyUnread,
			OnlyUnsurfaced: in.Inbox.OnlyUnsurfaced,
			Limit:          in.Inbox.Limit,
		}

		messages, err := store.Inbox(ctx, filter)
		if err != nil {
			return err
		}

		// Only mark as read if this is NOT a filtering query for unread messages.
		// This allows hooks to see the same unread messages multiple times until explicitly acked.
		if len(messages) > 0 && !in.Inbox.OnlyUnread {
			ids := make([]string, len(messages))
			for i, m := range messages {
				ids[i] = m.ID
			}
			// Best-effort mark as read; error is not actionable.
			_, _ = store.MarkRead(ctx, workspaceID, in.Inbox.ActorID, ids) //nolint:errcheck
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

	case "mark_surfaced":
		if in.MarkSurfaced == nil {
			return fmt.Errorf("mark_surfaced payload is required")
		}
		if in.MarkSurfaced.ActorID == "" {
			return fmt.Errorf("actor_id is required")
		}
		if len(in.MarkSurfaced.MessageIDs) == 0 {
			return fmt.Errorf("message_ids is required")
		}

		count, err := store.MarkSurfaced(ctx, workspaceID, in.MarkSurfaced.ActorID, in.MarkSurfaced.MessageIDs)
		if err != nil {
			return err
		}
		data = map[string]any{
			"surfaced_count": count,
			"summary":        fmt.Sprintf("marked %d messages as surfaced", count),
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

		// Create conflict lookup
		conflictSet := make(map[string]bool)
		for _, c := range conflicts {
			conflictSet[c.Path] = true
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
			if err := store.Reserve(ctx, &res); err != nil {
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
		return fmt.Errorf("unknown operation %q (expected send|inbox|ack|mark_surfaced|reserve|release|list_reservations)", op)
	}

	return skillout.Emit(rc, command, data)
}
