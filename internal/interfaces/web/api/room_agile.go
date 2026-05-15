package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
)

type RoomAgileRequest struct {
	Workspace string `json:"workspace,omitempty"`
	Action    string `json:"action"`
	Sender    string `json:"sender,omitempty"`
	Actor     string `json:"actor,omitempty"`

	EpicID      string `json:"epic_id,omitempty"`
	MilestoneID string `json:"milestone_id,omitempty"`
	StoryID     string `json:"story_id,omitempty"`
	Limit       int    `json:"limit,omitempty"`
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
	req.Workspace = strings.TrimSpace(firstNonEmpty(req.Workspace, r.URL.Query().Get("workspace"), r.URL.Query().Get("workspace_id")))
	req.Action = strings.TrimSpace(req.Action)
	req.EpicID = strings.TrimSpace(req.EpicID)
	req.MilestoneID = strings.TrimSpace(req.MilestoneID)
	req.StoryID = strings.TrimSpace(req.StoryID)
	req.Actor = strings.TrimSpace(req.Actor)
	req.Sender = strings.TrimSpace(req.Sender)
	if req.Workspace == "" {
		httpError(w, http.StatusBadRequest, "workspace is required")
		return
	}
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

	messages, err := store.ListRoomMessages(r.Context(), req.Workspace, roomID, req.Limit)
	if err != nil {
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			httpError(w, http.StatusNotFound, "room not found")
			return
		}
		log.Error().Err(err).Str("room_id", roomID).Str("action", req.Action).Msg("failed to load room-agile messages")
		httpError(w, http.StatusInternalServerError, "failed to load room-agile messages")
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
		"workspace": req.Workspace,
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
