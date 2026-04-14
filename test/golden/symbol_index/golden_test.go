package symbolindex_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/symbol"
)

var updateGolden = flag.Bool("update", false, "update golden files")

// GoldenSymbol is a simplified symbol representation for golden tests.
// Only includes fields that should be stable across re-indexing.
type GoldenSymbol struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	StartLine     int      `json:"start_line"`
	EndLine       int      `json:"end_line"`
	Documentation string   `json:"documentation,omitempty"`
	Signature     string   `json:"signature,omitempty"`
	Calls         []string `json:"calls,omitempty"`
}

// GoldenOutput is the complete golden file format.
type GoldenOutput struct {
	File    string         `json:"file"`
	Symbols []GoldenSymbol `json:"symbols"`
}

func TestGoldenSymbolIndex(t *testing.T) {
	// Load fixture
	fixtureDir := "fixtures"
	fixturePath := "fixture.go"
	goldenPath := filepath.Join(fixtureDir, "fixture.golden.json")

	content, err := os.ReadFile(filepath.Join(fixtureDir, fixturePath))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// Extract symbols
	extractor := symbol.NewGoExtractor()
	ctx := context.Background()

	symbols, err := extractor.Extract(ctx, fixturePath, content)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}

	// Build golden output
	output := GoldenOutput{
		File:    fixturePath,
		Symbols: make([]GoldenSymbol, 0, len(symbols)),
	}

	for _, sym := range symbols {
		// Extract calls for this symbol - errors are non-fatal for golden generation.
		calls, _ := extractor.ExtractCalls(ctx, sym, content) //nolint:errcheck

		gs := GoldenSymbol{
			ID:            sym.ID,
			Name:          sym.Name,
			Kind:          string(sym.Kind),
			StartLine:     sym.StartLine,
			EndLine:       sym.EndLine,
			Documentation: sym.Documentation,
			Signature:     sym.Signature,
			Calls:         calls,
		}
		output.Symbols = append(output.Symbols, gs)
	}

	// Sort symbols by ID for stable output
	sort.Slice(output.Symbols, func(i, j int) bool {
		return output.Symbols[i].ID < output.Symbols[j].ID
	})

	// Marshal current output
	currentJSON, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}

	// Update or compare
	if *updateGolden {
		if err := os.WriteFile(goldenPath, currentJSON, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("Updated golden file: %s", goldenPath)
		return
	}

	// Read and compare to golden
	goldenJSON, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}

	if string(currentJSON) != string(goldenJSON) {
		t.Errorf("symbol index output differs from golden.\n\nGot:\n%s\n\nWant:\n%s\n\nRun with -update to update golden file.",
			string(currentJSON), string(goldenJSON))
	}
}
