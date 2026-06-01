// Package main implements the quality/gate skill.
package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain/lite"
)

const command = "quality/gate"

var severityOrder = []string{"critical", "high", "medium", "low", "info", "unknown"}

var severityRank = map[string]int{
	"critical": 6,
	"high":     5,
	"medium":   4,
	"low":      3,
	"info":     2,
	"unknown":  1,
}

type input struct {
	Subject       string         `json:"subject"`
	Scope         string         `json:"scope"`
	LanguageHints []string       `json:"language_hints"`
	Findings      []findingIn    `json:"findings"`
	Checks        []checkIn      `json:"checks"`
	Policy        policyInput    `json:"policy"`
	Metadata      map[string]any `json:"metadata"`
}

type findingIn struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	Category string   `json:"category"`
	Severity string   `json:"severity"`
	Summary  string   `json:"summary"`
	Detail   string   `json:"detail"`
	Evidence []string `json:"evidence"`
	Blocking bool     `json:"blocking"`
	Waived   bool     `json:"waived"`
}

type finding struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	Category string   `json:"category"`
	Severity string   `json:"severity"`
	Summary  string   `json:"summary"`
	Detail   string   `json:"detail"`
	Evidence []string `json:"evidence"`
	Blocking bool     `json:"blocking"`
	Waived   bool     `json:"waived"`
}

type checkIn struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
	Notes    string `json:"notes"`
}

type check struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Required bool   `json:"required"`
	Status   string `json:"status"`
	Notes    string `json:"notes"`
}

type policyInput struct {
	BlockOnSeverities        []string `json:"block_on_severities"`
	MaxWarnFindings          *int     `json:"max_warn_findings"`
	RequireAllRequiredChecks *bool    `json:"require_all_required_checks"`
	FailOnUnknownSeverity    *bool    `json:"fail_on_unknown_severity"`
}

type policy struct {
	BlockOnSeverities        []string `json:"block_on_severities"`
	MaxWarnFindings          int      `json:"max_warn_findings"`
	RequireAllRequiredChecks bool     `json:"require_all_required_checks"`
	FailOnUnknownSeverity    bool     `json:"fail_on_unknown_severity"`
}

// main is the skill entry point for quality/gate.
func main() {
	lite.Main(command, run)
}

func run(ctx context.Context, rc *lite.RunContext, in input) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	subject := strings.TrimSpace(in.Subject)
	if subject == "" {
		subject = "change-set"
	}
	scope := strings.TrimSpace(in.Scope)
	if scope == "" {
		scope = "default"
	}

	appliedPolicy, blockSet := resolvePolicy(in.Policy)
	languageHints := normalizeStringList(in.LanguageHints)

	findings, unknownCount := normalizeFindings(in.Findings)
	checks := normalizeChecks(in.Checks)

	blockingFindings, warningFindings, waivedFindings, severityCounts := evaluateFindings(findings, blockSet, appliedPolicy.FailOnUnknownSeverity)
	failedChecks, incompleteRequiredChecks, advisoryChecks := evaluateChecks(checks, appliedPolicy.RequireAllRequiredChecks)

	decision := "approved"
	if len(blockingFindings) > 0 ||
		len(failedChecks) > 0 ||
		len(incompleteRequiredChecks) > 0 ||
		warningFindings > appliedPolicy.MaxWarnFindings {
		decision = "needs-changes"
	} else if warningFindings > 0 || len(advisoryChecks) > 0 {
		decision = "approved-with-known-risks"
	}

	recommendations := buildRecommendations(
		decision,
		len(blockingFindings),
		len(failedChecks),
		len(incompleteRequiredChecks),
		warningFindings,
		appliedPolicy.MaxWarnFindings,
	)

	rationale := fmt.Sprintf(
		"decision=%s blockers=%d failed_checks=%d incomplete_required_checks=%d warning_findings=%d max_warn_findings=%d waived=%d unknown_severity=%d",
		decision,
		len(blockingFindings),
		len(failedChecks),
		len(incompleteRequiredChecks),
		warningFindings,
		appliedPolicy.MaxWarnFindings,
		waivedFindings,
		unknownCount,
	)

	gateNote := fmt.Sprintf(
		"subject=%s scope=%s decision=%s blockers=%d failed_checks=%d warnings=%d waived=%d",
		subject,
		scope,
		decision,
		len(blockingFindings),
		len(failedChecks)+len(incompleteRequiredChecks),
		warningFindings,
		waivedFindings,
	)

	data := map[string]any{
		"subject":        subject,
		"scope":          scope,
		"language_hints": languageHints,
		"metadata":       in.Metadata,
		"decision":       decision,
		"rationale":      rationale,
		"policy":         appliedPolicy,
		"summary": map[string]any{
			"total_findings":             len(findings),
			"blocking_findings":          len(blockingFindings),
			"warning_findings":           warningFindings,
			"waived_findings":            waivedFindings,
			"unknown_severity_findings":  unknownCount,
			"total_checks":               len(checks),
			"failed_checks":              len(failedChecks),
			"incomplete_required_checks": len(incompleteRequiredChecks),
			"advisory_checks":            len(advisoryChecks),
		},
		"findings_by_severity":       orderedSeverityCounts(severityCounts),
		"blocking_findings":          blockingFindings,
		"failed_checks":              failedChecks,
		"incomplete_required_checks": incompleteRequiredChecks,
		"advisory_checks":            advisoryChecks,
		"recommendations":            recommendations,
		"gate_note":                  gateNote,
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	return lite.Emit(rc, command, data)
}

func resolvePolicy(in policyInput) (policy, map[string]struct{}) {
	out := policy{
		BlockOnSeverities:        []string{"critical", "high"},
		MaxWarnFindings:          5,
		RequireAllRequiredChecks: true,
		FailOnUnknownSeverity:    false,
	}

	if len(in.BlockOnSeverities) > 0 {
		out.BlockOnSeverities = normalizeSeverityList(in.BlockOnSeverities)
	}
	if in.MaxWarnFindings != nil {
		out.MaxWarnFindings = *in.MaxWarnFindings
		if out.MaxWarnFindings < 0 {
			out.MaxWarnFindings = 0
		}
	}
	if in.RequireAllRequiredChecks != nil {
		out.RequireAllRequiredChecks = *in.RequireAllRequiredChecks
	}
	if in.FailOnUnknownSeverity != nil {
		out.FailOnUnknownSeverity = *in.FailOnUnknownSeverity
	}

	blockSet := make(map[string]struct{}, len(out.BlockOnSeverities))
	for _, s := range out.BlockOnSeverities {
		blockSet[s] = struct{}{}
	}

	return out, blockSet
}

func normalizeFindings(in []findingIn) ([]finding, int) {
	out := make([]finding, 0, len(in))
	unknownCount := 0

	for i, f := range in {
		id := strings.TrimSpace(f.ID)
		if id == "" {
			id = fmt.Sprintf("finding-%d", i+1)
		}
		source := strings.TrimSpace(f.Source)
		if source == "" {
			source = "review"
		}
		category := strings.TrimSpace(f.Category)
		if category == "" {
			category = "general"
		}
		summary := strings.TrimSpace(f.Summary)
		if summary == "" {
			summary = "unspecified finding"
		}
		severity := normalizeSeverity(f.Severity)
		if severity == "unknown" {
			unknownCount++
		}

		out = append(out, finding{
			ID:       id,
			Source:   source,
			Category: category,
			Severity: severity,
			Summary:  summary,
			Detail:   strings.TrimSpace(f.Detail),
			Evidence: normalizeStringList(f.Evidence),
			Blocking: f.Blocking,
			Waived:   f.Waived,
		})
	}

	sortFindings(out)
	return out, unknownCount
}

func normalizeChecks(in []checkIn) []check {
	out := make([]check, 0, len(in))
	for i, c := range in {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			id = fmt.Sprintf("check-%d", i+1)
		}
		title := strings.TrimSpace(c.Title)
		if title == "" {
			title = "unnamed-check"
		}
		out = append(out, check{
			ID:       id,
			Title:    title,
			Required: c.Required,
			Status:   normalizeCheckStatus(c.Status),
			Notes:    strings.TrimSpace(c.Notes),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required && !out[j].Required
		}
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Title < out[j].Title
	})
	return out
}

func evaluateFindings(findings []finding, blockSet map[string]struct{}, failOnUnknown bool) ([]finding, int, int, map[string]int) {
	blocking := make([]finding, 0)
	warningCount := 0
	waivedCount := 0
	severityCounts := make(map[string]int, len(severityOrder))

	for _, f := range findings {
		severityCounts[f.Severity]++
		if f.Waived {
			waivedCount++
			continue
		}
		_, policyBlocking := blockSet[f.Severity]
		unknownBlocking := failOnUnknown && f.Severity == "unknown"
		if f.Blocking || policyBlocking || unknownBlocking {
			if unknownBlocking {
				f.Blocking = true
			}
			blocking = append(blocking, f)
			continue
		}
		warningCount++
	}

	sortFindings(blocking)
	return blocking, warningCount, waivedCount, severityCounts
}

func evaluateChecks(checks []check, requireAllRequired bool) ([]check, []check, []check) {
	failed := make([]check, 0)
	incompleteRequired := make([]check, 0)
	advisory := make([]check, 0)

	for _, c := range checks {
		if c.Required {
			if c.Status == "fail" {
				failed = append(failed, c)
				continue
			}
			if c.Status != "pass" {
				if requireAllRequired {
					incompleteRequired = append(incompleteRequired, c)
				} else {
					advisory = append(advisory, c)
				}
			}
			continue
		}

		switch c.Status {
		case "fail", "warn", "unknown":
			advisory = append(advisory, c)
		}
	}

	sortChecks(failed)
	sortChecks(incompleteRequired)
	sortChecks(advisory)
	return failed, incompleteRequired, advisory
}

func normalizeSeverity(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "critical", "blocker", "fatal":
		return "critical"
	case "high", "error":
		return "high"
	case "medium", "med", "warning", "warn":
		return "medium"
	case "low", "minor":
		return "low"
	case "info", "informational", "note":
		return "info"
	default:
		return "unknown"
	}
}

func normalizeSeverityList(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		sev := normalizeSeverity(s)
		if sev == "unknown" {
			continue
		}
		if _, ok := seen[sev]; ok {
			continue
		}
		seen[sev] = struct{}{}
		out = append(out, sev)
	}
	if len(out) == 0 {
		out = []string{"critical", "high"}
	}
	sort.Slice(out, func(i, j int) bool {
		return severityRank[out[i]] > severityRank[out[j]]
	})
	return out
}

func normalizeCheckStatus(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "pass", "passed", "ok", "success":
		return "pass"
	case "fail", "failed", "error":
		return "fail"
	case "warn", "warning":
		return "warn"
	case "skip", "skipped", "n/a", "na":
		return "skip"
	default:
		return "unknown"
	}
}

func normalizeStringList(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		s := strings.TrimSpace(v)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func orderedSeverityCounts(counts map[string]int) []map[string]any {
	out := make([]map[string]any, 0, len(severityOrder))
	for _, sev := range severityOrder {
		out = append(out, map[string]any{
			"severity": sev,
			"count":    counts[sev],
		})
	}
	return out
}

func sortFindings(in []finding) {
	sort.Slice(in, func(i, j int) bool {
		li := severityRank[in[i].Severity]
		lj := severityRank[in[j].Severity]
		if li != lj {
			return li > lj
		}
		if in[i].Source != in[j].Source {
			return in[i].Source < in[j].Source
		}
		if in[i].ID != in[j].ID {
			return in[i].ID < in[j].ID
		}
		return in[i].Summary < in[j].Summary
	})
}

func sortChecks(in []check) {
	sort.Slice(in, func(i, j int) bool {
		if in[i].Required != in[j].Required {
			return in[i].Required && !in[j].Required
		}
		if in[i].ID != in[j].ID {
			return in[i].ID < in[j].ID
		}
		return in[i].Title < in[j].Title
	})
}

func buildRecommendations(
	decision string,
	blockers int,
	failedChecks int,
	incompleteRequired int,
	warningFindings int,
	maxWarn int,
) []string {
	recs := make([]string, 0, 4)
	if blockers > 0 {
		recs = append(recs, "Address blocking findings or mark explicit waivers with rationale.")
	}
	if failedChecks > 0 {
		recs = append(recs, "Resolve failed checks before merge.")
	}
	if incompleteRequired > 0 {
		recs = append(recs, "Complete required checks currently marked warn/skip/unknown.")
	}
	if warningFindings > maxWarn {
		recs = append(recs, "Reduce warning-level findings to stay within policy threshold.")
	}
	if len(recs) == 0 {
		switch decision {
		case "approved":
			recs = append(recs, "Proceed; no policy blockers detected.")
		case "approved-with-known-risks":
			recs = append(recs, "Proceed with follow-up tasks for advisory findings.")
		}
	}
	return recs
}
