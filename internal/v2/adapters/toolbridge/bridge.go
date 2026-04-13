package toolbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	classictools "github.com/jkatigb/agentctl/internal/agent/tools"
	"github.com/jkatigb/agentctl/internal/context/contextplane"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	sysconfig "github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/jkatigb/agentctl/internal/tooling"
	obsidiantool "github.com/jkatigb/agentctl/internal/tooling/tools/obsidian"
	sourceimport "github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
	coretool "github.com/jkatigb/agentctl/internal/v2/core/tool"
	"github.com/jkatigb/agentctl/internal/v2/runtime/profiles"
	runner "github.com/jkatigb/agentctl/internal/v2/runtime/runner"
	runtimetoolnames "github.com/jkatigb/agentctl/internal/v2/runtime/toolnames"
	runtimetools "github.com/jkatigb/agentctl/internal/v2/runtime/tools"
)

type Config struct {
	AppConfig         sysconfig.Config
	WorkspaceRoot     string
	WorkspaceID       string
	VaultPath         string
	IncludeExtensions bool
	ClassicRegistry   *classictools.Registry
}

func NewDefaultCatalog(specs map[coretool.ProcessProfile]profiles.ProfileSpec, includeExtensions bool) (*runtimetools.Catalog, error) {
	return runtimetools.NewDefaultCatalog(specs, includeExtensions)
}

func NewDefaultCatalogAndDelegate(specs map[coretool.ProcessProfile]profiles.ProfileSpec, cfg Config) (*runtimetools.Catalog, *Delegate, error) {
	catalog, err := runtimetools.NewDefaultCatalog(specs, cfg.IncludeExtensions)
	if err != nil {
		return nil, nil, err
	}
	delegate, err := NewDelegate(cfg)
	if err != nil {
		return nil, nil, err
	}
	return catalog, delegate, nil
}

// NewDefaultExecutor builds a production-ready v2 runtime executor for one
// process profile from the default tool definitions and this adapter delegate.
func NewDefaultExecutor(
	profile coretool.ProcessProfile,
	specs map[coretool.ProcessProfile]profiles.ProfileSpec,
	cfg Config,
) (*runtimetools.Executor, error) {
	catalog, delegate, err := NewDefaultCatalogAndDelegate(specs, cfg)
	if err != nil {
		return nil, err
	}
	return runtimetools.NewExecutor(catalog, profile, delegate), nil
}

type Delegate struct {
	cfg            Config
	classicByCanon map[string]toolEntry
}

func NewDelegate(cfg Config) (*Delegate, error) {
	d := &Delegate{
		cfg:            cfg,
		classicByCanon: map[string]toolEntry{},
	}
	if cfg.ClassicRegistry != nil {
		for _, tool := range cfg.ClassicRegistry.List() {
			canon := runtimetoolnames.Canonical(tool.Name())
			if canon == "" {
				continue
			}
			d.classicByCanon[canon] = toolEntry{name: tool.Name(), tool: tool}
		}
	}
	return d, nil
}

func (d *Delegate) Execute(ctx context.Context, name string, args json.RawMessage) (runner.ToolResult, error) {
	canonical := runtimetoolnames.Canonical(name)
	argsMap := map[string]any{}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argsMap); err != nil {
			return runner.ToolResult{}, fmt.Errorf("parse tool args: %w", err)
		}
	}
	switch canonical {
	case "context_show":
		out, err := d.contextShow()
		if err != nil {
			return runner.ToolResult{}, err
		}
		return runner.ToolResult{Status: "ok", Output: out}, nil
	case "context_retrieve":
		out, err := d.contextRetrieve(ctx, argsMap)
		if err != nil {
			return runner.ToolResult{}, err
		}
		return runner.ToolResult{Status: "ok", Output: out}, nil
	case "obsidian_index_search":
		out, err := d.obsidianIndexSearch(ctx, argsMap)
		if err != nil {
			return runner.ToolResult{}, err
		}
		return runner.ToolResult{Status: "ok", Output: out}, nil
	case "obsidian_read":
		out, err := d.obsidianRead(ctx, argsMap)
		if err != nil {
			return runner.ToolResult{}, err
		}
		return runner.ToolResult{Status: "ok", Output: out}, nil
	case "obsidian_related":
		out, err := d.obsidianRelated(ctx, argsMap)
		if err != nil {
			return runner.ToolResult{}, err
		}
		return runner.ToolResult{Status: "ok", Output: out}, nil
	}
	entry, ok := d.classicByCanon[canonical]
	if !ok {
		return runner.ToolResult{}, fmt.Errorf("no delegate for tool %q", name)
	}
	out, err := d.executeClassicTool(ctx, entry, argsMap)
	if err != nil {
		return runner.ToolResult{}, err
	}
	return runner.ToolResult{Status: "ok", Output: out}, nil
}

type toolEntry struct {
	name string
	tool tooling.Tool
}

func (d *Delegate) executeClassicTool(ctx context.Context, entry toolEntry, args map[string]any) (string, error) {
	if entry.tool == nil {
		return "", fmt.Errorf("classic tool %q not configured", entry.name)
	}
	result, err := entry.tool.Call(classictools.WithHookDispatch(ctx), args)
	if err != nil {
		return "", err
	}
	if result == nil || len(result.Content) == 0 {
		return "", nil
	}
	for _, content := range result.Content {
		if tc, ok := content.(models.TextContent); ok {
			return tc.Text, nil
		}
	}
	body, err := json.Marshal(result.Content)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (d *Delegate) contextShow() (string, error) {
	store := contextplane.NewWorkspaceStore(d.cfg.WorkspaceRoot)
	top, err := store.LoadTopOfMind()
	if err != nil {
		return "", err
	}
	return marshalText(map[string]any{
		"workspace_path": d.cfg.WorkspaceRoot,
		"top_of_mind":    top,
	})
}

func (d *Delegate) contextRetrieve(ctx context.Context, args map[string]any) (string, error) {
	query := stringArg(args, "query")
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	vaultPath := d.resolveVaultPath(args)
	if vaultPath == "" {
		return "", fmt.Errorf("vault_path is required")
	}
	index, err := obsidianindex.Open(ctx, d.cfg.AppConfig.Storage.Root, vaultPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = index.Close() }()
	repo, err := repoindex.Open(ctx, d.cfg.AppConfig.Storage.Root, d.cfg.WorkspaceRoot)
	if err != nil {
		return "", err
	}
	defer func() { _ = repo.Close() }()
	store := contextplane.NewWorkspaceStore(d.cfg.WorkspaceRoot)
	result, err := store.Retrieve(ctx, index, repo, d.openObsidianSemanticProvider(), query, intArg(args, 5, "limit"))
	if err != nil {
		return "", err
	}
	return marshalText(map[string]any{
		"workspace_path": d.cfg.WorkspaceRoot,
		"vault_path":     vaultPath,
		"result":         result,
	})
}

func (d *Delegate) obsidianIndexSearch(ctx context.Context, args map[string]any) (string, error) {
	query := stringArg(args, "query")
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	vaultPath := d.resolveVaultPath(args)
	if vaultPath == "" {
		return "", fmt.Errorf("vault_path is required")
	}
	index, err := obsidianindex.Open(ctx, d.cfg.AppConfig.Storage.Root, vaultPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = index.Close() }()
	limit := intArg(args, 10, "limit")
	semanticSearch := boolArg(args, "semantic")
	var hits []obsidianindex.SearchHit
	if semanticSearch {
		provider := d.openObsidianSemanticProvider()
		if provider == nil {
			return "", fmt.Errorf("semantic note search requires configured embedding provider")
		}
		hits, err = index.SearchNotesSemantic(ctx, query, provider, limit)
	} else {
		hits, err = index.SearchNotes(ctx, query, limit)
	}
	if err != nil {
		return "", err
	}
	return marshalText(map[string]any{
		"vault_path": vaultPath,
		"semantic":   semanticSearch,
		"hits":       hits,
		"count":      len(hits),
	})
}

func (d *Delegate) obsidianRead(ctx context.Context, args map[string]any) (string, error) {
	path := stringArg(args, "path")
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	vaultPath := d.resolveVaultPath(args)
	if vaultPath == "" {
		return "", fmt.Errorf("vault_path is required")
	}
	writer := obsidiantool.NewWriter("", "", obsidiantool.DefaultPolicy())
	writer.VaultPath = vaultPath
	content, err := writer.Read(ctx, path)
	if err != nil {
		return "", err
	}
	return marshalText(map[string]any{
		"result": map[string]any{
			"vault_path": vaultPath,
			"note_path":  path,
			"content":    content,
		},
	})
}

func (d *Delegate) obsidianRelated(ctx context.Context, args map[string]any) (string, error) {
	path := stringArg(args, "path")
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	vaultPath := d.resolveVaultPath(args)
	if vaultPath == "" {
		return "", fmt.Errorf("vault_path is required")
	}
	limit := intArg(args, 10, "limit")
	index, err := obsidianindex.Open(ctx, d.cfg.AppConfig.Storage.Root, vaultPath)
	if err == nil {
		defer func() { _ = index.Close() }()
		if stats, statsErr := index.Stats(ctx); statsErr == nil && stats.Notes > 0 {
			hits, relErr := index.RelatedNotes(ctx, path, limit)
			if relErr == nil {
				return marshalText(map[string]any{
					"vault_path": vaultPath,
					"path":       path,
					"results":    hits,
				})
			}
		}
	}
	hits, err := obsidiantool.RelatedNotes(vaultPath, path, obsidiantool.LinkQueryOptions{
		Depth:         1,
		IncludeDirect: true,
		IncludeBack:   true,
		IncludeAlias:  true,
		Limit:         limit,
	})
	if err != nil {
		return "", err
	}
	return marshalText(map[string]any{
		"vault_path": vaultPath,
		"path":       path,
		"results":    hits,
	})
}

func (d *Delegate) resolveVaultPath(args map[string]any) string {
	if value := stringArg(args, "vault_path"); strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if strings.TrimSpace(d.cfg.VaultPath) != "" {
		return strings.TrimSpace(d.cfg.VaultPath)
	}
	return strings.TrimSpace(firstNonEmpty(
		d.lookupEnv("AGENTCTL_ACA_VAULT_PATH"),
		d.lookupEnv("AGENTCTL_OBSIDIAN_VAULT_PATH"),
	))
}

func (d *Delegate) lookupEnv(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func (d *Delegate) openObsidianSemanticProvider() semantic.EmbeddingProvider {
	if !obsidianSemanticEnabled(d.cfg.AppConfig) {
		return nil
	}
	if provider := d.openOpenAICompatSemanticProvider(); provider != nil {
		return provider
	}
	provider, err := semantic.NewProviderForScope(
		semantic.ScopeMemory,
		d.cfg.AppConfig,
		semantic.WithVoyageKey(d.lookupEnv("VOYAGE_API_KEY")),
		semantic.WithGeminiKey(d.lookupEnv("GEMINI_API_KEY")),
	)
	if err != nil {
		return nil
	}
	return provider
}

func obsidianSemanticEnabled(cfg sysconfig.Config) bool {
	if value, ok := lookupEnvBool("AGENTCTL_OBSIDIAN_SEMANTIC_ENABLED"); ok {
		return value
	}
	if strings.TrimSpace(os.Getenv("AGENTCTL_EMBEDDING_BASE_URL")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("AGENTCTL_EMBEDDING_MODEL")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("AGENTCTL_OPENAI_COMPAT_BASE_URL")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("AGENTCTL_OPENAI_COMPAT_EMBEDDING_MODEL")) != "" {
		return true
	}
	if strings.TrimSpace(cfg.Embedding.Provider) == "openai_compat" || strings.TrimSpace(cfg.Embedding.Provider) == "lmstudio" || strings.TrimSpace(cfg.Embedding.Provider) == "openai-compatible" {
		return true
	}
	return false
}

func (d *Delegate) openOpenAICompatSemanticProvider() semantic.EmbeddingProvider {
	providerName := strings.ToLower(strings.TrimSpace(d.cfg.AppConfig.Embedding.Provider))
	if providerName != "lmstudio" && providerName != "openai_compat" && providerName != "openai-compatible" {
		return nil
	}
	embedder, resolved, err := sourceimport.NewEmbedderFromConfig(sourceimport.EmbedderConfig{
		Provider: providerNameToSourceImport(providerName),
		Model: firstNonEmpty(
			strings.TrimSpace(d.lookupEnv("AGENTCTL_EMBEDDING_MODEL")),
			strings.TrimSpace(d.lookupEnv("AGENTCTL_OPENAI_COMPAT_EMBEDDING_MODEL")),
			d.cfg.AppConfig.Embedding.Model,
		),
		BaseURL: firstNonEmpty(
			strings.TrimSpace(d.lookupEnv("AGENTCTL_EMBEDDING_BASE_URL")),
			strings.TrimSpace(d.lookupEnv("AGENTCTL_OPENAI_COMPAT_BASE_URL")),
			d.cfg.AppConfig.Embedding.BaseURL,
		),
		APIKey: strings.TrimSpace(firstNonEmpty(
			d.lookupEnv("AGENTCTL_EMBEDDING_API_KEY"),
			d.lookupEnv("AGENTCTL_OPENAI_COMPAT_API_KEY"),
			d.cfg.AppConfig.Embedding.APIKey,
		)),
	})
	if err != nil {
		return nil
	}
	return &openAICompatSemanticProvider{
		inner: embedder,
		model: resolved.Model,
		dims:  resolved.Dimensions,
	}
}

func providerNameToSourceImport(name string) string {
	switch name {
	case "openai_compat", "openai-compatible":
		return sourceimport.EmbeddingProviderOpenAICompat
	default:
		return name
	}
}

type openAICompatSemanticProvider struct {
	inner sourceimport.Embedder
	model string
	dims  int
}

func (p *openAICompatSemanticProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	res, err := p.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	return res.Vector, nil
}

func (p *openAICompatSemanticProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec, err := p.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, vec)
	}
	return out, nil
}

func (p *openAICompatSemanticProvider) Model() string {
	return strings.TrimSpace(p.model)
}

func (p *openAICompatSemanticProvider) Dimensions() int {
	return p.dims
}

func marshalText(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func stringArg(args map[string]any, key string) string {
	if value, ok := args[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func intArg(args map[string]any, fallback int, key string) int {
	if raw, ok := args[key].(float64); ok && raw > 0 {
		return int(raw)
	}
	return fallback
}

func boolArg(args map[string]any, key string) bool {
	if raw, ok := args[key].(bool); ok {
		return raw
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func lookupEnvBool(name string) (bool, bool) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false, false
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}
