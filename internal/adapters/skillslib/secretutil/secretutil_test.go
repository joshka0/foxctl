package secretutil

import (
	"context"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/intelligence/codecontext"
	"github.com/joshka0/foxctl/internal/intelligence/codecontext/guard"
	"github.com/rs/zerolog"
)

func TestScanEvidenceMapsFindingsToAbsoluteSnippetLines(t *testing.T) {
	evidence := &codecontext.Evidence{
		Snippets: []codecontext.Snippet{
			{
				File:      "config.ts",
				StartLine: 40,
				Text:      "const mode = 'test'\nconst stripe = 'sk_live_123456789012345678901234'\n",
			},
		},
	}

	findings, high := ScanEvidence(context.Background(), evidence, zerolog.Nop(), guard.ModeWarn)
	if !high {
		t.Fatal("expected high severity finding")
	}
	if len(findings) != 1 {
		t.Fatalf("finding count=%d want 1: %#v", len(findings), findings)
	}
	if findings[0].File != "config.ts" {
		t.Fatalf("file=%q want config.ts", findings[0].File)
	}
	if findings[0].Line != 41 {
		t.Fatalf("line=%d want 41", findings[0].Line)
	}
	if findings[0].Severity != string(guard.SeverityHigh) {
		t.Fatalf("severity=%q want high", findings[0].Severity)
	}
	if strings.Contains(findings[0].Masked, "123456789012345678901234") {
		t.Fatalf("masked finding leaked full secret: %q", findings[0].Masked)
	}
}

func TestScanEvidenceEmptyInputsReturnNoFindings(t *testing.T) {
	tests := []*codecontext.Evidence{
		nil,
		{},
		{Snippets: []codecontext.Snippet{{File: "safe.go", StartLine: 1, Text: "package safe\n"}}},
	}

	for _, evidence := range tests {
		findings, high := ScanEvidence(context.Background(), evidence, zerolog.Nop(), guard.ModeWarn)
		if high || len(findings) != 0 {
			t.Fatalf("ScanEvidence(%#v) = findings=%#v high=%v, want none/false", evidence, findings, high)
		}
	}
}

func TestScanEvidenceGeneratedStartLineOffsetsArePreserved(t *testing.T) {
	err := quick.Check(func(start uint16) bool {
		startLine := int(start%1000) + 1
		evidence := &codecontext.Evidence{
			Snippets: []codecontext.Snippet{{
				File:      "generated.env",
				StartLine: startLine,
				Text:      "safe=true\naws = 'AKIAIOSFODNN7EXAMPLE'\n",
			}},
		}

		findings, high := ScanEvidence(context.Background(), evidence, zerolog.Nop(), guard.ModeWarn)
		return high && len(findings) == 1 && findings[0].Line == startLine+1
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}
