package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/platform/config"
	storagents "github.com/joshka0/foxctl/internal/storage/agents"
	"github.com/spf13/cobra"
)

func TestResolveOptimizePromptInputInline(t *testing.T) {
	t.Parallel()

	got, err := resolveOptimizePromptInput("  Be careful and concise.  ", "")
	if err != nil {
		t.Fatalf("resolveOptimizePromptInput() error = %v", err)
	}
	if got != "Be careful and concise." {
		t.Fatalf("prompt = %q, want %q", got, "Be careful and concise.")
	}
}

func TestResolveOptimizePromptInputFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("\nUse tools deliberately.\n"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	got, err := resolveOptimizePromptInput("", path)
	if err != nil {
		t.Fatalf("resolveOptimizePromptInput() error = %v", err)
	}
	if got != "Use tools deliberately." {
		t.Fatalf("prompt = %q, want %q", got, "Use tools deliberately.")
	}
}

func TestResolveOptimizePromptInputRejectsInvalidCombinations(t *testing.T) {
	t.Parallel()

	if _, err := resolveOptimizePromptInput("", ""); err == nil {
		t.Fatal("expected error when prompt source is missing")
	}
	if _, err := resolveOptimizePromptInput("inline", "/tmp/prompt.txt"); err == nil {
		t.Fatal("expected error when both prompt sources are set")
	}
}

func TestNewOptimizeCommandIncludesPromptCommands(t *testing.T) {
	t.Parallel()

	cmd := newOptimizeCommand()
	if cmd == nil {
		t.Fatal("expected optimize command")
	}

	want := map[string]bool{
		"dataset": false,
		"prompt":  false,
		"prompts": false,
	}
	for _, child := range cmd.Commands() {
		if _, ok := want[child.Name()]; ok {
			want[child.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("missing optimize subcommand %q", name)
		}
	}

	var promptCmd *cobra.Command
	for _, child := range cmd.Commands() {
		if child.Name() == "prompt" {
			promptCmd = child
			break
		}
	}
	if promptCmd == nil {
		t.Fatal("missing prompt command")
	}
	subcommands := map[string]bool{"propose": false, "cycle": false}
	for _, child := range promptCmd.Commands() {
		if _, ok := subcommands[child.Name()]; ok {
			subcommands[child.Name()] = true
		}
	}
	for name, found := range subcommands {
		if !found {
			t.Fatalf("missing prompt subcommand %q", name)
		}
	}
}

func TestResolvePromptBasePrefersLatestVariant(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg, err := config.Load(ctx, config.WithWorkspacePath(tmp))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenPromptVariantStore: %v", err)
	}
	defer variantStore.Close() //nolint:errcheck

	if _, err := variantStore.Save(ctx, optimization.PromptVariant{
		WorkspaceID:    tmp,
		AgentRole:      "coder",
		TargetProfile:  "local_lmstudio",
		Mode:           "gepa",
		OriginalPrompt: "orig",
		Prompt:         "saved variant prompt",
	}); err != nil {
		t.Fatalf("save variant: %v", err)
	}

	prompt, source, err := resolvePromptBase(ctx, cfg, tmp, "coder", "local_lmstudio", "", "")
	if err != nil {
		t.Fatalf("resolvePromptBase: %v", err)
	}
	if prompt != "saved variant prompt" {
		t.Fatalf("prompt=%q want saved variant prompt", prompt)
	}
	if source != "latest_variant_target_profile" {
		t.Fatalf("source=%q want latest_variant_target_profile", source)
	}
}

func TestResolvePromptBaseFallsBackToAgentPrompt(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg, err := config.Load(ctx, config.WithWorkspacePath(tmp))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	agentStore, err := storagents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("agents.Open: %v", err)
	}
	defer agentStore.Close() //nolint:errcheck

	if err := agentStore.Create(ctx, agent.Agent{
		ID:            "agent-1",
		Namespace:     "ns/test",
		Role:          "coder",
		Prompt:        "live agent prompt",
		WorkspaceRoot: tmp,
		ShareBB:       "scoped",
		State:         agent.StateRunning,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	prompt, source, err := resolvePromptBase(ctx, cfg, tmp, "coder", "local_lmstudio", "", "")
	if err != nil {
		t.Fatalf("resolvePromptBase: %v", err)
	}
	if prompt != "live agent prompt" {
		t.Fatalf("prompt=%q want live agent prompt", prompt)
	}
	if source != "latest_agent" {
		t.Fatalf("source=%q want latest_agent", source)
	}
}

func TestResolvePromptBaseFallsBackToGenericVariantForOtherTarget(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg, err := config.Load(ctx, config.WithWorkspacePath(tmp))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenPromptVariantStore: %v", err)
	}
	defer variantStore.Close() //nolint:errcheck

	if _, err := variantStore.Save(ctx, optimization.PromptVariant{
		WorkspaceID:    tmp,
		AgentRole:      "coder",
		TargetProfile:  "generic",
		Mode:           "gepa",
		OriginalPrompt: "orig",
		Prompt:         "generic variant prompt",
	}); err != nil {
		t.Fatalf("save variant: %v", err)
	}

	prompt, source, err := resolvePromptBase(ctx, cfg, tmp, "coder", "jido_openrouter", "", "")
	if err != nil {
		t.Fatalf("resolvePromptBase: %v", err)
	}
	if prompt != "generic variant prompt" {
		t.Fatalf("prompt=%q want generic variant prompt", prompt)
	}
	if source != "latest_variant_target_profile" {
		t.Fatalf("source=%q want latest_variant_target_profile", source)
	}
}

func TestResolveAgentForPromptPromotion(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg, err := config.Load(ctx, config.WithWorkspacePath(tmp))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	agentStore, err := storagents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("agents.Open: %v", err)
	}
	defer agentStore.Close() //nolint:errcheck

	entries := []agent.Agent{
		{
			ID:            "agent-older",
			Namespace:     "ns/older",
			Role:          "coder",
			Prompt:        "older prompt",
			WorkspaceRoot: tmp,
			ShareBB:       "scoped",
			State:         agent.StateRunning,
			CreatedAt:     time.Now().Add(-1 * time.Hour).UTC(),
		},
		{
			ID:            "agent-newer",
			Namespace:     "ns/newer",
			Role:          "coder",
			Prompt:        "newer prompt",
			WorkspaceRoot: tmp,
			ShareBB:       "scoped",
			State:         agent.StateRunning,
			CreatedAt:     time.Now().UTC(),
		},
	}
	for _, entry := range entries {
		if err := agentStore.Create(ctx, entry); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := resolveAgentForPromptPromotion(ctx, agentStore, tmp, "coder", "")
	if err != nil {
		t.Fatalf("resolveAgentForPromptPromotion: %v", err)
	}
	if got.ID != "agent-newer" {
		t.Fatalf("id=%q want agent-newer", got.ID)
	}

	got, err = resolveAgentForPromptPromotion(ctx, agentStore, tmp, "coder", "agent-older")
	if err != nil {
		t.Fatalf("resolveAgentForPromptPromotion explicit: %v", err)
	}
	if got.ID != "agent-older" {
		t.Fatalf("explicit id=%q want agent-older", got.ID)
	}
}
