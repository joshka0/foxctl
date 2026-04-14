package prompts

import (
	"strings"
	"testing"
)

func TestDefaultPrompt_AppendsStructuredShellGuidanceForShellRoles(t *testing.T) {
	prompt, ok := DefaultPrompt("coder")
	if !ok {
		t.Fatal("expected coder prompt")
	}
	for _, want := range []string{
		"You are a coding agent.",
		"STRUCTURED SHELL POLICY:",
		"Prefer the `shell` tool for supported read-only repo inspection commands",
		"Before editing, reread the target with `fs_read_file`, `context_grep`, or another raw file/context tool.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n%s", want, prompt)
		}
	}
}

func TestDefaultPrompt_DoesNotAppendStructuredShellGuidanceForPlanner(t *testing.T) {
	prompt, ok := DefaultPrompt("planner")
	if !ok {
		t.Fatal("expected planner prompt")
	}
	if strings.Contains(prompt, "STRUCTURED SHELL POLICY:") {
		t.Fatalf("planner prompt should not include shell guidance\n%s", prompt)
	}
}

func TestAppendStructuredShellGuidance_DoesNotDuplicate(t *testing.T) {
	base := "You are a coding agent."
	once := AppendStructuredShellGuidance("coder", base)
	twice := AppendStructuredShellGuidance("coder", once)
	if once != twice {
		t.Fatalf("guidance duplicated\nonce:\n%s\n\ntwice:\n%s", once, twice)
	}
}

func TestDefaultPrompt_RoomSpecializedRoles(t *testing.T) {
	for _, role := range []string{"frontend-eng", "backend-eng", "collaborator", "coordinator", "security-review"} {
		prompt, ok := DefaultPrompt(role)
		if !ok {
			t.Fatalf("expected default prompt for %s", role)
		}
		if !strings.Contains(prompt, "STRUCTURED SHELL POLICY:") {
			t.Fatalf("%s prompt should include structured shell guidance\n%s", role, prompt)
		}
	}
}

func TestComposeRoomAwarePrompt_AppendsOnboardingOnce(t *testing.T) {
	base := "You are a frontend engineering agent."
	opts := RoomOnboardingOptions{
		RoomID:      "triad-123",
		WorkspaceID: "/tmp/ws",
		Role:        "frontend-eng",
		RoomRole:    "frontend-eng",
	}
	once := ComposeRoomAwarePrompt(base, opts)
	twice := ComposeRoomAwarePrompt(once, opts)
	if once != twice {
		t.Fatalf("room onboarding duplicated\nonce:\n%s\n\ntwice:\n%s", once, twice)
	}
	for _, want := range []string{
		"ROOM ONBOARDING:",
		"`foxctl-room` and `foxctl-room-agent`",
		"`foxctl room status triad-123`",
		"Prioritize user-facing correctness",
	} {
		if !strings.Contains(once, want) {
			t.Fatalf("prompt missing %q\n%s", want, once)
		}
	}
}

func TestRoomOnboardingMessage_UsesRoleSpecificSubject(t *testing.T) {
	subject, body := RoomOnboardingMessage(RoomOnboardingOptions{
		RoomID:      "triad-123",
		WorkspaceID: "ws",
		Role:        "reviewer",
		RoomRole:    "security-review",
	})
	if got, want := subject, "Room onboarding: security-review"; got != want {
		t.Fatalf("subject=%q want %q", got, want)
	}
	if !strings.Contains(body, "Review behavior: findings first") {
		t.Fatalf("body missing reviewer guidance\n%s", body)
	}
}
