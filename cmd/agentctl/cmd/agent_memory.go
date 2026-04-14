package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/jkatigb/agentctl/internal/context/companion"
	agentdomain "github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
)

func init() {
	agentCmd.AddCommand(newAgentMemoryCommand())
}

func newAgentMemoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect and manage an agent's layered memory",
	}
	cmd.AddCommand(
		newAgentMemoryStatsCommand(),
		newAgentMemoryContextCommand(),
		newAgentMemorySearchCommand(),
		newAgentMemoryCompressCommand(),
	)
	return cmd
}

func newAgentMemoryStatsCommand() *cobra.Command {
	var conversationID string
	cmd := &cobra.Command{
		Use:   "stats <agent-ref>",
		Short: "Show memory statistics for an agent conversation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg := config.MustFromContext(ctx)

			agentRecord, cleanup, err := resolveAgentForMemory(ctx, cfg, args[0])
			if err != nil {
				return agentMemoryError(cmd, "agent/memory/stats", err)
			}
			defer cleanup()

			svc, svcCleanup, err := buildAgentMemoryService(ctx, cfg, agentRecord, false)
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/memory/stats", string(protocol.ErrorCodeERuntime), err.Error())
			}
			defer svcCleanup()

			stats, err := svc.GetMemoryStats(ctx, resolveAgentConversationID(agentRecord, conversationID))
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/memory/stats", string(protocol.ErrorCodeERuntime), fmt.Sprintf("memory stats failed: %v", err))
			}

			return writeOK(cmd, "agent/memory/stats", map[string]any{
				"agent_id":         agentRecord.ID,
				"conversation_id":  resolveAgentConversationID(agentRecord, conversationID),
				"stats":            stats,
				"memory_scope":     string(agentdomain.NormalizeMemoryScope(agentRecord.MemoryScope)),
				"memory_retention": string(normalizeAgentMemoryRetention(agentRecord)),
			}, "run", nil)
		},
	}
	cmd.Flags().StringVar(&conversationID, "conversation-id", "", "Override conversation lineage for the query")
	return cmd
}

func newAgentMemoryContextCommand() *cobra.Command {
	var conversationID string
	cmd := &cobra.Command{
		Use:   "context <agent-ref>",
		Short: "Render the current layered memory context for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg := config.MustFromContext(ctx)

			agentRecord, cleanup, err := resolveAgentForMemory(ctx, cfg, args[0])
			if err != nil {
				return agentMemoryError(cmd, "agent/memory/context", err)
			}
			defer cleanup()

			svc, svcCleanup, err := buildAgentMemoryService(ctx, cfg, agentRecord, false)
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/memory/context", string(protocol.ErrorCodeERuntime), err.Error())
			}
			defer svcCleanup()

			resolvedConversationID := resolveAgentConversationID(agentRecord, conversationID)
			contextText, err := svc.GetMemoryContext(ctx, resolvedConversationID)
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/memory/context", string(protocol.ErrorCodeERuntime), fmt.Sprintf("memory context failed: %v", err))
			}

			return writeOK(cmd, "agent/memory/context", map[string]any{
				"agent_id":             agentRecord.ID,
				"conversation_id":      resolvedConversationID,
				"context":              contextText,
				"context_token_hint":   agentMemoryConfig(agentRecord).TotalTokenBudget,
				"memory_scope":         string(agentdomain.NormalizeMemoryScope(agentRecord.MemoryScope)),
				"memory_retention":     string(normalizeAgentMemoryRetention(agentRecord)),
				"default_search_limit": defaultMemorySearchLimitForAgent(agentRecord),
			}, "run", nil)
		},
	}
	cmd.Flags().StringVar(&conversationID, "conversation-id", "", "Override conversation lineage for the query")
	return cmd
}

func newAgentMemorySearchCommand() *cobra.Command {
	var conversationID string
	var query string
	var limit int
	cmd := &cobra.Command{
		Use:   "search <agent-ref>",
		Short: "Search an agent's persistent memory artifacts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(query) == "" {
				return writeErrorEnvelope(cmd, "agent/memory/search", string(protocol.ErrorCodeEARG), "query is required", "Use --query to provide a memory search term.")
			}

			ctx := cmd.Context()
			cfg := config.MustFromContext(ctx)

			agentRecord, cleanup, err := resolveAgentForMemory(ctx, cfg, args[0])
			if err != nil {
				return agentMemoryError(cmd, "agent/memory/search", err)
			}
			defer cleanup()

			svc, svcCleanup, err := buildAgentMemoryService(ctx, cfg, agentRecord, true)
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/memory/search", string(protocol.ErrorCodeERuntime), err.Error())
			}
			defer svcCleanup()

			resolvedConversationID := resolveAgentConversationID(agentRecord, conversationID)
			effectiveLimit := clampMemorySearchLimitForAgent(agentRecord, limit)
			results, err := svc.SearchMemory(ctx, resolvedConversationID, strings.TrimSpace(query), effectiveLimit)
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/memory/search", string(protocol.ErrorCodeERuntime), fmt.Sprintf("memory search failed: %v", err))
			}

			out := make([]map[string]any, 0, len(results))
			for _, result := range results {
				updatedAt := ""
				if !result.Entry.UpdatedAt.IsZero() {
					updatedAt = result.Entry.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
				}
				out = append(out, map[string]any{
					"name":       result.Entry.Name,
					"type":       result.Entry.Type,
					"score":      result.Score,
					"summary":    result.Entry.Summary,
					"session_id": result.Entry.SessionID,
					"updated_at": updatedAt,
				})
			}

			return writeOK(cmd, "agent/memory/search", map[string]any{
				"agent_id":         agentRecord.ID,
				"conversation_id":  resolvedConversationID,
				"query":            strings.TrimSpace(query),
				"limit":            effectiveLimit,
				"results":          out,
				"default_limit":    defaultMemorySearchLimitForAgent(agentRecord),
				"memory_scope":     string(agentdomain.NormalizeMemoryScope(agentRecord.MemoryScope)),
				"memory_retention": string(normalizeAgentMemoryRetention(agentRecord)),
			}, "run", nil)
		},
	}
	cmd.Flags().StringVar(&conversationID, "conversation-id", "", "Override conversation lineage for the query")
	cmd.Flags().StringVar(&query, "query", "", "Search query")
	cmd.Flags().IntVar(&limit, "limit", 0, "Result limit (retention-aware defaults apply when omitted)")
	return cmd
}

func newAgentMemoryCompressCommand() *cobra.Command {
	var conversationID string
	var distill bool
	cmd := &cobra.Command{
		Use:   "compress <agent-ref>",
		Short: "Run layered memory compression for an agent conversation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg := config.MustFromContext(ctx)

			agentRecord, cleanup, err := resolveAgentForMemory(ctx, cfg, args[0])
			if err != nil {
				return agentMemoryError(cmd, "agent/memory/compress", err)
			}
			defer cleanup()

			svc, svcCleanup, err := buildAgentMemoryService(ctx, cfg, agentRecord, false)
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/memory/compress", string(protocol.ErrorCodeERuntime), err.Error())
			}
			defer svcCleanup()

			if svc.Memory() == nil {
				return writeErrorEnvelope(cmd, "agent/memory/compress", string(protocol.ErrorCodeERuntime), "memory features not enabled")
			}

			resolvedConversationID := resolveAgentConversationID(agentRecord, conversationID)
			shouldDistill := defaultDistillForAgent(agentRecord)
			if cmd.Flags().Changed("distill") {
				shouldDistill = distill
			}

			result, err := svc.Memory().CompressConversation(ctx, resolvedConversationID, companion.CompressionOptions{
				Distill: shouldDistill,
			})
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/memory/compress", string(protocol.ErrorCodeERuntime), fmt.Sprintf("memory compression failed: %v", err))
			}

			return writeOK(cmd, "agent/memory/compress", map[string]any{
				"agent_id":          agentRecord.ID,
				"conversation_id":   resolvedConversationID,
				"processed_dates":   result.ProcessedDates,
				"summarized":        result.Summarized,
				"skipped":           result.Skipped,
				"distilled":         result.Distilled,
				"default_distill":   defaultDistillForAgent(agentRecord),
				"effective_distill": shouldDistill,
				"memory_scope":      string(agentdomain.NormalizeMemoryScope(agentRecord.MemoryScope)),
				"memory_retention":  string(normalizeAgentMemoryRetention(agentRecord)),
			}, "run", nil)
		},
	}
	cmd.Flags().StringVar(&conversationID, "conversation-id", "", "Override conversation lineage for the compression run")
	cmd.Flags().BoolVar(&distill, "distill", false, "Force distilled summary generation during compression")
	return cmd
}

func resolveAgentForMemory(ctx context.Context, cfg config.Config, ref string) (agentdomain.Agent, func(), error) {
	store, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return agentdomain.Agent{}, nil, fmt.Errorf("failed to open agents store: %w", err)
	}
	agentRecord, err := store.Resolve(ctx, ref)
	if err != nil {
		_ = store.Close()
		return agentdomain.Agent{}, nil, err
	}
	return agentRecord, func() { errs.Ignore(store.Close(), "close agents store") }, nil
}

func buildAgentMemoryService(ctx context.Context, cfg config.Config, agentRecord agentdomain.Agent, enableSearch bool) (*companion.Service, func(), error) {
	contextStore, err := contextvar.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, nil, fmt.Errorf("open context store: %w", err)
	}

	dbPath := filepath.Join(cfg.Storage.Root, "companion.db")
	memoryDB, closeDB, err := dbutil.OpenStoreDB(ctx, cfg.Storage.Root, "COMPANION", filepath.Base(dbPath), nil)
	if err != nil {
		_ = contextStore.Close()
		return nil, nil, fmt.Errorf("open companion memory database: %w", err)
	}

	cleanupFns := []func() error{closeDB, contextStore.Close}
	serviceCfg := companion.ServiceConfig{
		Logger:         zerolog.Nop(),
		MemoryDB:       memoryDB,
		MemoryConfig:   ptrMemoryConfig(agentMemoryConfig(agentRecord)),
		MemoryBehavior: companion.MemoryBehaviorForRetention(normalizeAgentMemoryRetention(agentRecord)),
	}

	llmProvider := strings.TrimSpace(agentRecord.LLMProvider)
	if llmProvider == "" {
		llmProvider = cfg.LLM.Provider
	}
	llmAPIKey := strings.TrimSpace(agentRecord.LLMAPIKey)
	if llmAPIKey == "" {
		llmAPIKey = cfg.LLM.ResolveAPIKey(llmProvider)
	}
	llmModel := strings.TrimSpace(agentRecord.LLMModel)
	if llmModel == "" {
		llmModel = cfg.LLM.ResolveModel(llmProvider)
	}
	serviceCfg.LLMProvider = llmProvider
	serviceCfg.LLMAPIKey = llmAPIKey
	serviceCfg.LLMModel = llmModel
	serviceCfg.LLMBaseURL = cfg.LLM.ResolveBaseURL(llmProvider)
	serviceCfg.LLMAuthMode = cfg.LLM.ResolveAuthMode(llmProvider)
	serviceCfg.LLMAuthHeader = cfg.LLM.ResolveAuthHeader(llmProvider)
	serviceCfg.LLMAuthPrefix = cfg.LLM.ResolveAuthPrefix(llmProvider)

	if enableSearch {
		memStore, err := memorystore.OpenFromConfig(ctx, cfg)
		if err != nil {
			for _, closeFn := range cleanupFns {
				_ = closeFn()
			}
			return nil, nil, fmt.Errorf("open memory store: %w", err)
		}
		cleanupFns = append([]func() error{memStore.Close}, cleanupFns...)
		serviceCfg.MemoryStore = memStore
		serviceCfg.MemoryWorkspace = workspace.CanonicalID(cfg.Storage.Root)
		serviceCfg.Config = &cfg
	}

	cleanup := func() {
		for _, closeFn := range cleanupFns {
			_ = closeFn()
		}
	}
	return companion.NewService(contextStore, serviceCfg, nil), cleanup, nil
}

func agentMemoryConfig(agentRecord agentdomain.Agent) companion.MemoryConfig {
	cfg := companion.DefaultMemoryConfig()
	switch normalizeAgentMemoryRetention(agentRecord) {
	case agentdomain.MemoryRetentionCompanion:
		cfg.VividWindowHours = 72
		cfg.VividMaxTurns = 120
		cfg.VividTokenBudget = 30000
		cfg.RecentWindowDays = 21
		cfg.RecentTokenBudget = 12000
		cfg.HistoryTokenBudget = 10000
		cfg.TotalTokenBudget = 52000
	case agentdomain.MemoryRetentionTask:
		cfg.VividWindowHours = 12
		cfg.VividMaxTurns = 24
		cfg.VividTokenBudget = 12000
		cfg.RecentWindowDays = 3
		cfg.RecentTokenBudget = 4000
		cfg.HistoryTokenBudget = 2000
		cfg.TotalTokenBudget = 18000
	case agentdomain.MemoryRetentionEphemeral:
		cfg.VividWindowHours = 6
		cfg.VividMaxTurns = 12
		cfg.VividTokenBudget = 6000
		cfg.RecentWindowDays = 1
		cfg.RecentTokenBudget = 2000
		cfg.HistoryTokenBudget = 1000
		cfg.TotalTokenBudget = 9000
	}
	return cfg
}

func normalizeAgentMemoryRetention(agentRecord agentdomain.Agent) agentdomain.MemoryRetention {
	if strings.TrimSpace(string(agentRecord.MemoryRetention)) != "" {
		return agentdomain.NormalizeMemoryRetention(agentRecord.MemoryRetention)
	}
	return agentdomain.DefaultMemoryRetentionForScope(agentdomain.NormalizeMemoryScope(agentRecord.MemoryScope))
}

func defaultDistillForAgent(agentRecord agentdomain.Agent) bool {
	switch normalizeAgentMemoryRetention(agentRecord) {
	case agentdomain.MemoryRetentionTask, agentdomain.MemoryRetentionEphemeral:
		return false
	default:
		return true
	}
}

func defaultMemorySearchLimitForAgent(agentRecord agentdomain.Agent) int {
	switch normalizeAgentMemoryRetention(agentRecord) {
	case agentdomain.MemoryRetentionCompanion:
		return 12
	case agentdomain.MemoryRetentionTask:
		return 5
	case agentdomain.MemoryRetentionEphemeral:
		return 3
	default:
		return 8
	}
}

func clampMemorySearchLimitForAgent(agentRecord agentdomain.Agent, requested int) int {
	maxLimit := 12
	switch normalizeAgentMemoryRetention(agentRecord) {
	case agentdomain.MemoryRetentionCompanion:
		maxLimit = 20
	case agentdomain.MemoryRetentionTask:
		maxLimit = 8
	case agentdomain.MemoryRetentionEphemeral:
		maxLimit = 5
	}
	if requested <= 0 {
		return defaultMemorySearchLimitForAgent(agentRecord)
	}
	if requested > maxLimit {
		return maxLimit
	}
	return requested
}

func resolveAgentConversationID(agentRecord agentdomain.Agent, requested string) string {
	if trimmed := strings.TrimSpace(requested); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(agentRecord.ConversationID); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(agentRecord.ID)
}

func ptrMemoryConfig(cfg companion.MemoryConfig) *companion.MemoryConfig {
	return &cfg
}

func agentMemoryError(cmd *cobra.Command, command string, err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "not found") {
		return writeErrorEnvelope(cmd, command, string(protocol.ErrorCodeENotFound), fmt.Sprintf("agent not found: %v", err))
	}
	return writeErrorEnvelope(cmd, command, string(protocol.ErrorCodeERuntime), err.Error())
}
