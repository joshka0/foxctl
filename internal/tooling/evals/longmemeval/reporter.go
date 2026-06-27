package longmemeval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Summarize computes aggregate metrics from per-case results.
func Summarize(cases []CaseResult) Metrics {
	m := Metrics{CaseCount: len(cases)}
	if len(cases) == 0 {
		return m
	}
	var (
		hits5, hits10, hits50, hits100 int
		mrrSum                         float64
		latencySum                     int64
	)
	for _, c := range cases {
		if c.Error != "" {
			m.FailureCount++
			continue
		}
		if c.HitAt5 {
			hits5++
		}
		if c.HitAt10 {
			hits10++
		}
		if c.HitAt50 {
			hits50++
		}
		if c.HitAt100 {
			hits100++
		}
		mrrSum += c.ReciprocalRank
		latencySum += c.DurationMS
	}
	denom := float64(m.CaseCount - m.FailureCount)
	if denom > 0 {
		m.HitAt5 = float64(hits5) / denom
		m.HitAt10 = float64(hits10) / denom
		m.HitAt50 = float64(hits50) / denom
		m.HitAt100 = float64(hits100) / denom
		m.MRR = mrrSum / denom
		m.MeanLatencyMS = float64(latencySum) / denom
	}
	return m
}

func mergeAnswerMetrics(metrics *Metrics, cases []CaseResult) {
	if metrics == nil {
		return
	}
	metrics.AnswerCaseCount = len(cases)
	if len(cases) == 0 {
		return
	}
	var (
		scoreSum   float64
		latencySum int64
	)
	for _, c := range cases {
		if c.Error != "" {
			metrics.AnswerFailureCount++
			continue
		}
		if c.AnswerMatched {
			metrics.AnswerMatchedCount++
		}
		scoreSum += c.AnswerScore
		latencySum += c.AnswerDurationMS
		if c.AnswerJudgeScore != 0 || c.AnswerJudgeReason != "" {
			metrics.AnswerJudgeCaseCount++
			if c.AnswerJudgeScore > 0 {
				metrics.AnswerJudgeMatched++
			}
		}
	}
	denom := float64(metrics.AnswerCaseCount - metrics.AnswerFailureCount)
	if denom > 0 {
		metrics.AnswerAccuracy = float64(metrics.AnswerMatchedCount) / denom
		metrics.AnswerMeanScore = scoreSum / denom
		metrics.AnswerMeanLatencyMS = float64(latencySum) / denom
	}
	if metrics.AnswerJudgeCaseCount > 0 {
		metrics.AnswerJudgeAccuracy = float64(metrics.AnswerJudgeMatched) / float64(metrics.AnswerJudgeCaseCount)
	}
}

func mergeCaseResults(base, updates []CaseResult) []CaseResult {
	if len(base) == 0 {
		return updates
	}
	index := make(map[string]int, len(base))
	for i, c := range base {
		index[c.CaseID] = i
	}
	out := append([]CaseResult(nil), base...)
	for _, update := range updates {
		if i, ok := index[update.CaseID]; ok {
			out[i] = mergeCaseResult(out[i], update)
			continue
		}
		index[update.CaseID] = len(out)
		out = append(out, update)
	}
	return out
}

func mergeCaseResult(base, update CaseResult) CaseResult {
	base.Answer = update.Answer
	base.ExpectedAnswer = update.ExpectedAnswer
	base.AnswerMatched = update.AnswerMatched
	base.AnswerScore = update.AnswerScore
	base.AnswerMethod = update.AnswerMethod
	base.AnswerToolNames = update.AnswerToolNames
	base.AnswerEvidenceNames = update.AnswerEvidenceNames
	base.AnswerEvidenceRefs = update.AnswerEvidenceRefs
	base.AnswerMatchedEvidence = update.AnswerMatchedEvidence
	base.AnswerIterations = update.AnswerIterations
	base.AnswerDurationMS = update.AnswerDurationMS
	base.AnswerJudgeScore = update.AnswerJudgeScore
	base.AnswerJudgeReason = update.AnswerJudgeReason
	if len(base.RetrievedNames) == 0 {
		base.RetrievedNames = update.RetrievedNames
		base.RetrievedRanks = update.RetrievedRanks
		base.MatchedNames = update.MatchedNames
		base.HitAt5 = update.HitAt5
		base.HitAt10 = update.HitAt10
		base.HitAt50 = update.HitAt50
		base.HitAt100 = update.HitAt100
		base.ReciprocalRank = update.ReciprocalRank
	}
	if base.Error == "" {
		base.Error = update.Error
	}
	return base
}

// WriteArtifacts writes report.json and per-case files to dir. If dir is
// empty the call is a no-op. Existing files are overwritten.
func WriteArtifacts(dir string, result RunResult) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	result.Cases = SortCases(result.Cases)
	if err := os.MkdirAll(filepath.Join(dir, "cases"), 0o755); err != nil {
		return fmt.Errorf("create artifact dir: %w", err)
	}
	reportPath := filepath.Join(dir, "report.json")
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	body = append(body, '\n')
	if err := os.WriteFile(reportPath, body, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	headToHeadPath := filepath.Join(dir, "head-to-head.md")
	headToHeadBody := []byte(RenderHeadToHeadMarkdown(result))
	if err := os.WriteFile(headToHeadPath, headToHeadBody, 0o644); err != nil {
		return fmt.Errorf("write head-to-head report: %w", err)
	}
	for i, c := range result.Cases {
		name := sanitizeArtifactName(c.CaseID)
		if name == "" {
			name = fmt.Sprintf("case-%03d", i+1)
		}
		casePath := filepath.Join(dir, "cases", name+".json")
		caseBody, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal case %s: %w", c.CaseID, err)
		}
		caseBody = append(caseBody, '\n')
		if err := os.WriteFile(casePath, caseBody, 0o644); err != nil {
			return fmt.Errorf("write case %s: %w", c.CaseID, err)
		}
	}
	return nil
}

// RenderHeadToHeadMarkdown renders an honest local comparison report from a
// completed RunResult. It never runs retrieval, models, or external baselines.
func RenderHeadToHeadMarkdown(result RunResult) string {
	var b strings.Builder
	b.WriteString("# LongMem Head-To-Head Report\n\n")
	if !result.GeneratedAt.IsZero() {
		b.WriteString("Generated: ")
		b.WriteString(result.GeneratedAt.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	b.WriteString("Suite: ")
	b.WriteString(firstNonEmptyString(result.Suite, "longmem"))
	b.WriteString("\n")
	if result.DatasetPath != "" {
		b.WriteString("Dataset: `")
		b.WriteString(result.DatasetPath)
		b.WriteString("`\n")
	}
	if result.WorkspaceID != "" {
		b.WriteString("Workspace ID: `")
		b.WriteString(result.WorkspaceID)
		b.WriteString("`\n")
	}
	if result.Limit > 0 {
		b.WriteString("Limit: ")
		b.WriteString(fmt.Sprintf("%d", result.Limit))
		b.WriteString("\n")
	}
	if len(result.Modes) > 0 {
		b.WriteString("Modes: `")
		b.WriteString(strings.Join(result.Modes, ","))
		b.WriteString("`\n")
	}
	b.WriteString("\n")

	b.WriteString("## Comparison\n\n")
	b.WriteString("| System | Status | Retrieval | Answer | Latency | Failures | Notes |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	b.WriteString(renderRetrievalReportRow(result))
	b.WriteString(renderAnswerReportRow(result))
	b.WriteString("| HydraDB baseline | not run | unavailable | unavailable | unavailable | unavailable | No HydraDB or external baseline data is attached to this local artifact. |\n\n")

	b.WriteString("## Reproduce\n\n")
	b.WriteString("Current eval:\n")
	b.WriteString("```bash\n")
	b.WriteString(renderLongmemCommand(result))
	b.WriteString("\n```\n\n")
	b.WriteString("Drain memory embedding queue:\n")
	b.WriteString("```bash\n")
	b.WriteString(renderQueueDrainCommand(result))
	b.WriteString("\n```\n\n")
	b.WriteString("Optional answer-mode eval:\n")
	b.WriteString("```bash\n")
	b.WriteString(renderAnswerModeCommand(result))
	b.WriteString("\n```\n\n")

	b.WriteString("## Limitations\n\n")
	b.WriteString("- The foxctl raw `memory/query` row is the local retrieval-only equivalent: BM25 named-memory search through `memoryrecall.Search`, not an invoked `memory/query` skill/tool run.\n")
	if result.Metrics != nil && result.Metrics.AnswerJudgeCaseCount > 0 {
		b.WriteString("- Answer scoring uses deterministic matching with an LLM semantic-equivalence judge as a secondary metric.\n")
	} else {
		b.WriteString("- Answer scoring is deterministic non-refusal exact/answer-contains-expected text matching. Add `--answer-judge` to enable LLM semantic-equivalence scoring.\n")
	}
	b.WriteString("- The CLI answer-mode default targets RLM `memory_recall`, but this artifact only records answer/evidence output from the configured runner.\n")
	b.WriteString("- HydraDB or external baseline rows stay `not run` unless real baseline data is attached by a future slice.\n")
	return b.String()
}

func renderRetrievalReportRow(result RunResult) string {
	if !runResultHasMode(result, ModeRetrieval) || result.Metrics == nil {
		return "| foxctl raw memory/query equivalent | not run | unavailable | n/a | unavailable | unavailable | Run `--mode retrieval` to populate the local retrieval-only equivalent. |\n"
	}
	m := result.Metrics
	return fmt.Sprintf(
		"| foxctl raw memory/query equivalent | run | hit@5 %.3f; hit@10 %.3f; hit@50 %.3f; hit@100 %.3f; MRR %.3f | n/a | mean %.1f ms | %d/%d | Local retrieval-only equivalent via `memoryrecall.Search`; no `memory/query` skill/tool invocation is recorded. |\n",
		m.HitAt5, m.HitAt10, m.HitAt50, m.HitAt100, m.MRR, m.MeanLatencyMS, m.FailureCount, m.CaseCount,
	)
}

func renderAnswerReportRow(result RunResult) string {
	if !runResultHasMode(result, ModeAnswer) || result.Metrics == nil {
		return "| foxctl answer-mode | not run | unavailable | unavailable | unavailable | unavailable | Run `--mode answer` when an answer runner is configured. |\n"
	}
	m := result.Metrics
	evidenceHitRate, evidenceCases := answerEvidenceHitRate(result.Cases)
	evidence := "unavailable"
	if evidenceCases > 0 {
		evidence = fmt.Sprintf("evidence-hit %.3f", evidenceHitRate)
	}
	answer := fmt.Sprintf("accuracy %.3f; mean score %.3f", m.AnswerAccuracy, m.AnswerMeanScore)
	if m.AnswerJudgeCaseCount > 0 {
		answer += fmt.Sprintf("; judge accuracy %.3f", m.AnswerJudgeAccuracy)
	}
	notes := "CLI default targets RLM `memory_recall`; artifact records answer/evidence output from the configured runner plus deterministic scoring"
	if m.AnswerJudgeCaseCount > 0 {
		notes += " augmented with an LLM semantic-equivalence judge"
	} else {
		notes += " (add `--answer-judge` for LLM semantic-equivalence scoring)"
	}
	notes += "."
	return fmt.Sprintf(
		"| foxctl answer-mode | run | %s | %s | mean %.1f ms | %d/%d | %s |\n",
		evidence, answer, m.AnswerMeanLatencyMS, m.AnswerFailureCount, m.AnswerCaseCount, notes,
	)
}

func answerEvidenceHitRate(cases []CaseResult) (float64, int) {
	if len(cases) == 0 {
		return 0, 0
	}
	total := 0
	hits := 0
	for _, c := range cases {
		if c.Answer != "" || len(c.AnswerEvidenceNames) > 0 || len(c.AnswerMatchedEvidence) > 0 {
			total++
			if len(c.AnswerMatchedEvidence) > 0 {
				hits++
			}
		}
	}
	if total == 0 {
		return 0, 0
	}
	return float64(hits) / float64(total), total
}

func renderLongmemCommand(result RunResult) string {
	return renderLongmemCommandWithModes(result, result.Modes)
}

func renderAnswerModeCommand(result RunResult) string {
	return renderLongmemCommandWithModes(result, []string{string(ModeAnswer)})
}

func renderLongmemCommandWithModes(result RunResult, modes []string) string {
	parts := []string{"foxctl", "eval", "longmem"}
	if result.DatasetPath != "" {
		parts = append(parts, "--dataset", result.DatasetPath)
	}
	if result.WorkspaceID != "" {
		parts = append(parts, "--workspace-id", result.WorkspaceID)
	}
	for _, mode := range modes {
		mode = strings.TrimSpace(mode)
		if mode != "" {
			parts = append(parts, "--mode", mode)
		}
	}
	if result.Limit > 0 {
		parts = append(parts, "--limit", fmt.Sprintf("%d", result.Limit))
	}
	artifactDir := strings.TrimSpace(result.ArtifactDir)
	if artifactDir == "" {
		artifactDir = "<artifact-dir>"
	}
	parts = append(parts, "--artifact-dir", artifactDir)
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func renderQueueDrainCommand(result RunResult) string {
	workspaceID := strings.TrimSpace(result.WorkspaceID)
	if workspaceID == "" {
		workspaceID = "<workspace-id>"
	}
	payload, _ := json.Marshal(struct {
		WorkspaceID string `json:"workspace_id"`
		Kind        string `json:"kind"`
		BatchSize   int    `json:"batch_size"`
		MaxDuration int    `json:"max_duration"`
		Parallelism int    `json:"parallelism"`
		ProcessAll  bool   `json:"process_all"`
	}{
		WorkspaceID: workspaceID,
		Kind:        "memory",
		BatchSize:   5,
		MaxDuration: 60,
		Parallelism: 1,
		ProcessAll:  true,
	})
	parts := []string{"foxctl", "run", "embedding/worker", "--ephemeral", "--input", string(payload)}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func runResultHasMode(result RunResult, want Mode) bool {
	for _, raw := range result.Modes {
		if Mode(strings.TrimSpace(raw)) == want {
			return true
		}
	}
	return false
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if value == "<artifact-dir>" {
		return value
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=,", r))
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// SortCases returns a copy of cases sorted by CaseID for deterministic
// reporting. Returned slice is a copy; the input is not mutated.
func SortCases(cases []CaseResult) []CaseResult {
	out := append([]CaseResult(nil), cases...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].CaseID < out[j].CaseID })
	return out
}

func sanitizeArtifactName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	cleaned := make([]rune, 0, len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			cleaned = append(cleaned, r)
		default:
			cleaned = append(cleaned, '-')
		}
	}
	return strings.Trim(string(cleaned), "-")
}
