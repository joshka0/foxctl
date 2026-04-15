package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRoomEpicImportFactoryImportsCanonicalRoomGraph(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	missionDir := writeFactoryMissionFixture(t, "demo-mission")

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicImportFactory(cmd, workspace, "human-a", "alpha", missionDir, true); err != nil {
		t.Fatalf("runRoomEpicImportFactory: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epicID := data["epic_id"].(string)
	if got, want := epicID, factoryEpicImportID("demo-mission"); got != want {
		t.Fatalf("epic_id=%q want %q", got, want)
	}
	if got := int(data["imported"].(float64)); got == 0 {
		t.Fatalf("imported=%d want >0", got)
	}
	if got := int(data["skipped"].(float64)); got != 0 {
		t.Fatalf("skipped=%d want 0", got)
	}

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()

	messages, err := store.ListRoomMessages(ctx, workspace, "alpha", roomTaskScanLimit)
	if err != nil {
		t.Fatalf("ListRoomMessages: %v", err)
	}

	epic := roomEpicViewByID(buildRoomEpicViews(messages), epicID)
	if epic == nil {
		t.Fatalf("epic %q not found", epicID)
	}
	if got := intField(epic, "milestone_count"); got != 2 {
		t.Fatalf("milestone_count=%d want 2", got)
	}
	if got := intField(epic, "story_count"); got != 3 {
		t.Fatalf("story_count=%d want 3", got)
	}

	stories := buildRoomStoryViews(messages)
	coreStory := roomStoryViewByID(stories, factoryStoryImportID("demo-mission", "m1-backend-core"))
	if coreStory == nil {
		t.Fatalf("core story not found")
	}
	if got := stringField(coreStory, "state"); got != "done" {
		t.Fatalf("core story state=%q want done", got)
	}
	if got := stringField(coreStory, "latest_validation_status"); got != "pass" {
		t.Fatalf("core story latest_validation_status=%q want pass", got)
	}

	wireupStory := roomStoryViewByID(stories, factoryStoryImportID("demo-mission", "m1-ui-wireup"))
	if wireupStory == nil {
		t.Fatalf("wireup story not found")
	}
	if got := stringField(wireupStory, "state"); got != "in_progress" {
		t.Fatalf("wireup story state=%q want in_progress", got)
	}
	if got := stringField(wireupStory, "latest_validation_status"); got != "blocked" {
		t.Fatalf("wireup story latest_validation_status=%q want blocked", got)
	}

	docsStory := roomStoryViewByID(stories, factoryStoryImportID("demo-mission", "m2-docs"))
	if docsStory == nil {
		t.Fatalf("docs story not found")
	}
	if got := stringField(docsStory, "state"); got != "accepted" {
		t.Fatalf("docs story state=%q want accepted", got)
	}
	if got := intField(docsStory, "state_update_count"); got != 1 {
		t.Fatalf("docs story state_update_count=%d want 1", got)
	}
	if got := intField(docsStory, "validation_count"); got != 0 {
		t.Fatalf("docs story validation_count=%d want 0", got)
	}

	logs := buildRoomDeliveryLogViews(messages)
	importedLogs := 0
	for _, entry := range logs {
		if stringField(entry, "epic_id") == epicID {
			importedLogs++
		}
	}
	if importedLogs != 4 {
		t.Fatalf("delivery log count=%d want 4", importedLogs)
	}

	epicPath := filepath.Join(home, ".foxctl", "epics", epicID, "epic.md")
	if _, err := os.Stat(epicPath); err != nil {
		t.Fatalf("epic workpack %s: %v", epicPath, err)
	}
}

func TestRunRoomEpicImportFactoryIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	missionDir := writeFactoryMissionFixture(t, "demo-mission")

	cmd, firstOut := newRoomTestCommand(ctx)
	if err := runRoomEpicImportFactory(cmd, workspace, "human-a", "alpha", missionDir, true); err != nil {
		t.Fatalf("first import: %v", err)
	}
	first := decodeRoomEnvelope(t, firstOut)
	if got := int(first["imported"].(float64)); got == 0 {
		t.Fatalf("first imported=%d want >0", got)
	}

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()

	beforeMessages, err := store.ListRoomMessages(ctx, workspace, "alpha", roomTaskScanLimit)
	if err != nil {
		t.Fatalf("ListRoomMessages before second import: %v", err)
	}

	cmd, secondOut := newRoomTestCommand(ctx)
	if err := runRoomEpicImportFactory(cmd, workspace, "human-a", "alpha", missionDir, true); err != nil {
		t.Fatalf("second import: %v", err)
	}
	second := decodeRoomEnvelope(t, secondOut)
	if got := int(second["imported"].(float64)); got != 0 {
		t.Fatalf("second imported=%d want 0", got)
	}
	if got := int(second["skipped"].(float64)); got != len(beforeMessages) {
		t.Fatalf("second skipped=%d want %d", got, len(beforeMessages))
	}

	afterMessages, err := store.ListRoomMessages(ctx, workspace, "alpha", roomTaskScanLimit)
	if err != nil {
		t.Fatalf("ListRoomMessages after second import: %v", err)
	}
	if len(afterMessages) != len(beforeMessages) {
		t.Fatalf("message count after second import=%d want %d", len(afterMessages), len(beforeMessages))
	}
}

func TestRunRoomEpicImportFactoryMapsFailedWorkerCompletionWithoutClaimingCompletion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	missionDir := writeFactoryMissionFixture(t, "failed-mission")
	progress := []map[string]any{
		{
			"timestamp": "2026-04-11T12:00:00Z",
			"type":      "mission_accepted",
			"title":     "Failed Mission",
		},
		{
			"timestamp": "2026-04-11T12:01:00Z",
			"type":      "mission_run_started",
			"message":   "Starting failed mission.",
		},
		{
			"timestamp":            "2026-04-11T12:02:00Z",
			"type":                 "worker_completed",
			"featureId":            "m2-docs",
			"successState":         "failure",
			"returnToOrchestrator": true,
			"exitCode":             0,
			"handoff": map[string]any{
				"salientSummary": "No work was performed because the worker setup was invalid.",
			},
		},
		{
			"timestamp": "2026-04-11T12:03:00Z",
			"type":      "mission_paused",
		},
	}
	writeFactoryJSONLFixture(t, filepath.Join(missionDir, "progress_log.jsonl"), progress)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicImportFactory(cmd, workspace, "human-a", "alpha", missionDir, true); err != nil {
		t.Fatalf("runRoomEpicImportFactory: %v", err)
	}

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()

	messages, err := store.ListRoomMessages(ctx, workspace, "alpha", roomTaskScanLimit)
	if err != nil {
		t.Fatalf("ListRoomMessages: %v", err)
	}

	stories := buildRoomStoryViews(messages)
	docsStory := roomStoryViewByID(stories, factoryStoryImportID("failed-mission", "m2-docs"))
	if docsStory == nil {
		t.Fatalf("docs story not found")
	}
	if got := stringField(docsStory, "state"); got != "blocked" {
		t.Fatalf("docs story state=%q want blocked", got)
	}
	if got := stringField(docsStory, "state_reason"); got != "No work was performed because the worker setup was invalid." {
		t.Fatalf("docs story state_reason=%q want worker failure summary", got)
	}

	logs := buildRoomDeliveryLogViews(messages)
	var sawFailure bool
	for _, entry := range logs {
		if stringField(entry, "label") == "Feature m2-docs completed" {
			t.Fatalf("unexpected completion log for failed worker completion")
		}
		if stringField(entry, "label") == "Feature m2-docs worker completion reported issues" {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Fatalf("did not find failed worker completion delivery log")
	}
}

func TestRunRoomEpicImportFactoryMapsValidatorArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	missionDir := writeFactoryMissionFixture(t, "validator-mission")
	features := map[string]any{
		"features": []map[string]any{
			{
				"id":                "user-testing-validator-checkout",
				"description":       "Validate checkout flows with user testing evidence.",
				"skillName":         "user-testing-validator",
				"preconditions":     []string{"checkout implementation complete"},
				"expectedBehavior":  []string{"happy path validated"},
				"verificationSteps": []string{"go test ./internal/checkout/..."},
				"fulfills":          []string{"VAL-001", "VAL-002"},
				"milestone":         "m1",
				"status":            "completed",
				"workerSessionIds":  []string{"worker-validator-1"},
			},
		},
	}
	writeFactoryJSONFixture(t, filepath.Join(missionDir, "features.json"), features)
	validationState := map[string]any{
		"assertions": map[string]any{
			"VAL-001": map[string]any{"status": "passed", "validatedAtMilestone": "m1"},
			"VAL-002": map[string]any{"status": "pending"},
		},
	}
	writeFactoryJSONFixture(t, filepath.Join(missionDir, "validation-state.json"), validationState)
	progress := []map[string]any{
		{
			"timestamp": "2026-04-11T12:00:00Z",
			"type":      "mission_accepted",
			"title":     "Validator Mission",
		},
		{
			"timestamp":            "2026-04-11T12:02:00Z",
			"type":                 "worker_completed",
			"featureId":            "user-testing-validator-checkout",
			"workerSessionId":      "worker-validator-1",
			"successState":         "failure",
			"returnToOrchestrator": true,
			"exitCode":             0,
			"validatorsPassed":     true,
			"handoff": map[string]any{
				"salientSummary": "Validated checkout flow locally; one environment-dependent assertion remains pending.",
			},
		},
	}
	writeFactoryJSONLFixture(t, filepath.Join(missionDir, "progress_log.jsonl"), progress)
	handoffPath := filepath.Join(missionDir, "handoffs", "2026-04-11T12-02-00Z__user-testing-validator-checkout__worker-validator-1.json")
	writeFactoryJSONFixture(t, handoffPath, map[string]any{
		"timestamp":       "2026-04-11T12:02:00Z",
		"workerSessionId": "worker-validator-1",
		"featureId":       "user-testing-validator-checkout",
		"milestone":       "m1",
		"commitId":        "deadbeef",
		"successState":    "failure",
		"handoff": map[string]any{
			"salientSummary":    "Validated checkout flow locally; one environment-dependent assertion remains pending.",
			"whatWasLeftUndone": "Browser-based verification still needs a real staging environment.",
			"verification": map[string]any{
				"commandsRun": []map[string]any{
					{"command": "go test ./internal/checkout/...", "exitCode": 0},
					{"command": "go test ./cmd/foxctl/cmd -run TestCheckoutFlow", "exitCode": 0},
				},
			},
			"discoveredIssues": []map[string]any{
				{"description": "Staging-only redirect assertion still pending."},
			},
		},
	})

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicImportFactory(cmd, workspace, "human-a", "alpha", missionDir, true); err != nil {
		t.Fatalf("runRoomEpicImportFactory: %v", err)
	}

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()

	messages, err := store.ListRoomMessages(ctx, workspace, "alpha", roomTaskScanLimit)
	if err != nil {
		t.Fatalf("ListRoomMessages: %v", err)
	}

	stories := buildRoomStoryViews(messages)
	story := roomStoryViewByID(stories, factoryStoryImportID("validator-mission", "user-testing-validator-checkout"))
	if story == nil {
		t.Fatalf("validator story not found")
	}
	if got := stringField(story, "state"); got != "in_review" {
		t.Fatalf("story state=%q want in_review", got)
	}
	validations, _ := story["validations"].([]map[string]any)
	if len(validations) != 1 {
		t.Fatalf("len(validations)=%d want 1", len(validations))
	}
	meta := anyMap(validations[0]["meta"])
	if got := stringField(meta, "validator_type"); got != "user_test" {
		t.Fatalf("validator_type=%q want user_test", got)
	}
	if got := stringField(meta, "artifact_path"); got != handoffPath {
		t.Fatalf("artifact_path=%q want %q", got, handoffPath)
	}
	if got := stringField(meta, "artifact_digest"); got == "" || got[:7] != "sha256:" {
		t.Fatalf("artifact_digest=%q want sha256:*", got)
	}
	if got := stringField(meta, "command"); got == "" || !containsAllSubstrings(got, []string{"go test ./internal/checkout/...", "go test ./cmd/foxctl/cmd -run TestCheckoutFlow"}) {
		t.Fatalf("command=%q missing expected verification commands", got)
	}
	if got := stringField(meta, "notes"); !containsAllSubstrings(got, []string{"handoff_summary=Validated checkout flow locally", "left_undone=Browser-based verification still needs a real staging environment.", "issue=Staging-only redirect assertion still pending."}) {
		t.Fatalf("notes=%q missing handoff evidence details", got)
	}
}

func TestRunRoomEpicImportFactoryDetectsDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	missionDir := writeFactoryMissionFixture(t, "demo-mission")

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicImportFactory(cmd, workspace, "human-a", "alpha", missionDir, true); err != nil {
		t.Fatalf("first import: %v", err)
	}

	if err := os.WriteFile(filepath.Join(missionDir, "mission.md"), []byte("# Demo Mission\n\n## Plan Overview\nShip changed import slice.\n\n## Expected Functionality\nRoom-agile graph reflects the mission.\n"), 0o644); err != nil {
		t.Fatalf("rewrite mission.md: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomEpicImportFactory(cmd, workspace, "human-a", "alpha", missionDir, true)
	assertRoomErrorContains(t, err, "factory import drift")
}

func writeFactoryMissionFixture(t *testing.T, dirName string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), dirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	missionMarkdown := `# Demo Mission

## Plan Overview

Ship demo import slice.

## Expected Functionality

Room-agile graph reflects the mission.
`
	if err := os.WriteFile(filepath.Join(root, "mission.md"), []byte(missionMarkdown), 0o644); err != nil {
		t.Fatalf("write mission.md: %v", err)
	}

	state := map[string]any{
		"missionId":        "mis_demo",
		"state":            "paused",
		"workingDirectory": "/tmp/demo-workspace",
		"createdAt":        "2026-04-11T12:00:00Z",
		"updatedAt":        "2026-04-12T07:23:31Z",
	}
	writeFactoryJSONFixture(t, filepath.Join(root, "state.json"), state)

	features := map[string]any{
		"features": []map[string]any{
			{
				"id":                "m1-backend-core",
				"description":       "Implement backend core.",
				"skillName":         "go-backend",
				"preconditions":     []string{},
				"expectedBehavior":  []string{"Core works"},
				"verificationSteps": []string{"go test ./..."},
				"fulfills":          []string{"VAL-001", "VAL-002"},
				"milestone":         "m1",
				"status":            "completed",
				"workerSessionIds":  []string{"worker-1"},
			},
			{
				"id":                "m1-ui-wireup",
				"description":       "Wire the UI.",
				"skillName":         "frontend-worker",
				"preconditions":     []string{"m1-backend-core complete"},
				"expectedBehavior":  []string{"UI renders"},
				"verificationSteps": []string{"pnpm test ui"},
				"fulfills":          []string{"VAL-003"},
				"milestone":         "m1",
				"status":            "in_progress",
				"workerSessionIds":  []string{"worker-2"},
			},
			{
				"id":                "m2-docs",
				"description":       "Write docs.",
				"skillName":         "docs-worker",
				"preconditions":     []string{},
				"expectedBehavior":  []string{"Docs exist"},
				"verificationSteps": []string{"make check-doc-links"},
				"fulfills":          []string{},
				"milestone":         "m2",
				"status":            "pending",
				"workerSessionIds":  []string{},
			},
		},
	}
	writeFactoryJSONFixture(t, filepath.Join(root, "features.json"), features)

	validationState := map[string]any{
		"assertions": map[string]any{
			"VAL-001": map[string]any{"status": "passed", "validatedAtMilestone": "m1"},
			"VAL-002": map[string]any{"status": "passed", "validatedAtMilestone": "m1"},
			"VAL-003": map[string]any{"status": "pending"},
		},
	}
	writeFactoryJSONFixture(t, filepath.Join(root, "validation-state.json"), validationState)

	progress := []map[string]any{
		{
			"timestamp": "2026-04-11T12:00:00Z",
			"type":      "mission_accepted",
			"title":     "Demo Mission",
		},
		{
			"timestamp": "2026-04-11T12:01:00Z",
			"type":      "mission_run_started",
			"message":   "Starting demo mission.",
		},
		{
			"timestamp":       "2026-04-11T12:02:00Z",
			"type":            "worker_started",
			"featureId":       "m1-ui-wireup",
			"workerSessionId": "worker-2",
		},
		{
			"timestamp":            "2026-04-11T12:03:00Z",
			"type":                 "worker_completed",
			"featureId":            "m1-backend-core",
			"successState":         "success",
			"returnToOrchestrator": false,
			"commitId":             "abc123",
			"exitCode":             0,
			"validatorsPassed":     true,
			"handoff": map[string]any{
				"salientSummary": "Backend core landed cleanly.",
			},
		},
		{
			"timestamp": "2026-04-11T12:04:00Z",
			"type":      "mission_paused",
		},
	}
	writeFactoryJSONLFixture(t, filepath.Join(root, "progress_log.jsonl"), progress)

	if err := os.WriteFile(filepath.Join(root, "working_directory.txt"), []byte("/tmp/demo-workspace\n"), 0o644); err != nil {
		t.Fatalf("write working_directory.txt: %v", err)
	}
	return root
}

func writeFactoryJSONFixture(t *testing.T, path string, payload any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func writeFactoryJSONLFixture(t *testing.T, path string, payloads []map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
	var b bytes.Buffer
	for _, payload := range payloads {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("Marshal %s: %v", path, err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func containsAllSubstrings(haystack string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
