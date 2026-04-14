package transcriptmemoryeval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/transcriptcache"
)

type SavedArtifact struct {
	Suite        string `json:"suite"`
	JSONPath     string `json:"json_path"`
	MarkdownPath string `json:"markdown_path"`
}

type ExperimentRecord struct {
	RunID                string          `json:"run_id"`
	Label                string          `json:"label,omitempty"`
	Description          string          `json:"description,omitempty"`
	ConfigID             string          `json:"config_id"`
	Suites               []string        `json:"suites"`
	Score                float64         `json:"score"`
	MeanPrecision        float64         `json:"mean_precision"`
	MeanRecall           float64         `json:"mean_recall"`
	MeanKindAccuracy     float64         `json:"mean_kind_accuracy"`
	MeanFallbackRate     float64         `json:"mean_fallback_rate"`
	ForbiddenHitRate     float64         `json:"forbidden_hit_rate"`
	PersistedInRangeRate float64         `json:"persisted_in_range_rate"`
	Artifacts            []SavedArtifact `json:"artifacts,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
}

func SaveRunOutputs(dir, suite string, result RunResult, markdown string) (string, string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", "", fmt.Errorf("save outputs: dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	stamp := result.GeneratedAt.UTC().Format("20060102T150405Z")
	base := sanitizeName(suite)
	jsonPath := filepath.Join(dir, base+"-"+stamp+".json")
	markdownPath := filepath.Join(dir, base+"-"+stamp+".md")
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", "", err
	}
	body = append(body, '\n')
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(markdownPath, []byte(markdown), 0o644); err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

func AppendExperimentRecord(path string, record ExperimentRecord) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("append experiment record: path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_, err = file.Write(body)
	return err
}

func BuildExperimentRecord(label, description, configID string, results []RunResult, artifacts []SavedArtifact) ExperimentRecord {
	summary := AggregateResults(results)
	suites := make([]string, 0, len(results))
	for _, result := range results {
		suites = append(suites, result.Suite)
	}
	return ExperimentRecord{
		RunID:                transcriptcache.DigestText(fmt.Sprintf("%s|%s|%s|%d", label, description, configID, time.Now().UTC().UnixNano())),
		Label:                strings.TrimSpace(label),
		Description:          strings.TrimSpace(description),
		ConfigID:             strings.TrimSpace(configID),
		Suites:               suites,
		Score:                summary.MeanScore,
		MeanPrecision:        summary.MeanPrecision,
		MeanRecall:           summary.MeanRecall,
		MeanKindAccuracy:     summary.MeanKindAccuracy,
		MeanFallbackRate:     summary.MeanFallbackRate,
		ForbiddenHitRate:     summary.ForbiddenHitRate,
		PersistedInRangeRate: summary.PersistedInRangeRate,
		Artifacts:            artifacts,
		CreatedAt:            time.Now().UTC(),
	}
}

func AggregateResults(results []RunResult) Summary {
	if len(results) == 0 {
		return Summary{}
	}
	var summary Summary
	for _, result := range results {
		summary.Cases += result.Summary.Cases
		summary.MeanScore += result.Summary.MeanScore
		summary.MeanPrecision += result.Summary.MeanPrecision
		summary.MeanRecall += result.Summary.MeanRecall
		summary.MeanKindAccuracy += result.Summary.MeanKindAccuracy
		summary.MeanFallbackRate += result.Summary.MeanFallbackRate
		summary.ForbiddenHitRate += result.Summary.ForbiddenHitRate
		summary.PersistedInRangeRate += result.Summary.PersistedInRangeRate
	}
	div := float64(len(results))
	summary.MeanScore /= div
	summary.MeanPrecision /= div
	summary.MeanRecall /= div
	summary.MeanKindAccuracy /= div
	summary.MeanFallbackRate /= div
	summary.ForbiddenHitRate /= div
	summary.PersistedInRangeRate /= div
	return summary
}

func sanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "eval"
	}
	return out
}
