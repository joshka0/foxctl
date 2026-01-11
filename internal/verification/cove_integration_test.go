//go:build integration

package verification

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/llms"
)

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func getTestLLM(t *testing.T) core.LLM {
	t.Helper()
	llms.EnsureFactory()

	if apiKey := os.Getenv("GROQ_API_KEY"); apiKey != "" {
		llm, err := llms.NewOpenAICompatible("groq", "llama-3.1-8b-instant",
			"https://api.groq.com/openai", llms.WithAPIKey(apiKey))
		if err != nil {
			t.Fatalf("failed to create Groq LLM: %v", err)
		}
		t.Log("Using Groq with llama-3.1-8b-instant")
		return llm
	}

	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		llm, err := llms.NewOpenAICompatible("openrouter", "meta-llama/llama-3.1-8b-instruct:free",
			"https://openrouter.ai/api", llms.WithAPIKey(apiKey))
		if err != nil {
			t.Fatalf("failed to create OpenRouter LLM: %v", err)
		}
		t.Log("Using OpenRouter with llama-3.1-8b-instruct:free")
		return llm
	}

	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		llm, err := llms.NewGeminiLLM(apiKey, "gemini-2.0-flash")
		if err != nil {
			t.Fatalf("failed to create Gemini LLM: %v", err)
		}
		t.Log("Using Gemini")
		return llm
	}

	t.Skip("No API key found (GROQ_API_KEY, OPENROUTER_API_KEY, or GEMINI_API_KEY)")
	return nil
}

func TestCoVeIntegration_FullPipeline(t *testing.T) {
	llm := getTestLLM(t)

	cove := NewCoVe(llm, CoVeConfig{
		MaxVerifiers:        3,
		VerificationTimeout: 45 * time.Second,
		BaselineTimeout:     60 * time.Second,
		RefinementTimeout:   60 * time.Second,
		ExtractionTimeout:   30 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	response, err := cove.Run(ctx, CoVeRequest{
		Question: "What is the capital of France and what is its approximate population?",
	})
	if err != nil {
		t.Fatalf("CoVe.Run() error: %v", err)
	}

	t.Logf("\n=== BASELINE RESPONSE ===\n%s\n", response.BaselineResponse)
	t.Logf("\n=== CLAIMS EXTRACTED (%d) ===", len(response.Claims))
	for i, c := range response.Claims {
		t.Logf("  [%s] %s", c.ID, c.Text)
		if i >= 4 {
			t.Logf("  ... and %d more", len(response.Claims)-5)
			break
		}
	}

	t.Logf("\n=== VERIFICATION RESULTS ===")
	t.Logf("  Total: %d | True: %d | False: %d | Uncertain: %d | Errors: %d",
		response.Verification.TotalClaims,
		response.Verification.TrueCount,
		response.Verification.FalseCount,
		response.Verification.UncertainCount,
		response.Verification.ErrorCount)

	for id, vr := range response.Verification.Results {
		status := "✓"
		if vr.Verdict == VerdictFalse {
			status = "✗"
		} else if vr.Verdict == VerdictUncertain {
			status = "?"
		}
		t.Logf("  %s [%s] %s", status, id, truncate(vr.RawOutput, 80))
	}

	t.Logf("\n=== FINAL ANSWER ===\n%s", response.FinalAnswer)

	t.Logf("\n=== METRICS ===")
	t.Logf("  Baseline:     %v", response.Metrics.BaselineDuration.Round(time.Millisecond))
	t.Logf("  Extraction:   %v", response.Metrics.ExtractionDuration.Round(time.Millisecond))
	t.Logf("  Verification: %v", response.Metrics.VerificationDuration.Round(time.Millisecond))
	t.Logf("  Refinement:   %v", response.Metrics.RefinementDuration.Round(time.Millisecond))
	t.Logf("  TOTAL:        %v", response.Metrics.TotalDuration.Round(time.Millisecond))

	// Assertions
	if response.BaselineResponse == "" {
		t.Error("BaselineResponse is empty")
	}
	if len(response.Claims) == 0 {
		t.Error("No claims extracted")
	}
	if response.FinalAnswer == "" {
		t.Error("FinalAnswer is empty")
	}
}

func TestCoVeIntegration_VerifyOnly(t *testing.T) {
	llm := getTestLLM(t)

	cove := NewCoVe(llm, DefaultCoVeConfig())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	claims := []Claim{
		{ID: "c1", Text: "Paris is the capital of France", Category: "factual"},
		{ID: "c2", Text: "The Eiffel Tower is located in London", Category: "factual"},
		{ID: "c3", Text: "Water boils at 100 degrees Celsius at sea level", Category: "factual"},
	}

	t.Log("\n=== VERIFYING CLAIMS ===")
	for _, c := range claims {
		t.Logf("  [%s] %s", c.ID, c.Text)
	}

	result, err := cove.VerifyOnly(ctx, "Geography and science facts", claims)
	if err != nil {
		t.Fatalf("VerifyOnly() error: %v", err)
	}

	t.Logf("\n=== RESULTS ===")
	for _, c := range claims {
		vr := result.Results[c.ID]
		status := "✓ TRUE"
		if vr.Verdict == VerdictFalse {
			status = "✗ FALSE"
		} else if vr.Verdict == VerdictUncertain {
			status = "? UNCERTAIN"
		}
		t.Logf("  [%s] %s - %s", c.ID, status, vr.Evidence)
	}

	t.Logf("\nDuration: %v", result.TotalDuration.Round(time.Millisecond))

	// c1 should be True (Paris IS the capital of France)
	if c1 := result.Results["c1"]; c1.Verdict != VerdictTrue {
		t.Errorf("Claim c1 (Paris is capital) expected True, got %s", c1.Verdict)
	}

	// c2 should be False (Eiffel Tower is NOT in London)
	if c2 := result.Results["c2"]; c2.Verdict != VerdictFalse {
		t.Errorf("Claim c2 (Eiffel Tower in London) expected False, got %s", c2.Verdict)
	}

	// c3 should be True (water does boil at 100C at sea level)
	if c3 := result.Results["c3"]; c3.Verdict != VerdictTrue {
		t.Errorf("Claim c3 (water boils at 100C) expected True, got %s", c3.Verdict)
	}
}
