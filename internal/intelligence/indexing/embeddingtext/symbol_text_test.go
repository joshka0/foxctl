package embeddingtext

import (
	"strings"
	"testing"
)

func TestBuildSymbolEmbeddingText(t *testing.T) {
	info := SymbolInfo{
		Name:      "SearchHybrid",
		Kind:      "function",
		Package:   "internal/storage/memory",
		Signature: "func(ctx, query, vec, workspace, limit) ([]SearchResult, error)",
		Doc:       "SearchHybrid performs combined BM25 + vector search.",
		Calls:     []string{"SearchBM25", "SearchVector"},
	}

	text := BuildSymbolEmbeddingText(info, DefaultSymbolTextOptionsSummaryOnly())
	if !strings.Contains(text, "[function] SearchHybrid") {
		t.Fatalf("expected header, got: %s", text)
	}
	if !strings.Contains(text, "Signature:") {
		t.Fatalf("expected signature, got: %s", text)
	}
	if !strings.Contains(text, "Documentation:") {
		t.Fatalf("expected documentation, got: %s", text)
	}
	if strings.Contains(text, "Calls:") {
		t.Fatalf("did not expect calls in summary-only mode: %s", text)
	}
}

func TestBuildSymbolEmbeddingText_DedupSortCalls(t *testing.T) {
	info := SymbolInfo{
		Name:  "Example",
		Kind:  "function",
		Calls: []string{"B", "A", "B"},
	}

	text := BuildSymbolEmbeddingText(info, DefaultSymbolTextOptionsDocEnriched())
	if !strings.Contains(text, "Calls: A, B") {
		t.Fatalf("expected sorted calls, got: %s", text)
	}
}

func TestBuildSymbolEmbeddingText_IncludesAliases(t *testing.T) {
	info := SymbolInfo{
		Name:    "Jido.AgentServer.SignalRouter",
		Kind:    "class",
		Aliases: []string{"jido agent server signal router", "signal_router"},
	}

	text := BuildSymbolEmbeddingText(info, DefaultSymbolTextOptionsDocEnriched())
	if !strings.Contains(text, "Aliases:") {
		t.Fatalf("expected aliases, got: %s", text)
	}
	if !strings.Contains(text, "signal_router") {
		t.Fatalf("expected alias value, got: %s", text)
	}
}

func TestBuildSymbolAliases(t *testing.T) {
	info := SymbolInfo{
		Name:     "Jido.AgentServer.SignalRouter",
		FilePath: "lib/jido/agent_server/signal_router.ex",
		Package:  "lib/jido/agent_server",
	}

	aliases := BuildSymbolAliases(info)
	joined := strings.Join(aliases, "\n")
	for _, want := range []string{"jido agent server signal router", "signal_router", "agent_server signal_router"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected alias %q in %v", want, aliases)
		}
	}
}
