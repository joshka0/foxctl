package taskhistory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/context/transcriptpipeline"
	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	platformcfg "github.com/jkatigb/agentctl/internal/platform/config"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
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
	memoryDB, err := memorystore.Open(ctx, storageRoot, "")
	if err != nil {
		_ = taskDB.Close()
		_ = sessionDB.Close()
		return Collector{}, nil, fmt.Errorf("open memory store: %w", err)
	}
	repo, err := repoindex.Open(ctx, storageRoot, workspacePath)
	if err != nil {
		_ = taskDB.Close()
		_ = sessionDB.Close()
		_ = memoryDB.Close()
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
			_ = memoryDB.Close()
			return Collector{}, nil, fmt.Errorf("open obsidian index: %w", err)
		}
		index = idx
	}

	cleanup := func() {
		if index != nil {
			_ = index.Close()
		}
		_ = repo.Close()
		_ = memoryDB.Close()
		_ = sessionDB.Close()
		_ = taskDB.Close()
	}

	var semanticProvider semantic.EmbeddingProvider
	var transcriptWorker *TranscriptSummaryWorker
	if cfg, ok := platformcfg.FromContext(ctx); ok {
		if provider, err := semantic.NewProviderForScope(semantic.ScopeMemory, cfg); err == nil {
			semanticProvider = provider
		}
		transcriptWorker = TranscriptSummaryWorkerConfig(cfg, "", "")
	}

	return Collector{
		WorkspaceStore:   contextplane.NewWorkspaceStore(workspacePath),
		TaskStore:        taskDB,
		SessionStore:     sessionDB,
		MemoryStore:      memoryDB,
		RepoStore:        repo,
		VaultIndex:       index,
		SemanticProvider: semanticProvider,
		TranscriptWorker: transcriptWorker,
		GitRunner:        DefaultGitRunner{},
	}, cleanup, nil
}

func TranscriptSummaryWorkerConfig(cfg platformcfg.Config, providerOverride, modelOverride string) *TranscriptSummaryWorker {
	provider := strings.ToLower(strings.TrimSpace(providerOverride))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(os.Getenv("AGENTCTL_TRANSCRIPT_SUMMARY_PROVIDER")))
	}
	if provider == "" {
		provider = "lmstudio"
		if strings.TrimSpace(cfg.LLM.ResolveAPIKey("openrouter")) != "" {
			provider = "openrouter"
		}
	}
	model := strings.TrimSpace(modelOverride)
	if model == "" {
		model = strings.TrimSpace(os.Getenv("AGENTCTL_TRANSCRIPT_SUMMARY_MODEL"))
	}
	if model == "" {
		model = strings.TrimSpace(cfg.LLM.ResolveModel(provider))
	}
	if model == "" {
		model = llmproviders.DefaultModelForProvider(provider)
	}
	runConfig := transcriptpipeline.WorkerConfig{
		Provider:         provider,
		APIKey:           strings.TrimSpace(cfg.LLM.ResolveAPIKey(provider)),
		AuthMode:         strings.TrimSpace(cfg.LLM.ResolveAuthMode(provider)),
		AuthHeader:       strings.TrimSpace(cfg.LLM.ResolveAuthHeader(provider)),
		AuthPrefix:       cfg.LLM.ResolveAuthPrefix(provider),
		Model:            model,
		BaseURL:          cfg.LLM.ResolveBaseURL(provider),
		MaxContextTokens: transcriptpipeline.DefaultMaxContextTokens,
		Timeout:          20 * time.Second,
	}
	return &TranscriptSummaryWorker{
		Provider:  provider,
		Model:     model,
		runConfig: runConfig,
	}
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

func DefaultTranscriptHistoryScope() TranscriptHistoryScope {
	if raw := strings.TrimSpace(os.Getenv("AGENTCTL_TRANSCRIPT_HISTORY_SCOPE")); raw != "" {
		if scope, err := ParseTranscriptHistoryScope(raw); err == nil {
			return scope
		}
	}
	return TranscriptHistoryScopeAuto
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

func PersistValue(ctx context.Context, casRoot string, value any, tag string) (string, error) {
	casRoot = strings.TrimSpace(casRoot)
	if casRoot == "" {
		return "", nil
	}
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	tags := []string{}
	if strings.TrimSpace(tag) != "" {
		tags = append(tags, strings.TrimSpace(tag))
	}
	obj, err := store.Put(ctx, bytes.NewReader(append(body, '\n')), "application/json", tags)
	if err != nil {
		return "", err
	}
	return obj.Digest, nil
}
