package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	config "github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/context/sessionkit/claudejsonl"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

func TestBuildClaudeSessionFromMessages(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	messages := []*claudejsonl.ReadMessage{
		{
			Message: &claudejsonl.Message{
				Type:    "user",
				Message: []byte(`{"role":"user","content":"Fix the build"}`),
			},
			Timestamp: now.Add(-2 * time.Minute),
		},
		{
			Message: &claudejsonl.Message{
				Type:    "assistant",
				Message: []byte(`{"role":"assistant","content":[{"type":"tool_use","name":"Read"},{"type":"text","text":"I found the issue."}]}`),
			},
			Timestamp: now.Add(-1 * time.Minute),
		},
	}

	session := buildClaudeSessionFromMessages("sess-1", "/tmp/raw.jsonl", "/tmp/ws", messages)
	if session.ID != "sess-1" {
		t.Fatalf("id=%q want sess-1", session.ID)
	}
	if session.AgentType != "claude" {
		t.Fatalf("agent_type=%q want claude", session.AgentType)
	}
	if session.UserTurns != 1 {
		t.Fatalf("user_turns=%d want 1", session.UserTurns)
	}
	if session.ToolInvocations == 0 {
		t.Fatalf("tool_invocations=%d want >0", session.ToolInvocations)
	}
	if session.ProjectName != "ws" {
		t.Fatalf("project_name=%q want ws", session.ProjectName)
	}
}

func TestShouldKeepClaudeTrainingPair(t *testing.T) {
	t.Parallel()

	if shouldKeepClaudeTrainingPair("# MCP Builder Mode\nTask: this", "ok") {
		t.Fatal("expected MCP Builder Mode prompt to be filtered")
	}
	if shouldKeepClaudeTrainingPair("Base directory for this skill: /tmp/skill", "ok") {
		t.Fatal("expected skill bootstrap prompt to be filtered")
	}
	if shouldKeepClaudeTrainingPair("<task-notification>\n<summary>Background command completed</summary>\n</task-notification>\nRead the output file to retrieve the result", "ok") {
		t.Fatal("expected task notification wrapper to be filtered")
	}
	if !shouldKeepClaudeTrainingPair("Please review the v2 implementation", "I reviewed it and found two concrete issues.") {
		t.Fatal("expected natural review pair to be kept")
	}
}

func TestShouldKeepClaudeTrainingPairForCoderProfile(t *testing.T) {
	t.Parallel()

	if shouldKeepClaudeTrainingPairForProfile("commit it", "Committed on feature/gui-noise-reduction as 28ba492a.", "coder") {
		t.Fatal("expected commit chatter to be filtered for coder profile")
	}
	if shouldKeepClaudeTrainingPairForProfile("continue", "Picking up where we left off.", "coder") {
		t.Fatal("expected continuation stub to be filtered for coder profile")
	}
	if !shouldKeepClaudeTrainingPairForProfile("Should we make annotation_scout more specialized?", "Option A is simpler and already supports categories/sorting.", "coder") {
		t.Fatal("expected technical design discussion to be kept for coder profile")
	}
	if !shouldKeepClaudeTrainingPairForProfile("What is the longer term fix here, inline embedding?", "The longer-term fix is to separate annotation from embedding.", "coder") {
		t.Fatal("expected technical architecture discussion to be kept for coder profile")
	}
}

func TestShouldKeepClaudeTrainingPairForCoderStrongProfile(t *testing.T) {
	t.Parallel()

	if shouldKeepClaudeTrainingPairForProfile("check the job", "Pipeline passed.", "coder-strong") {
		t.Fatal("expected release workflow chatter to be filtered for coder-strong profile")
	}
	if !shouldKeepClaudeTrainingPairForProfile("Should we make annotation_scout more specialized?", "Option A is simpler and already supports categories/sorting.", "coder-strong") {
		t.Fatal("expected specialized technical design discussion to be kept for coder-strong profile")
	}
}

func TestExportClaudeProjectDatasetSkipsExistingSessionsWithoutForce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	root := t.TempDir()
	workspacePath := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspace) error = %v", err)
	}

	claudeHome := filepath.Join(root, ".claude")
	projectDir := filepath.Join(claudeHome, "projects", strings.ReplaceAll(workspacePath, string(filepath.Separator), "-"))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir) error = %v", err)
	}

	raw := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"Please review the diff"},"timestamp":"2026-03-19T10:00:00Z"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I found two concrete issues."}]},"timestamp":"2026-03-19T10:00:01Z"}`,
	}, "\n") + "\n"
	rawPath := filepath.Join(projectDir, "sess-1.jsonl")
	if err := os.WriteFile(rawPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile(raw) error = %v", err)
	}

	sessionStore, err := sessions.Open(ctx, root)
	if err != nil {
		t.Fatalf("sessions.Open() error = %v", err)
	}
	defer sessionStore.Close() //nolint:errcheck

	memStore, err := memory.Open(ctx, root, filepath.Join(root, "cas"))
	if err != nil {
		t.Fatalf("memory.Open() error = %v", err)
	}
	defer memStore.Close() //nolint:errcheck

	_, firstIngested, _, err := exportClaudeProjectDataset(ctx, sessionStore, memStore, workspacePath, claudeHome, false, false, 10, false, false, false, "general", "standalone")
	if err != nil {
		t.Fatalf("first exportClaudeProjectDataset() error = %v", err)
	}
	if firstIngested != 1 {
		t.Fatalf("first ingested=%d want 1", firstIngested)
	}

	_, secondIngested, _, err := exportClaudeProjectDataset(ctx, sessionStore, memStore, workspacePath, claudeHome, false, false, 10, false, false, false, "general", "standalone")
	if err != nil {
		t.Fatalf("second exportClaudeProjectDataset() error = %v", err)
	}
	if secondIngested != 0 {
		t.Fatalf("second ingested=%d want 0", secondIngested)
	}
}

func TestClaudeProjectDirForHomeUsesProvidedHomeForHashFallback(t *testing.T) {
	t.Parallel()

	claudeHome := filepath.Join(t.TempDir(), ".claude-custom")
	workspacePath := filepath.Join("/tmp", "missing-workspace")

	got := claudeProjectDirForHome(workspacePath, claudeHome)
	wantPrefix := filepath.Join(claudeHome, "projects") + string(filepath.Separator)
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("got=%q want prefix %q", got, wantPrefix)
	}
	if strings.Contains(got, filepath.Join(os.Getenv("HOME"), ".claude")) {
		t.Fatalf("got=%q unexpectedly rooted in default home", got)
	}
}

func TestFilterClaudeRawPairsDeterministic(t *testing.T) {
	t.Parallel()

	pairs := []claudeRawPair{
		{
			UserRequest:       "# MCP Builder Mode\nTask: this",
			AssistantResponse: "Builder response",
		},
		{
			UserRequest:       "Was there m3 and m4?",
			AssistantResponse: "Yes. M3 was episodes, and M4 was narrative/self-model.",
		},
	}

	filtered := filterClaudeRawPairsDeterministic(pairs, "general")
	if len(filtered) != 1 {
		t.Fatalf("len(filtered)=%d want 1", len(filtered))
	}
	if filtered[0].UserRequest != "Was there m3 and m4?" {
		t.Fatalf("kept user_request=%q", filtered[0].UserRequest)
	}
}

func TestFilterClaudeRawPairsDeterministicCoderProfile(t *testing.T) {
	t.Parallel()

	pairs := []claudeRawPair{
		{
			UserRequest:       "commit it",
			AssistantResponse: "Committed on feature/gui-noise-reduction as 28ba492a.",
		},
		{
			UserRequest:       "What is the longer term fix here, inline embedding?",
			AssistantResponse: "The longer-term fix is to separate annotation from embedding.",
		},
	}

	filtered := filterClaudeRawPairsDeterministic(pairs, "coder")
	if len(filtered) != 1 {
		t.Fatalf("len(filtered)=%d want 1", len(filtered))
	}
	if filtered[0].UserRequest != "What is the longer term fix here, inline embedding?" {
		t.Fatalf("kept user_request=%q", filtered[0].UserRequest)
	}
}

func TestShouldKeepClaudeExampleForMode(t *testing.T) {
	t.Parallel()

	coder := optimization.TranscriptTrainingExample{
		Input:    optimization.TranscriptTrainingInput{UserRequest: "Please review the v2 implementation"},
		Output:   optimization.TranscriptTrainingOutput{Response: "I found two issues in the schema changes."},
		Metadata: optimization.TranscriptTrainingMetadata{Category: "coder_impl"},
	}
	cont := optimization.TranscriptTrainingExample{
		Input:    optimization.TranscriptTrainingInput{UserRequest: "continue"},
		Output:   optimization.TranscriptTrainingOutput{Response: "Picking up where we left off."},
		Metadata: optimization.TranscriptTrainingMetadata{Category: "continuation"},
	}
	ops := optimization.TranscriptTrainingExample{
		Input:    optimization.TranscriptTrainingInput{UserRequest: "is the embedding worker running?"},
		Output:   optimization.TranscriptTrainingOutput{Response: "Yes, it's still running."},
		Metadata: optimization.TranscriptTrainingMetadata{Category: "ops_infra"},
	}

	if !shouldKeepClaudeExampleForMode(coder, "standalone") {
		t.Fatal("expected coder_impl example in standalone mode")
	}
	if shouldKeepClaudeExampleForMode(cont, "standalone") {
		t.Fatal("expected continuation example to be filtered from standalone mode")
	}
	if !shouldKeepClaudeExampleForMode(cont, "continuation") {
		t.Fatal("expected continuation example in continuation mode")
	}
	if shouldKeepClaudeExampleForMode(coder, "continuation") {
		t.Fatal("expected coder_impl example to be filtered from continuation mode")
	}
	if shouldKeepClaudeExampleForMode(ops, "standalone") {
		t.Fatal("expected weak ops example to be filtered from standalone mode")
	}
	if !shouldKeepClaudeExampleForMode(ops, "ops") {
		t.Fatal("expected ops example in ops mode")
	}
}

func TestIsFaithfulClaudeRewriteAcceptsConciseFaithfulRewrite(t *testing.T) {
	t.Parallel()

	pair := claudeRawPair{
		UserRequest:       "Was there m3 and m4?",
		AssistantResponse: "Yes — M3 groups turns into semantic chapters, and M4 applies a working context gate before ranking.",
	}
	rewritten := claudeRewriteContent{
		CleanUserRequest:       "Was there m3 and m4?",
		CleanAssistantResponse: "Yes — M3 groups turns into semantic chapters, and M4 applies a working context gate before ranking.",
	}

	if !isFaithfulClaudeRewrite(pair, rewritten) {
		t.Fatal("expected faithful rewrite to pass")
	}
}

func TestIsFaithfulClaudeRewriteRejectsRoleDrift(t *testing.T) {
	t.Parallel()

	pair := claudeRawPair{
		UserRequest:       "lets fix the formatting noise",
		AssistantResponse: "Let me check what formatter is configured so we can either pre-format the originals or revert the formatting noise.",
	}
	rewritten := claudeRewriteContent{
		CleanUserRequest:       "Let me check what formatter is configured so we can either pre-format the originals or revert the formatting noise.",
		CleanAssistantResponse: "I'll review the current settings and determine if the formatting noise is coming from a specific source.",
	}

	if isFaithfulClaudeRewrite(pair, rewritten) {
		t.Fatal("expected role-drift rewrite to be rejected")
	}
}

func TestIsFaithfulClaudeRewriteRejectsPlaceholderOutput(t *testing.T) {
	t.Parallel()

	pair := claudeRawPair{
		UserRequest:       "I applied the changes, can you review",
		AssistantResponse: "The diff is massive due to formatting changes. Let me focus on the semantic changes only.",
	}
	rewritten := claudeRewriteContent{
		CleanUserRequest:       "...",
		CleanAssistantResponse: "...",
	}

	if isFaithfulClaudeRewrite(pair, rewritten) {
		t.Fatal("expected placeholder rewrite to be rejected")
	}
}

func TestIsFaithfulClaudeRewriteRejectsInventedWorkflow(t *testing.T) {
	t.Parallel()

	pair := claudeRawPair{
		UserRequest:       "can you apply them on the branch we were working on then we can apply to main",
		AssistantResponse: "Good. Now I'll apply only the semantic changes manually, preserving the original code style.",
	}
	rewritten := claudeRewriteContent{
		CleanUserRequest:       "can you apply them on the branch we were working on then we can apply to main",
		CleanAssistantResponse: "I'll apply the changes to the branch you mentioned, then merge them into main. First fetch the remote branch, then cherry-pick commits, push, and create a pull request.",
	}

	if isFaithfulClaudeRewrite(pair, rewritten) {
		t.Fatal("expected invented workflow rewrite to be rejected")
	}
}

func TestIsFaithfulClaudeRewriteWithConfigDisabled(t *testing.T) {
	t.Parallel()

	pair := claudeRawPair{
		UserRequest:       "lets fix the formatting noise",
		AssistantResponse: "Let me check what formatter is configured.",
	}
	rewritten := claudeRewriteContent{
		CleanUserRequest:       "...",
		CleanAssistantResponse: "...",
	}

	cfg := defaultClaudeRewriteFidelityConfig()
	cfg.Enabled = false
	if !isFaithfulClaudeRewriteWithConfig(pair, rewritten, cfg) {
		t.Fatal("expected disabled gate to allow rewrite")
	}
}

func TestClassifyClaudeRewriteFailure(t *testing.T) {
	t.Parallel()

	if got := classifyClaudeRewriteFailure(errors.New("boom"), ""); got != "request_error" {
		t.Fatalf("classify request error = %q", got)
	}
	if got := classifyClaudeRewriteFailure(errors.New("parse"), `{"bad":true}`); got != "parse_error" {
		t.Fatalf("classify parse error = %q", got)
	}
}

func TestResolveClaudeLeaderboardTargets(t *testing.T) {
	t.Parallel()

	targets, err := resolveClaudeLeaderboardTargets("lmstudio", []string{"liquid/lfm2.5-1.2b"}, []string{"openrouter:minimax/minimax-m2.7"})
	if err != nil {
		t.Fatalf("resolve targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("len(targets)=%d want 2", len(targets))
	}
	if targets[0].Provider != "lmstudio" || targets[0].Model != "liquid/lfm2.5-1.2b" {
		t.Fatalf("unexpected target[0]=%+v", targets[0])
	}
	if targets[1].Provider != "openrouter" || targets[1].Model != "minimax/minimax-m2.7" {
		t.Fatalf("unexpected target[1]=%+v", targets[1])
	}
}

func TestResolveClaudeLeaderboardTargetsRejectsInvalidTarget(t *testing.T) {
	t.Parallel()

	if _, err := resolveClaudeLeaderboardTargets("lmstudio", nil, []string{"broken"}); err == nil {
		t.Fatal("expected invalid target to fail")
	}
}

func TestResolveClaudeRewriteTargetsDefaultsToRemoteBestWithFallback(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		LLM: config.LLMSettings{
			OpenRouterAPIKey: "openrouter-key",
		},
	}

	primary, fallback, err := resolveClaudeRewriteTargets(cfg, "", "", "", "")
	if err != nil {
		t.Fatalf("resolve rewrite targets: %v", err)
	}
	if primary.Provider != "openrouter" || primary.Model != "openai/gpt-5.4-nano" {
		t.Fatalf("unexpected primary target: %+v", primary)
	}
	if fallback == nil {
		t.Fatal("expected fallback target")
		return
	}
	got := *fallback
	if got.Provider != "lmstudio" || got.Model != "liquid/lfm2.5-1.2b" {
		t.Fatalf("unexpected fallback target: %+v", got)
	}
}

func TestResolveClaudeRewriteTargetsHonorsExplicitModel(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	primary, fallback, err := resolveClaudeRewriteTargets(cfg, "lmstudio", "", "", "liquid/lfm2-24b-a2b")
	if err != nil {
		t.Fatalf("resolve rewrite targets: %v", err)
	}
	if primary.Provider != "lmstudio" || primary.Model != "liquid/lfm2-24b-a2b" {
		t.Fatalf("unexpected primary target: %+v", primary)
	}
	if fallback != nil {
		t.Fatalf("unexpected fallback: %+v", *fallback)
	}
}

func TestSanitizeClaudeLeaderboardName(t *testing.T) {
	t.Parallel()

	got := sanitizeClaudeLeaderboardName("openrouter-minimax/minimax-m2.7")
	if got != "openrouter_minimax_minimax_m2_7" {
		t.Fatalf("sanitized=%q", got)
	}
}
