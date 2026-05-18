package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildBlueprintIncludesAllSocialSkillsInDryRunMode(t *testing.T) {
	out, err := buildBlueprint(input{})
	if err != nil {
		t.Fatalf("buildBlueprint() error = %v", err)
	}

	if out.RunID != "2026-05-praze-launch" {
		t.Fatalf("RunID = %q", out.RunID)
	}
	if len(out.Rooms) != 5 {
		t.Fatalf("rooms len = %d, want 5", len(out.Rooms))
	}
	if out.ExecutionPlan.AgentProvider != "codex" {
		t.Fatalf("ExecutionPlan.AgentProvider = %q, want codex", out.ExecutionPlan.AgentProvider)
	}
	if len(out.ExecutionPlan.Rooms) != 5 {
		t.Fatalf("execution rooms len = %d, want 5", len(out.ExecutionPlan.Rooms))
	}
	if len(out.ExecutionPlan.DebateRoutes) == 0 {
		t.Fatal("ExecutionPlan.DebateRoutes empty")
	}
	if !strings.Contains(out.ExecutionPlan.Rooms[0].CreateCommand, "brand_brief=planner@codex:auto") {
		t.Fatalf("create command missing codex member binding: %s", out.ExecutionPlan.Rooms[0].CreateCommand)
	}
	claimRoom := findExecutionRoom(out.ExecutionPlan.Rooms, "praze-launch-claim-arena")
	if !strings.Contains(claimRoom.CreateCommand, "technical_specialist=reviewer@codex:auto") {
		t.Fatalf("claim room missing shared technical reviewer: %s", claimRoom.CreateCommand)
	}
	if !strings.Contains(claimRoom.CreateCommand, "cta_writer=writer@codex:auto") || !strings.Contains(claimRoom.CreateCommand, "cta_manager=reviewer@codex:auto") {
		t.Fatalf("claim room missing CTA agents: %s", claimRoom.CreateCommand)
	}
	if !strings.Contains(strings.Join(out.RoomCommands, "\n"), "room send praze-launch-foundation") {
		t.Fatalf("room commands missing directed send commands: %v", out.RoomCommands)
	}
	agentIDs := map[string]bool{}
	for _, agent := range out.Agents {
		agentIDs[agent.ID] = true
	}
	for _, id := range []string{"research_planner", "claim_manager", "counterpositioning", "demo_narrative", "pastoral_tone"} {
		if !agentIDs[id] {
			t.Fatalf("missing agent %s", id)
		}
	}
	claimSpec := findRoomSpec(out.Rooms, "praze-launch-claim-arena")
	for _, id := range []string{"cta_writer", "cta_manager"} {
		if !containsString(claimSpec.Agents, id) {
			t.Fatalf("claim room spec missing %s: %v", id, claimSpec.Agents)
		}
	}
	for _, path := range []string{"copy/cta-options.md", "copy/cta-manager-review.md"} {
		if !containsString(claimSpec.Outputs, path) {
			t.Fatalf("claim room outputs missing %s: %v", path, claimSpec.Outputs)
		}
	}

	seen := map[string]bool{}
	for _, call := range out.SocialSkillCalls {
		seen[call.Skill] = true
		if got, ok := call.Input["dry_run"].(bool); !ok || !got {
			t.Fatalf("call %s dry_run = %#v, want true", call.Skill, call.Input["dry_run"])
		}
		if call.Command == "" || !strings.Contains(call.Command, "foxctl run "+call.Skill) {
			t.Fatalf("call command for %s = %q", call.Skill, call.Command)
		}
	}

	for _, skill := range []string{
		"social/x_collect",
		"social/reddit_collect",
		"social/youtube_collect",
		"social/facebook_collect",
		"social/instagram_collect",
	} {
		if !seen[skill] {
			t.Fatalf("missing social skill call for %s", skill)
		}
	}
	for _, call := range out.SocialSkillCalls {
		if strings.Contains(call.Command, `\u003c`) {
			t.Fatalf("command for %s escaped placeholder angle brackets: %s", call.Skill, call.Command)
		}
	}
}

func TestBuildBlueprintPrototypeIncludesMockPipelineRun(t *testing.T) {
	out, err := buildBlueprint(input{Prototype: true, MockData: true})
	if err != nil {
		t.Fatalf("buildBlueprint() error = %v", err)
	}
	if out.Prototype == nil {
		t.Fatal("Prototype = nil, want mocked run")
	}
	if !out.Prototype.MockData {
		t.Fatal("Prototype.MockData = false, want true")
	}
	if len(out.Prototype.Stages) != 5 {
		t.Fatalf("prototype stages len = %d, want 5", len(out.Prototype.Stages))
	}
	if len(out.Prototype.MockResearch) != 4 {
		t.Fatalf("mock research len = %d, want 4", len(out.Prototype.MockResearch))
	}
	for _, path := range []string{
		"runs/2026-05-praze-launch/prototype/mock-social-research.md",
		"runs/2026-05-praze-launch/prototype/room-debate-timeline.md",
		"runs/2026-05-praze-launch/prototype/07-final-launch-pack.md",
		"final/launch-pack/x-launch-post.md",
		"final/launch-pack/video-storyboard.md",
		"final/launch-pack/rejected-angles.md",
	} {
		if !hasFile(out.Files, path) {
			t.Fatalf("prototype output missing file %s", path)
		}
	}
}

func TestBuildBlueprintProvisionPlanUsesCodexRoomsHerdrAndPi(t *testing.T) {
	out, err := buildBlueprint(input{
		Provision:     true,
		MuxBackend:    "zellij",
		MuxSession:    "praze-live",
		HerdrRelay:    true,
		HerdrSession:  "praze-herdr",
		PiOperator:    true,
		AgentProvider: "codex",
	})
	if err != nil {
		t.Fatalf("buildBlueprint() error = %v", err)
	}
	first := out.ExecutionPlan.Rooms[0]
	for _, want := range []string{"--provision", "--agent codex", "--mode auto", "--mux-backend zellij", "--mux-session praze-live"} {
		if !strings.Contains(first.CreateCommand, want) {
			t.Fatalf("create command missing %q: %s", want, first.CreateCommand)
		}
	}
	if first.RelayCommand == "" || !strings.Contains(first.RelayCommand, "--backend herdr") || !strings.Contains(first.RelayCommand, "--session praze-herdr") {
		t.Fatalf("RelayCommand = %q", first.RelayCommand)
	}
	if out.ExecutionPlan.Herdr == nil || !out.ExecutionPlan.Herdr.Enabled {
		t.Fatal("ExecutionPlan.Herdr not enabled")
	}
	if out.ExecutionPlan.Pi == nil || !out.ExecutionPlan.Pi.Enabled {
		t.Fatal("ExecutionPlan.Pi not enabled")
	}
	if !containsString(out.ExecutionPlan.Pi.Tools, "foxctl_room_agile") {
		t.Fatalf("Pi tools missing foxctl_room_agile: %v", out.ExecutionPlan.Pi.Tools)
	}
	if !containsString(out.ExecutionPlan.Pi.Tools, "foxctl_story_validate") {
		t.Fatalf("Pi tools missing foxctl_story_validate: %v", out.ExecutionPlan.Pi.Tools)
	}
	if !strings.Contains(strings.Join(out.ExecutionPlan.Pi.Commands, "\n"), "room epic start praze-launch-review-delivery") {
		t.Fatalf("Pi commands missing room-agile epic bootstrap: %v", out.ExecutionPlan.Pi.Commands)
	}
	for _, path := range []string{
		"rooms/codex-execution-plan.md",
		"rooms/debate-routes.md",
		"rooms/pi-herdr-ops.md",
	} {
		if !hasFile(out.Files, path) {
			t.Fatalf("output missing file %s", path)
		}
	}
}

func TestBuildBlueprintRejectsHerdrAsProvisionBackend(t *testing.T) {
	if _, err := buildBlueprint(input{MuxBackend: "herdr"}); err == nil {
		t.Fatal("buildBlueprint() error = nil, want error")
	}
}

func TestBuildBlueprintLiveModeDisablesDryRunAndCapsLimit(t *testing.T) {
	out, err := buildBlueprint(input{ResearchMode: "live", Limit: 500})
	if err != nil {
		t.Fatalf("buildBlueprint() error = %v", err)
	}
	if out.ResearchMode != "live" {
		t.Fatalf("ResearchMode = %q", out.ResearchMode)
	}
	for _, call := range out.SocialSkillCalls {
		if got, ok := call.Input["dry_run"].(bool); !ok || got {
			t.Fatalf("call %s dry_run = %#v, want false", call.Skill, call.Input["dry_run"])
		}
		if value, ok := call.Input["limit"].(int); ok && value > 50 {
			t.Fatalf("call %s limit = %d, want capped at 50", call.Skill, value)
		}
	}
}

func TestBuildBlueprintRejectsUnknownResearchMode(t *testing.T) {
	if _, err := buildBlueprint(input{ResearchMode: "scrape"}); err == nil {
		t.Fatal("buildBlueprint() error = nil, want error")
	}
}

func TestWriteFilesCreatesPromptPack(t *testing.T) {
	out, err := buildBlueprint(input{})
	if err != nil {
		t.Fatalf("buildBlueprint() error = %v", err)
	}

	root := t.TempDir()
	if err := writeFiles(root, out.Files); err != nil {
		t.Fatalf("writeFiles() error = %v", err)
	}

	for _, path := range []string{
		"README.md",
		"research/social-skill-callbook.md",
		"agents/youtube_research.md",
		"agents/meta_channel_research.md",
		"rooms/message-contract.md",
		"rooms/codex-execution-plan.md",
		"rooms/debate-routes.md",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("expected %s to be written: %v", path, err)
		}
	}
}

func TestAgentPromptsAreRoleSpecific(t *testing.T) {
	out, err := buildBlueprint(input{})
	if err != nil {
		t.Fatalf("buildBlueprint() error = %v", err)
	}

	files := map[string]string{}
	for _, file := range out.Files {
		files[file.Path] = file.Content
	}
	for _, agent := range out.Agents {
		content := files[agent.PromptFile]
		for _, want := range []string{"## Role Instructions", "## Required Artifact Shape", "## Handoff"} {
			if !strings.Contains(content, want) {
				t.Fatalf("prompt %s missing %q:\n%s", agent.PromptFile, want, content)
			}
		}
	}

	required := map[string][]string{
		"agents/youtube_research.md":      {"opening frames", "30-second launch video structure"},
		"agents/x_research.md":            {"first-line hooks", "Ten hook skeletons"},
		"agents/reddit_forum_research.md": {"Do not mock users", "Objection map"},
		"agents/technical_specialist.md":  {"VERIFIED, UNVERIFIED, TOO STRONG, NEEDS REWRITE, BLOCKER", "Claim inventory"},
		"agents/cta_manager.md":           {"Best primary CTA", "Tracking notes"},
		"agents/deliver.md":               {"Final X launch post", "Rejected claims archive"},
	}
	for path, snippets := range required {
		content := files[path]
		if content == "" {
			t.Fatalf("missing prompt file %s", path)
		}
		for _, snippet := range snippets {
			if !strings.Contains(content, snippet) {
				t.Fatalf("prompt %s missing snippet %q:\n%s", path, snippet, content)
			}
		}
	}
}

func hasFile(files []filePlan, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func findRoomSpec(rooms []roomSpec, roomID string) roomSpec {
	for _, room := range rooms {
		if room.ID == roomID {
			return room
		}
	}
	return roomSpec{}
}

func findExecutionRoom(rooms []roomExecutionPlan, roomID string) roomExecutionPlan {
	for _, room := range rooms {
		if room.RoomID == roomID {
			return room
		}
	}
	return roomExecutionPlan{}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
