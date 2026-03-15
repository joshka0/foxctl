package taskhistory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
)

func OpenCollector(ctx context.Context, storageRoot, workspacePath, vaultPath string) (Collector, func(), error) {
	taskDB, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		return Collector{}, nil, fmt.Errorf("open task store: %w", err)
	}
	sessionDB, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		_ = taskDB.Close()
		return Collector{}, nil, fmt.Errorf("open session store: %w", err)
	}
	repo, err := repoindex.Open(ctx, storageRoot, workspacePath)
	if err != nil {
		_ = taskDB.Close()
		_ = sessionDB.Close()
		return Collector{}, nil, fmt.Errorf("open repo index: %w", err)
	}

	var index obsidianindex.Store
	resolvedVault := resolveVaultPath(vaultPath)
	if resolvedVault != "" {
		idx, err := obsidianindex.Open(ctx, storageRoot, resolvedVault)
		if err != nil {
			_ = repo.Close()
			_ = taskDB.Close()
			_ = sessionDB.Close()
			return Collector{}, nil, fmt.Errorf("open obsidian index: %w", err)
		}
		index = idx
	}

	cleanup := func() {
		if index != nil {
			_ = index.Close()
		}
		_ = repo.Close()
		_ = sessionDB.Close()
		_ = taskDB.Close()
	}

	return Collector{
		WorkspaceStore: contextplane.NewWorkspaceStore(workspacePath),
		TaskStore:      taskDB,
		SessionStore:   sessionDB,
		RepoStore:      repo,
		VaultIndex:     index,
		GitRunner:      DefaultGitRunner{},
	}, cleanup, nil
}

func resolveVaultPath(explicit string) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	for _, key := range []string{"AGENTCTL_ACA_VAULT_PATH", "AGENTCTL_OBSIDIAN_VAULT_PATH", "AGENTCTL_RLM_VAULT_PATH"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func PersistPack(ctx context.Context, casRoot string, pack Pack) (string, error) {
	casRoot = strings.TrimSpace(casRoot)
	if casRoot == "" {
		return "", nil
	}
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()
	body, err := json.MarshalIndent(pack, "", "  ")
	if err != nil {
		return "", err
	}
	obj, err := store.Put(ctx, bytes.NewReader(append(body, '\n')), "application/json", []string{"task-continuity-pack"})
	if err != nil {
		return "", err
	}
	return obj.Digest, nil
}
