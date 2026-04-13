package main

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/secretutil"
	"github.com/jkatigb/agentctl/internal/intelligence/codecontext"
	"github.com/jkatigb/agentctl/internal/intelligence/codecontext/guard"
)

func TestScanForSecrets_NoSecrets(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()

	evidence := &codecontext.Evidence{
		Snippets: []codecontext.Snippet{
			{
				File:      "main.go",
				StartLine: 1,
				EndLine:   5,
				Text:      "func main() {\n\tfmt.Println(\"Hello\")\n}",
			},
		},
	}

	warnings, hasHigh := secretutil.ScanEvidence(ctx, evidence, logger, guard.ModeWarn)

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %d", len(warnings))
	}
	if hasHigh {
		t.Error("expected hasHighSeverity=false")
	}
}

func TestScanForSecrets_HighSeveritySecret(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()

	evidence := &codecontext.Evidence{
		Snippets: []codecontext.Snippet{
			{
				File:      "config.go",
				StartLine: 10,
				EndLine:   15,
				Text:      "const apiKey = \"AKIAIOSFODNN7EXAMPLE\"",
			},
		},
	}

	warnings, hasHigh := secretutil.ScanEvidence(ctx, evidence, logger, guard.ModeWarn)

	if len(warnings) == 0 {
		t.Fatal("expected warnings, got none")
	}
	if !hasHigh {
		t.Error("expected hasHighSeverity=true")
	}

	// Check warning details
	w := warnings[0]
	if w.Pattern != "aws_access_key" {
		t.Errorf("expected pattern 'aws_access_key', got %q", w.Pattern)
	}
	if w.Severity != "high" {
		t.Errorf("expected severity 'high', got %q", w.Severity)
	}
}

func TestScanForSecrets_EmptyEvidence(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()

	// Nil evidence
	warnings, hasHigh := secretutil.ScanEvidence(ctx, nil, logger, guard.ModeWarn)
	if warnings != nil {
		t.Error("expected nil warnings for nil evidence")
	}
	if hasHigh {
		t.Error("expected hasHighSeverity=false for nil evidence")
	}

	// Empty snippets
	evidence := &codecontext.Evidence{
		Snippets: []codecontext.Snippet{},
	}
	warnings, hasHigh = secretutil.ScanEvidence(ctx, evidence, logger, guard.ModeWarn)
	if warnings != nil {
		t.Error("expected nil warnings for empty snippets")
	}
	if hasHigh {
		t.Error("expected hasHighSeverity=false for empty snippets")
	}
}

func TestRenderEvidenceForLLM(t *testing.T) {
	evidence := &codecontext.Evidence{
		Snippets: []codecontext.Snippet{
			{
				File:      "main.go",
				StartLine: 1,
				EndLine:   5,
				Text:      "func main() {\n\tfmt.Println(\"Hello\")\n}",
			},
			{
				File:      "util.go",
				StartLine: 10,
				EndLine:   15,
				SymbolID:  "util.go:Helper",
				Text:      "func Helper() string {\n\treturn \"help\"\n}",
			},
		},
	}

	output := renderEvidenceForLLM(evidence)

	// Check that output contains file references
	if len(output) == 0 {
		t.Error("expected non-empty output")
	}
	if !contains(output, "main.go") {
		t.Error("expected output to contain 'main.go'")
	}
	if !contains(output, "util.go") {
		t.Error("expected output to contain 'util.go'")
	}
	if !contains(output, "Symbol: util.go:Helper") {
		t.Error("expected output to contain symbol reference")
	}
}

func TestDetectProvider(t *testing.T) {
	// Store original values
	cerebras := getEnvDefault("CEREBRAS_API_KEY", "")
	anthropic := getEnvDefault("ANTHROPIC_API_KEY", "")
	openai := getEnvDefault("OPENAI_API_KEY", "")

	// Test priority: cerebras > anthropic > openai
	// Note: This is just testing the function logic, not modifying env

	provider := detectProvider()
	// Should return some provider (we don't know which keys are set)
	if provider == "" {
		t.Error("expected a provider to be returned")
	}

	// Valid providers
	validProviders := map[string]bool{
		"cerebras":  true,
		"anthropic": true,
		"openai":    true,
	}
	if !validProviders[provider] {
		t.Errorf("unexpected provider: %s", provider)
	}

	_ = cerebras
	_ = anthropic
	_ = openai
}

func TestGenerateOverallSummary(t *testing.T) {
	analyses := []PerspectiveAnalysis{
		{
			Perspective: "security",
			Summary:     "No major issues found",
			Score:       0.9,
		},
		{
			Perspective: "performance",
			Summary:     "Some optimization opportunities",
			Score:       0.7,
		},
	}

	summary := generateOverallSummary(analyses)

	if len(summary) == 0 {
		t.Error("expected non-empty summary")
	}
	if !contains(summary, "No major issues found") {
		t.Error("expected summary to contain security analysis")
	}
	if !contains(summary, "Some optimization opportunities") {
		t.Error("expected summary to contain performance analysis")
	}
	if !contains(summary, "Overall Score") {
		t.Error("expected summary to contain overall score")
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func getEnvDefault(key, def string) string {
	// This is a helper for testing, actual implementation would use os.Getenv
	return def
}
