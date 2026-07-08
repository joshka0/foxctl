// Command symbolembed backfills embeddings for named-memory entries that have
// none yet (chiefly the code_symbol entries), so semantic_search's fast vector
// path (SearchSimilarByType) works instead of re-embedding on every search.
// One-off maintenance tool: batches against the configured GPU embedder.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

func die(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...); os.Exit(1) }

func main() {
	ctx := context.Background()
	wsPath := os.Getenv("FOXCTL_WORKSPACE")
	if wsPath == "" {
		wsPath, _ = os.Getwd()
	}
	maxN := 0 // 0 = unlimited
	if v := os.Getenv("MAX"); v != "" {
		maxN, _ = strconv.Atoi(v)
	}
	batch := 32
	if v := os.Getenv("BATCH"); v != "" {
		if n, _ := strconv.Atoi(v); n > 0 {
			batch = n
		}
	}

	cfg, err := config.Load(ctx, config.WithWorkspacePath(wsPath))
	if err != nil {
		die("config: %v", err)
	}
	wsID := workspace.ID(wsPath)

	provider, err := semantic.NewProviderForModel(
		cfg.Embedding.Model, cfg,
		semantic.WithProvider(cfg.Embedding.Provider),
		semantic.WithAPIKey(cfg.Embedding.APIKey),
		semantic.WithBaseURL(cfg.Embedding.BaseURL),
	)
	if err != nil {
		die("provider: %v", err)
	}
	fmt.Fprintf(os.Stderr, "model=%s dim=%d ws=%s batch=%d max=%d\n", provider.Model(), provider.Dimensions(), wsID, batch, maxN)

	store, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		die("store: %v", err)
	}
	defer store.Close()

	// Snapshot the unembedded set ONCE via paginated reads. Re-querying
	// ListWithoutEmbedding after each write is O(n^2): every call re-scans past
	// the growing set of already-embedded rows, which is what makes a naive
	// backfill decelerate. One read pass up front avoids that.
	fmt.Fprintf(os.Stderr, "collecting unembedded entries...\n")
	var pending []storage.NamedEntry
	for offset := 0; ; {
		page, err := store.ListWithoutEmbeddingPage(ctx, wsID, 2000, offset)
		if err != nil {
			die("list page: %v", err)
		}
		if len(page) == 0 {
			break
		}
		offset += len(page)
		// Only embed types the vector search paths actually query. Call edges
		// and file-meta rows are relationship data, not searchable content.
		for _, e := range page {
			if e.Type == symbol.SymbolType || e.Type == symbol.FileSummaryType {
				pending = append(pending, e)
			}
		}
		if maxN > 0 && len(pending) >= maxN {
			pending = pending[:maxN]
			break
		}
	}
	fmt.Fprintf(os.Stderr, "pending=%d\n", len(pending))

	total, start := 0, time.Now()
	for i := 0; i < len(pending); i += batch {
		end := i + batch
		if end > len(pending) {
			end = len(pending)
		}
		chunk := pending[i:end]
		texts := make([]string, len(chunk))
		for j := range chunk {
			texts[j] = entryText(chunk[j])
		}
		embs, err := provider.EmbedBatch(ctx, texts)
		if err != nil || len(embs) != len(chunk) {
			fmt.Fprintf(os.Stderr, "embed batch failed (%v); stopping at %d\n", err, total)
			break
		}
		for j := range chunk {
			if len(embs[j]) == 0 {
				continue
			}
			if err := store.UpdateEmbedding(ctx, chunk[j].Name, wsID, embs[j]); err != nil {
				fmt.Fprintf(os.Stderr, "update %q: %v\n", chunk[j].Name, err)
			}
		}
		total += len(chunk)
		if total%320 == 0 || end == len(pending) {
			rate := float64(total) / time.Since(start).Seconds()
			eta := time.Duration(float64(len(pending)-total)/rate) * time.Second
			fmt.Fprintf(os.Stderr, "embedded %d/%d (%.1f/s, eta %s)\n", total, len(pending), rate, eta.Round(time.Second))
		}
	}
	fmt.Fprintf(os.Stderr, "DONE embedded=%d in %v\n", total, time.Since(start))
}

func entryText(e storage.NamedEntry) string {
	// Front-load the highest-signal fields (name + signature) and keep the text
	// short: fewer tokens per embed means less GPU work and far less thermal
	// throttling on a laptop card. Documentation/language/path are dropped.
	parts := []string{e.Name}
	if e.Type == symbol.SymbolType {
		if parsed, err := symbol.UnmarshalResult(e.Result); err == nil && parsed != nil {
			s := parsed.Symbol
			parts = append(parts, s.Signature, string(s.Kind))
		}
	}
	if e.Summary != "" {
		parts = append(parts, e.Summary)
	}
	text := strings.Join(nonEmpty(parts), "\n")
	if len(text) > 1000 {
		text = text[:1000]
	}
	return text
}

func nonEmpty(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
