package main

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/secretutil"
	"github.com/jkatigb/agentctl/internal/codecontext"
	"github.com/jkatigb/agentctl/internal/codecontext/guard"
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

	findings, hasHigh := secretutil.ScanEvidence(ctx, evidence, logger, guard.ModeBlock)

	if len(findings) != 0 {
		t.Errorf("expected no findings, got %d", len(findings))
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

	findings, hasHigh := secretutil.ScanEvidence(ctx, evidence, logger, guard.ModeBlock)

	if len(findings) == 0 {
		t.Fatal("expected findings, got none")
	}
	if !hasHigh {
		t.Error("expected hasHighSeverity=true")
	}

	// Check finding details
	f := findings[0]
	if f.Pattern != "aws_access_key" {
		t.Errorf("expected pattern 'aws_access_key', got %q", f.Pattern)
	}
	if f.Severity != "high" {
		t.Errorf("expected severity 'high', got %q", f.Severity)
	}
	if f.File != "config.go" {
		t.Errorf("expected file 'config.go', got %q", f.File)
	}
}

func TestScanForSecrets_MediumSeveritySecret(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()

	evidence := &codecontext.Evidence{
		Snippets: []codecontext.Snippet{
			{
				File:      "db.go",
				StartLine: 5,
				EndLine:   10,
				Text:      "password = \"mySecretPassword123\"",
			},
		},
	}

	findings, hasHigh := secretutil.ScanEvidence(ctx, evidence, logger, guard.ModeBlock)

	if len(findings) == 0 {
		t.Fatal("expected findings, got none")
	}
	// Medium severity should not trigger hasHigh
	if hasHigh {
		t.Error("expected hasHighSeverity=false for medium severity")
	}

	f := findings[0]
	if f.Severity != "medium" {
		t.Errorf("expected severity 'medium', got %q", f.Severity)
	}
}

func TestScanForSecrets_EmptyEvidence(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()

	// Nil evidence
	findings, hasHigh := secretutil.ScanEvidence(ctx, nil, logger, guard.ModeBlock)
	if findings != nil {
		t.Error("expected nil findings for nil evidence")
	}
	if hasHigh {
		t.Error("expected hasHighSeverity=false for nil evidence")
	}

	// Empty snippets
	evidence := &codecontext.Evidence{
		Snippets: []codecontext.Snippet{},
	}
	findings, hasHigh = secretutil.ScanEvidence(ctx, evidence, logger, guard.ModeBlock)
	if findings != nil {
		t.Error("expected nil findings for empty snippets")
	}
	if hasHigh {
		t.Error("expected hasHighSeverity=false for empty snippets")
	}
}

func TestScanForSecrets_LineNumberAdjustment(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()

	// Snippet starting at line 100
	evidence := &codecontext.Evidence{
		Snippets: []codecontext.Snippet{
			{
				File:      "secrets.go",
				StartLine: 100,
				EndLine:   105,
				Text:      "line1\nAKIAIOSFODNN7EXAMPLE\nline3",
			},
		},
	}

	findings, _ := secretutil.ScanEvidence(ctx, evidence, logger, guard.ModeBlock)

	if len(findings) == 0 {
		t.Fatal("expected findings, got none")
	}

	// Secret is on line 2 of the snippet, which is line 101 in the file
	if findings[0].Line != 101 {
		t.Errorf("expected line 101 (100 + 2 - 1), got %d", findings[0].Line)
	}
}

func TestMapMode(t *testing.T) {
	tests := []struct {
		input    string
		expected codecontext.RenderMode
	}{
		{"general", codecontext.ModeSnippets},
		{"structure", codecontext.ModeStructure},
		{"flow", codecontext.ModeFlow},
		{"masked", codecontext.ModeMasked},
		{"unknown", codecontext.ModeSnippets}, // default
		{"", codecontext.ModeSnippets},        // empty
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapMode(tt.input)
			if got != tt.expected {
				t.Errorf("mapMode(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
