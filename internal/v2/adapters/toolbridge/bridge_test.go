package toolbridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	classictools "github.com/jkatigb/agentctl/internal/agent/tools"
	"github.com/jkatigb/agentctl/internal/context/contextplane"
	sysconfig "github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	coretool "github.com/jkatigb/agentctl/internal/v2/core/tool"
)

func TestDelegateContextShow(t *testing.T) {
	workspaceRoot := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspaceRoot)
	if _, err := store.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: workspaceRoot,
		Objective:   "Test ACA",
		Phase:       "design",
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	delegate, err := NewDelegate(Config{
		WorkspaceRoot: workspaceRoot,
		AppConfig: sysconfig.Config{
			Storage: sysconfig.StorageSettings{Root: t.TempDir()},
		},
	})
	if err != nil {
		t.Fatalf("NewDelegate: %v", err)
	}

	result, err := delegate.Execute(context.Background(), "context/show", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, `"objective":"Test ACA"`) {
		t.Fatalf("unexpected output: %s", result.Output)
	}
}

func TestDelegateObsidianReadAndIndexSearch(t *testing.T) {
	workspaceRoot := t.TempDir()
	storageRoot := t.TempDir()
	vaultRoot := filepath.Join(t.TempDir(), "vault")
	notePath := filepath.Join(vaultRoot, "notes", "repo", "agentctl", "index.md")
	if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	content := "---\ntitle: agentctl Repo Graph\ntype: map\nproject: agentctl\nstatus: reviewed\ntrust: canonical\n---\n\n# agentctl Repo Graph\n\nACA bridge.\n"
	if err := os.WriteFile(notePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	cfg := sysconfig.Config{
		Storage: sysconfig.StorageSettings{Root: storageRoot},
	}
	delegate, err := NewDelegate(Config{
		AppConfig:     cfg,
		WorkspaceRoot: workspaceRoot,
		VaultPath:     vaultRoot,
	})
	if err != nil {
		t.Fatalf("NewDelegate: %v", err)
	}

	readArgs, _ := json.Marshal(map[string]any{
		"path":       "notes/repo/agentctl/index.md",
		"vault_path": vaultRoot,
	})
	readResult, err := delegate.Execute(context.Background(), "obsidian/read", readArgs)
	if err != nil {
		t.Fatalf("obsidian/read Execute: %v", err)
	}
	if !strings.Contains(readResult.Output, "agentctl Repo Graph") {
		t.Fatalf("unexpected read output: %s", readResult.Output)
	}

	indexStore, err := obsidianindex.Open(context.Background(), storageRoot, vaultRoot)
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer func() { _ = indexStore.Close() }()
	if _, err := indexStore.Rebuild(context.Background(), vaultRoot); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	searchArgs, _ := json.Marshal(map[string]any{
		"query":      "Repo Graph",
		"vault_path": vaultRoot,
		"limit":      5,
	})
	searchResult, err := delegate.Execute(context.Background(), "obsidian/index_search", searchArgs)
	if err != nil {
		t.Fatalf("obsidian/index_search Execute: %v", err)
	}
	if !strings.Contains(searchResult.Output, "notes/repo/agentctl/index.md") {
		t.Fatalf("unexpected search output: %s", searchResult.Output)
	}
}

func TestDelegateFallsBackToClassicRegistry(t *testing.T) {
	workspaceRoot := t.TempDir()
	filePath := filepath.Join(workspaceRoot, "README.md")
	if err := os.WriteFile(filePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	registry, err := classictools.NewRegistry(classictools.Config{
		WorkspaceRoot: workspaceRoot,
		WorkspaceID:   workspaceRoot,
	}, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	delegate, err := NewDelegate(Config{
		WorkspaceRoot:   workspaceRoot,
		ClassicRegistry: registry,
		AppConfig: sysconfig.Config{
			Storage: sysconfig.StorageSettings{Root: t.TempDir()},
		},
	})
	if err != nil {
		t.Fatalf("NewDelegate: %v", err)
	}

	args, _ := json.Marshal(map[string]any{"path": "README.md"})
	result, err := delegate.Execute(context.Background(), "fs/read_file", args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Fatalf("unexpected output: %s", result.Output)
	}
}

func TestNewDefaultExecutorUsesProfileGatingAndACADelegate(t *testing.T) {
	workspaceRoot := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspaceRoot)
	if _, err := store.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: workspaceRoot,
		Objective:   "Profile-gated ACA",
		Phase:       "design",
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	exec, err := NewDefaultExecutor(coretool.ProfileCompanion, nil, Config{
		WorkspaceRoot: workspaceRoot,
		AppConfig: sysconfig.Config{
			Storage: sysconfig.StorageSettings{Root: t.TempDir()},
		},
	})
	if err != nil {
		t.Fatalf("NewDefaultExecutor: %v", err)
	}

	result, err := exec.Execute(context.Background(), "context/show", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, `"objective":"Profile-gated ACA"`) {
		t.Fatalf("unexpected output: %s", result.Output)
	}
}
