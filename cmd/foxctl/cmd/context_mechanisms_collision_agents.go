package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/memoryblur"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/spf13/cobra"
)

const (
	defaultMemoryCollisionAgentCount  = 2
	maxMemoryCollisionAgentCount      = 3
	maxMemoryCollisionAgentRuns       = 24
	defaultMemoryCollisionPoolLimit   = 20
	defaultMemoryDomainMinDistance    = 2
	defaultMemoryCollisionConcurrency = 3
	maxMemoryCollisionConcurrency     = 6
)

type repoSymbolCollisionAgentsOptions struct {
	WorkspacePath     string
	WorkspaceID       string
	MaxSymbols        int
	PerNodeCap        int
	Query             mechanismCandidateFilter
	CandidateLimit    int
	CollisionLimit    int
	MaxAgents         int
	DomainDiversity   bool
	MinDomainDistance int
	Entropy           float64
	Threshold         float64
	IncludeSameDomain bool
	BisociationMode   string
	AgentRuns         []string
	AgentConcurrency  int
	AgentProvider     string
	AgentModel        string
	PiMode            string
	PiBin             string
	PiSDKBin          string
	PiSDKScript       string
	PiSDKCWD          string
	PiAgentDir        string
	PiThinking        string
	PiNoExtensions    bool
	Timeout           time.Duration
	VaultPath         string
	WriteCache        bool
	IncludeCache      bool
	CompareModes      bool
	IncludePrompt     bool
	IncludeRaw        bool
}

type repoSymbolCollisionAgentView struct {
	AgentIndex        int                                          `json:"agent_index"`
	AgentRole         string                                       `json:"agent_role"`
	AgentProvider     string                                       `json:"agent_provider,omitempty"`
	AgentModel        string                                       `json:"agent_model,omitempty"`
	AgentRunLabel     string                                       `json:"agent_run_label,omitempty"`
	BisociationMode   string                                       `json:"bisociation_mode,omitempty"`
	SelectionMode     string                                       `json:"selection_mode,omitempty"`
	PromptAbstraction string                                       `json:"prompt_abstraction,omitempty"`
	Collision         contextplane.MemoryCollisionCell             `json:"collision"`
	PromptInput       contextplane.MemoryCollisionAgentPromptInput `json:"prompt_input"`
	AgentOutput       contextplane.MemoryCollisionAgentOutput      `json:"agent_output,omitempty"`
	Validation        contextplane.MemoryCollisionAgentValidation  `json:"validation"`
	Prompt            string                                       `json:"prompt,omitempty"`
	RawAgentOutput    string                                       `json:"raw_agent_output,omitempty"`
	Error             string                                       `json:"error,omitempty"`
}

func newContextMechanismsRepoSymbolsCollideMemoryAgentsCommand() *cobra.Command {
	opts := repoSymbolCollisionAgentsOptions{
		MaxSymbols:        defaultContextMechanismMaxSymbols,
		PerNodeCap:        200,
		MaxAgents:         defaultMemoryCollisionAgentCount,
		DomainDiversity:   true,
		MinDomainDistance: defaultMemoryDomainMinDistance,
		AgentConcurrency:  defaultMemoryCollisionConcurrency,
		Entropy:           0.35,
		Threshold:         0.70,
		BisociationMode:   contextplane.MemoryCollisionAgentModeBalanced,
		PiMode:            memoryblur.PiModeSDK,
		PiBin:             "pi",
		PiSDKBin:          "bun",
		PiThinking:        "off",
		PiNoExtensions:    true,
		Timeout:           2 * time.Minute,
	}
	cmd := &cobra.Command{
		Use:   "collide-memory-agents",
		Short: "Ask bounded Pi agents to synthesize new collisions from persisted mechanism matches",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRepoSymbolsCollideMemoryAgents(cmd.Context(), cmd, opts)
		},
	}

	addContextMechanismRepoSymbolFlags(cmd, &opts.WorkspacePath, &opts.WorkspaceID, &opts.MaxSymbols, &opts.PerNodeCap)
	cmd.Flags().StringVar(&opts.Query.SymbolID, "query-symbol-id", "", "Query symbol ID; accepts repoindex node ID, raw symbol key, or embedding symbol ID")
	cmd.Flags().StringVar(&opts.Query.Name, "query-name", "", "Exact query symbol name")
	cmd.Flags().StringVar(&opts.Query.File, "query-file", "", "Exact query symbol file")
	cmd.Flags().IntVar(&opts.CandidateLimit, "candidate-limit", 0, "Maximum persisted structural candidates to rehydrate before planning")
	cmd.Flags().IntVar(&opts.CollisionLimit, "collision-limit", 0, "Maximum collision cells in the diversity pool before agent fan-out")
	cmd.Flags().IntVar(&opts.MaxAgents, "max-agents", defaultMemoryCollisionAgentCount, "Maximum Pi agents to run for one query (capped at 3)")
	cmd.Flags().BoolVar(&opts.DomainDiversity, "domain-diversity", true, "Prefer distinct and farther memory domains when selecting agent collision packets")
	cmd.Flags().IntVar(&opts.MinDomainDistance, "min-domain-distance", defaultMemoryDomainMinDistance, "Preferred package/domain path distance from the query domain during diverse selection")
	cmd.Flags().Float64Var(&opts.Entropy, "entropy", 0.35, "Literal-similarity penalty and structural boost factor")
	cmd.Flags().Float64Var(&opts.Threshold, "threshold", 0.70, "Minimum collision score")
	cmd.Flags().BoolVar(&opts.IncludeSameDomain, "include-same-domain", false, "Allow same-domain collisions")
	cmd.Flags().StringVar(&opts.BisociationMode, "bisociation-mode", contextplane.MemoryCollisionAgentModeBalanced, "Bisociation preset (balanced|far|alien|far-alien)")
	cmd.Flags().StringArrayVar(&opts.AgentRuns, "agent-run", nil, "Explicit model run spec provider/model=count; repeatable, e.g. --agent-run openrouter/vendor/model=5")
	cmd.Flags().IntVar(&opts.AgentConcurrency, "agent-concurrency", defaultMemoryCollisionConcurrency, "Maximum model agents to run concurrently")
	cmd.Flags().StringVar(&opts.AgentProvider, "agent-provider", "", "Optional Pi provider override")
	cmd.Flags().StringVar(&opts.AgentModel, "agent-model", "", "Optional Pi model override")
	cmd.Flags().StringVar(&opts.PiMode, "pi-mode", memoryblur.PiModeSDK, "Pi runner mode (sdk|cli)")
	cmd.Flags().StringVar(&opts.PiBin, "pi-bin", "pi", "Executable for Pi CLI runner")
	cmd.Flags().StringVar(&opts.PiSDKBin, "pi-sdk-bin", "bun", "Executable for Pi SDK runner")
	cmd.Flags().StringVar(&opts.PiSDKScript, "pi-sdk-script", "", "Pi SDK JSON runner script path")
	cmd.Flags().StringVar(&opts.PiSDKCWD, "pi-sdk-cwd", "", "Working directory passed to Pi SDK session")
	cmd.Flags().StringVar(&opts.PiAgentDir, "pi-agent-dir", "", "Pi agent directory for auth/models; defaults to Pi SDK default")
	cmd.Flags().StringVar(&opts.PiThinking, "pi-thinking", "off", "Pi SDK thinking level")
	cmd.Flags().BoolVar(&opts.PiNoExtensions, "pi-no-extensions", true, "Disable Pi extension discovery for collision agent runs")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", 2*time.Minute, "Per-agent call timeout")
	cmd.Flags().StringVar(&opts.VaultPath, "vault-path", "", "Vault path for Obsidian collision cache notes")
	cmd.Flags().BoolVar(&opts.WriteCache, "write-cache", false, "Write successful collision syntheses as a ContextWiki/Obsidian cache note")
	cmd.Flags().BoolVar(&opts.IncludeCache, "include-collision-cache", false, "Include prior Obsidian collision cache syntheses for this query as secondary candidates")
	cmd.Flags().BoolVar(&opts.CompareModes, "compare-modes", false, "Compare balanced/far/alien/far-alien collision selection and exit without agent calls")
	cmd.Flags().BoolVar(&opts.IncludePrompt, "include-prompt", false, "Include generated agent prompts in output")
	cmd.Flags().BoolVar(&opts.IncludeRaw, "include-raw", false, "Include raw agent stdout in output")
	return cmd
}

func runRepoSymbolsCollideMemoryAgents(ctx context.Context, cmd *cobra.Command, opts repoSymbolCollisionAgentsOptions) error {
	target := resolveContextWorkspace(opts.WorkspacePath)
	cfg, err := loadConfig(ctx, config.WithWorkspacePath(target))
	if err != nil {
		return err
	}
	result, resolvedWorkspaceID, err := buildRepoSymbolMechanismsForCommand(ctx, cfg, target, opts.WorkspaceID, opts.MaxSymbols, opts.PerNodeCap)
	if err != nil {
		return err
	}
	queryCandidate, ok := selectMechanismQueryCandidate(result.Candidates, opts.Query)
	if !ok {
		return fmt.Errorf("query symbol not found in mechanism candidate corpus; pass --query-name, --query-file, or --query-symbol-id")
	}

	memStore, err := memorystore.OpenWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = memStore.Close() }()

	provider, err := semantic.NewProviderForScope(
		semantic.ScopeMemory,
		cfg,
		semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
	)
	if err != nil {
		return err
	}

	agentTargets, err := memoryCollisionAgentTargetsFromOptions(opts)
	if err != nil {
		return err
	}
	opts.BisociationMode = contextplane.NormalizeMemoryCollisionAgentMode(opts.BisociationMode)
	plannedRuns := expandMemoryCollisionAgentRuns(agentTargets)
	if len(plannedRuns) == 0 {
		return fmt.Errorf("no collision agent runs planned")
	}
	collisionLimit := normalizeCollisionPoolLimit(opts.CollisionLimit, len(plannedRuns))
	if opts.CollisionLimit <= 0 && memoryCollisionSelectionMode(opts.BisociationMode) == "far" {
		collisionLimit = max(collisionLimit, 40)
	}
	searchResult, err := contextplane.SearchMechanismMemoryCollisions(ctx, memStore, provider, queryCandidate.Projection, contextplane.MechanismMemoryCollisionSearchOptions{
		WorkspaceID:       resolvedWorkspaceID,
		CandidateLimit:    opts.CandidateLimit,
		Entropy:           opts.Entropy,
		Threshold:         opts.Threshold,
		Limit:             collisionLimit,
		IncludeSameDomain: opts.IncludeSameDomain,
	})
	if err != nil {
		return err
	}
	cells := searchResult.Plan.Cells
	cacheRecordsLoaded := 0
	cacheCollisionCandidates := 0
	if opts.IncludeCache {
		vaultPath := strings.TrimSpace(opts.VaultPath)
		if vaultPath == "" {
			return fmt.Errorf("--vault-path is required when --include-collision-cache is set")
		}
		cacheRecords, err := contextplane.LoadMemoryCollisionCacheRecords(ctx, vaultPath, contextplane.MemoryCollisionCacheLoadOptions{
			WorkspaceID: resolvedWorkspaceID,
			QueryID:     searchResult.Query.ID,
		})
		if err != nil {
			return err
		}
		cacheCells := contextplane.MemoryCollisionCacheRecordsToCells(resolvedWorkspaceID, searchResult.Query, cacheRecords)
		cacheRecordsLoaded = len(cacheRecords)
		cacheCollisionCandidates = len(cacheCells)
		cells = appendMemoryCollisionCellsDeduped(cells, cacheCells)
	}
	if len(cells) == 0 {
		return fmt.Errorf("no collision cells available for agent synthesis")
	}
	if opts.CompareModes {
		comparison := buildMemoryCollisionModeComparison(searchResult.Query.Domain, cells, len(plannedRuns), opts)
		return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/mechanisms_repo_symbols_collide_memory_agents_compare_modes", map[string]any{
			"workspace_path":             target,
			"workspace_id":               resolvedWorkspaceID,
			"query":                      mechanismCandidateViewFor(queryCandidate, true, false),
			"collision_pool_count":       len(cells),
			"structural_candidates":      searchResult.StructuralCandidates,
			"memories_loaded":            searchResult.MemoriesLoaded,
			"skipped_pairs":              searchResult.SkippedPairs,
			"skipped":                    searchResult.Plan.Skipped,
			"include_collision_cache":    opts.IncludeCache,
			"cache_records_loaded":       cacheRecordsLoaded,
			"cache_collision_candidates": cacheCollisionCandidates,
			"agent_targets":              agentTargets,
			"planned_agent_runs":         len(plannedRuns),
			"embedding_provider_model":   provider.Model(),
			"mode_comparison":            comparison,
			"cache_note_written":         false,
			"read_only":                  true,
		}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
	}
	rankedCells := rankMemoryCollisionCellsForBisociationMode(searchResult.Query.Domain, cells, opts.BisociationMode)
	selectedCells, selection := selectMemoryCollisionCellsForAgents(searchResult.Query.Domain, rankedCells, len(plannedRuns), opts)
	if len(selectedCells) == 0 {
		return fmt.Errorf("no collision cells selected for agent synthesis")
	}
	agentRuns := assignMemoryCollisionAgentCells(plannedRuns, selectedCells)

	agentViews := runCollisionSynthesisAgentRuns(ctx, searchResult.Query, agentRuns, opts)
	successes := 0
	errors := 0
	for _, view := range agentViews {
		if view.Validation.Valid && strings.TrimSpace(view.Error) == "" {
			successes++
		}
		if strings.TrimSpace(view.Error) != "" {
			errors++
		}
	}
	if successes == 0 {
		var messages []string
		for _, view := range agentViews {
			if strings.TrimSpace(view.Error) != "" {
				messages = append(messages, view.Error)
			}
		}
		return fmt.Errorf("all collision agents failed: %s", strings.Join(messages, "; "))
	}

	response := map[string]any{
		"workspace_path":             target,
		"workspace_id":               resolvedWorkspaceID,
		"query":                      mechanismCandidateViewFor(queryCandidate, true, false),
		"bisociation_mode":           opts.BisociationMode,
		"selection_mode":             memoryCollisionSelectionMode(opts.BisociationMode),
		"prompt_abstraction":         memoryCollisionPromptAbstraction(opts.BisociationMode),
		"collision_count":            len(selectedCells),
		"collision_pool_count":       len(cells),
		"collision_selection":        selection,
		"structural_candidates":      searchResult.StructuralCandidates,
		"memories_loaded":            searchResult.MemoriesLoaded,
		"skipped_pairs":              searchResult.SkippedPairs,
		"skipped":                    searchResult.Plan.Skipped,
		"include_collision_cache":    opts.IncludeCache,
		"cache_records_loaded":       cacheRecordsLoaded,
		"cache_collision_candidates": cacheCollisionCandidates,
		"agent_count":                len(agentViews),
		"agent_successes":            successes,
		"agent_errors":               errors,
		"max_agents_cap":             maxMemoryCollisionAgentCount,
		"max_agent_runs_cap":         maxMemoryCollisionAgentRuns,
		"agent_concurrency":          normalizeMemoryCollisionAgentConcurrency(opts.AgentConcurrency, len(agentRuns)),
		"agent_targets":              agentTargets,
		"per_agent_collision_cap":    1,
		"embedding_provider_model":   provider.Model(),
		"syntheses":                  agentViews,
	}

	if opts.WriteCache {
		vaultPath := strings.TrimSpace(opts.VaultPath)
		if vaultPath == "" {
			return fmt.Errorf("--vault-path is required when --write-cache is set")
		}
		note, err := contextplane.PlanMemoryCollisionCacheNote(contextplane.MemoryCollisionCacheInput{
			WorkspaceID:   resolvedWorkspaceID,
			WorkspacePath: target,
			Query:         searchResult.Query,
			Syntheses:     memoryCollisionSynthesesFromViews(agentViews),
			CreatedAt:     time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		if err := contextplane.WriteMemoryCollisionCacheNote(ctx, vaultPath, note); err != nil {
			return err
		}
		response["cache_note_written"] = true
		response["cache_note_path"] = note.NotePath
		response["cache_note_dedupe_key"] = note.DedupeKey
		response["cache_synthesis_count"] = note.SynthesisCount
		response["vault_path"] = vaultPath
	} else {
		response["cache_note_written"] = false
	}

	return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/mechanisms_repo_symbols_collide_memory_agents", response, envelope.WithMeta(envelope.Meta{Source: "cli"})))
}

func runCollisionSynthesisAgentRuns(ctx context.Context, query contextplane.MechanismQuery, runs []memoryCollisionAgentRun, opts repoSymbolCollisionAgentsOptions) []repoSymbolCollisionAgentView {
	views := make([]repoSymbolCollisionAgentView, len(runs))
	concurrency := normalizeMemoryCollisionAgentConcurrency(opts.AgentConcurrency, len(runs))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, run := range runs {
		i := i
		run := run
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			views[i] = runOneCollisionSynthesisAgent(ctx, run, query, opts)
		}()
	}
	wg.Wait()
	return views
}

func runOneCollisionSynthesisAgent(ctx context.Context, run memoryCollisionAgentRun, query contextplane.MechanismQuery, opts repoSymbolCollisionAgentsOptions) repoSymbolCollisionAgentView {
	input := contextplane.MemoryCollisionAgentPromptInput{
		AgentIndex:      run.Index,
		AgentRole:       run.Role,
		BisociationMode: opts.BisociationMode,
		Query:           query,
		Collision:       run.Cell,
	}
	prompt, err := contextplane.BuildMemoryCollisionAgentPrompt(input)
	agentProvider := firstNonEmpty(run.Provider, opts.AgentProvider)
	agentModel := firstNonEmpty(run.Model, opts.AgentModel)
	view := repoSymbolCollisionAgentView{
		AgentIndex:        run.Index,
		AgentRole:         run.Role,
		AgentProvider:     strings.TrimSpace(agentProvider),
		AgentModel:        strings.TrimSpace(agentModel),
		AgentRunLabel:     strings.TrimSpace(run.Label),
		BisociationMode:   contextplane.NormalizeMemoryCollisionAgentMode(opts.BisociationMode),
		SelectionMode:     memoryCollisionSelectionMode(opts.BisociationMode),
		PromptAbstraction: memoryCollisionPromptAbstraction(opts.BisociationMode),
		Collision:         run.Cell,
		PromptInput:       input,
	}
	if opts.IncludePrompt {
		view.Prompt = prompt
	}
	if err != nil {
		view.Error = err.Error()
		return view
	}

	agentOpts := repoSymbolBlurAgentOptions{
		Agent:             memoryblur.BackendPi,
		AgentBin:          opts.PiBin,
		AgentProvider:     agentProvider,
		AgentModel:        agentModel,
		PiMode:            firstNonEmpty(opts.PiMode, memoryblur.PiModeSDK),
		PiSDKBin:          firstNonEmpty(opts.PiSDKBin, "bun"),
		PiSDKScript:       opts.PiSDKScript,
		PiSDKCWD:          opts.PiSDKCWD,
		PiAgentDir:        opts.PiAgentDir,
		PiThinking:        opts.PiThinking,
		PiNoExtensions:    opts.PiNoExtensions,
		HermesIgnoreRules: true,
		Timeout:           opts.Timeout,
	}
	agent, err := memoryBlurAgentForOptions(agentOpts)
	if err != nil {
		view.Error = err.Error()
		return view
	}
	raw, err := runAgentPrompt(ctx, agent, prompt)
	if opts.IncludeRaw {
		view.RawAgentOutput = raw
	}
	if err != nil {
		view.Error = err.Error()
		return view
	}
	output, err := contextplane.ParseMemoryCollisionAgentOutput(raw)
	if err != nil {
		view.Error = err.Error()
		return view
	}
	view.AgentOutput = output
	view.Validation = contextplane.ValidateMemoryCollisionAgentOutput(input, output)
	if !view.Validation.Valid {
		view.Error = strings.Join(view.Validation.Errors, "; ")
	}
	return view
}

func normalizeCollisionAgentCount(value int) int {
	if value <= 0 {
		return defaultMemoryCollisionAgentCount
	}
	if value > maxMemoryCollisionAgentCount {
		return maxMemoryCollisionAgentCount
	}
	return value
}

func normalizeCollisionPoolLimit(value int, agentCount int) int {
	if agentCount <= 0 {
		agentCount = defaultMemoryCollisionAgentCount
	}
	if value <= 0 {
		value = defaultMemoryCollisionPoolLimit
	}
	if value < agentCount {
		return agentCount
	}
	return value
}

func collisionAgentRole(index int) string {
	roles := []string{
		"structural_translator",
		"constraint_mapper",
		"failure_mode_scout",
	}
	return roles[index%len(roles)]
}

type memoryCollisionAgentTarget struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Count    int    `json:"count"`
	Label    string `json:"label"`
}

type memoryCollisionAgentRun struct {
	Index    int
	Role     string
	Provider string
	Model    string
	Label    string
	Cell     contextplane.MemoryCollisionCell
}

func memoryCollisionAgentTargetsFromOptions(opts repoSymbolCollisionAgentsOptions) ([]memoryCollisionAgentTarget, error) {
	if len(opts.AgentRuns) == 0 {
		count := normalizeCollisionAgentCount(opts.MaxAgents)
		provider := strings.TrimSpace(opts.AgentProvider)
		model := strings.TrimSpace(opts.AgentModel)
		return []memoryCollisionAgentTarget{{
			Provider: provider,
			Model:    model,
			Count:    count,
			Label:    memoryCollisionAgentTargetLabel(provider, model),
		}}, nil
	}
	targets := make([]memoryCollisionAgentTarget, 0, len(opts.AgentRuns))
	total := 0
	for _, raw := range opts.AgentRuns {
		target, err := parseMemoryCollisionAgentRunSpec(raw)
		if err != nil {
			return nil, err
		}
		total += target.Count
		if total > maxMemoryCollisionAgentRuns {
			return nil, fmt.Errorf("too many collision agent runs requested: %d > %d", total, maxMemoryCollisionAgentRuns)
		}
		targets = append(targets, target)
	}
	if total == 0 {
		return nil, fmt.Errorf("at least one collision agent run is required")
	}
	return targets, nil
}

func parseMemoryCollisionAgentRunSpec(raw string) (memoryCollisionAgentTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return memoryCollisionAgentTarget{}, fmt.Errorf("--agent-run cannot be empty")
	}
	spec := raw
	count := 1
	if left, right, ok := strings.Cut(raw, "="); ok {
		spec = strings.TrimSpace(left)
		parsed, err := strconv.Atoi(strings.TrimSpace(right))
		if err != nil || parsed <= 0 {
			return memoryCollisionAgentTarget{}, fmt.Errorf("invalid --agent-run count in %q", raw)
		}
		count = parsed
	}
	provider, model, ok := splitMemoryCollisionModelSpec(spec)
	if !ok {
		return memoryCollisionAgentTarget{}, fmt.Errorf("invalid --agent-run %q; expected provider/model=count or provider:model=count", raw)
	}
	if count > maxMemoryCollisionAgentRuns {
		return memoryCollisionAgentTarget{}, fmt.Errorf("too many runs for %s: %d > %d", spec, count, maxMemoryCollisionAgentRuns)
	}
	return memoryCollisionAgentTarget{
		Provider: provider,
		Model:    model,
		Count:    count,
		Label:    memoryCollisionAgentTargetLabel(provider, model),
	}, nil
}

func splitMemoryCollisionModelSpec(spec string) (provider, model string, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", false
	}
	if left, right, found := strings.Cut(spec, ":"); found {
		provider = strings.TrimSpace(left)
		model = strings.TrimSpace(right)
		return provider, model, provider != "" && model != ""
	}
	if left, right, found := strings.Cut(spec, "/"); found {
		provider = strings.TrimSpace(left)
		model = strings.TrimSpace(right)
		return provider, model, provider != "" && model != ""
	}
	return "", "", false
}

func memoryCollisionAgentTargetLabel(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	switch {
	case provider != "" && model != "":
		return provider + "/" + model
	case model != "":
		return model
	case provider != "":
		return provider
	default:
		return "pi-default"
	}
}

func expandMemoryCollisionAgentRuns(targets []memoryCollisionAgentTarget) []memoryCollisionAgentRun {
	var runs []memoryCollisionAgentRun
	for _, target := range targets {
		for i := 0; i < target.Count; i++ {
			index := len(runs) + 1
			runs = append(runs, memoryCollisionAgentRun{
				Index:    index,
				Role:     collisionAgentRole(index - 1),
				Provider: strings.TrimSpace(target.Provider),
				Model:    strings.TrimSpace(target.Model),
				Label:    firstNonEmpty(strings.TrimSpace(target.Label), memoryCollisionAgentTargetLabel(target.Provider, target.Model)),
			})
		}
	}
	return runs
}

func assignMemoryCollisionAgentCells(runs []memoryCollisionAgentRun, cells []contextplane.MemoryCollisionCell) []memoryCollisionAgentRun {
	if len(runs) > len(cells) {
		runs = runs[:len(cells)]
	}
	out := make([]memoryCollisionAgentRun, len(runs))
	for i := range runs {
		out[i] = runs[i]
		out[i].Index = i + 1
		out[i].Role = collisionAgentRole(i)
		out[i].Cell = cells[i]
	}
	return out
}

func appendMemoryCollisionCellsDeduped(base []contextplane.MemoryCollisionCell, extra []contextplane.MemoryCollisionCell) []contextplane.MemoryCollisionCell {
	if len(extra) == 0 {
		return base
	}
	out := append([]contextplane.MemoryCollisionCell(nil), base...)
	seen := map[string]struct{}{}
	for _, cell := range out {
		key := firstNonEmpty(strings.TrimSpace(cell.DedupeKey), strings.TrimSpace(cell.CollisionID))
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, cell := range extra {
		key := firstNonEmpty(strings.TrimSpace(cell.DedupeKey), strings.TrimSpace(cell.CollisionID))
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cell)
	}
	return out
}

func normalizeMemoryCollisionAgentConcurrency(value int, total int) int {
	if total <= 0 {
		return 1
	}
	if value <= 0 {
		value = defaultMemoryCollisionConcurrency
	}
	if value > maxMemoryCollisionConcurrency {
		value = maxMemoryCollisionConcurrency
	}
	if value > total {
		value = total
	}
	if value <= 0 {
		return 1
	}
	return value
}

type memoryCollisionSelectionReport struct {
	PoolCount         int       `json:"pool_count"`
	SelectedCount     int       `json:"selected_count"`
	BisociationMode   string    `json:"bisociation_mode"`
	SelectionMode     string    `json:"selection_mode"`
	PromptAbstraction string    `json:"prompt_abstraction"`
	DomainDiversity   bool      `json:"domain_diversity"`
	MinDomainDistance int       `json:"min_domain_distance"`
	FallbackCount     int       `json:"fallback_count"`
	SelectedDomains   []string  `json:"selected_domains,omitempty"`
	SelectedDistances []int     `json:"selected_domain_distances,omitempty"`
	SelectedScores    []float64 `json:"selected_bisociation_scores,omitempty"`
}

type memoryCollisionModeComparisonReport struct {
	BisociationMode   string                             `json:"bisociation_mode"`
	SelectionMode     string                             `json:"selection_mode"`
	PromptAbstraction string                             `json:"prompt_abstraction"`
	SelectedCount     int                                `json:"selected_count"`
	SelectedDomains   []string                           `json:"selected_domains,omitempty"`
	SelectedDistances []int                              `json:"selected_domain_distances,omitempty"`
	SelectedScores    []float64                          `json:"selected_bisociation_scores,omitempty"`
	Selected          []memoryCollisionModeCandidateView `json:"selected,omitempty"`
}

type memoryCollisionModeCandidateView struct {
	CollisionID          string  `json:"collision_id"`
	MemoryID             string  `json:"memory_id,omitempty"`
	MemoryDomain         string  `json:"memory_domain"`
	MemorySummary        string  `json:"memory_summary,omitempty"`
	CollisionScore       float64 `json:"collision_score"`
	StructuralSimilarity float64 `json:"structural_similarity"`
	LiteralSimilarity    float64 `json:"literal_similarity"`
	BisociationScore     float64 `json:"bisociation_score,omitempty"`
}

func buildMemoryCollisionModeComparison(queryDomain string, cells []contextplane.MemoryCollisionCell, agentCount int, opts repoSymbolCollisionAgentsOptions) []memoryCollisionModeComparisonReport {
	modes := []string{
		contextplane.MemoryCollisionAgentModeBalanced,
		contextplane.MemoryCollisionAgentModeFar,
		contextplane.MemoryCollisionAgentModeAlien,
		contextplane.MemoryCollisionAgentModeFarAlien,
	}
	out := make([]memoryCollisionModeComparisonReport, 0, len(modes))
	for _, mode := range modes {
		modeOpts := opts
		modeOpts.BisociationMode = mode
		ranked := rankMemoryCollisionCellsForBisociationMode(queryDomain, cells, mode)
		selected, selection := selectMemoryCollisionCellsForAgents(queryDomain, ranked, agentCount, modeOpts)
		out = append(out, memoryCollisionModeComparisonReport{
			BisociationMode:   mode,
			SelectionMode:     memoryCollisionSelectionMode(mode),
			PromptAbstraction: memoryCollisionPromptAbstraction(mode),
			SelectedCount:     selection.SelectedCount,
			SelectedDomains:   selection.SelectedDomains,
			SelectedDistances: selection.SelectedDistances,
			SelectedScores:    selection.SelectedScores,
			Selected:          memoryCollisionModeCandidateViews(queryDomain, selected, mode),
		})
	}
	return out
}

func memoryCollisionModeCandidateViews(queryDomain string, cells []contextplane.MemoryCollisionCell, mode string) []memoryCollisionModeCandidateView {
	out := make([]memoryCollisionModeCandidateView, 0, len(cells))
	for _, cell := range cells {
		view := memoryCollisionModeCandidateView{
			CollisionID:          cell.CollisionID,
			MemoryID:             cell.MemoryID,
			MemoryDomain:         cell.MemoryDomain,
			MemorySummary:        cell.MemorySummary,
			CollisionScore:       cell.CollisionScore,
			StructuralSimilarity: cell.StructuralSimilarity,
			LiteralSimilarity:    cell.LiteralSimilarity,
		}
		if memoryCollisionSelectionMode(mode) == "far" {
			view.BisociationScore = farBisociationCellScore(queryDomain, cell)
		}
		out = append(out, view)
	}
	return out
}

func selectMemoryCollisionCellsForAgents(queryDomain string, cells []contextplane.MemoryCollisionCell, agentCount int, opts repoSymbolCollisionAgentsOptions) ([]contextplane.MemoryCollisionCell, memoryCollisionSelectionReport) {
	mode := contextplane.NormalizeMemoryCollisionAgentMode(opts.BisociationMode)
	report := memoryCollisionSelectionReport{
		PoolCount:         len(cells),
		BisociationMode:   mode,
		SelectionMode:     memoryCollisionSelectionMode(mode),
		PromptAbstraction: memoryCollisionPromptAbstraction(mode),
		DomainDiversity:   opts.DomainDiversity,
		MinDomainDistance: normalizeMinDomainDistance(opts.MinDomainDistance),
	}
	if agentCount <= 0 || len(cells) == 0 {
		return nil, report
	}
	if agentCount > len(cells) {
		agentCount = len(cells)
	}
	if !opts.DomainDiversity {
		selected := append([]contextplane.MemoryCollisionCell(nil), cells[:agentCount]...)
		report.SelectedCount = len(selected)
		report.SelectedDomains = selectedMemoryCollisionDomains(selected)
		report.SelectedDistances = selectedMemoryCollisionDistances(queryDomain, selected)
		report.SelectedScores = selectedMemoryCollisionBisociationScores(queryDomain, selected, mode)
		return selected, report
	}

	selected := []contextplane.MemoryCollisionCell{cells[0]}
	usedDomain := map[string]struct{}{normalizedMemoryDomain(cells[0].MemoryDomain): {}}
	usedMemory := map[string]struct{}{strings.TrimSpace(cells[0].MemoryID): {}}
	passes := []struct {
		requireDistance bool
		requireDomain   bool
	}{
		{requireDistance: true, requireDomain: true},
		{requireDistance: false, requireDomain: true},
		{requireDistance: false, requireDomain: false},
	}
	for _, pass := range passes {
		for _, cell := range cells[1:] {
			if len(selected) >= agentCount {
				break
			}
			memoryID := strings.TrimSpace(cell.MemoryID)
			if memoryID != "" {
				if _, exists := usedMemory[memoryID]; exists {
					continue
				}
			}
			domainKey := normalizedMemoryDomain(cell.MemoryDomain)
			if pass.requireDomain {
				if _, exists := usedDomain[domainKey]; exists {
					continue
				}
			}
			if pass.requireDistance && memoryDomainDistance(queryDomain, cell.MemoryDomain) < report.MinDomainDistance {
				continue
			}
			selected = append(selected, cell)
			if domainKey != "" {
				usedDomain[domainKey] = struct{}{}
			}
			if memoryID != "" {
				usedMemory[memoryID] = struct{}{}
			}
		}
	}
	report.SelectedCount = len(selected)
	report.SelectedDomains = selectedMemoryCollisionDomains(selected)
	report.SelectedDistances = selectedMemoryCollisionDistances(queryDomain, selected)
	report.SelectedScores = selectedMemoryCollisionBisociationScores(queryDomain, selected, mode)
	for _, distance := range report.SelectedDistances {
		if distance < report.MinDomainDistance {
			report.FallbackCount++
		}
	}
	return selected, report
}

func rankMemoryCollisionCellsForBisociationMode(queryDomain string, cells []contextplane.MemoryCollisionCell, mode string) []contextplane.MemoryCollisionCell {
	out := append([]contextplane.MemoryCollisionCell(nil), cells...)
	mode = contextplane.NormalizeMemoryCollisionAgentMode(mode)
	if memoryCollisionSelectionMode(mode) != "far" {
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := farBisociationCellScore(queryDomain, out[i])
		right := farBisociationCellScore(queryDomain, out[j])
		if left != right {
			return left > right
		}
		if out[i].CollisionScore != out[j].CollisionScore {
			return out[i].CollisionScore > out[j].CollisionScore
		}
		return out[i].DedupeKey < out[j].DedupeKey
	})
	return out
}

func farBisociationCellScore(queryDomain string, cell contextplane.MemoryCollisionCell) float64 {
	structural := normalizedSimilarity(cell.StructuralSimilarity)
	literalDistance := 1 - normalizedSimilarity(cell.LiteralSimilarity)
	domainDistance := float64(memoryDomainDistance(queryDomain, cell.MemoryDomain))
	if domainDistance > 8 {
		domainDistance = 8
	}
	domainDistance = domainDistance / 8
	return roundMemoryCollisionModeScore(structural*0.55 + literalDistance*0.25 + domainDistance*0.20)
}

func roundMemoryCollisionModeScore(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func normalizedSimilarity(value float64) float64 {
	if value < -1 {
		value = -1
	}
	if value > 1 {
		value = 1
	}
	return (value + 1) / 2
}

func memoryCollisionSelectionMode(mode string) string {
	switch contextplane.NormalizeMemoryCollisionAgentMode(mode) {
	case contextplane.MemoryCollisionAgentModeFar, contextplane.MemoryCollisionAgentModeFarAlien:
		return "far"
	default:
		return "balanced"
	}
}

func memoryCollisionPromptAbstraction(mode string) string {
	switch contextplane.NormalizeMemoryCollisionAgentMode(mode) {
	case contextplane.MemoryCollisionAgentModeAlien, contextplane.MemoryCollisionAgentModeFarAlien:
		return "alien"
	default:
		return "grounded"
	}
}

func normalizeMinDomainDistance(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func selectedMemoryCollisionDomains(cells []contextplane.MemoryCollisionCell) []string {
	domains := make([]string, 0, len(cells))
	seen := map[string]struct{}{}
	for _, cell := range cells {
		domain := strings.TrimSpace(cell.MemoryDomain)
		if domain == "" {
			continue
		}
		key := normalizedMemoryDomain(domain)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}

func selectedMemoryCollisionDistances(queryDomain string, cells []contextplane.MemoryCollisionCell) []int {
	distances := make([]int, 0, len(cells))
	for _, cell := range cells {
		distances = append(distances, memoryDomainDistance(queryDomain, cell.MemoryDomain))
	}
	return distances
}

func selectedMemoryCollisionBisociationScores(queryDomain string, cells []contextplane.MemoryCollisionCell, mode string) []float64 {
	if memoryCollisionSelectionMode(mode) != "far" {
		return nil
	}
	scores := make([]float64, 0, len(cells))
	for _, cell := range cells {
		scores = append(scores, farBisociationCellScore(queryDomain, cell))
	}
	return scores
}

func memoryDomainDistance(left, right string) int {
	leftParts := memoryDomainParts(left)
	rightParts := memoryDomainParts(right)
	if len(leftParts) == 0 || len(rightParts) == 0 {
		if strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right)) {
			return 0
		}
		return 1
	}
	common := 0
	for common < len(leftParts) && common < len(rightParts) && leftParts[common] == rightParts[common] {
		common++
	}
	return (len(leftParts) - common) + (len(rightParts) - common)
}

func memoryDomainParts(value string) []string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ':', '/', '\\', '.', ' ', '\t', '\n':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizedMemoryDomain(value string) string {
	return strings.Join(memoryDomainParts(value), "/")
}

func memoryCollisionSynthesesFromViews(views []repoSymbolCollisionAgentView) []contextplane.MemoryCollisionSynthesis {
	out := make([]contextplane.MemoryCollisionSynthesis, 0, len(views))
	for _, view := range views {
		if !view.Validation.Valid || strings.TrimSpace(view.Error) != "" {
			continue
		}
		out = append(out, contextplane.MemoryCollisionSynthesis{
			AgentIndex:        view.AgentIndex,
			AgentRole:         view.AgentRole,
			AgentProvider:     view.AgentProvider,
			AgentModel:        view.AgentModel,
			BisociationMode:   view.BisociationMode,
			SelectionMode:     view.SelectionMode,
			PromptAbstraction: view.PromptAbstraction,
			Collision:         view.Collision,
			Output:            view.AgentOutput,
			Validation:        view.Validation,
		})
	}
	return out
}
