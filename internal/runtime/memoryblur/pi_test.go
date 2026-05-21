package memoryblur

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
)

func TestPiAgentReal(t *testing.T) {
	if os.Getenv("FOXCTL_TEST_REAL_PI") != "1" {
		t.Skip("set FOXCTL_TEST_REAL_PI=1 to exercise the real Pi blur agent")
	}
	agent := NewPiAgent(PiAgentOptions{
		PiBin:        firstNonEmpty(os.Getenv("FOXCTL_TEST_PI_BIN"), "pi"),
		Mode:         firstNonEmpty(os.Getenv("FOXCTL_TEST_PI_MODE"), PiModeSDK),
		SDKBin:       firstNonEmpty(os.Getenv("FOXCTL_TEST_PI_SDK_BIN"), "bun"),
		SDKScript:    os.Getenv("FOXCTL_TEST_PI_SDK_SCRIPT"),
		NoExtensions: true,
		Timeout:      90 * time.Second,
	})
	output, raw, err := agent.BlurMemory(context.Background(), contextplane.MemoryBlurAgentPromptInput{
		ID:             "test",
		OriginalDomain: "literal domain",
		Summary:        "Literal component handled a domain-specific operation.",
		LiteralText:    "Literal component handled a domain-specific operation.",
		Shape: contextplane.MemoryStructuralShape{
			Mechanism:  "bounded local coordination",
			Actors:     []string{"local actor", "neighbor actor"},
			Operations: []string{"detect signal", "coordinate response"},
		},
		ForbiddenTerms: []string{"literal domain", "Literal component"},
	})
	if err != nil {
		t.Fatalf("BlurMemory() error = %v raw=%s", err, raw)
	}
	validation := contextplane.ValidateMemoryBlurAgentOutput(contextplane.MemoryBlurAgentPromptInput{
		ID:             "test",
		OriginalDomain: "literal domain",
		Summary:        "Literal component handled a domain-specific operation.",
		Shape: contextplane.MemoryStructuralShape{
			Mechanism: "bounded local coordination",
		},
		ForbiddenTerms: []string{"literal domain", "Literal component"},
	}, output)
	if !validation.Valid {
		t.Fatalf("validation errors=%v leaked=%v raw=%s", validation.Errors, validation.LeakedTerms, raw)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
