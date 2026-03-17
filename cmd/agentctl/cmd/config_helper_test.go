package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestLoadConfigReloadsWorkspaceOverridesOverContextConfig(t *testing.T) {
	oldHome := os.Getenv("HOME")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Cleanup(func() {
		if oldHome == "" {
			_ = os.Unsetenv("HOME")
		} else {
			_ = os.Setenv("HOME", oldHome)
		}
	})

	home := filepath.Join(tmp, ".agentctl")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	globalConfig := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(globalConfig, []byte(`
llm:
  model: global-model
embedding:
  provider: global-embed
  model: global-embed-model
`), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	workspace := filepath.Join(tmp, "foxway")
	if err := os.MkdirAll(filepath.Join(workspace, ".agentctl"), 0o755); err != nil {
		t.Fatalf("mkdir workspace config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".agentctl", "config.yaml"), []byte(`
embedding:
  provider: workspace-embed
  model: workspace-embed-model
`), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	baseCfg, err := config.Load(context.Background())
	if err != nil {
		t.Fatalf("load base config: %v", err)
	}
	if baseCfg.Embedding.Provider != "global-embed" {
		t.Fatalf("base embedding provider = %q, want global-embed", baseCfg.Embedding.Provider)
	}

	ctx := config.WithContext(context.Background(), baseCfg)
	reloaded, err := loadConfig(ctx, config.WithWorkspacePath(workspace))
	if err != nil {
		t.Fatalf("loadConfig with workspace: %v", err)
	}
	if reloaded.Embedding.Provider != "workspace-embed" {
		t.Fatalf("workspace embedding provider = %q, want workspace-embed", reloaded.Embedding.Provider)
	}
	if reloaded.Embedding.Model != "workspace-embed-model" {
		t.Fatalf("workspace embedding model = %q, want workspace-embed-model", reloaded.Embedding.Model)
	}
}
