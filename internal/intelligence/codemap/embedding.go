package codemap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

// EmbeddingChunk represents a chunk of codemap content to embed.
type EmbeddingChunk struct {
	ID      string
	Title   string
	Content string
	Summary string
	TraceID string
}

// EmbeddingPlan contains the overview text and per-trace chunks.
type EmbeddingPlan struct {
	Overview string
	Chunks   []EmbeddingChunk
}

type codemapChunkResult struct {
	CodemapID string `json:"codemap_id"`
	ChunkID   string `json:"chunk_id"`
	Title     string `json:"title,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// BuildEmbeddingPlan builds embedding text for the internal codemap format.
// BuildEmbeddingPlan converts a codemap into embedding chunks.
func BuildEmbeddingPlan(cm *Codemap) EmbeddingPlan {
	if cm == nil {
		return EmbeddingPlan{}
	}

	overview := buildOverviewText(cm.Title, cm.Description, cm.Query, cm.FileCount, cm.SymbolCount)

	chunks := make([]EmbeddingChunk, 0, len(cm.Traces))
	for _, trace := range cm.Traces {
		var b strings.Builder
		if trace.Title != "" {
			b.WriteString(fmt.Sprintf("Trace %d: %s\n", trace.Number, trace.Title))
		}
		if trace.Summary != "" {
			b.WriteString("Summary: ")
			b.WriteString(trace.Summary)
			b.WriteString("\n")
		}
		if trace.Tree != "" {
			b.WriteString("Tree:\n")
			b.WriteString(trace.Tree)
			b.WriteString("\n")
		}
		if len(trace.Annotations) > 0 {
			b.WriteString("Annotations:\n")
			for _, ann := range trace.Annotations {
				b.WriteString("- ")
				if ann.Label != "" {
					b.WriteString("[")
					b.WriteString(ann.Label)
					b.WriteString("] ")
				}
				if ann.Title != "" {
					b.WriteString(ann.Title)
					b.WriteString(": ")
				}
				if ann.Description != "" {
					b.WriteString(ann.Description)
					b.WriteString(" ")
				}
				if ann.Path != "" {
					b.WriteString("(")
					b.WriteString(ann.Path)
					b.WriteString(")")
				}
				b.WriteString("\n")
			}
		}

		content := strings.TrimSpace(b.String())
		if content == "" {
			continue
		}

		chunkID := fmt.Sprintf("trace-%d", trace.Number)
		chunkTitle := trace.Title
		if chunkTitle == "" {
			chunkTitle = fmt.Sprintf("Trace %d", trace.Number)
		}

		chunks = append(chunks, EmbeddingChunk{
			ID:      chunkID,
			Title:   chunkTitle,
			Content: content,
			Summary: buildChunkSummary(chunkTitle, trace.Summary, content),
		})
	}

	return EmbeddingPlan{
		Overview: overview,
		Chunks:   chunks,
	}
}

// BuildEmbeddingPlanFromWindsurf builds embedding text for Windsurf codemaps.
// BuildEmbeddingPlanFromWindsurf converts a Windsurf codemap into embedding chunks.
func BuildEmbeddingPlanFromWindsurf(cm *WindsurfCodemap) EmbeddingPlan {
	if cm == nil {
		return EmbeddingPlan{}
	}

	overview := buildWindsurfOverview(cm)

	chunks := make([]EmbeddingChunk, 0, len(cm.Traces)+1)
	for i, trace := range cm.Traces {
		var b strings.Builder
		traceTitle := trace.Title
		if traceTitle == "" {
			traceTitle = fmt.Sprintf("Trace %d", i+1)
		}
		b.WriteString(traceTitle)
		b.WriteString("\n")
		if trace.Description != "" {
			b.WriteString(trace.Description)
			b.WriteString("\n")
		}
		if trace.TraceTextDiagram != "" {
			b.WriteString("Diagram:\n")
			b.WriteString(trace.TraceTextDiagram)
			b.WriteString("\n")
		}
		if trace.TraceGuide != "" {
			b.WriteString("Guide:\n")
			b.WriteString(trace.TraceGuide)
			b.WriteString("\n")
		}
		if len(trace.Locations) > 0 {
			b.WriteString("Locations:\n")
			for _, loc := range trace.Locations {
				b.WriteString("- ")
				if loc.Path != "" {
					b.WriteString(loc.Path)
					if loc.LineNumber > 0 {
						b.WriteString(":")
						b.WriteString(fmt.Sprintf("%d", loc.LineNumber))
					}
					b.WriteString(" ")
				}
				if loc.Title != "" {
					b.WriteString(loc.Title)
					b.WriteString(": ")
				}
				if loc.Description != "" {
					b.WriteString(loc.Description)
				}
				b.WriteString("\n")
			}
		}

		content := strings.TrimSpace(b.String())
		if content == "" {
			continue
		}

		chunkID := trace.ID
		if chunkID == "" {
			chunkID = fmt.Sprintf("trace-%d", i+1)
		} else {
			chunkID = "trace-" + chunkID
		}

		chunks = append(chunks, EmbeddingChunk{
			ID:      chunkID,
			Title:   traceTitle,
			Content: content,
			Summary: buildChunkSummary(traceTitle, trace.Description, content),
			TraceID: trace.ID,
		})
	}

	if cm.MermaidDiagram != "" {
		chunks = append(chunks, EmbeddingChunk{
			ID:      "mermaid",
			Title:   "Mermaid Diagram",
			Content: cm.MermaidDiagram,
			Summary: buildChunkSummary("Mermaid Diagram", "", cm.MermaidDiagram),
		})
	}

	return EmbeddingPlan{
		Overview: overview,
		Chunks:   chunks,
	}
}

// StoreEmbeddingPlan saves embeddings for the codemap entry and its chunks.
func StoreEmbeddingPlan(ctx context.Context, store storage.MemoryStore, cfg config.Config, workspace, codemapName string, plan EmbeddingPlan) (int, error) {
	if store == nil {
		return 0, fmt.Errorf("memory store is nil")
	}
	if strings.TrimSpace(plan.Overview) == "" {
		return 0, nil
	}

	embedder, err := semantic.NewEmbedderFromConfig(
		semantic.ScopeCodemaps,
		cfg,
		semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
	)
	if err != nil {
		return 0, err
	}

	overviewEmbedding, err := embedder.Embed(ctx, plan.Overview)
	if err != nil {
		return 0, err
	}

	if err := store.ValidateEmbeddingDimensions(ctx, workspace, overviewEmbedding.Dims); err != nil {
		return 0, err
	}
	if err := store.UpdateEmbedding(ctx, codemapName, workspace, overviewEmbedding.Vec); err != nil {
		return 0, err
	}

	meta := memory.EmbeddingMetadata{
		Workspace:  workspace,
		Provider:   overviewEmbedding.Provider,
		Model:      overviewEmbedding.Model,
		Dimensions: overviewEmbedding.Dims,
	}
	if err := store.SetEmbeddingMetadata(ctx, meta); err != nil {
		return 0, err
	}

	codemapID := codemapIDFromName(codemapName)
	stored := 0
	for _, chunk := range plan.Chunks {
		if strings.TrimSpace(chunk.Content) == "" {
			continue
		}

		embedding, err := embedder.Embed(ctx, chunk.Content)
		if err != nil {
			return stored, err
		}

		chunkName := fmt.Sprintf("%s#chunk:%s", codemapName, chunk.ID)
		chunkSummary := strings.TrimSpace(chunk.Summary)
		if chunkSummary == "" {
			chunkSummary = chunk.Title
		}
		chunkSummary = truncateRunes(chunkSummary, 240)

		chunkResult := codemapChunkResult{
			CodemapID: codemapID,
			ChunkID:   chunk.ID,
			Title:     chunk.Title,
			TraceID:   chunk.TraceID,
			Summary:   chunkSummary,
		}
		chunkResultJSON, err := json.Marshal(chunkResult)
		if err != nil {
			return stored, err
		}

		entry := memory.NamedEntry{
			Name:      chunkName,
			Type:      "codemap_chunk",
			Summary:   chunkSummary,
			Result:    chunkResultJSON,
			Workspace: workspace,
		}
		if _, err := store.Save(ctx, entry); err != nil {
			return stored, err
		}
		if err := store.UpdateEmbedding(ctx, chunkName, workspace, embedding.Vec); err != nil {
			return stored, err
		}
		stored++
	}

	return stored, nil
}

func buildOverviewText(title, description, query string, fileCount, symbolCount int) string {
	var b strings.Builder
	if title != "" {
		b.WriteString("Title: ")
		b.WriteString(title)
		b.WriteString("\n")
	}
	if description != "" {
		b.WriteString("Description: ")
		b.WriteString(description)
		b.WriteString("\n")
	}
	if query != "" {
		b.WriteString("Query: ")
		b.WriteString(query)
		b.WriteString("\n")
	}
	if fileCount > 0 || symbolCount > 0 {
		b.WriteString(fmt.Sprintf("Files: %d | Symbols: %d\n", fileCount, symbolCount))
	}
	return strings.TrimSpace(b.String())
}

func buildWindsurfOverview(cm *WindsurfCodemap) string {
	var b strings.Builder
	if cm.Title != "" {
		b.WriteString("Title: ")
		b.WriteString(cm.Title)
		b.WriteString("\n")
	}
	if cm.Description != "" {
		b.WriteString("Description: ")
		b.WriteString(cm.Description)
		b.WriteString("\n")
	}
	if cm.Metadata.OriginalPrompt != "" {
		b.WriteString("Prompt: ")
		b.WriteString(cm.Metadata.OriginalPrompt)
		b.WriteString("\n")
	}
	if cm.Metadata.GenerationTimestamp != "" {
		b.WriteString("Generated: ")
		b.WriteString(cm.Metadata.GenerationTimestamp)
		b.WriteString("\n")
	}
	if cm.Metadata.Mode != "" {
		b.WriteString("Mode: ")
		b.WriteString(cm.Metadata.Mode)
		b.WriteString("\n")
	}
	if cm.SchemaVersion > 0 {
		b.WriteString(fmt.Sprintf("Schema: %d\n", cm.SchemaVersion))
	}
	return strings.TrimSpace(b.String())
}

func buildChunkSummary(title, summary, content string) string {
	if summary != "" {
		return truncateRunes(fmt.Sprintf("%s - %s", title, summary), 240)
	}
	return truncateRunes(content, 240)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func codemapIDFromName(name string) string {
	name = strings.TrimPrefix(name, "codemap://")
	name = strings.TrimPrefix(name, "codemap:")
	if idx := strings.Index(name, "#chunk:"); idx >= 0 {
		name = name[:idx]
	}
	return name
}
