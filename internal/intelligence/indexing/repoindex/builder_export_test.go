package repoindex

import (
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
)

func TestIsExportedSymbolTreatsGoMethodsByMethodSegment(t *testing.T) {
	t.Parallel()

	if !isExportedSymbol(symbol.Symbol{
		Language: "go",
		Kind:     symbol.KindMethod,
		Name:     "AgentActor.HandleAsk",
	}) {
		t.Fatal("expected exported Go method to be exported")
	}
	if isExportedSymbol(symbol.Symbol{
		Language: "go",
		Kind:     symbol.KindMethod,
		Name:     "AgentActor.handleAsk",
	}) {
		t.Fatal("expected unexported Go method to stay unexported")
	}
}
