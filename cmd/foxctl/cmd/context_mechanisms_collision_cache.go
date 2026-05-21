package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/spf13/cobra"
)

type contextMechanismsCollisionCacheOptions struct {
	VaultPath         string
	WorkspaceID       string
	QueryID           string
	QueryDomain       string
	BisociationMode   string
	SelectionMode     string
	PromptAbstraction string
	MemoryDomain      string
	MechanismTag      string
	AgentModel        string
	Limit             int
	IncludeSyntheses  bool
}

type memoryCollisionCacheRecordView struct {
	NotePath           string                                             `json:"note_path"`
	Title              string                                             `json:"title"`
	WorkspaceID        string                                             `json:"workspace_id"`
	DedupeKey          string                                             `json:"dedupe_key"`
	GeneratedAt        string                                             `json:"generated_at,omitempty"`
	Query              contextplane.MemoryCollisionCacheQueryRecord       `json:"query"`
	SynthesisCount     int                                                `json:"synthesis_count"`
	BisociationModes   []string                                           `json:"bisociation_modes,omitempty"`
	SelectionModes     []string                                           `json:"selection_modes,omitempty"`
	PromptAbstractions []string                                           `json:"prompt_abstractions,omitempty"`
	MemoryDomains      []string                                           `json:"memory_domains,omitempty"`
	MechanismTags      []string                                           `json:"mechanism_tags,omitempty"`
	AgentModels        []string                                           `json:"agent_models,omitempty"`
	Syntheses          []contextplane.MemoryCollisionCacheSynthesisRecord `json:"syntheses,omitempty"`
}

func newContextMechanismsCollisionCacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collision-cache",
		Short: "Inspect Obsidian-backed mechanism collision cache notes",
	}
	cmd.AddCommand(
		newContextMechanismsCollisionCacheListCommand(),
		newContextMechanismsCollisionCacheSearchCommand(),
	)
	return cmd
}

func newContextMechanismsCollisionCacheListCommand() *cobra.Command {
	opts := contextMechanismsCollisionCacheOptions{Limit: 20}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List typed mechanism collision cache records from an Obsidian vault",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runContextMechanismsCollisionCache(cmd, opts, "context/mechanisms_collision_cache_list")
		},
	}
	addContextMechanismsCollisionCacheFlags(cmd, &opts)
	return cmd
}

func newContextMechanismsCollisionCacheSearchCommand() *cobra.Command {
	opts := contextMechanismsCollisionCacheOptions{Limit: 20}
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Filter typed mechanism collision cache records by explicit fields",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runContextMechanismsCollisionCache(cmd, opts, "context/mechanisms_collision_cache_search")
		},
	}
	addContextMechanismsCollisionCacheFlags(cmd, &opts)
	return cmd
}

func addContextMechanismsCollisionCacheFlags(cmd *cobra.Command, opts *contextMechanismsCollisionCacheOptions) {
	cmd.Flags().StringVar(&opts.VaultPath, "vault-path", "", "Obsidian vault path")
	cmd.Flags().StringVar(&opts.WorkspaceID, "workspace-id", "", "Filter by workspace ID")
	cmd.Flags().StringVar(&opts.QueryID, "query-id", "", "Filter by mechanism query ID")
	cmd.Flags().StringVar(&opts.QueryDomain, "query-domain", "", "Filter by mechanism query domain")
	cmd.Flags().StringVar(&opts.BisociationMode, "mode", "", "Filter by bisociation mode (balanced|far|alien|far-alien)")
	cmd.Flags().StringVar(&opts.SelectionMode, "selection-mode", "", "Filter by selection mode")
	cmd.Flags().StringVar(&opts.PromptAbstraction, "prompt-abstraction", "", "Filter by prompt abstraction")
	cmd.Flags().StringVar(&opts.MemoryDomain, "memory-domain", "", "Filter by collided memory domain")
	cmd.Flags().StringVar(&opts.MechanismTag, "tag", "", "Filter by exact mechanism tag")
	cmd.Flags().StringVar(&opts.AgentModel, "model", "", "Filter by provider/model label")
	cmd.Flags().IntVar(&opts.Limit, "limit", 20, "Maximum records to return")
	cmd.Flags().BoolVar(&opts.IncludeSyntheses, "include-syntheses", false, "Include full cached syntheses in output")
}

func runContextMechanismsCollisionCache(cmd *cobra.Command, opts contextMechanismsCollisionCacheOptions, commandName string) error {
	vaultPath := strings.TrimSpace(opts.VaultPath)
	if vaultPath == "" {
		return fmt.Errorf("--vault-path is required")
	}
	loadOpts := contextplane.MemoryCollisionCacheLoadOptions{
		WorkspaceID:       strings.TrimSpace(opts.WorkspaceID),
		QueryID:           strings.TrimSpace(opts.QueryID),
		QueryDomain:       strings.TrimSpace(opts.QueryDomain),
		BisociationMode:   strings.TrimSpace(opts.BisociationMode),
		SelectionMode:     strings.TrimSpace(opts.SelectionMode),
		PromptAbstraction: strings.TrimSpace(opts.PromptAbstraction),
		MemoryDomain:      strings.TrimSpace(opts.MemoryDomain),
		MechanismTag:      strings.TrimSpace(opts.MechanismTag),
		AgentModel:        strings.TrimSpace(opts.AgentModel),
		Limit:             opts.Limit,
	}
	records, err := contextplane.LoadMemoryCollisionCacheRecords(cmd.Context(), vaultPath, loadOpts)
	if err != nil {
		return err
	}
	views := memoryCollisionCacheRecordViews(records, opts.IncludeSyntheses)
	return envelope.Write(cmd.OutOrStdout(), envelope.OK(commandName, map[string]any{
		"vault_path":        vaultPath,
		"record_count":      len(views),
		"records":           views,
		"include_syntheses": opts.IncludeSyntheses,
		"read_only":         true,
	}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
}

func memoryCollisionCacheRecordViews(records []contextplane.MemoryCollisionCacheRecord, includeSyntheses bool) []memoryCollisionCacheRecordView {
	views := make([]memoryCollisionCacheRecordView, 0, len(records))
	for _, record := range records {
		view := memoryCollisionCacheRecordView{
			NotePath:           record.NotePath,
			Title:              record.Title,
			WorkspaceID:        record.WorkspaceID,
			DedupeKey:          record.DedupeKey,
			Query:              record.Query,
			SynthesisCount:     len(record.Syntheses),
			BisociationModes:   memoryCollisionCacheViewModes(record),
			SelectionModes:     memoryCollisionCacheViewSelectionModes(record),
			PromptAbstractions: memoryCollisionCacheViewPromptAbstractions(record),
			MemoryDomains:      memoryCollisionCacheViewMemoryDomains(record),
			MechanismTags:      memoryCollisionCacheViewMechanismTags(record),
			AgentModels:        memoryCollisionCacheViewAgentModels(record),
		}
		if !record.GeneratedAt.IsZero() {
			view.GeneratedAt = record.GeneratedAt.UTC().Format(time.RFC3339)
		}
		if includeSyntheses {
			view.Syntheses = record.Syntheses
		}
		views = append(views, view)
	}
	return views
}

func memoryCollisionCacheViewModes(record contextplane.MemoryCollisionCacheRecord) []string {
	values := make([]string, 0, len(record.Syntheses))
	for _, synthesis := range record.Syntheses {
		values = append(values, synthesis.BisociationMode)
	}
	return compactViewStrings(values)
}

func memoryCollisionCacheViewSelectionModes(record contextplane.MemoryCollisionCacheRecord) []string {
	values := make([]string, 0, len(record.Syntheses))
	for _, synthesis := range record.Syntheses {
		values = append(values, synthesis.SelectionMode)
	}
	return compactViewStrings(values)
}

func memoryCollisionCacheViewPromptAbstractions(record contextplane.MemoryCollisionCacheRecord) []string {
	values := make([]string, 0, len(record.Syntheses))
	for _, synthesis := range record.Syntheses {
		values = append(values, synthesis.PromptAbstraction)
	}
	return compactViewStrings(values)
}

func memoryCollisionCacheViewMemoryDomains(record contextplane.MemoryCollisionCacheRecord) []string {
	values := make([]string, 0, len(record.Syntheses))
	for _, synthesis := range record.Syntheses {
		values = append(values, synthesis.Collision.MemoryDomain)
	}
	return compactViewStrings(values)
}

func memoryCollisionCacheViewMechanismTags(record contextplane.MemoryCollisionCacheRecord) []string {
	values := append([]string(nil), record.Query.MechanismTags...)
	for _, synthesis := range record.Syntheses {
		values = append(values, synthesis.Collision.MechanismTags...)
	}
	return compactViewStrings(values)
}

func memoryCollisionCacheViewAgentModels(record contextplane.MemoryCollisionCacheRecord) []string {
	values := make([]string, 0, len(record.Syntheses))
	for _, synthesis := range record.Syntheses {
		values = append(values, memoryCollisionCacheViewModelLabel(synthesis))
	}
	return compactViewStrings(values)
}

func memoryCollisionCacheViewModelLabel(synthesis contextplane.MemoryCollisionCacheSynthesisRecord) string {
	provider := strings.TrimSpace(synthesis.AgentProvider)
	model := strings.TrimSpace(synthesis.AgentModel)
	switch {
	case provider != "" && model != "":
		return provider + "/" + model
	case model != "":
		return model
	case provider != "":
		return provider
	default:
		return ""
	}
}

func compactViewStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
