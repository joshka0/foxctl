package secretutil

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/intelligence/codecontext"
	"github.com/jkatigb/agentctl/internal/intelligence/codecontext/guard"
)

// Finding represents a detected secret.
type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Pattern  string `json:"pattern"`
	Severity string `json:"severity"`
	Masked   string `json:"masked"`
}

// ScanEvidence scans all evidence snippets for secrets.
// Returns the findings and whether any high-severity secrets were detected.
func ScanEvidence(ctx context.Context, evidence *codecontext.Evidence, logger zerolog.Logger, mode guard.Mode) ([]Finding, bool) {
	if evidence == nil || len(evidence.Snippets) == 0 {
		return nil, false
	}

	scanner := guard.New(guard.Opts{Mode: mode})
	findings := make([]Finding, 0)
	hasHighSeverity := false

	for _, snippet := range evidence.Snippets {
		result := scanner.ScanString(ctx, snippet.File, snippet.Text)
		if !result.HasFindings() {
			continue
		}

		for _, f := range result.Findings {
			findings = append(findings, Finding{
				File:     snippet.File,
				Line:     snippet.StartLine + f.Line - 1, // Adjust to absolute line number
				Pattern:  f.Pattern,
				Severity: string(f.Severity),
				Masked:   f.Masked,
			})

			if f.Severity == guard.SeverityHigh {
				hasHighSeverity = true
			}
		}
	}

	if len(findings) > 0 {
		logger.Debug().Int("count", len(findings)).Bool("high_severity", hasHighSeverity).Msg("secrets detected in evidence")
	}

	return findings, hasHighSeverity
}
