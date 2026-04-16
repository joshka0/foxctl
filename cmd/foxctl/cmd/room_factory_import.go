package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/spf13/cobra"
)

type factoryMissionState struct {
	MissionID        string    `json:"missionId"`
	State            string    `json:"state"`
	WorkingDirectory string    `json:"workingDirectory"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type factoryFeatureEnvelope struct {
	Features []factoryFeature `json:"features"`
}

type factoryFeature struct {
	ID                string   `json:"id"`
	Description       string   `json:"description"`
	SkillName         string   `json:"skillName"`
	Preconditions     []string `json:"preconditions"`
	ExpectedBehavior  []string `json:"expectedBehavior"`
	VerificationSteps []string `json:"verificationSteps"`
	Fulfills          []string `json:"fulfills"`
	Milestone         string   `json:"milestone"`
	Status            string   `json:"status"`
	WorkerSessionIDs  []string `json:"workerSessionIds"`
}

type factoryValidationEnvelope struct {
	Assertions map[string]factoryAssertion `json:"assertions"`
}

type factoryAssertion struct {
	Status               string `json:"status"`
	ValidatedAtMilestone string `json:"validatedAtMilestone"`
}

type factoryProgressEvent struct {
	Index            int
	Timestamp        time.Time
	Type             string
	Message          string
	FeatureID        string
	Milestone        string
	WorkerSessionID  string
	SuccessState     string
	CommitID         string
	ExitCode         int
	ValidatorsPassed bool
	Handoff          factoryProgressHandoff
	Dismissals       []factoryDismissal
}

type factoryProgressHandoff struct {
	SalientSummary string `json:"salientSummary"`
}

type factoryDismissal struct {
	Type    string `json:"type"`
	Summary string `json:"summary"`
}

type factoryMissionImport struct {
	DirName          string
	DirPath          string
	Title            string
	Goal             string
	Outcome          string
	State            factoryMissionState
	Features         []factoryFeature
	Assertions       map[string]factoryAssertion
	Progress         []factoryProgressEvent
	BaseTime         time.Time
	WorkingDirectory string
}

type factoryMilestoneGroup struct {
	Key      string
	Features []factoryFeature
}

type factoryFeatureProgress struct {
	LatestEvent      *factoryProgressEvent
	LatestCompletion *factoryProgressEvent
}

type roomImportedMessage struct {
	Message agent.BoardMessage
}

func newRoomEpicImportFactoryCommand() *cobra.Command {
	var (
		workspace              string
		sender                 string
		missionDir             string
		includeProgressHistory bool
	)
	cmd := &cobra.Command{
		Use:   "import-factory <room-id>",
		Short: "Import one Factory mission as a canonical room-agile epic graph",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomEpicImportFactory(cmd, workspace, sender, args[0], missionDir, includeProgressHistory)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&missionDir, "mission-dir", "", "Path to one Factory mission directory")
	cmd.Flags().BoolVar(&includeProgressHistory, "include-progress-history", true, "Import high-signal progress log events into the epic delivery log")
	_ = cmd.MarkFlagRequired("mission-dir")
	return cmd
}

func runRoomEpicImportFactory(cmd *cobra.Command, workspace, sender, roomID, missionDir string, includeProgressHistory bool) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "foxctl.room.epic.import_factory", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.epic.import_factory", protocol.ErrorCodeEARG, "factory import requires coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	loaded, err := loadFactoryMissionImport(missionDir)
	if err != nil {
		code := protocol.ErrorCodeEARG
		if os.IsNotExist(err) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.epic.import_factory", code, err.Error(), map[string]any{
			"hint": "Pass --mission-dir pointing at one concrete directory under ~/.factory/missions.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	imported, skipped, epicID, err := importFactoryMissionIntoRoom(cmd, store, absWorkspace, roomID, identity.Sender, loaded, includeProgressHistory)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.epic.import_factory", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, epicID); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.epic.import_factory", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	messages, err := store.ListRoomMessages(cmd.Context(), absWorkspace, roomID, roomTaskScanLimit)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.epic.import_factory", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	epic := roomEpicViewByID(buildRoomEpicViews(messages), epicID)
	workpack := map[string]any{"root": roomAgileWorkpackRootPath(epicID)}
	if epic != nil {
		workpack = buildRoomAgileWorkpackInfo(epic)
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.room.epic.import_factory", map[string]any{
		"room_id":     roomID,
		"epic_id":     epicID,
		"actor":       identity.Sender,
		"source":      loaded.DirPath,
		"imported":    imported,
		"skipped":     skipped,
		"workpack":    workpack,
		"epic":        epic,
		"workspace":   absWorkspace,
		"mission_dir": loaded.DirPath,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func importFactoryMissionIntoRoom(cmd *cobra.Command, store blackboard.BoardStore, workspaceID, roomID, sender string, mission factoryMissionImport, includeProgressHistory bool) (int, int, string, error) {
	specs := buildFactoryMissionImportMessages(workspaceID, roomID, sender, mission, includeProgressHistory)
	if len(specs) == 0 {
		return 0, 0, "", fmt.Errorf("factory import produced no messages for %s", mission.DirPath)
	}
	epicID := specs[0].Message.ID

	existing, err := store.ListRoomMessages(cmd.Context(), workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return 0, 0, "", err
	}
	existingByID := make(map[string]agent.BoardMessage, len(existing))
	for _, msg := range existing {
		existingByID[msg.ID] = msg
	}

	imported := 0
	skipped := 0
	for _, spec := range specs {
		if current, ok := existingByID[spec.Message.ID]; ok {
			if roomImportedMessageEqual(current, spec.Message) {
				skipped++
				continue
			}
			return 0, 0, "", fmt.Errorf("factory import drift for message %q in room %q", spec.Message.ID, roomID)
		}
		msg := spec.Message
		if err := store.SendMessage(cmd.Context(), &msg); err != nil {
			return 0, 0, "", err
		}
		imported++
	}
	return imported, skipped, epicID, nil
}

func roomImportedMessageEqual(existing, want agent.BoardMessage) bool {
	return existing.ID == want.ID &&
		existing.WorkspaceID == want.WorkspaceID &&
		existing.TaskID == want.TaskID &&
		existing.RelatedMessageID == want.RelatedMessageID &&
		existing.Stream == want.Stream &&
		existing.Sender == want.Sender &&
		existing.Recipient == want.Recipient &&
		existing.Kind == want.Kind &&
		existing.Priority == want.Priority &&
		existing.AckRequired == want.AckRequired &&
		existing.ReplyExpected == want.ReplyExpected &&
		existing.Interrupt == want.Interrupt &&
		existing.Status == want.Status &&
		existing.Subject == want.Subject &&
		existing.Body == want.Body &&
		existing.CreatedAt.Unix() == want.CreatedAt.Unix()
}

func loadFactoryMissionImport(missionDir string) (factoryMissionImport, error) {
	dir := strings.TrimSpace(expandHomePath(missionDir))
	if dir == "" {
		return factoryMissionImport{}, fmt.Errorf("mission-dir is required")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return factoryMissionImport{}, fmt.Errorf("resolve mission-dir %q: %w", missionDir, err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return factoryMissionImport{}, err
	}
	if !info.IsDir() {
		return factoryMissionImport{}, fmt.Errorf("mission-dir %q is not a directory", absDir)
	}

	md, err := os.ReadFile(filepath.Join(absDir, "mission.md"))
	if err != nil {
		return factoryMissionImport{}, err
	}
	title, goal, outcome := parseFactoryMissionMarkdown(string(md))
	if title == "" {
		return factoryMissionImport{}, fmt.Errorf("mission.md in %q does not contain a mission title", absDir)
	}

	state := factoryMissionState{}
	statePath := filepath.Join(absDir, "state.json")
	if _, err := os.Stat(statePath); err == nil {
		if err := decodeFactoryJSONFile(statePath, &state); err != nil {
			return factoryMissionImport{}, fmt.Errorf("decode %s: %w", statePath, err)
		}
	} else if !os.IsNotExist(err) {
		return factoryMissionImport{}, err
	}

	featuresEnvelope := factoryFeatureEnvelope{}
	featuresPath := filepath.Join(absDir, "features.json")
	if err := decodeFactoryJSONFile(featuresPath, &featuresEnvelope); err != nil {
		return factoryMissionImport{}, fmt.Errorf("decode %s: %w", featuresPath, err)
	}

	validationEnvelope := factoryValidationEnvelope{}
	validationPath := filepath.Join(absDir, "validation-state.json")
	if _, err := os.Stat(validationPath); err == nil {
		if err := decodeFactoryJSONFile(validationPath, &validationEnvelope); err != nil {
			return factoryMissionImport{}, fmt.Errorf("decode %s: %w", validationPath, err)
		}
	} else if !os.IsNotExist(err) {
		return factoryMissionImport{}, err
	}

	progress, err := loadFactoryProgressEvents(filepath.Join(absDir, "progress_log.jsonl"))
	if err != nil {
		return factoryMissionImport{}, err
	}

	workingDirectory := strings.TrimSpace(state.WorkingDirectory)
	if workingDirectory == "" {
		if raw, err := os.ReadFile(filepath.Join(absDir, "working_directory.txt")); err == nil {
			workingDirectory = strings.TrimSpace(string(raw))
		}
	}

	baseTime := firstNonZeroTime(state.CreatedAt, firstFactoryProgressTimestamp(progress), info.ModTime().UTC())
	baseTime = baseTime.UTC().Truncate(time.Second)

	return factoryMissionImport{
		DirName:          filepath.Base(absDir),
		DirPath:          absDir,
		Title:            title,
		Goal:             goal,
		Outcome:          outcome,
		State:            state,
		Features:         featuresEnvelope.Features,
		Assertions:       validationEnvelope.Assertions,
		Progress:         progress,
		BaseTime:         baseTime,
		WorkingDirectory: workingDirectory,
	}, nil
}

func decodeFactoryJSONFile(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return err
	}
	return nil
}

func loadFactoryProgressEvents(path string) ([]factoryProgressEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	events := make([]factoryProgressEvent, 0, len(lines))
	for idx, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		raw := map[string]any{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("decode %s line %d: %w", path, idx+1, err)
		}
		event, err := parseFactoryProgressEvent(raw, idx)
		if err != nil {
			return nil, fmt.Errorf("parse %s line %d: %w", path, idx+1, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func parseFactoryProgressEvent(raw map[string]any, index int) (factoryProgressEvent, error) {
	event := factoryProgressEvent{
		Index:            index,
		Type:             strings.TrimSpace(anyString(raw["type"])),
		Message:          strings.TrimSpace(firstNonEmpty(anyString(raw["message"]), anyString(raw["reason"]))),
		FeatureID:        strings.TrimSpace(anyString(raw["featureId"])),
		Milestone:        strings.TrimSpace(anyString(raw["milestone"])),
		WorkerSessionID:  strings.TrimSpace(anyString(raw["workerSessionId"])),
		SuccessState:     strings.TrimSpace(anyString(raw["successState"])),
		CommitID:         strings.TrimSpace(anyString(raw["commitId"])),
		ExitCode:         anyInt(raw["exitCode"]),
		ValidatorsPassed: anyBool(raw["validatorsPassed"]),
	}
	ts := strings.TrimSpace(anyString(raw["timestamp"]))
	if ts != "" {
		parsed, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return factoryProgressEvent{}, err
		}
		event.Timestamp = parsed.UTC().Truncate(time.Second)
	}
	if handoffRaw, ok := raw["handoff"].(map[string]any); ok {
		event.Handoff = factoryProgressHandoff{
			SalientSummary: strings.TrimSpace(anyString(handoffRaw["salientSummary"])),
		}
	}
	if dismissalsRaw, ok := raw["dismissals"].([]any); ok {
		event.Dismissals = make([]factoryDismissal, 0, len(dismissalsRaw))
		for _, item := range dismissalsRaw {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			event.Dismissals = append(event.Dismissals, factoryDismissal{
				Type:    strings.TrimSpace(anyString(entry["type"])),
				Summary: strings.TrimSpace(anyString(entry["summary"])),
			})
		}
	}
	return event, nil
}

func buildFactoryMissionImportMessages(workspaceID, roomID, sender string, mission factoryMissionImport, includeProgressHistory bool) []roomImportedMessage {
	stream := agent.RoomStreamName(roomID)
	scope := make([]string, 0)
	for _, milestone := range groupFactoryFeaturesByMilestone(mission.Features) {
		scope = append(scope, milestone.Key)
	}
	success := []string{buildFactoryAssertionCoverageSummary(mission.Assertions)}
	epicID := factoryEpicImportID(mission.DirName)
	epicBody := buildRoomEpicBody(
		mission.Title,
		mission.Goal,
		sender,
		mission.Outcome,
		factoryEpicHorizon(mission),
		scope,
		success,
		"factory_import",
		false,
	)
	specs := []roomImportedMessage{
		{Message: agent.BoardMessage{
			ID:          epicID,
			WorkspaceID: workspaceID,
			Stream:      stream,
			Sender:      sender,
			Recipient:   agent.BroadcastRecipient,
			Kind:        agent.BoardMessageKindEpic,
			Priority:    agent.DefaultPriority,
			Status:      agent.BoardMessageStatusUnread,
			Subject:     "Epic: " + mission.Title,
			Body:        epicBody,
			CreatedAt:   mission.BaseTime,
		}},
		{Message: agent.BoardMessage{
			ID:               epicID + "-finalize",
			WorkspaceID:      workspaceID,
			RelatedMessageID: epicID,
			Stream:           stream,
			Sender:           sender,
			Recipient:        agent.BroadcastRecipient,
			Kind:             agent.BoardMessageKindEpicFinalize,
			Priority:         agent.DefaultPriority,
			Status:           agent.BoardMessageStatusUnread,
			Subject:          "Epic Finalized: " + mission.Title,
			Body:             buildFactoryEpicFinalizeSummary(mission),
			CreatedAt:        mission.BaseTime.Add(time.Second),
		}},
	}

	featureProgress := indexFactoryFeatureProgress(mission.Progress)
	milestones := groupFactoryFeaturesByMilestone(mission.Features)
	nextMilestoneOffset := int64(2)
	nextStoryOffset := int64(100)
	for _, milestone := range milestones {
		milestoneID := factoryMilestoneImportID(mission.DirName, milestone.Key)
		specs = append(specs, roomImportedMessage{Message: agent.BoardMessage{
			ID:               milestoneID,
			WorkspaceID:      workspaceID,
			RelatedMessageID: epicID,
			Stream:           stream,
			Sender:           sender,
			Recipient:        agent.BroadcastRecipient,
			Kind:             agent.BoardMessageKindMilestone,
			Priority:         agent.DefaultPriority,
			Status:           agent.BoardMessageStatusUnread,
			Subject:          "Milestone: " + factoryMilestoneTitle(milestone.Key),
			Body: buildRoomMilestoneBody(
				epicID,
				factoryMilestoneTitle(milestone.Key),
				fmt.Sprintf("Imported Factory milestone %s", milestone.Key),
				buildFactoryMilestoneObjective(milestone),
				sender,
				factoryMilestoneScope(milestone.Features),
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				"",
				"",
			),
			CreatedAt: mission.BaseTime.Add(time.Duration(nextMilestoneOffset) * time.Second),
		}})
		nextMilestoneOffset++

		for _, feature := range milestone.Features {
			storyID := factoryStoryImportID(mission.DirName, feature.ID)
			specs = append(specs, roomImportedMessage{Message: agent.BoardMessage{
				ID:               storyID,
				WorkspaceID:      workspaceID,
				RelatedMessageID: milestoneID,
				Stream:           stream,
				Sender:           sender,
				Recipient:        agent.BroadcastRecipient,
				Kind:             agent.BoardMessageKindStory,
				Priority:         agent.DefaultPriority,
				Status:           agent.BoardMessageStatusUnread,
				Subject:          "Story: " + factoryStoryTitle(feature.ID),
				Body:             buildRoomStoryBody("", buildFactoryStoryDescription(feature)),
				CreatedAt:        firstNonZeroTime(factoryFeatureCreatedAt(featureProgress[feature.ID]), mission.BaseTime.Add(time.Duration(nextStoryOffset)*time.Second)),
			}})
			nextStoryOffset++

			validation := deriveFactoryStoryValidation(mission, feature, storyID, milestoneID, epicID, sender, workspaceID, stream, featureProgress[feature.ID])
			if validation != nil {
				specs = append(specs, roomImportedMessage{Message: *validation})
			}
			if state := deriveFactoryStoryState(mission, feature, storyID, stream, workspaceID, sender, featureProgress[feature.ID], validation); state != nil {
				specs = append(specs, roomImportedMessage{Message: *state})
			}
		}
	}

	if includeProgressHistory {
		specs = append(specs, buildFactoryDeliveryLogMessages(workspaceID, stream, sender, epicID, mission.Progress)...)
	}
	return specs
}

func deriveFactoryStoryValidation(mission factoryMissionImport, feature factoryFeature, storyID, milestoneID, epicID, sender, workspaceID, stream string, progress factoryFeatureProgress) *agent.BoardMessage {
	payload, ok := buildFactoryValidationPayload(mission, feature, progress)
	if !ok {
		return nil
	}
	msg := &agent.BoardMessage{
		ID:               factoryStoryValidationImportID(mission.DirName, feature.ID),
		WorkspaceID:      workspaceID,
		RelatedMessageID: storyID,
		Stream:           stream,
		Sender:           sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStoryValidation,
		Priority:         agent.DefaultPriority,
		Status:           agent.BoardMessageStatusUnread,
		Subject:          fmt.Sprintf("Story Validation (%s/%s): %s", payload.ValidatorType, payload.Status, factoryStoryTitle(feature.ID)),
		Body:             buildRoomStoryValidationBody(epicID, milestoneID, storyID, payload.ValidatorType, payload.Status, payload.Summary, payload.ArtifactPath, payload.ArtifactDigest, payload.CommandText, payload.Notes, nil),
		CreatedAt:        payload.CreatedAt,
	}
	return msg
}

func deriveFactoryStoryState(mission factoryMissionImport, feature factoryFeature, storyID, stream, workspaceID, sender string, progress factoryFeatureProgress, validation *agent.BoardMessage) *agent.BoardMessage {
	state, reason, ok := buildFactoryStoryState(feature, progress, validation)
	if !ok {
		return nil
	}
	createdAt := mission.BaseTime.Add(2 * time.Second)
	if validation != nil {
		createdAt = validation.CreatedAt.Add(time.Second)
	}
	msg := &agent.BoardMessage{
		ID:               factoryStoryStateImportID(mission.DirName, feature.ID),
		WorkspaceID:      workspaceID,
		RelatedMessageID: storyID,
		Stream:           stream,
		Sender:           sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStoryState,
		Priority:         agent.DefaultPriority,
		Status:           agent.BoardMessageStatusUnread,
		Subject:          fmt.Sprintf("Story State (%s): %s", state, factoryStoryTitle(feature.ID)),
		Body:             buildRoomStoryStateBody(state, reason, "", ""),
		CreatedAt:        createdAt,
	}
	return msg
}

type factoryValidationImportPayload struct {
	ValidatorType  string
	Status         string
	Summary        string
	Notes          string
	CommandText    string
	ArtifactPath   string
	ArtifactDigest string
	CreatedAt      time.Time
}

func buildFactoryValidationPayload(mission factoryMissionImport, feature factoryFeature, progress factoryFeatureProgress) (factoryValidationImportPayload, bool) {
	passed := 0
	failed := 0
	pending := 0
	linked := make([]string, 0, len(feature.Fulfills))
	for _, assertionID := range feature.Fulfills {
		assertionID = strings.TrimSpace(assertionID)
		if assertionID == "" {
			continue
		}
		linked = append(linked, assertionID)
		switch strings.ToLower(strings.TrimSpace(mission.Assertions[assertionID].Status)) {
		case "passed":
			passed++
		case "failed":
			failed++
		case "pending":
			pending++
		}
	}

	createdAt := mission.BaseTime.Add(2 * time.Second)
	if progress.LatestCompletion != nil && !progress.LatestCompletion.Timestamp.IsZero() {
		createdAt = progress.LatestCompletion.Timestamp
	} else if progress.LatestEvent != nil && !progress.LatestEvent.Timestamp.IsZero() {
		createdAt = progress.LatestEvent.Timestamp
	}
	artifact := loadFactoryHandoffArtifact(mission, progress.LatestCompletion)
	validatorType := deriveFactoryValidatorType(feature)

	if len(linked) > 0 {
		status := "pass"
		switch {
		case failed > 0:
			status = "fail"
		case pending > 0:
			status = "blocked"
		}
		summary := fmt.Sprintf("Imported Factory assertions for %s: %d passed, %d failed, %d pending.", feature.ID, passed, failed, pending)
		noteParts := []string{
			fmt.Sprintf("Source mission=%s", mission.DirName),
			fmt.Sprintf("assertions=%s", strings.Join(linked, ", ")),
		}
		if progress.LatestCompletion != nil {
			if progress.LatestCompletion.CommitID != "" {
				noteParts = append(noteParts, "commit="+progress.LatestCompletion.CommitID)
			}
			if progress.LatestCompletion.SuccessState != "" {
				noteParts = append(noteParts, "worker_success_state="+progress.LatestCompletion.SuccessState)
			}
		}
		noteParts = append(noteParts, buildFactoryArtifactNoteParts(artifact)...)
		return factoryValidationImportPayload{
			ValidatorType:  validatorType,
			Status:         status,
			Summary:        summary,
			Notes:          strings.Join(cleanRoomItems(noteParts), "; "),
			CommandText:    artifact.CommandText,
			ArtifactPath:   artifact.Path,
			ArtifactDigest: artifact.Digest,
			CreatedAt:      createdAt,
		}, true
	}

	if progress.LatestCompletion == nil {
		return factoryValidationImportPayload{}, false
	}

	status := "blocked"
	switch {
	case progress.LatestCompletion.ExitCode != 0:
		status = "fail"
	case strings.EqualFold(progress.LatestCompletion.SuccessState, "success") && progress.LatestCompletion.ValidatorsPassed:
		status = "pass"
	}
	summary := fmt.Sprintf("Imported Factory worker result for %s: success_state=%s, exit_code=%d.", feature.ID, firstNonEmpty(progress.LatestCompletion.SuccessState, "unknown"), progress.LatestCompletion.ExitCode)
	noteParts := []string{fmt.Sprintf("Source mission=%s", mission.DirName)}
	if progress.LatestCompletion.CommitID != "" {
		noteParts = append(noteParts, "commit="+progress.LatestCompletion.CommitID)
	}
	noteParts = append(noteParts, buildFactoryArtifactNoteParts(artifact)...)
	return factoryValidationImportPayload{
		ValidatorType:  validatorType,
		Status:         status,
		Summary:        summary,
		Notes:          strings.Join(cleanRoomItems(noteParts), "; "),
		CommandText:    artifact.CommandText,
		ArtifactPath:   artifact.Path,
		ArtifactDigest: artifact.Digest,
		CreatedAt:      createdAt,
	}, true
}

func buildFactoryStoryState(feature factoryFeature, progress factoryFeatureProgress, validation *agent.BoardMessage) (string, string, bool) {
	status := strings.ToLower(strings.TrimSpace(feature.Status))
	switch status {
	case "pending":
		return buildFactoryPendingStoryState(progress, validation)
	case "in_progress":
		return "in_progress", "Imported Factory feature is currently in progress.", true
	case "completed":
		if validation == nil {
			return "in_review", "Imported Factory feature is complete but has no imported validation evidence.", true
		}
		meta := parseRoomStoryValidationBody(validation.Body)
		switch meta.Status {
		case "pass", "waived":
			return "done", "Imported Factory feature is complete and validated.", true
		case "fail":
			return "blocked", "Imported Factory feature is complete but imported validation failed.", true
		case "blocked":
			return "in_review", "Imported Factory feature is complete but still has pending imported validation work.", true
		}
	}
	if status == "" {
		return "accepted", "Imported Factory feature has no explicit source status.", true
	}
	return "accepted", fmt.Sprintf("Imported Factory feature has source status %q.", status), true
}

func buildFactoryPendingStoryState(progress factoryFeatureProgress, validation *agent.BoardMessage) (string, string, bool) {
	if progress.LatestCompletion != nil {
		if isFactoryWorkerCompletionSuccessful(progress.LatestCompletion) {
			if validation == nil {
				return "in_review", "Imported Factory feature has a successful worker completion, but the source feature status is still pending.", true
			}
			meta := parseRoomStoryValidationBody(validation.Body)
			switch meta.Status {
			case "pass", "waived":
				return "validated", "Imported Factory feature has successful worker completion and imported validation evidence, but the source feature status is still pending.", true
			case "fail":
				return "blocked", "Imported Factory feature has successful worker completion, but imported validation failed.", true
			case "blocked":
				return "blocked", "Imported Factory feature has a worker completion attempt, but imported validation remains blocked.", true
			}
			return "in_review", "Imported Factory feature has a worker completion attempt awaiting imported validation review.", true
		}
		return "blocked", buildFactoryWorkerFailureReason(progress.LatestCompletion), true
	}
	if progress.LatestEvent != nil {
		switch progress.LatestEvent.Type {
		case "worker_started", "worker_selected_feature", "worker_paused":
			return "in_progress", "Imported Factory feature has worker activity, but the source feature status is still pending.", true
		case "worker_failed":
			return "blocked", buildFactoryWorkerFailureReason(progress.LatestEvent), true
		}
	}
	return "accepted", "Imported Factory feature is pending and not yet started.", true
}

func isFactoryWorkerCompletionSuccessful(event *factoryProgressEvent) bool {
	if event == nil {
		return false
	}
	if event.ExitCode != 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(event.SuccessState), "success")
}

func buildFactoryWorkerFailureReason(event *factoryProgressEvent) string {
	if event == nil {
		return "Imported Factory worker attempt was unsuccessful."
	}
	if summary := strings.TrimSpace(event.Handoff.SalientSummary); summary != "" {
		return summary
	}
	if msg := strings.TrimSpace(event.Message); msg != "" {
		return msg
	}
	if state := strings.TrimSpace(event.SuccessState); state != "" {
		return fmt.Sprintf("Imported Factory worker attempt ended with success_state=%s.", state)
	}
	if event.ExitCode != 0 {
		return fmt.Sprintf("Imported Factory worker attempt exited with code %d.", event.ExitCode)
	}
	return "Imported Factory worker attempt was unsuccessful."
}

type factoryHandoffArtifact struct {
	Path        string
	Digest      string
	Summary     string
	LeftUndone  string
	CommandText string
	Issues      []string
}

func deriveFactoryValidatorType(feature factoryFeature) string {
	id := strings.ToLower(strings.TrimSpace(feature.ID))
	skill := strings.ToLower(strings.TrimSpace(feature.SkillName))
	switch {
	case strings.HasPrefix(id, "user-testing-validator-") || strings.Contains(skill, "user-testing-validator"):
		return "user_test"
	case strings.HasPrefix(id, "scrutiny-validator-") || strings.Contains(skill, "scrutiny-validator") || strings.Contains(skill, "review-worker"):
		return "review"
	case strings.Contains(skill, "test-worker"):
		return "test"
	default:
		return "audit"
	}
}

func buildFactoryArtifactNoteParts(artifact factoryHandoffArtifact) []string {
	parts := make([]string, 0, 3+len(artifact.Issues))
	if artifact.Summary != "" {
		parts = append(parts, "handoff_summary="+artifact.Summary)
	}
	if artifact.LeftUndone != "" {
		parts = append(parts, "left_undone="+artifact.LeftUndone)
	}
	for _, issue := range artifact.Issues {
		if issue = strings.TrimSpace(issue); issue != "" {
			parts = append(parts, "issue="+issue)
		}
	}
	return parts
}

func loadFactoryHandoffArtifact(mission factoryMissionImport, event *factoryProgressEvent) factoryHandoffArtifact {
	path := findFactoryHandoffArtifactPath(mission.DirPath, event)
	if path == "" {
		return factoryHandoffArtifact{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return factoryHandoffArtifact{Path: path}
	}
	sum := sha256.Sum256(data)
	artifact := factoryHandoffArtifact{
		Path:   path,
		Digest: "sha256:" + hex.EncodeToString(sum[:]),
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return artifact
	}
	handoff, _ := raw["handoff"].(map[string]any)
	artifact.Summary = strings.TrimSpace(anyString(handoff["salientSummary"]))
	artifact.LeftUndone = strings.TrimSpace(anyString(handoff["whatWasLeftUndone"]))
	if verification, ok := handoff["verification"].(map[string]any); ok {
		if commands, ok := verification["commandsRun"].([]any); ok {
			commandList := make([]string, 0, len(commands))
			for _, item := range commands {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if command := strings.TrimSpace(anyString(entry["command"])); command != "" {
					commandList = append(commandList, command)
				}
			}
			artifact.CommandText = compactFactoryCommands(commandList, 3)
		}
	}
	if discovered, ok := handoff["discoveredIssues"].([]any); ok {
		for _, item := range discovered {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			description := strings.TrimSpace(anyString(entry["description"]))
			if description != "" {
				artifact.Issues = append(artifact.Issues, description)
			}
		}
	}
	return artifact
}

func findFactoryHandoffArtifactPath(missionDir string, event *factoryProgressEvent) string {
	if event == nil || missionDir == "" {
		return ""
	}
	handoffDir := filepath.Join(missionDir, "handoffs")
	featureID := sanitizeFactoryHandoffToken(event.FeatureID)
	workerID := sanitizeFactoryHandoffToken(event.WorkerSessionID)
	patterns := make([]string, 0, 2)
	if featureID != "" && workerID != "" {
		patterns = append(patterns, filepath.Join(handoffDir, "*__"+featureID+"__"+workerID+".json"))
	}
	if featureID != "" {
		patterns = append(patterns, filepath.Join(handoffDir, "*__"+featureID+"__*.json"))
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		if len(matches) == 0 {
			continue
		}
		sort.Strings(matches)
		return matches[len(matches)-1]
	}
	return ""
}

func sanitizeFactoryHandoffToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, string(filepath.Separator), "-")
}

func compactFactoryCommands(commands []string, limit int) string {
	commands = cleanRoomItems(commands)
	if len(commands) == 0 {
		return ""
	}
	if limit <= 0 || len(commands) <= limit {
		return strings.Join(commands, " ; ")
	}
	return strings.Join(commands[:limit], " ; ") + " ; ..."
}

func buildFactoryDeliveryLogMessages(workspaceID, stream, sender, epicID string, events []factoryProgressEvent) []roomImportedMessage {
	out := make([]roomImportedMessage, 0)
	for _, event := range events {
		if !factoryProgressEventIsImported(event.Type) {
			continue
		}
		label, completed, nextFocus, notes := buildFactoryDeliveryLogPayload(event)
		out = append(out, roomImportedMessage{Message: agent.BoardMessage{
			ID:               factoryDeliveryLogImportID(epicID, event.Index),
			WorkspaceID:      workspaceID,
			RelatedMessageID: epicID,
			Stream:           stream,
			Sender:           sender,
			Recipient:        agent.BroadcastRecipient,
			Kind:             agent.BoardMessageKindDeliveryLog,
			Priority:         agent.DefaultPriority,
			Status:           agent.BoardMessageStatusUnread,
			Subject:          "Delivery Log: " + label,
			Body:             buildRoomDeliveryLogBody(label, completed, nil, nil, nextFocus, notes),
			CreatedAt:        event.Timestamp,
		}})
	}
	return out
}

func buildFactoryDeliveryLogPayload(event factoryProgressEvent) (string, []string, []string, string) {
	switch event.Type {
	case "mission_accepted":
		return "Mission accepted", nil, nil, "Imported Factory mission was accepted."
	case "mission_run_started":
		return "Mission run started", nil, nil, firstNonEmpty(event.Message, "Imported Factory mission run started.")
	case "worker_completed":
		notes := firstNonEmpty(event.Handoff.SalientSummary, event.Message, "Imported Factory worker completion.")
		if event.CommitID != "" {
			notes += " Commit: " + event.CommitID + "."
		}
		if isFactoryWorkerCompletionSuccessful(&event) {
			label := "Worker completed"
			if event.FeatureID != "" {
				label = fmt.Sprintf("Feature %s completed", event.FeatureID)
			}
			completed := []string{}
			if event.FeatureID != "" {
				completed = append(completed, event.FeatureID)
			}
			return label, completed, nil, notes
		}
		label := "Worker completion reported issues"
		if event.FeatureID != "" {
			label = fmt.Sprintf("Feature %s worker completion reported issues", event.FeatureID)
		}
		next := []string{}
		if event.FeatureID != "" {
			next = append(next, event.FeatureID)
		}
		return label, nil, next, notes
	case "worker_failed":
		label := "Worker failed"
		if event.FeatureID != "" {
			label = fmt.Sprintf("Feature %s worker failed", event.FeatureID)
		}
		next := []string{}
		if event.FeatureID != "" {
			next = append(next, event.FeatureID)
		}
		return label, nil, next, firstNonEmpty(event.Message, "Imported Factory worker failed.")
	case "handoff_items_dismissed":
		dismissed := make([]string, 0, len(event.Dismissals))
		for _, item := range event.Dismissals {
			summary := strings.TrimSpace(item.Summary)
			if summary == "" {
				continue
			}
			dismissed = append(dismissed, summary)
		}
		return "Handoff items dismissed", dismissed, nil, fmt.Sprintf("Imported %d handoff dismissal(s).", len(dismissed))
	case "mission_paused":
		return "Mission paused", nil, nil, "Imported Factory mission is paused."
	case "mission_resumed":
		return "Mission resumed", nil, nil, "Imported Factory mission resumed."
	case "milestone_validation_triggered":
		label := "Milestone validation triggered"
		if event.Milestone != "" {
			label = fmt.Sprintf("Milestone %s validation triggered", event.Milestone)
		}
		next := []string{}
		if event.FeatureID != "" {
			next = append(next, event.FeatureID)
		}
		return label, nil, next, "Imported Factory milestone validation trigger."
	default:
		return strings.ReplaceAll(event.Type, "_", " "), nil, nil, firstNonEmpty(event.Message, "Imported Factory progress event.")
	}
}

func factoryProgressEventIsImported(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "mission_accepted", "mission_run_started", "worker_completed", "worker_failed", "handoff_items_dismissed", "mission_paused", "mission_resumed", "milestone_validation_triggered":
		return true
	default:
		return false
	}
}

func indexFactoryFeatureProgress(events []factoryProgressEvent) map[string]factoryFeatureProgress {
	out := make(map[string]factoryFeatureProgress)
	for i := range events {
		event := events[i]
		featureID := strings.TrimSpace(event.FeatureID)
		if featureID == "" {
			continue
		}
		current := out[featureID]
		if current.LatestEvent == nil || current.LatestEvent.Timestamp.Before(event.Timestamp) {
			ev := event
			current.LatestEvent = &ev
		}
		if event.Type == "worker_completed" {
			if current.LatestCompletion == nil || current.LatestCompletion.Timestamp.Before(event.Timestamp) {
				ev := event
				current.LatestCompletion = &ev
			}
		}
		out[featureID] = current
	}
	return out
}

func groupFactoryFeaturesByMilestone(features []factoryFeature) []factoryMilestoneGroup {
	groups := make([]factoryMilestoneGroup, 0)
	indexByKey := make(map[string]int)
	for _, feature := range features {
		key := strings.TrimSpace(feature.Milestone)
		if key == "" {
			key = "imported"
		}
		if idx, ok := indexByKey[key]; ok {
			groups[idx].Features = append(groups[idx].Features, feature)
			continue
		}
		indexByKey[key] = len(groups)
		groups = append(groups, factoryMilestoneGroup{Key: key, Features: []factoryFeature{feature}})
	}
	return groups
}

func parseFactoryMissionMarkdown(markdown string) (string, string, string) {
	lines := strings.Split(markdown, "\n")
	title := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			if title != "" {
				break
			}
		}
	}
	return title, firstMarkdownSectionParagraph(lines, "Plan Overview"), firstMarkdownSectionParagraph(lines, "Expected Functionality")
}

func firstMarkdownSectionParagraph(lines []string, heading string) string {
	inSection := false
	paragraph := make([]string, 0)
	target := "## " + strings.TrimSpace(heading)
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}
			inSection = line == target
			continue
		}
		if !inSection {
			continue
		}
		if line == "" {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		paragraph = append(paragraph, line)
	}
	return strings.Join(paragraph, " ")
}

func factoryEpicImportID(dirName string) string {
	return "factory-mission-" + sanitizeFactoryImportIDPart(dirName, "mission")
}

func factoryMilestoneImportID(dirName, milestone string) string {
	return fmt.Sprintf("factory-mission-%s-milestone-%s", sanitizeFactoryImportIDPart(dirName, "mission"), sanitizeFactoryImportIDPart(milestone, "milestone"))
}

func factoryStoryImportID(dirName, featureID string) string {
	return fmt.Sprintf("factory-mission-%s-story-%s", sanitizeFactoryImportIDPart(dirName, "mission"), sanitizeFactoryImportIDPart(featureID, "story"))
}

func factoryStoryValidationImportID(dirName, featureID string) string {
	return factoryStoryImportID(dirName, featureID) + "-validation"
}

func factoryStoryStateImportID(dirName, featureID string) string {
	return factoryStoryImportID(dirName, featureID) + "-state"
}

func factoryDeliveryLogImportID(epicID string, index int) string {
	return fmt.Sprintf("%s-log-%04d", epicID, index)
}

func sanitizeFactoryImportIDPart(value, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if lastDash || b.Len() == 0 {
				continue
			}
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return fallback
	}
	return slug
}

func factoryMilestoneTitle(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
	if len(parts) == 0 {
		return "Imported"
	}
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		if len(part) <= 3 && strings.ToUpper(part) == part {
			parts[i] = part
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

func factoryStoryTitle(featureID string) string {
	return factoryMilestoneTitle(featureID)
}

func buildFactoryMilestoneObjective(milestone factoryMilestoneGroup) string {
	featureIDs := make([]string, 0, len(milestone.Features))
	for _, feature := range milestone.Features {
		featureIDs = append(featureIDs, feature.ID)
	}
	sort.Strings(featureIDs)
	return fmt.Sprintf("Deterministic import of %d Factory feature(s): %s.", len(featureIDs), strings.Join(featureIDs, ", "))
}

func factoryMilestoneScope(features []factoryFeature) []string {
	out := make([]string, 0, len(features))
	for _, feature := range features {
		if id := strings.TrimSpace(feature.ID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func buildFactoryStoryDescription(feature factoryFeature) string {
	parts := []string{strings.TrimSpace(feature.Description)}
	if strings.TrimSpace(feature.SkillName) != "" {
		parts = append(parts, "Skill="+strings.TrimSpace(feature.SkillName))
	}
	if len(feature.Preconditions) > 0 {
		parts = append(parts, "Preconditions="+strings.Join(cleanRoomItems(feature.Preconditions), "; "))
	}
	if len(feature.ExpectedBehavior) > 0 {
		parts = append(parts, "Expected="+strings.Join(cleanRoomItems(feature.ExpectedBehavior), "; "))
	}
	if len(feature.VerificationSteps) > 0 {
		parts = append(parts, "Verification="+strings.Join(cleanRoomItems(feature.VerificationSteps), "; "))
	}
	if len(feature.Fulfills) > 0 {
		parts = append(parts, "Assertions="+strings.Join(cleanRoomItems(feature.Fulfills), ", "))
	}
	if len(feature.WorkerSessionIDs) > 0 {
		parts = append(parts, "WorkerSessions="+strings.Join(cleanRoomItems(feature.WorkerSessionIDs), ", "))
	}
	return strings.Join(cleanRoomItems(parts), " | ")
}

func buildFactoryEpicFinalizeSummary(mission factoryMissionImport) string {
	parts := []string{
		"Imported Factory mission as canonical room-agile epic graph.",
	}
	if mission.Goal != "" {
		parts = append(parts, mission.Goal)
	}
	if mission.Outcome != "" {
		parts = append(parts, mission.Outcome)
	}
	if mission.State.State != "" {
		parts = append(parts, "Factory state="+mission.State.State+".")
	}
	if mission.WorkingDirectory != "" {
		parts = append(parts, "Working directory="+mission.WorkingDirectory+".")
	}
	return strings.Join(parts, " ")
}

func buildFactoryAssertionCoverageSummary(assertions map[string]factoryAssertion) string {
	passed := 0
	failed := 0
	pending := 0
	for _, assertion := range assertions {
		switch strings.ToLower(strings.TrimSpace(assertion.Status)) {
		case "passed":
			passed++
		case "failed":
			failed++
		case "pending":
			pending++
		}
	}
	return fmt.Sprintf("Factory assertions imported: %d total (%d passed, %d failed, %d pending)", passed+failed+pending, passed, failed, pending)
}

func factoryEpicHorizon(mission factoryMissionImport) string {
	switch strings.ToLower(strings.TrimSpace(mission.State.State)) {
	case "paused":
		return "paused"
	case "completed":
		return "completed"
	default:
		return ""
	}
}

func firstFactoryProgressTimestamp(events []factoryProgressEvent) time.Time {
	for _, event := range events {
		if !event.Timestamp.IsZero() {
			return event.Timestamp
		}
	}
	return time.Time{}
}

func factoryFeatureCreatedAt(progress factoryFeatureProgress) time.Time {
	if progress.LatestEvent != nil && !progress.LatestEvent.Timestamp.IsZero() {
		return progress.LatestEvent.Timestamp.Add(-time.Second)
	}
	return time.Time{}
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC().Truncate(time.Second)
		}
	}
	return time.Time{}
}

func anyString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}

func anyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func anyBool(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}
