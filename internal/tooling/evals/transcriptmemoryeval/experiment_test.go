package transcriptmemoryeval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveRunOutputsAndAppendExperimentRecord(t *testing.T) {
	dir := t.TempDir()
	result := RunResult{
		Suite:       "demo",
		GeneratedAt: time.Date(2026, 3, 25, 20, 0, 0, 0, time.UTC),
	}
	jsonPath, markdownPath, err := SaveRunOutputs(dir, "demo", result, "# Demo\n")
	if err != nil {
		t.Fatalf("SaveRunOutputs() error = %v", err)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("json output missing: %v", err)
	}
	if _, err := os.Stat(markdownPath); err != nil {
		t.Fatalf("markdown output missing: %v", err)
	}

	logPath := filepath.Join(dir, "runs.ndjson")
	record := BuildExperimentRecord("baseline", "first run", "cfg-1", []RunResult{{
		Suite: "demo",
		Summary: Summary{
			Cases:                1,
			MeanScore:            0.7,
			MeanPrecision:        0.8,
			MeanRecall:           0.6,
			MeanKindAccuracy:     1.0,
			MeanFallbackRate:     0.2,
			ForbiddenHitRate:     0.0,
			PersistedInRangeRate: 1.0,
		},
	}}, []SavedArtifact{{Suite: "demo", JSONPath: jsonPath, MarkdownPath: markdownPath}})
	if err := AppendExperimentRecord(logPath, record); err != nil {
		t.Fatalf("AppendExperimentRecord() error = %v", err)
	}
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(log) error = %v", err)
	}
	if !strings.Contains(string(body), `"config_id":"cfg-1"`) {
		t.Fatalf("log missing record: %s", string(body))
	}
}
