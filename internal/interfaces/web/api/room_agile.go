package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
)

type RoomAgileRequest struct {
	Workspace string `json:"workspace,omitempty"`
	Action    string `json:"action"`
	Actor     string `json:"actor,omitempty"`

	EpicID      string `json:"epic_id,omitempty"`
	MilestoneID string `json:"milestone_id,omitempty"`
	StoryID     string `json:"story_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`

	// Mutating-action fields (M4 story lifecycle).
	State         string `json:"state,omitempty"`          // target state for story_state
	Verdict       string `json:"verdict,omitempty"`        // pass/fail/waived for story_validate
	ValidatorType string `json:"validator_type,omitempty"` // human/agent/harness for story_validate
	Title         string `json:"title,omitempty"`          // story title for story_propose
	Goal          string `json:"goal,omitempty"`           // story goal for story_propose
	Notes         string `json:"notes,omitempty"`          // notes for story_propose/story_validate
	ProposalID    string `json:"proposal_id,omitempty"`    // for story_accept
	Command       string `json:"command,omitempty"`        // command that produced validation
	Artifact      string `json:"artifact,omitempty"`       // artifact ref (sha256:...) for validation
}

func handleRoomAgileRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, roomID string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req RoomAgileRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	workspace := strings.TrimSpace(req.Workspace)
	if workspace == "" {
		workspace = roomWorkspaceID(r)
	}
	if workspace == "" {
		httpError(w, http.StatusBadRequest, "workspace is required")
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	req.EpicID = strings.TrimSpace(req.EpicID)
	req.MilestoneID = strings.TrimSpace(req.MilestoneID)
	req.StoryID = strings.TrimSpace(req.StoryID)
	req.Actor = strings.TrimSpace(req.Actor)
	req.State = strings.TrimSpace(req.State)
	req.Verdict = strings.TrimSpace(req.Verdict)
	req.ValidatorType = strings.TrimSpace(req.ValidatorType)
	req.Title = strings.TrimSpace(req.Title)
	req.Goal = strings.TrimSpace(req.Goal)
	req.Notes = strings.TrimSpace(req.Notes)
	req.ProposalID = strings.TrimSpace(req.ProposalID)
	req.Command = strings.TrimSpace(req.Command)
	req.Artifact = strings.TrimSpace(req.Artifact)
	if req.Action == "" {
		httpError(w, http.StatusBadRequest, "action is required")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 200
	}

	store, err := blackboard.OpenBoardStore(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open board store")
		httpError(w, http.StatusInternalServerError, "failed to open board store")
		return
	}
	defer store.Close()

	messages, err := store.ListRoomMessages(r.Context(), workspace, roomID, req.Limit)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Str("action", req.Action).Msg("failed to load room-agile messages")
		httpError(w, http.StatusInternalServerError, "failed to load room-agile messages")
		return
	}

	mutatingActions := map[string]bool{
		"story_state":    true,
		"story_validate": true,
		"story_propose":  true,
		"story_accept":   true,
	}

	if mutatingActions[req.Action] {
		result, err := buildRoomAgileMutatingResult(r.Context(), roomID, workspace, req, messages, store)
		if err != nil {
			if errors.Is(err, blackboard.ErrRoomNotFound) {
				httpError(w, http.StatusNotFound, err.Error())
				return
			}
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "not found") {
				status = http.StatusNotFound
			}
			httpError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"action":    req.Action,
			"room_id":   roomID,
			"workspace": workspace,
			"result": map[string]any{
				"version": 1,
				"status":  "ok",
				"command": "foxctl.room.agile." + strings.ReplaceAll(req.Action, "_", "."),
				"data":    result,
				"meta": map[string]any{
					"source": "web:/api/rooms/{room_id}/agile",
				},
				"error": map[string]any{},
			},
		})
		return
	}

	result, err := buildRoomAgileReadResult(roomID, req, messages)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"action":    req.Action,
		"room_id":   roomID,
		"workspace": workspace,
		"result": map[string]any{
			"version": 1,
			"status":  "ok",
			"command": "foxctl.room.agile." + strings.ReplaceAll(req.Action, "_", "."),
			"data":    result,
			"meta": map[string]any{
				"source": "web:/api/rooms/{room_id}/agile",
			},
			"error": map[string]any{},
		},
	})
}

func buildRoomAgileReadResult(roomID string, req RoomAgileRequest, messages []agent.BoardMessage) (map[string]any, error) {
	epics := apiRoomAgileEpics(messages)
	milestones := apiRoomAgileMilestones(messages)
	stories := apiRoomAgileStories(messages)

	switch req.Action {
	case "epic_show":
		if req.EpicID == "" {
			return map[string]any{"room_id": roomID, "epics": epics, "count": len(epics)}, nil
		}
		epic := findByID(epics, req.EpicID)
		if epic == nil {
			return nil, fmt.Errorf("epic %q not found", req.EpicID)
		}
		return map[string]any{"room_id": roomID, "epic": epic}, nil
	case "epic_resume", "epic_health", "epic_next":
		if req.EpicID == "" {
			return nil, fmt.Errorf("epic_id is required for %s", req.Action)
		}
		epic := findByID(epics, req.EpicID)
		if epic == nil {
			return nil, fmt.Errorf("epic %q not found", req.EpicID)
		}
		relatedMilestones := filterByField(milestones, "epic_id", req.EpicID)
		relatedStories := filterStoriesByEpic(stories, relatedMilestones)
		status := apiRoomAgileEpicStatus(epic, relatedMilestones, relatedStories)
		data := map[string]any{
			"room_id":    roomID,
			"epic":       epic,
			"status":     status,
			"milestones": relatedMilestones,
			"stories":    relatedStories,
		}
		if req.Action == "epic_health" {
			data["health"] = apiRoomAgileHealth(status)
		}
		if req.Action == "epic_next" {
			data["next"] = apiRoomAgileNext(status, relatedMilestones, relatedStories)
		}
		return data, nil
	case "milestone_show":
		if req.MilestoneID == "" {
			return map[string]any{"room_id": roomID, "milestones": milestones, "count": len(milestones)}, nil
		}
		milestone := findByID(milestones, req.MilestoneID)
		if milestone == nil {
			return nil, fmt.Errorf("milestone %q not found", req.MilestoneID)
		}
		return map[string]any{"room_id": roomID, "milestone": milestone, "stories": filterByField(stories, "milestone_id", req.MilestoneID)}, nil
	case "story_show":
		if req.StoryID == "" {
			return map[string]any{"room_id": roomID, "stories": stories, "count": len(stories)}, nil
		}
		story := findByID(stories, req.StoryID)
		if story == nil {
			return nil, fmt.Errorf("story %q not found", req.StoryID)
		}
		return map[string]any{"room_id": roomID, "story": story}, nil
	default:
		return nil, fmt.Errorf("unsupported room-agile action %q", req.Action)
	}
}

func apiRoomAgileEpics(messages []agent.BoardMessage) []map[string]any {
	closed := map[string]bool{}
	finalized := map[string]map[string]any{}
	for _, msg := range messages {
		switch msg.Kind {
		case agent.BoardMessageKindEpicFinalize:
			if msg.RelatedMessageID != "" {
				finalized[msg.RelatedMessageID] = apiMessageView(msg)
			}
		case agent.BoardMessageKindEpicClose:
			if msg.RelatedMessageID != "" {
				closed[msg.RelatedMessageID] = true
			}
		}
	}
	var out []map[string]any
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindEpic {
			continue
		}
		view := apiMessageView(msg)
		view["title"] = strings.TrimSpace(strings.TrimPrefix(msg.Subject, "Epic:"))
		if final, ok := finalized[msg.ID]; ok {
			view["finalized"] = true
			view["final_brief"] = final
			view["status"] = "finalized"
		} else {
			view["status"] = "discovery"
		}
		if closed[msg.ID] {
			view["closed"] = true
			view["status"] = "closed"
		}
		out = append(out, view)
	}
	return out
}

func apiRoomAgileMilestones(messages []agent.BoardMessage) []map[string]any {
	summarized := map[string]bool{}
	reviewed := map[string]map[string]any{}
	for _, msg := range messages {
		switch msg.Kind {
		case agent.BoardMessageKindMilestoneSummary:
			if msg.RelatedMessageID != "" {
				summarized[msg.RelatedMessageID] = true
			}
		case agent.BoardMessageKindMilestoneReview:
			if msg.RelatedMessageID != "" {
				reviewed[msg.RelatedMessageID] = apiMessageView(msg)
			}
		}
	}
	var out []map[string]any
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindMilestone {
			continue
		}
		view := apiMessageView(msg)
		meta := parseLineMeta(msg.Body)
		view["epic_id"] = firstNonEmpty(meta["EpicID"], msg.RelatedMessageID)
		view["title"] = strings.TrimSpace(strings.TrimPrefix(msg.Subject, "Milestone:"))
		view["objective"] = firstNonEmpty(meta["Objective"], meta["Goal"])
		view["status"] = "open"
		if reviewed[msg.ID] != nil {
			view["review"] = reviewed[msg.ID]
		}
		if summarized[msg.ID] {
			view["summarized"] = true
			view["status"] = "summarized"
		}
		out = append(out, view)
	}
	return out
}

func apiRoomAgileStories(messages []agent.BoardMessage) []map[string]any {
	states := map[string]string{}
	validations := map[string][]map[string]any{}
	for _, msg := range messages {
		switch msg.Kind {
		case agent.BoardMessageKindStoryState:
			if msg.RelatedMessageID != "" {
				meta := parseLineMeta(msg.Body)
				states[msg.RelatedMessageID] = firstNonEmpty(meta["State"], strings.TrimSpace(strings.TrimPrefix(msg.Subject, "Story State:")))
			}
		case agent.BoardMessageKindStoryValidation:
			if msg.RelatedMessageID != "" {
				validations[msg.RelatedMessageID] = append(validations[msg.RelatedMessageID], apiMessageView(msg))
			}
		}
	}
	var out []map[string]any
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindStory {
			continue
		}
		view := apiMessageView(msg)
		view["milestone_id"] = msg.RelatedMessageID
		view["title"] = strings.TrimSpace(strings.TrimPrefix(msg.Subject, "Story:"))
		view["status"] = firstNonEmpty(states[msg.ID], "accepted")
		if vals := validations[msg.ID]; len(vals) > 0 {
			view["validations"] = vals
			view["validation_count"] = len(vals)
		}
		out = append(out, view)
	}
	return out
}

func apiMessageView(msg agent.BoardMessage) map[string]any {
	return map[string]any{
		"id":                 msg.ID,
		"kind":               string(msg.Kind),
		"subject":            msg.Subject,
		"body":               msg.Body,
		"sender":             msg.Sender,
		"recipient":          msg.Recipient,
		"related_message_id": msg.RelatedMessageID,
		"created_at":         msg.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func parseLineMeta(body string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" {
			out[key] = value
		}
	}
	return out
}

func findByID(items []map[string]any, id string) map[string]any {
	for _, item := range items {
		if fmt.Sprint(item["id"]) == id {
			return item
		}
	}
	return nil
}

func filterByField(items []map[string]any, field, value string) []map[string]any {
	var out []map[string]any
	for _, item := range items {
		if fmt.Sprint(item[field]) == value {
			out = append(out, item)
		}
	}
	return out
}

func filterStoriesByEpic(stories, milestones []map[string]any) []map[string]any {
	milestoneIDs := map[string]struct{}{}
	for _, milestone := range milestones {
		if id := fmt.Sprint(milestone["id"]); id != "" {
			milestoneIDs[id] = struct{}{}
		}
	}
	var out []map[string]any
	for _, story := range stories {
		if _, ok := milestoneIDs[fmt.Sprint(story["milestone_id"])]; ok {
			out = append(out, story)
		}
	}
	return out
}

func apiRoomAgileEpicStatus(epic map[string]any, milestones, stories []map[string]any) map[string]any {
	openStories := 0
	validatedStories := 0
	for _, story := range stories {
		status := fmt.Sprint(story["status"])
		if status != "done" && status != "validated" && status != "waived" {
			openStories++
		}
		if count, ok := story["validation_count"].(int); ok && count > 0 {
			validatedStories++
		}
	}
	return map[string]any{
		"epic_id":           epic["id"],
		"epic_status":       epic["status"],
		"milestone_count":   len(milestones),
		"story_count":       len(stories),
		"open_story_count":  openStories,
		"validated_stories": validatedStories,
	}
}

func apiRoomAgileHealth(status map[string]any) map[string]any {
	var warnings []string
	if status["epic_status"] == "discovery" {
		warnings = append(warnings, "epic_not_finalized")
	}
	if status["milestone_count"] == 0 {
		warnings = append(warnings, "no_milestones")
	}
	if status["story_count"] == 0 {
		warnings = append(warnings, "no_stories")
	}
	return map[string]any{
		"status":   map[bool]string{true: "healthy", false: "needs_attention"}[len(warnings) == 0],
		"warnings": warnings,
	}
}

func apiRoomAgileNext(status map[string]any, milestones, stories []map[string]any) []map[string]any {
	if status["epic_status"] == "discovery" {
		return []map[string]any{{"action": "finalize_epic", "reason": "epic is still in discovery"}}
	}
	if len(milestones) == 0 {
		return []map[string]any{{"action": "start_milestone", "reason": "no milestones exist"}}
	}
	for _, story := range stories {
		if fmt.Sprint(story["status"]) == "accepted" {
			return []map[string]any{{"action": "start_story", "story_id": story["id"], "title": story["title"]}}
		}
	}
	return []map[string]any{{"action": "review_milestone", "reason": "no accepted stories are waiting to start"}}
}

// validStoryStates is the set of allowed lifecycle states for a story.
var validStoryStates = map[string]bool{
	"in_progress": true,
	"in_review":   true,
	"done":        true,
	"waived":      true,
}

func validateStoryState(target string) error {
	if !validStoryStates[target] {
		return fmt.Errorf("invalid story state %q; valid: in_progress, in_review, done, waived", target)
	}
	return nil
}

// buildRoomAgileMutatingResult handles the four mutating story-lifecycle actions
// (story_state, story_validate, story_propose, story_accept). Each action
// builds a BoardMessage, persists it via store.SendMessage, and returns a
// result map in the standard envelope shape.
func buildRoomAgileMutatingResult(
	ctx context.Context,
	roomID string,
	workspace string,
	req RoomAgileRequest,
	messages []agent.BoardMessage,
	store blackboard.BoardStore,
) (map[string]any, error) {
	switch req.Action {
	case "story_state":
		return handleStoryState(ctx, roomID, workspace, req, messages, store)
	case "story_validate":
		return handleStoryValidate(ctx, roomID, workspace, req, messages, store)
	case "story_propose":
		return handleStoryPropose(ctx, roomID, workspace, req, store)
	case "story_accept":
		return handleStoryAccept(ctx, roomID, workspace, req, messages, store)
	default:
		return nil, fmt.Errorf("unsupported mutating action %q", req.Action)
	}
}

func handleStoryState(
	ctx context.Context,
	roomID, workspace string,
	req RoomAgileRequest,
	messages []agent.BoardMessage,
	store blackboard.BoardStore,
) (map[string]any, error) {
	if req.StoryID == "" {
		return nil, fmt.Errorf("story_id is required for story_state")
	}
	if req.State == "" {
		return nil, fmt.Errorf("state is required for story_state")
	}
	if err := validateStoryState(req.State); err != nil {
		return nil, err
	}

	// Look up the story in existing messages.
	stories := apiRoomAgileStories(messages)
	story := findByID(stories, req.StoryID)
	if story == nil {
		return nil, fmt.Errorf("story %q not found", req.StoryID)
	}

	oldStatus := fmt.Sprint(story["status"])

	body := fmt.Sprintf("State: %s\nPreviousStatus: %s", req.State, oldStatus)
	if req.Notes != "" {
		body += "\nNotes: " + req.Notes
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      workspace,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           firstNonEmpty(req.Actor, "api"),
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStoryState,
		RelatedMessageID: req.StoryID,
		Subject:          "Story State: " + req.State,
		Body:             body,
		Status:           agent.BoardMessageStatusUnread,
		Priority:         agent.DefaultPriority,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.SendMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to send story_state message: %w", err)
	}

	return map[string]any{
		"message": apiMessageView(*msg),
		"story": map[string]any{
			"id":       story["id"],
			"title":    story["title"],
			"status":   req.State,
			"previous": oldStatus,
		},
	}, nil
}

func handleStoryValidate(
	ctx context.Context,
	roomID, workspace string,
	req RoomAgileRequest,
	messages []agent.BoardMessage,
	store blackboard.BoardStore,
) (map[string]any, error) {
	if req.StoryID == "" {
		return nil, fmt.Errorf("story_id is required for story_validate")
	}
	if req.Verdict == "" {
		return nil, fmt.Errorf("verdict is required for story_validate")
	}
	if req.ValidatorType == "" {
		return nil, fmt.Errorf("validator_type is required for story_validate")
	}

	// Look up the story in existing messages.
	stories := apiRoomAgileStories(messages)
	story := findByID(stories, req.StoryID)
	if story == nil {
		return nil, fmt.Errorf("story %q not found", req.StoryID)
	}

	var bodyLines []string
	bodyLines = append(bodyLines, "Verdict: "+req.Verdict)
	bodyLines = append(bodyLines, "ValidatorType: "+req.ValidatorType)
	if req.Command != "" {
		bodyLines = append(bodyLines, "Command: "+req.Command)
	}
	if req.Artifact != "" {
		bodyLines = append(bodyLines, "Artifact: "+req.Artifact)
	}
	if req.Notes != "" {
		bodyLines = append(bodyLines, "Notes: "+req.Notes)
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      workspace,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           firstNonEmpty(req.Actor, "api"),
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStoryValidation,
		RelatedMessageID: req.StoryID,
		Subject:          "Story Validation: " + req.Verdict,
		Body:             strings.Join(bodyLines, "\n"),
		Status:           agent.BoardMessageStatusUnread,
		Priority:         agent.DefaultPriority,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.SendMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to send story_validate message: %w", err)
	}

	return map[string]any{
		"message":  apiMessageView(*msg),
		"story_id": req.StoryID,
	}, nil
}

func handleStoryPropose(
	ctx context.Context,
	roomID, workspace string,
	req RoomAgileRequest,
	store blackboard.BoardStore,
) (map[string]any, error) {
	if req.MilestoneID == "" {
		return nil, fmt.Errorf("milestone_id is required for story_propose")
	}
	if req.Title == "" {
		return nil, fmt.Errorf("title is required for story_propose")
	}

	var bodyLines []string
	bodyLines = append(bodyLines, "Title: "+req.Title)
	if req.Goal != "" {
		bodyLines = append(bodyLines, "Goal: "+req.Goal)
	}
	if req.Notes != "" {
		bodyLines = append(bodyLines, "Notes: "+req.Notes)
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      workspace,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           firstNonEmpty(req.Actor, "api"),
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStoryProposal,
		RelatedMessageID: req.MilestoneID,
		Subject:          "Story Proposal: " + req.Title,
		Body:             strings.Join(bodyLines, "\n"),
		Status:           agent.BoardMessageStatusUnread,
		Priority:         agent.DefaultPriority,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.SendMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to send story_propose message: %w", err)
	}

	return map[string]any{
		"message": apiMessageView(*msg),
	}, nil
}

func handleStoryAccept(
	ctx context.Context,
	roomID, workspace string,
	req RoomAgileRequest,
	messages []agent.BoardMessage,
	store blackboard.BoardStore,
) (map[string]any, error) {
	if req.ProposalID == "" {
		return nil, fmt.Errorf("proposal_id is required for story_accept")
	}

	// Find the proposal message by scanning raw messages.
	var proposal *agent.BoardMessage
	for i := range messages {
		if messages[i].Kind == agent.BoardMessageKindStoryProposal && messages[i].ID == req.ProposalID {
			proposal = &messages[i]
			break
		}
	}
	if proposal == nil {
		return nil, fmt.Errorf("proposal %q not found", req.ProposalID)
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      workspace,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           firstNonEmpty(req.Actor, "api"),
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStory,
		RelatedMessageID: proposal.RelatedMessageID, // the milestone ID
		Subject:          "Story: " + strings.TrimPrefix(proposal.Subject, "Story Proposal: "),
		Body:             proposal.Body,
		Status:           agent.BoardMessageStatusUnread,
		Priority:         agent.DefaultPriority,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.SendMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to send story_accept message: %w", err)
	}

	return map[string]any{
		"message":     apiMessageView(*msg),
		"proposal_id": req.ProposalID,
	}, nil
}
