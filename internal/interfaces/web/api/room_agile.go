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

type RoomAgileResponse struct {
	Action    string            `json:"action"`
	RoomID    string            `json:"room_id"`
	Workspace string            `json:"workspace"`
	Result    RoomAgileEnvelope `json:"result"`
}

type RoomAgileEnvelope struct {
	Version int              `json:"version"`
	Status  string           `json:"status"`
	Command string           `json:"command"`
	Data    roomAgileResult  `json:"data"`
	Meta    RoomAgileMeta    `json:"meta"`
	Error   RoomAgileNoError `json:"error"`
}

type RoomAgileMeta struct {
	Source string `json:"source"`
}

type RoomAgileNoError struct{}

type roomAgileMessageView struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Subject          string `json:"subject"`
	Body             string `json:"body"`
	Sender           string `json:"sender"`
	Recipient        string `json:"recipient"`
	RelatedMessageID string `json:"related_message_id"`
	CreatedAt        string `json:"created_at"`
}

type roomAgileEpicView struct {
	roomAgileMessageView
	Title      string                `json:"title"`
	Status     string                `json:"status"`
	Finalized  bool                  `json:"finalized,omitempty"`
	FinalBrief *roomAgileMessageView `json:"final_brief,omitempty"`
	Closed     bool                  `json:"closed,omitempty"`
}

type roomAgileMilestoneView struct {
	roomAgileMessageView
	EpicID     string                `json:"epic_id"`
	Title      string                `json:"title"`
	Objective  string                `json:"objective"`
	Status     string                `json:"status"`
	Review     *roomAgileMessageView `json:"review,omitempty"`
	Summarized bool                  `json:"summarized,omitempty"`
}

type roomAgileStoryView struct {
	roomAgileMessageView
	MilestoneID     string                 `json:"milestone_id"`
	Title           string                 `json:"title"`
	Status          string                 `json:"status"`
	Validations     []roomAgileMessageView `json:"validations,omitempty"`
	ValidationCount int                    `json:"validation_count,omitempty"`
}

type roomAgileEpicStatus struct {
	EpicID           string `json:"epic_id"`
	EpicStatus       string `json:"epic_status"`
	MilestoneCount   int    `json:"milestone_count"`
	StoryCount       int    `json:"story_count"`
	OpenStoryCount   int    `json:"open_story_count"`
	ValidatedStories int    `json:"validated_stories"`
}

type roomAgileHealth struct {
	Status   string   `json:"status"`
	Warnings []string `json:"warnings"`
}

type roomAgileNextAction struct {
	Action  string `json:"action"`
	Reason  string `json:"reason,omitempty"`
	StoryID string `json:"story_id,omitempty"`
	Title   string `json:"title,omitempty"`
}

type roomAgileEpicListResult struct {
	RoomID string              `json:"room_id"`
	Epics  []roomAgileEpicView `json:"epics"`
	Count  int                 `json:"count"`
}

type roomAgileEpicResult struct {
	RoomID string             `json:"room_id"`
	Epic   *roomAgileEpicView `json:"epic"`
}

type roomAgileEpicPlanResult struct {
	RoomID     string                   `json:"room_id"`
	Epic       *roomAgileEpicView       `json:"epic"`
	Status     roomAgileEpicStatus      `json:"status"`
	Milestones []roomAgileMilestoneView `json:"milestones"`
	Stories    []roomAgileStoryView     `json:"stories"`
	Health     *roomAgileHealth         `json:"health,omitempty"`
	Next       []roomAgileNextAction    `json:"next,omitempty"`
}

type roomAgileMilestoneListResult struct {
	RoomID     string                   `json:"room_id"`
	Milestones []roomAgileMilestoneView `json:"milestones"`
	Count      int                      `json:"count"`
}

type roomAgileMilestoneResult struct {
	RoomID    string                  `json:"room_id"`
	Milestone *roomAgileMilestoneView `json:"milestone"`
	Stories   []roomAgileStoryView    `json:"stories"`
}

type roomAgileStoryListResult struct {
	RoomID  string               `json:"room_id"`
	Stories []roomAgileStoryView `json:"stories"`
	Count   int                  `json:"count"`
}

type roomAgileStoryResult struct {
	RoomID string              `json:"room_id"`
	Story  *roomAgileStoryView `json:"story"`
}

type roomAgileStoryStateResult struct {
	Message roomAgileMessageView   `json:"message"`
	Story   roomAgileStoryMutation `json:"story"`
}

type roomAgileStoryMutation struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Previous string `json:"previous"`
}

type roomAgileStoryValidationResult struct {
	Message roomAgileMessageView `json:"message"`
	StoryID string               `json:"story_id"`
}

type roomAgileStoryProposeResult struct {
	Message roomAgileMessageView `json:"message"`
}

type roomAgileStoryAcceptResult struct {
	Message    roomAgileMessageView `json:"message"`
	ProposalID string               `json:"proposal_id"`
}

type roomAgileResult interface {
	roomAgileResult()
}

func (roomAgileEpicListResult) roomAgileResult()        {}
func (roomAgileEpicResult) roomAgileResult()            {}
func (roomAgileEpicPlanResult) roomAgileResult()        {}
func (roomAgileMilestoneListResult) roomAgileResult()   {}
func (roomAgileMilestoneResult) roomAgileResult()       {}
func (roomAgileStoryListResult) roomAgileResult()       {}
func (roomAgileStoryResult) roomAgileResult()           {}
func (roomAgileStoryStateResult) roomAgileResult()      {}
func (roomAgileStoryValidationResult) roomAgileResult() {}
func (roomAgileStoryProposeResult) roomAgileResult()    {}
func (roomAgileStoryAcceptResult) roomAgileResult()     {}

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

	if isRoomAgileMutatingAction(req.Action) {
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
		writeJSON(w, http.StatusOK, roomAgileResponse(req.Action, roomID, workspace, result))
		return
	}

	result, err := buildRoomAgileReadResult(roomID, req, messages)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roomAgileResponse(req.Action, roomID, workspace, result))
}

func isRoomAgileMutatingAction(action string) bool {
	switch action {
	case "story_state", "story_validate", "story_propose", "story_accept":
		return true
	default:
		return false
	}
}

func roomAgileResponse(action, roomID, workspace string, data roomAgileResult) RoomAgileResponse {
	return RoomAgileResponse{
		Action:    action,
		RoomID:    roomID,
		Workspace: workspace,
		Result: RoomAgileEnvelope{
			Version: 1,
			Status:  "ok",
			Command: "foxctl.room.agile." + strings.ReplaceAll(action, "_", "."),
			Data:    data,
			Meta: RoomAgileMeta{
				Source: "web:/api/rooms/{room_id}/agile",
			},
			Error: RoomAgileNoError{},
		},
	}
}

func buildRoomAgileReadResult(roomID string, req RoomAgileRequest, messages []agent.BoardMessage) (roomAgileResult, error) {
	epics := apiRoomAgileEpics(messages)
	milestones := apiRoomAgileMilestones(messages)
	stories := apiRoomAgileStories(messages)

	switch req.Action {
	case "epic_show":
		if req.EpicID == "" {
			return roomAgileEpicListResult{RoomID: roomID, Epics: epics, Count: len(epics)}, nil
		}
		epic := findRoomAgileEpicByID(epics, req.EpicID)
		if epic == nil {
			return nil, fmt.Errorf("epic %q not found", req.EpicID)
		}
		return roomAgileEpicResult{RoomID: roomID, Epic: epic}, nil
	case "epic_resume", "epic_health", "epic_next":
		if req.EpicID == "" {
			return nil, fmt.Errorf("epic_id is required for %s", req.Action)
		}
		epic := findRoomAgileEpicByID(epics, req.EpicID)
		if epic == nil {
			return nil, fmt.Errorf("epic %q not found", req.EpicID)
		}
		relatedMilestones := filterMilestonesByEpicID(milestones, req.EpicID)
		relatedStories := filterStoriesByEpic(stories, relatedMilestones)
		status := apiRoomAgileEpicStatus(epic, relatedMilestones, relatedStories)
		data := roomAgileEpicPlanResult{
			RoomID:     roomID,
			Epic:       epic,
			Status:     status,
			Milestones: relatedMilestones,
			Stories:    relatedStories,
		}
		if req.Action == "epic_health" {
			health := apiRoomAgileHealth(status)
			data.Health = &health
		}
		if req.Action == "epic_next" {
			data.Next = apiRoomAgileNext(status, relatedMilestones, relatedStories)
		}
		return data, nil
	case "milestone_show":
		if req.MilestoneID == "" {
			return roomAgileMilestoneListResult{RoomID: roomID, Milestones: milestones, Count: len(milestones)}, nil
		}
		milestone := findRoomAgileMilestoneByID(milestones, req.MilestoneID)
		if milestone == nil {
			return nil, fmt.Errorf("milestone %q not found", req.MilestoneID)
		}
		return roomAgileMilestoneResult{RoomID: roomID, Milestone: milestone, Stories: filterStoriesByMilestoneID(stories, req.MilestoneID)}, nil
	case "story_show":
		if req.StoryID == "" {
			return roomAgileStoryListResult{RoomID: roomID, Stories: stories, Count: len(stories)}, nil
		}
		story := findRoomAgileStoryByID(stories, req.StoryID)
		if story == nil {
			return nil, fmt.Errorf("story %q not found", req.StoryID)
		}
		return roomAgileStoryResult{RoomID: roomID, Story: story}, nil
	default:
		return nil, fmt.Errorf("unsupported room-agile action %q", req.Action)
	}
}

func apiRoomAgileEpics(messages []agent.BoardMessage) []roomAgileEpicView {
	closed := map[string]bool{}
	finalized := map[string]roomAgileMessageView{}
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
	var out []roomAgileEpicView
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindEpic {
			continue
		}
		view := roomAgileEpicView{
			roomAgileMessageView: apiMessageView(msg),
			Title:                strings.TrimSpace(strings.TrimPrefix(msg.Subject, "Epic:")),
			Status:               "discovery",
		}
		if final, ok := finalized[msg.ID]; ok {
			view.Finalized = true
			view.FinalBrief = &final
			view.Status = "finalized"
		}
		if closed[msg.ID] {
			view.Closed = true
			view.Status = "closed"
		}
		out = append(out, view)
	}
	return out
}

func apiRoomAgileMilestones(messages []agent.BoardMessage) []roomAgileMilestoneView {
	summarized := map[string]bool{}
	reviewed := map[string]roomAgileMessageView{}
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
	var out []roomAgileMilestoneView
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindMilestone {
			continue
		}
		meta := parseLineMeta(msg.Body)
		view := roomAgileMilestoneView{
			roomAgileMessageView: apiMessageView(msg),
			EpicID:               firstNonEmpty(meta["EpicID"], msg.RelatedMessageID),
			Title:                strings.TrimSpace(strings.TrimPrefix(msg.Subject, "Milestone:")),
			Objective:            firstNonEmpty(meta["Objective"], meta["Goal"]),
			Status:               "open",
		}
		if review, ok := reviewed[msg.ID]; ok {
			view.Review = &review
		}
		if summarized[msg.ID] {
			view.Summarized = true
			view.Status = "summarized"
		}
		out = append(out, view)
	}
	return out
}

func apiRoomAgileStories(messages []agent.BoardMessage) []roomAgileStoryView {
	states := map[string]string{}
	validations := map[string][]roomAgileMessageView{}
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
	var out []roomAgileStoryView
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindStory {
			continue
		}
		view := roomAgileStoryView{
			roomAgileMessageView: apiMessageView(msg),
			MilestoneID:          msg.RelatedMessageID,
			Title:                strings.TrimSpace(strings.TrimPrefix(msg.Subject, "Story:")),
			Status:               firstNonEmpty(states[msg.ID], "accepted"),
		}
		if vals := validations[msg.ID]; len(vals) > 0 {
			view.Validations = vals
			view.ValidationCount = len(vals)
		}
		out = append(out, view)
	}
	return out
}

func apiMessageView(msg agent.BoardMessage) roomAgileMessageView {
	return roomAgileMessageView{
		ID:               msg.ID,
		Kind:             string(msg.Kind),
		Subject:          msg.Subject,
		Body:             msg.Body,
		Sender:           msg.Sender,
		Recipient:        msg.Recipient,
		RelatedMessageID: msg.RelatedMessageID,
		CreatedAt:        msg.CreatedAt.Format(time.RFC3339),
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

func findRoomAgileEpicByID(items []roomAgileEpicView, id string) *roomAgileEpicView {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func findRoomAgileMilestoneByID(items []roomAgileMilestoneView, id string) *roomAgileMilestoneView {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func findRoomAgileStoryByID(items []roomAgileStoryView, id string) *roomAgileStoryView {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

func filterMilestonesByEpicID(items []roomAgileMilestoneView, epicID string) []roomAgileMilestoneView {
	var out []roomAgileMilestoneView
	for _, item := range items {
		if item.EpicID == epicID {
			out = append(out, item)
		}
	}
	return out
}

func filterStoriesByMilestoneID(items []roomAgileStoryView, milestoneID string) []roomAgileStoryView {
	var out []roomAgileStoryView
	for _, item := range items {
		if item.MilestoneID == milestoneID {
			out = append(out, item)
		}
	}
	return out
}

func filterStoriesByEpic(stories []roomAgileStoryView, milestones []roomAgileMilestoneView) []roomAgileStoryView {
	milestoneIDs := map[string]struct{}{}
	for _, milestone := range milestones {
		if milestone.ID != "" {
			milestoneIDs[milestone.ID] = struct{}{}
		}
	}
	var out []roomAgileStoryView
	for _, story := range stories {
		if _, ok := milestoneIDs[story.MilestoneID]; ok {
			out = append(out, story)
		}
	}
	return out
}

func apiRoomAgileEpicStatus(epic *roomAgileEpicView, milestones []roomAgileMilestoneView, stories []roomAgileStoryView) roomAgileEpicStatus {
	openStories := 0
	validatedStories := 0
	for _, story := range stories {
		if story.Status != "done" && story.Status != "validated" && story.Status != "waived" {
			openStories++
		}
		if story.ValidationCount > 0 {
			validatedStories++
		}
	}
	return roomAgileEpicStatus{
		EpicID:           epic.ID,
		EpicStatus:       epic.Status,
		MilestoneCount:   len(milestones),
		StoryCount:       len(stories),
		OpenStoryCount:   openStories,
		ValidatedStories: validatedStories,
	}
}

func apiRoomAgileHealth(status roomAgileEpicStatus) roomAgileHealth {
	var warnings []string
	if status.EpicStatus == "discovery" {
		warnings = append(warnings, "epic_not_finalized")
	}
	if status.MilestoneCount == 0 {
		warnings = append(warnings, "no_milestones")
	}
	if status.StoryCount == 0 {
		warnings = append(warnings, "no_stories")
	}
	healthStatus := "needs_attention"
	if len(warnings) == 0 {
		healthStatus = "healthy"
	}
	return roomAgileHealth{
		Status:   healthStatus,
		Warnings: warnings,
	}
}

func apiRoomAgileNext(status roomAgileEpicStatus, milestones []roomAgileMilestoneView, stories []roomAgileStoryView) []roomAgileNextAction {
	if status.EpicStatus == "discovery" {
		return []roomAgileNextAction{{Action: "finalize_epic", Reason: "epic is still in discovery"}}
	}
	if len(milestones) == 0 {
		return []roomAgileNextAction{{Action: "start_milestone", Reason: "no milestones exist"}}
	}
	for _, story := range stories {
		if story.Status == "accepted" {
			return []roomAgileNextAction{{Action: "start_story", StoryID: story.ID, Title: story.Title}}
		}
	}
	return []roomAgileNextAction{{Action: "review_milestone", Reason: "no accepted stories are waiting to start"}}
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
// typed result in the standard envelope shape.
func buildRoomAgileMutatingResult(
	ctx context.Context,
	roomID string,
	workspace string,
	req RoomAgileRequest,
	messages []agent.BoardMessage,
	store blackboard.BoardStore,
) (roomAgileResult, error) {
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
) (roomAgileStoryStateResult, error) {
	if req.StoryID == "" {
		return roomAgileStoryStateResult{}, fmt.Errorf("story_id is required for story_state")
	}
	if req.State == "" {
		return roomAgileStoryStateResult{}, fmt.Errorf("state is required for story_state")
	}
	if err := validateStoryState(req.State); err != nil {
		return roomAgileStoryStateResult{}, err
	}

	stories := apiRoomAgileStories(messages)
	story := findRoomAgileStoryByID(stories, req.StoryID)
	if story == nil {
		return roomAgileStoryStateResult{}, fmt.Errorf("story %q not found", req.StoryID)
	}

	oldStatus := story.Status

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
		return roomAgileStoryStateResult{}, fmt.Errorf("failed to send story_state message: %w", err)
	}

	return roomAgileStoryStateResult{
		Message: apiMessageView(*msg),
		Story: roomAgileStoryMutation{
			ID:       story.ID,
			Title:    story.Title,
			Status:   req.State,
			Previous: oldStatus,
		},
	}, nil
}

func handleStoryValidate(
	ctx context.Context,
	roomID, workspace string,
	req RoomAgileRequest,
	messages []agent.BoardMessage,
	store blackboard.BoardStore,
) (roomAgileStoryValidationResult, error) {
	if req.StoryID == "" {
		return roomAgileStoryValidationResult{}, fmt.Errorf("story_id is required for story_validate")
	}
	if req.Verdict == "" {
		return roomAgileStoryValidationResult{}, fmt.Errorf("verdict is required for story_validate")
	}
	if req.ValidatorType == "" {
		return roomAgileStoryValidationResult{}, fmt.Errorf("validator_type is required for story_validate")
	}

	stories := apiRoomAgileStories(messages)
	story := findRoomAgileStoryByID(stories, req.StoryID)
	if story == nil {
		return roomAgileStoryValidationResult{}, fmt.Errorf("story %q not found", req.StoryID)
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
		return roomAgileStoryValidationResult{}, fmt.Errorf("failed to send story_validate message: %w", err)
	}

	return roomAgileStoryValidationResult{
		Message: apiMessageView(*msg),
		StoryID: req.StoryID,
	}, nil
}

func handleStoryPropose(
	ctx context.Context,
	roomID, workspace string,
	req RoomAgileRequest,
	store blackboard.BoardStore,
) (roomAgileStoryProposeResult, error) {
	if req.MilestoneID == "" {
		return roomAgileStoryProposeResult{}, fmt.Errorf("milestone_id is required for story_propose")
	}
	if req.Title == "" {
		return roomAgileStoryProposeResult{}, fmt.Errorf("title is required for story_propose")
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
		return roomAgileStoryProposeResult{}, fmt.Errorf("failed to send story_propose message: %w", err)
	}

	return roomAgileStoryProposeResult{Message: apiMessageView(*msg)}, nil
}

func handleStoryAccept(
	ctx context.Context,
	roomID, workspace string,
	req RoomAgileRequest,
	messages []agent.BoardMessage,
	store blackboard.BoardStore,
) (roomAgileStoryAcceptResult, error) {
	if req.ProposalID == "" {
		return roomAgileStoryAcceptResult{}, fmt.Errorf("proposal_id is required for story_accept")
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
		return roomAgileStoryAcceptResult{}, fmt.Errorf("proposal %q not found", req.ProposalID)
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
		return roomAgileStoryAcceptResult{}, fmt.Errorf("failed to send story_accept message: %w", err)
	}

	return roomAgileStoryAcceptResult{
		Message:    apiMessageView(*msg),
		ProposalID: req.ProposalID,
	}, nil
}
