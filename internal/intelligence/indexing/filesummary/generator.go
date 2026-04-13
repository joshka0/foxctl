package filesummary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/codefilter"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// SummaryLLM is the interface for LLM-based summary generation.
type SummaryLLM interface {
	GenerateSummary(ctx context.Context, prompt string) (string, error)
}

// Generator creates file-level summaries for semantic search trees and file-summary indexing.
type Generator struct {
	store         storage.MemoryStore
	llm           SummaryLLM
	embedProvider semantic.EmbeddingProvider
	workspace     string
}

func NewFileSummaryGenerator(
	store storage.MemoryStore,
	llm SummaryLLM,
	embedProvider semantic.EmbeddingProvider,
	workspace string,
) *Generator {
	return &Generator{
		store:         store,
		llm:           llm,
		embedProvider: embedProvider,
		workspace:     workspace,
	}
}

func (g *Generator) GetOrCreateSummary(ctx context.Context, input symbol.FileSummaryInput) (string, bool, error) {
	input = symbol.NormalizeFileSummaryInput(input)
	if codefilter.ShouldSkipPath(input.FilePath) {
		return "", true, nil
	}
	entryName := symbol.FileSummaryEntryName(g.workspace, input.FilePath)

	entry, err := g.store.Get(ctx, entryName, g.workspace)
	if err != nil {
		if !errors.Is(err, memory.ErrNotFound) {
			observability.Emit(ctx, observability.NewEvent("file_summary.cache_check_failed").
				WithComponent("filesummary").
				WithWorkspace(g.workspace).
				WithData("path", input.FilePath).
				Error(err, 0))
			return "", false, fmt.Errorf("check cache for %s: %w", input.FilePath, err)
		}
	} else {
		var result symbol.FileSummaryResult
		if err := json.Unmarshal(entry.Result, &result); err == nil {
			currentDigest := symbol.ComputeFileSummaryDigest(input)
			if result.Digest == currentDigest {
				return entry.Summary, true, nil
			}
		}
	}

	summary, err := g.generateSummary(ctx, input)
	if err != nil {
		return "", false, fmt.Errorf("generate summary for %s: %w", input.FilePath, err)
	}

	if err := g.storeSummary(ctx, input, summary); err != nil {
		observability.Emit(ctx, observability.NewEvent("file_summary.store_failed").
			WithComponent("filesummary").
			WithWorkspace(g.workspace).
			WithData("path", input.FilePath).
			Error(err, 0))
	}

	return summary, false, nil
}

func (g *Generator) generateSummary(ctx context.Context, input symbol.FileSummaryInput) (string, error) {
	if g.llm != nil {
		prompt := g.buildPrompt(input)
		summary, err := g.llm.GenerateSummary(ctx, prompt)
		if err == nil && summary != "" {
			return summary, nil
		}
	}
	return g.deterministicSummary(input), nil
}

func (g *Generator) buildPrompt(input symbol.FileSummaryInput) string {
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
	sb.WriteString("\nSummary:")
	return sb.String()
}

func (g *Generator) deterministicSummary(input symbol.FileSummaryInput) string {
	var parts []string

	if input.Package != "" {
		parts = append(parts, fmt.Sprintf("Package %s", input.Package))
	} else {
		dir := filepath.Dir(input.FilePath)
		if dir != "." && dir != "" {
			parts = append(parts, fmt.Sprintf("In %s", dir))
		}
	}

	base := filepath.Base(input.FilePath)
	name := strings.TrimSuffix(base, filepath.Ext(base))

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

func (g *Generator) storeSummary(ctx context.Context, input symbol.FileSummaryInput, summary string) error {
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
			observability.Emit(ctx, observability.NewEvent("file_summary.embedding_error").
				WithComponent("filesummary").
				WithWorkspace(g.workspace).
				WithData("path", input.FilePath).
				WithData("entry_name", entryName).
				WithData("provider", providerNameFromModel(g.embedProvider.Model())).
				WithData("model", g.embedProvider.Model()).
				Error(err, 0))
			return nil
		}
		dimensions := g.embedProvider.Dimensions()
		if err := g.store.ValidateEmbeddingDimensions(ctx, g.workspace, dimensions); err != nil {
			observability.Emit(ctx, observability.NewEvent("file_summary.embedding_dimensions_mismatch").
				WithComponent("filesummary").
				WithWorkspace(g.workspace).
				WithData("path", input.FilePath).
				WithData("entry_name", entryName).
				WithData("provider", providerNameFromModel(g.embedProvider.Model())).
				WithData("model", g.embedProvider.Model()).
				WithData("dimensions", dimensions).
				Error(err, 0))
			return nil
		}
		if err := g.store.UpdateEmbedding(ctx, entryName, g.workspace, embedding); err != nil {
			observability.Emit(ctx, observability.NewEvent("file_summary.embedding_error").
				WithComponent("filesummary").
				WithWorkspace(g.workspace).
				WithData("path", input.FilePath).
				WithData("entry_name", entryName).
				WithData("provider", providerNameFromModel(g.embedProvider.Model())).
				WithData("model", g.embedProvider.Model()).
				WithData("dimensions", dimensions).
				Error(err, 0))
			return nil
		}
		meta := memory.EmbeddingMetadata{
			Workspace:  g.workspace,
			Provider:   providerNameFromModel(g.embedProvider.Model()),
			Model:      g.embedProvider.Model(),
			Dimensions: dimensions,
		}
		if err := g.store.SetEmbeddingMetadata(ctx, meta); err != nil {
			observability.Emit(ctx, observability.NewEvent("file_summary.embedding_error").
				WithComponent("filesummary").
				WithWorkspace(g.workspace).
				WithData("path", input.FilePath).
				WithData("entry_name", entryName).
				WithData("provider", meta.Provider).
				WithData("model", meta.Model).
				WithData("dimensions", meta.Dimensions).
				Error(err, 0))
		}
	}

	return nil
}

func (g *Generator) GetSummaries(ctx context.Context, filePaths []string) (map[string]string, error) {
	summaries := make(map[string]string)
	for _, path := range filePaths {
		entryName := symbol.FileSummaryEntryName(g.workspace, path)
		entry, err := g.store.Get(ctx, entryName, g.workspace)
		if err != nil {
			continue
		}
		summaries[path] = entry.Summary
	}
	return summaries, nil
}

func (g *Generator) BatchCreateSummaries(ctx context.Context, inputs []symbol.FileSummaryInput, maxNew int) (int, error) {
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
			observability.Emit(ctx, observability.NewEvent("file_summary.batch_create_failed").
				WithComponent("filesummary").
				WithWorkspace(g.workspace).
				WithData("path", input.FilePath).
				Error(err, 0))
			continue
		}
		if !cached {
			created++
		}
	}
	return created, nil
}

func (g *Generator) BackfillEmbeddings(ctx context.Context, max int) (int, error) {
	if g.embedProvider == nil || max <= 0 {
		return 0, nil
	}

	dimensions := g.embedProvider.Dimensions()
	if err := g.store.ValidateEmbeddingDimensions(ctx, g.workspace, dimensions); err != nil {
		observability.Emit(ctx, observability.NewEvent("file_summary.backfill_dimensions_mismatch").
			WithComponent("filesummary").
			WithWorkspace(g.workspace).
			Error(err, 0))
		return 0, nil
	}
	meta := memory.EmbeddingMetadata{
		Workspace:  g.workspace,
		Provider:   providerNameFromModel(g.embedProvider.Model()),
		Model:      g.embedProvider.Model(),
		Dimensions: dimensions,
	}
	if err := g.store.SetEmbeddingMetadata(ctx, meta); err != nil {
		_ = err
	}

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
			continue
		}
		if err := g.store.UpdateEmbedding(ctx, entry.Name, g.workspace, embedding); err != nil {
			continue
		}
		updated++
		if updated >= max {
			break
		}
	}

	return updated, nil
}

func (g *Generator) GenerateRootSummary(ctx context.Context, topSummaries []string) (string, error) {
	if len(topSummaries) == 0 {
		return "Repository with mixed content.", nil
	}

	if g.llm != nil {
		prompt := g.buildRootPrompt(topSummaries)
		summary, err := g.llm.GenerateSummary(ctx, prompt)
		if err == nil && summary != "" {
			return summary, nil
		}
	}

	return g.deterministicRootSummary(topSummaries), nil
}

func (g *Generator) buildRootPrompt(summaries []string) string {
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

	sb.WriteString("\nOverview:")
	return sb.String()
}

func (g *Generator) deterministicRootSummary(summaries []string) string {
	if len(summaries) == 0 {
		return "Repository with mixed content."
	}

	maxItems := 3
	if len(summaries) < maxItems {
		maxItems = len(summaries)
	}

	parts := make([]string, maxItems)
	for i := 0; i < maxItems; i++ {
		parts[i] = strings.TrimSuffix(strings.TrimSpace(summaries[i]), ".")
	}

	return strings.Join(parts, "; ") + "."
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func providerNameFromModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(model, "voyage"):
		return "voyage"
	case strings.HasPrefix(model, "gemini"):
		return "gemini"
	case strings.HasPrefix(model, "openai"):
		return "openai"
	default:
		return "unknown"
	}
}
