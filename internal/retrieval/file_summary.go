package retrieval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

// FileSummaryGenerator creates file-level summaries for the semantic search tree.
type FileSummaryGenerator struct {
	store         storage.MemoryStore
	llm           SummaryLLM
	embedProvider semantic.EmbeddingProvider
	workspace     string
	logger        zerolog.Logger
}

// SummaryLLM is the interface for LLM-based summary generation.
type SummaryLLM interface {
	// GenerateSummary generates a short summary from the given input.
	// Returns the summary text (1-2 sentences, <= 40 words).
	GenerateSummary(ctx context.Context, prompt string) (string, error)
}

// NewFileSummaryGenerator creates a new file summary generator.
func NewFileSummaryGenerator(
	store storage.MemoryStore,
	llm SummaryLLM,
	embedProvider semantic.EmbeddingProvider,
	workspace string,
	logger zerolog.Logger,
) *FileSummaryGenerator {
	return &FileSummaryGenerator{
		store:         store,
		llm:           llm,
		embedProvider: embedProvider,
		workspace:     workspace,
		logger:        logger.With().Str("component", "file_summary").Logger(),
	}
}

// GetOrCreateSummary returns an existing summary or creates a new one.
// Returns the summary text and whether it was cached.
func (g *FileSummaryGenerator) GetOrCreateSummary(
	ctx context.Context,
	input symbol.FileSummaryInput,
) (string, bool, error) {
	entryName := symbol.FileSummaryEntryName(g.workspace, input.FilePath)

	// Try to get existing summary
	entry, err := g.store.Get(ctx, entryName, g.workspace)
	if err != nil {
		// Distinguish "not found" (expected) from actual errors (unexpected)
		if !errors.Is(err, memory.ErrNotFound) {
			// Transient DB error - log and don't attempt LLM call that may also fail to persist
			g.logger.Warn().Err(err).Str("path", input.FilePath).Msg("failed to check cache, skipping")
			return "", false, fmt.Errorf("check cache for %s: %w", input.FilePath, err)
		}
		// Not found - continue to generate
	} else {
		// Found cached entry - check if digest matches
		var result symbol.FileSummaryResult
		if err := json.Unmarshal(entry.Result, &result); err == nil {
			currentDigest := symbol.ComputeFileSummaryDigest(input)
			if result.Digest == currentDigest {
				g.logger.Debug().
					Str("path", input.FilePath).
					Msg("using cached file summary")
				return entry.Summary, true, nil
			}
			g.logger.Debug().
				Str("path", input.FilePath).
				Msg("digest changed, regenerating summary")
		}
	}

	// Generate new summary
	summary, err := g.generateSummary(ctx, input)
	if err != nil {
		return "", false, fmt.Errorf("generate summary for %s: %w", input.FilePath, err)
	}

	// Store the summary
	if err := g.storeSummary(ctx, input, summary); err != nil {
		g.logger.Warn().Err(err).Str("path", input.FilePath).Msg("failed to store summary")
		// Don't fail - we still have the summary
	}

	return summary, false, nil
}

// generateSummary generates a summary using LLM or deterministic fallback.
func (g *FileSummaryGenerator) generateSummary(
	ctx context.Context,
	input symbol.FileSummaryInput,
) (string, error) {
	// Try LLM if available
	if g.llm != nil {
		prompt := g.buildPrompt(input)
		summary, err := g.llm.GenerateSummary(ctx, prompt)
		if err == nil && summary != "" {
			g.logger.Debug().
				Str("path", input.FilePath).
				Msg("generated summary via LLM")
			return summary, nil
		}
		g.logger.Debug().Err(err).Str("path", input.FilePath).Msg("LLM failed, using fallback")
	}

	// Fallback to deterministic summary
	return g.deterministicSummary(input), nil
}

// buildPrompt constructs the LLM prompt for summary generation.
func (g *FileSummaryGenerator) buildPrompt(input symbol.FileSummaryInput) string {
	var sb strings.Builder

	sb.WriteString("Write a concise summary (1-2 sentences, max 35 words) for semantic search indexing.\n")
	sb.WriteString("Rules:\n")
	sb.WriteString("- Start directly with action verb or noun (NO 'This file...' or 'The...')\n")
	sb.WriteString("- Include key function/type names that users might search for\n")
	sb.WriteString("- Use technical terms over generic descriptions\n")
	sb.WriteString("- Format: '<action/purpose>, <key capabilities/symbols>'\n\n")

	sb.WriteString(fmt.Sprintf("File: %s\n", input.FilePath))

	if input.Package != "" {
		sb.WriteString(fmt.Sprintf("Package: %s\n", input.Package))
	}

	if input.PackageDoc != "" {
		sb.WriteString(fmt.Sprintf("Doc: %s\n", truncate(input.PackageDoc, 200)))
	}

	if input.FirstComment != "" {
		sb.WriteString(fmt.Sprintf("Comment: %s\n", truncate(input.FirstComment, 200)))
	}

	if len(input.TopSymbols) > 0 {
		sb.WriteString(fmt.Sprintf("Symbols: %s\n", strings.Join(input.TopSymbols, ", ")))
	}

	sb.WriteString("\nGood: 'Implements SQLite-backed session store with Save, Get, List, Delete operations.'\n")
	sb.WriteString("Bad: 'This file contains code for storing sessions in a database.'\n")
	sb.WriteString("\nSummary:")

	return sb.String()
}

// deterministicSummary creates a fallback summary without LLM.
func (g *FileSummaryGenerator) deterministicSummary(input symbol.FileSummaryInput) string {
	var parts []string

	// Start with package info
	if input.Package != "" {
		parts = append(parts, fmt.Sprintf("Package %s", input.Package))
	} else {
		// Use directory name as fallback
		dir := filepath.Dir(input.FilePath)
		if dir != "." && dir != "" {
			parts = append(parts, fmt.Sprintf("In %s", dir))
		}
	}

	// Add file purpose hint from path
	base := filepath.Base(input.FilePath)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	// Common naming patterns
	switch {
	case strings.HasSuffix(name, "_test"):
		parts = append(parts, "tests for "+strings.TrimSuffix(name, "_test"))
	case name == "main":
		parts = append(parts, "entry point")
	case strings.Contains(name, "handler"):
		parts = append(parts, "request handlers")
	case strings.Contains(name, "store"):
		parts = append(parts, "data storage")
	case strings.Contains(name, "types"):
		parts = append(parts, "type definitions")
	case strings.Contains(name, "util") || strings.Contains(name, "helper"):
		parts = append(parts, "utility functions")
	}

	// Add symbol info
	if len(input.TopSymbols) > 0 {
		maxSymbols := 5
		if len(input.TopSymbols) < maxSymbols {
			maxSymbols = len(input.TopSymbols)
		}
		symbols := input.TopSymbols[:maxSymbols]
		if len(parts) == 0 {
			parts = append(parts, fmt.Sprintf("Defines %s", strings.Join(symbols, ", ")))
		} else {
			parts = append(parts, fmt.Sprintf("defines %s", strings.Join(symbols, ", ")))
		}
	}

	if len(parts) == 0 {
		return fmt.Sprintf("Source file: %s", base)
	}

	return strings.Join(parts, "; ") + "."
}

// storeSummary persists the summary to the memory store.
func (g *FileSummaryGenerator) storeSummary(
	ctx context.Context,
	input symbol.FileSummaryInput,
	summary string,
) error {
	entryName := symbol.FileSummaryEntryName(g.workspace, input.FilePath)
	digest := symbol.ComputeFileSummaryDigest(input)

	result := symbol.FileSummaryResult{
		FilePath: input.FilePath,
		Package:  input.Package,
		Symbols:  input.TopSymbols,
		Digest:   digest,
	}

	resultJSON, err := symbol.MarshalFileSummaryResult(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	entry := storage.NamedEntry{
		Name:      entryName,
		Type:      symbol.FileSummaryType,
		Workspace: g.workspace,
		Summary:   summary,
		Result:    resultJSON,
	}

	if _, err = g.store.Save(ctx, entry); err != nil {
		return err
	}

	if g.embedProvider != nil && summary != "" {
		embedding, err := g.embedProvider.Embed(ctx, summary)
		if err != nil {
			g.logger.Debug().Err(err).Str("path", input.FilePath).Msg("failed to embed summary")
			return nil
		}
		dimensions := g.embedProvider.Dimensions()
		if err := g.store.ValidateEmbeddingDimensions(ctx, g.workspace, dimensions); err != nil {
			g.logger.Warn().Err(err).Str("path", input.FilePath).Msg("embedding dimensions mismatch; skipping summary embedding")
			return nil
		}
		if err := g.store.UpdateEmbedding(ctx, entryName, g.workspace, embedding); err != nil {
			g.logger.Debug().Err(err).Str("path", input.FilePath).Msg("failed to store summary embedding")
		}
		meta := memory.EmbeddingMetadata{
			Workspace:  g.workspace,
			Provider:   providerNameFromModel(g.embedProvider.Model()),
			Model:      g.embedProvider.Model(),
			Dimensions: dimensions,
		}
		if err := g.store.SetEmbeddingMetadata(ctx, meta); err != nil {
			g.logger.Debug().Err(err).Str("path", input.FilePath).Msg("failed to store embedding metadata")
		}
	}

	return nil
}

// GetSummaries retrieves summaries for multiple files.
// Returns a map from file path to summary.
func (g *FileSummaryGenerator) GetSummaries(
	ctx context.Context,
	filePaths []string,
) (map[string]string, error) {
	summaries := make(map[string]string)

	for _, path := range filePaths {
		entryName := symbol.FileSummaryEntryName(g.workspace, path)
		entry, err := g.store.Get(ctx, entryName, g.workspace)
		if err != nil {
			continue // Skip missing summaries
		}
		summaries[path] = entry.Summary
	}

	return summaries, nil
}

// BatchCreateSummaries generates summaries for multiple files with a limit.
// Returns the number of summaries created and any error.
func (g *FileSummaryGenerator) BatchCreateSummaries(
	ctx context.Context,
	inputs []symbol.FileSummaryInput,
	maxNew int,
) (int, error) {
	created := 0

	for _, input := range inputs {
		if ctx.Err() != nil {
			return created, ctx.Err()
		}

		if created >= maxNew {
			break
		}

		_, cached, err := g.GetOrCreateSummary(ctx, input)
		if err != nil {
			g.logger.Warn().Err(err).Str("path", input.FilePath).Msg("failed to create summary")
			continue
		}

		if !cached {
			created++
		}
	}

	return created, nil
}

// BackfillEmbeddings generates embeddings for existing file summaries missing vectors.
// This is best-effort and limited to the provided max count.
func (g *FileSummaryGenerator) BackfillEmbeddings(ctx context.Context, max int) (int, error) {
	if g.embedProvider == nil {
		return 0, nil
	}
	if max <= 0 {
		return 0, nil
	}
	dimensions := g.embedProvider.Dimensions()
	if err := g.store.ValidateEmbeddingDimensions(ctx, g.workspace, dimensions); err != nil {
		g.logger.Warn().Err(err).Msg("embedding dimensions mismatch; skipping summary backfill")
		return 0, nil
	}
	meta := memory.EmbeddingMetadata{
		Workspace:  g.workspace,
		Provider:   providerNameFromModel(g.embedProvider.Model()),
		Model:      g.embedProvider.Model(),
		Dimensions: dimensions,
	}
	if err := g.store.SetEmbeddingMetadata(ctx, meta); err != nil {
		g.logger.Debug().Err(err).Msg("failed to store embedding metadata")
	}

	// List a superset to allow filtering to file_summary entries.
	oversample := max * 3
	if oversample < 50 {
		oversample = 50
	}

	entries, err := g.store.ListWithoutEmbedding(ctx, g.workspace, oversample)
	if err != nil {
		return 0, err
	}

	updated := 0
	for _, entry := range entries {
		if entry.Type != symbol.FileSummaryType && !strings.HasPrefix(entry.Name, "file://") {
			continue
		}
		if entry.Summary == "" {
			continue
		}
		embedding, err := g.embedProvider.Embed(ctx, entry.Summary)
		if err != nil {
			g.logger.Debug().Err(err).Str("name", entry.Name).Msg("failed to embed summary")
			continue
		}
		if err := g.store.UpdateEmbedding(ctx, entry.Name, g.workspace, embedding); err != nil {
			g.logger.Debug().Err(err).Str("name", entry.Name).Msg("failed to store summary embedding")
			continue
		}
		updated++
		if updated >= max {
			break
		}
	}

	return updated, nil
}

// truncate truncates a string to maxLen and adds ellipsis if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// GenerateRootSummary creates a summary for the root node based on top file summaries.
func (g *FileSummaryGenerator) GenerateRootSummary(
	ctx context.Context,
	topSummaries []string,
) (string, error) {
	if len(topSummaries) == 0 {
		return "Repository with mixed content.", nil
	}

	// Try LLM if available
	if g.llm != nil {
		prompt := g.buildRootPrompt(topSummaries)
		summary, err := g.llm.GenerateSummary(ctx, prompt)
		if err == nil && summary != "" {
			return summary, nil
		}
		g.logger.Debug().Err(err).Msg("LLM failed for root summary, using fallback")
	}

	// Deterministic fallback: stitch top summaries
	return g.deterministicRootSummary(topSummaries), nil
}

// buildRootPrompt constructs the LLM prompt for root summary generation.
func (g *FileSummaryGenerator) buildRootPrompt(summaries []string) string {
	var sb strings.Builder

	sb.WriteString("Synthesize these file summaries into a concise codebase overview (1-2 sentences, max 35 words).\n")
	sb.WriteString("Rules:\n")
	sb.WriteString("- Start directly with verb or noun (NO 'This codebase...')\n")
	sb.WriteString("- Include key technologies, patterns, or domain terms\n")
	sb.WriteString("- Be specific about what it does, not generic\n\n")

	sb.WriteString("File summaries:\n")
	for i, s := range summaries {
		if i >= 10 {
			break
		}
		sb.WriteString(fmt.Sprintf("- %s\n", s))
	}

	sb.WriteString("\nGood: 'Agent runtime managing Claude Code sessions with SQLite persistence, embedding search, and skill orchestration.'\n")
	sb.WriteString("Bad: 'A system for managing various types of data and operations.'\n")
	sb.WriteString("\nOverview:")

	return sb.String()
}

// deterministicRootSummary creates a fallback root summary.
func (g *FileSummaryGenerator) deterministicRootSummary(summaries []string) string {
	if len(summaries) == 0 {
		return "Repository with mixed content."
	}

	// Take first 3 summaries and combine
	max := 3
	if len(summaries) < max {
		max = len(summaries)
	}

	parts := make([]string, max)
	for i := 0; i < max; i++ {
		parts[i] = strings.TrimSuffix(strings.TrimSpace(summaries[i]), ".")
	}

	return strings.Join(parts, "; ") + "."
}

// SearchFileSummaries searches file_summary entries using vector search when available,
// falling back to BM25. Results are filtered to file_summary entries only.
func SearchFileSummaries(
	ctx context.Context,
	store storage.MemoryStore,
	workspace string,
	query string,
	queryEmbedding []float32,
	limit int,
) ([]FileEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	oversample := limit * 4
	if oversample < 50 {
		oversample = 50
	}

	var scored []storage.ScoredEntry
	var err error
	var entries []FileEntry

	if queryEmbedding != nil {
		scored, err = store.SearchSimilar(ctx, workspace, queryEmbedding, oversample)
		if err != nil {
			scored = nil
		}
		entries = filterFileSummaryEntries(scored, limit)
	}
	if len(entries) == 0 {
		scored, err = store.Search(ctx, workspace, query, oversample)
		if err != nil {
			return nil, err
		}
		entries = filterFileSummaryEntries(scored, limit)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		return entries[i].Path < entries[j].Path
	})

	return entries, nil
}

func filterFileSummaryEntries(scored []storage.ScoredEntry, limit int) []FileEntry {
	entries := make([]FileEntry, 0, limit)
	seen := make(map[string]bool)
	for _, s := range scored {
		entry := s.Entry
		if entry.Type != symbol.FileSummaryType && !strings.HasPrefix(entry.Name, "file://") {
			continue
		}
		path := extractFilePath(entry.Name)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true

		score := s.Score
		if score > 1.0 {
			score = 1.0
		}
		entries = append(entries, FileEntry{
			Path:    path,
			Score:   score,
			Summary: entry.Summary,
		})
		if len(entries) >= limit {
			break
		}
	}
	return entries
}

type SymbolSummaryGenerator struct {
	store         storage.MemoryStore
	llm           SummaryLLM
	embedProvider semantic.EmbeddingProvider
	workspace     string
	logger        zerolog.Logger
}

func NewSymbolSummaryGenerator(
	store storage.MemoryStore,
	llm SummaryLLM,
	embedProvider semantic.EmbeddingProvider,
	workspace string,
	logger zerolog.Logger,
) *SymbolSummaryGenerator {
	return &SymbolSummaryGenerator{
		store:         store,
		llm:           llm,
		embedProvider: embedProvider,
		workspace:     workspace,
		logger:        logger.With().Str("component", "symbol_summary").Logger(),
	}
}

func (g *SymbolSummaryGenerator) GetOrCreateSummary(
	ctx context.Context,
	input symbol.SymbolSummaryInput,
) (string, bool, error) {
	entryName := symbol.SymbolSummaryEntryName(g.workspace, input.SymbolID)

	entry, err := g.store.Get(ctx, entryName, g.workspace)
	if err != nil {
		if !errors.Is(err, memory.ErrNotFound) {
			g.logger.Warn().Err(err).Str("symbol_id", input.SymbolID).Msg("failed to check cache, skipping")
			return "", false, fmt.Errorf("check cache for %s: %w", input.SymbolID, err)
		}
	} else {
		var result symbol.SymbolSummaryResult
		if err := json.Unmarshal(entry.Result, &result); err == nil {
			currentDigest := symbol.ComputeSymbolSummaryDigest(input)
			if result.Digest == currentDigest {
				g.logger.Debug().Str("symbol_id", input.SymbolID).Msg("using cached symbol summary")
				return entry.Summary, true, nil
			}
			g.logger.Debug().Str("symbol_id", input.SymbolID).Msg("digest changed, regenerating summary")
		}
	}

	summary, err := g.generateSummary(ctx, input)
	if err != nil {
		return "", false, fmt.Errorf("generate summary for %s: %w", input.SymbolID, err)
	}

	if err := g.storeSummary(ctx, input, summary); err != nil {
		g.logger.Warn().Err(err).Str("symbol_id", input.SymbolID).Msg("failed to store summary")
	}

	return summary, false, nil
}

func (g *SymbolSummaryGenerator) generateSummary(
	ctx context.Context,
	input symbol.SymbolSummaryInput,
) (string, error) {
	if g.llm != nil {
		prompt := g.buildPrompt(input)
		summary, err := g.llm.GenerateSummary(ctx, prompt)
		if err == nil && summary != "" {
			g.logger.Debug().Str("symbol_id", input.SymbolID).Msg("generated summary via LLM")
			return summary, nil
		}
		g.logger.Debug().Err(err).Str("symbol_id", input.SymbolID).Msg("LLM failed, using fallback")
	}

	return g.deterministicSummary(input), nil
}

func (g *SymbolSummaryGenerator) buildPrompt(input symbol.SymbolSummaryInput) string {
	var sb strings.Builder

	sb.WriteString("Write a concise summary (1-2 sentences, max 30 words) for a code symbol.\n")
	sb.WriteString("Rules:\n")
	sb.WriteString("- Start with an action verb or noun phrase (no This function/The method)\n")
	sb.WriteString("- Mention key behavior, side effects, and domain terms\n")
	sb.WriteString("- Include important types or resources referenced by the signature\n\n")

	sb.WriteString(fmt.Sprintf("Symbol: %s\n", input.Name))
	if input.Kind != "" {
		sb.WriteString(fmt.Sprintf("Kind: %s\n", input.Kind))
	}
	if input.Signature != "" {
		sb.WriteString(fmt.Sprintf("Signature: %s\n", truncate(input.Signature, 200)))
	}
	if input.FilePath != "" {
		sb.WriteString(fmt.Sprintf("File: %s\n", input.FilePath))
	}
	if input.Documentation != "" {
		sb.WriteString(fmt.Sprintf("Doc: %s\n", truncate(input.Documentation, 240)))
	}

	sb.WriteString("\nSummary:")

	return sb.String()
}

func (g *SymbolSummaryGenerator) deterministicSummary(input symbol.SymbolSummaryInput) string {
	if input.Documentation != "" {
		doc := strings.TrimSpace(input.Documentation)
		if doc != "" {
			return truncate(doc, 160)
		}
	}

	labelParts := make([]string, 0, 2)
	if input.Kind != "" {
		labelParts = append(labelParts, string(input.Kind))
	}
	if input.Name != "" {
		labelParts = append(labelParts, input.Name)
	}
	label := strings.TrimSpace(strings.Join(labelParts, " "))
	if label == "" {
		label = "Symbol"
	}
	if input.Signature != "" {
		return fmt.Sprintf("%s: %s.", label, input.Signature)
	}
	return label + "."
}

func (g *SymbolSummaryGenerator) storeSummary(
	ctx context.Context,
	input symbol.SymbolSummaryInput,
	summary string,
) error {
	entryName := symbol.SymbolSummaryEntryName(g.workspace, input.SymbolID)
	digest := symbol.ComputeSymbolSummaryDigest(input)

	result := symbol.SymbolSummaryResult{
		SymbolID:  input.SymbolID,
		FilePath:  input.FilePath,
		Name:      input.Name,
		Kind:      input.Kind,
		Signature: input.Signature,
		Digest:    digest,
		Language:  input.Language,
	}

	resultJSON, err := symbol.MarshalSymbolSummaryResult(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	entry := storage.NamedEntry{
		Name:      entryName,
		Type:      symbol.SymbolSummaryType,
		Workspace: g.workspace,
		Summary:   summary,
		Result:    resultJSON,
	}

	if _, err = g.store.Save(ctx, entry); err != nil {
		return err
	}

	if g.embedProvider != nil && summary != "" {
		embedding, err := g.embedProvider.Embed(ctx, summary)
		if err != nil {
			g.logger.Debug().Err(err).Str("symbol_id", input.SymbolID).Msg("failed to embed summary")
			return nil
		}
		dimensions := g.embedProvider.Dimensions()
		if err := g.store.ValidateEmbeddingDimensions(ctx, g.workspace, dimensions); err != nil {
			g.logger.Warn().Err(err).Str("symbol_id", input.SymbolID).Msg("embedding dimensions mismatch; skipping summary embedding")
			return nil
		}
		if err := g.store.UpdateEmbedding(ctx, entryName, g.workspace, embedding); err != nil {
			g.logger.Debug().Err(err).Str("symbol_id", input.SymbolID).Msg("failed to store summary embedding")
		}
		meta := memory.EmbeddingMetadata{
			Workspace:  g.workspace,
			Provider:   providerNameFromModel(g.embedProvider.Model()),
			Model:      g.embedProvider.Model(),
			Dimensions: dimensions,
		}
		if err := g.store.SetEmbeddingMetadata(ctx, meta); err != nil {
			g.logger.Debug().Err(err).Str("symbol_id", input.SymbolID).Msg("failed to store embedding metadata")
		}
	}

	return nil
}

func providerNameFromModel(model string) string {
	lower := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(lower, "gemini-"):
		return "gemini"
	case strings.HasPrefix(lower, "voyage-"):
		return "voyage"
	default:
		return ""
	}
}
