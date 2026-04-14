package symbol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/runtime/observability"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// SummaryLLM is the interface for LLM-based summary generation.
type SummaryLLM interface {
	GenerateSummary(ctx context.Context, prompt string) (string, error)
}

// SymbolSummaryGenerator produces symbol-level summaries for semantic search.
type SymbolSummaryGenerator struct {
	store         storage.MemoryStore
	llm           SummaryLLM
	embedProvider semantic.EmbeddingProvider
	workspace     string
}

// NewSymbolSummaryGenerator creates a symbol summary generator.
func NewSymbolSummaryGenerator(
	store storage.MemoryStore,
	llm SummaryLLM,
	embedProvider semantic.EmbeddingProvider,
	workspace string,
) *SymbolSummaryGenerator {
	return &SymbolSummaryGenerator{
		store:         store,
		llm:           llm,
		embedProvider: embedProvider,
		workspace:     workspace,
	}
}

// GetOrCreateSummary returns an existing symbol summary or creates a new one.
// Returns the summary text and whether it was cached.
func (g *SymbolSummaryGenerator) GetOrCreateSummary(
	ctx context.Context,
	input SymbolSummaryInput,
) (string, bool, error) {
	entryName := symbolSummaryEntryName(g.workspace, input)

	entry, err := g.store.Get(ctx, entryName, g.workspace)
	if err != nil {
		if !errors.Is(err, memory.ErrNotFound) {
			observability.Emit(ctx, observability.NewEvent("symbol_summary.cache_check_failed").
				WithComponent("retrieval").
				WithWorkspace(g.workspace).
				WithData("symbol_id", input.SymbolID).
				Error(err, 0))
			return "", false, fmt.Errorf("check cache for %s: %w", input.SymbolID, err)
		}
	} else {
		var result SymbolSummaryResult
		if err := json.Unmarshal(entry.Result, &result); err == nil {
			currentDigest := ComputeSymbolSummaryDigest(input)
			if result.Digest == currentDigest {
				return entry.Summary, true, nil
			}
			// Digest changed - regenerate
		}
	}

	summary, err := g.generateSummary(ctx, input)
	if err != nil {
		return "", false, fmt.Errorf("generate summary for %s: %w", input.SymbolID, err)
	}

	if err := g.storeSummary(ctx, input, summary); err != nil {
		observability.Emit(ctx, observability.NewEvent("symbol_summary.store_failed").
			WithComponent("retrieval").
			WithWorkspace(g.workspace).
			WithData("symbol_id", input.SymbolID).
			Error(err, 0))
	}

	return summary, false, nil
}

func (g *SymbolSummaryGenerator) generateSummary(
	ctx context.Context,
	input SymbolSummaryInput,
) (string, error) {
	if g.llm != nil {
		prompt := g.buildPrompt(input)
		summary, err := g.llm.GenerateSummary(ctx, prompt)
		if err == nil && summary != "" {
			return summary, nil
		}
		// LLM failed - fall back to deterministic
	}

	return g.deterministicSummary(input), nil
}

func (g *SymbolSummaryGenerator) buildPrompt(input SymbolSummaryInput) string {
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

func (g *SymbolSummaryGenerator) deterministicSummary(input SymbolSummaryInput) string {
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
	input SymbolSummaryInput,
	summary string,
) error {
	entryName := symbolSummaryEntryName(g.workspace, input)
	digest := ComputeSymbolSummaryDigest(input)

	result := SymbolSummaryResult{
		Pkg:       input.Pkg,
		SymbolID:  input.SymbolID,
		SymbolKey: input.SymbolKey,
		FilePath:  input.FilePath,
		Name:      input.Name,
		Kind:      input.Kind,
		Signature: input.Signature,
		Digest:    digest,
		Language:  input.Language,
	}

	resultJSON, err := MarshalSymbolSummaryResult(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	entry := storage.NamedEntry{
		Name:      entryName,
		Type:      SymbolSummaryType,
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
			// Embedding failed - non-fatal, skip
			return nil
		}
		dimensions := g.embedProvider.Dimensions()
		if err := g.store.ValidateEmbeddingDimensions(ctx, g.workspace, dimensions); err != nil {
			observability.Emit(ctx, observability.NewEvent("symbol_summary.embedding_dimensions_mismatch").
				WithComponent("retrieval").
				WithWorkspace(g.workspace).
				WithData("symbol_id", input.SymbolID).
				Error(err, 0))
			return nil
		}
		if err := g.store.UpdateEmbedding(ctx, entryName, g.workspace, embedding); err != nil {
			// Embedding store failed - non-fatal
			return nil
		}
		meta := memory.EmbeddingMetadata{
			Workspace:  g.workspace,
			Provider:   providerNameFromModel(g.embedProvider.Model()),
			Model:      g.embedProvider.Model(),
			Dimensions: dimensions,
		}
		if err := g.store.SetEmbeddingMetadata(ctx, meta); err != nil {
			// Metadata store failed - non-fatal
			_ = err
		}
	}

	return nil
}

func symbolSummaryEntryName(workspace string, input SymbolSummaryInput) string {
	if strings.TrimSpace(input.Pkg) != "" && strings.TrimSpace(input.SymbolKey) != "" {
		return SymbolSummaryKeyEntryName(workspace, input.Pkg, input.SymbolKey)
	}
	return SymbolSummaryEntryName(workspace, input.SymbolID)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
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
