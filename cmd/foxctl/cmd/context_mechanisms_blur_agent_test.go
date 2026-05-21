package cmd

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
)

func TestRunPiMemoryBlurAgentReal(t *testing.T) {
	if os.Getenv("FOXCTL_TEST_REAL_PI") != "1" {
		t.Skip("set FOXCTL_TEST_REAL_PI=1 to exercise the real Pi blur agent")
	}
	raw, err := runPiMemoryBlurAgent(context.Background(), repoSymbolBlurAgentOptions{
		AgentBin:       firstNonEmpty(os.Getenv("FOXCTL_TEST_PI_BIN"), "pi"),
		PiNoExtensions: true,
		Timeout:        90 * time.Second,
	}, `Return exactly one JSON object:
{"abstract_schema":"local actors detect bounded contention and coordinate a constrained response","mechanism_tags":["bounded_coordination"],"domains_to_avoid":["forbidden literal"],"confidence":0.8,"leakage_risk":0.01}`)
	if err != nil {
		t.Fatalf("runPiMemoryBlurAgent() error = %v", err)
	}
	output, err := contextplane.ParseMemoryBlurAgentOutput(raw)
	if err != nil {
		t.Fatalf("ParseMemoryBlurAgentOutput() error = %v\nraw=%s", err, raw)
	}
	validation := contextplane.ValidateMemoryBlurAgentOutput(contextplane.MemoryBlurAgentPromptInput{
		ID:             "test",
		OriginalDomain: "literal domain",
		Summary:        "literal source",
		Shape: contextplane.MemoryStructuralShape{
			Mechanism: "bounded coordination",
		},
		ForbiddenTerms: []string{"literal domain"},
	}, output)
	if !validation.Valid {
		t.Fatalf("validation errors=%v leaked=%v raw=%s", validation.Errors, validation.LeakedTerms, raw)
	}
}
