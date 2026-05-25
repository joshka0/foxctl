package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

func TestCodeSecurityBasic(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	safeCode := `package main

func main() {
	println("hello world")
}
`
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte(safeCode), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path:     work,
		ScanMode: "quick",
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env["status"] != "ok" {
		t.Fatalf("expected ok status, got %v", env["status"])
	}
}

func TestSensitiveDataFindingsRedactSecretValues(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	githubToken := "ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ"
	source := `package main

const PASSWORD = "hunter2secret"
const token = "` + githubToken + `"
`
	if err := os.WriteFile(filepath.Join(work, "secrets.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	err := run(ctx, rc, input{
		Path:              work,
		ScanMode:          "secrets",
		SeverityThreshold: "low",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	output := decodeSecurityOutput(t, buf)
	if output.VulnerabilityCount < 2 {
		t.Fatalf("vulnerability_count=%d want at least password and token findings", output.VulnerabilityCount)
	}

	for _, finding := range output.Vulnerabilities {
		if finding.Category != "sensitive_data" {
			continue
		}
		if !strings.Contains(finding.CodeSnippet, "[REDACTED]") {
			t.Fatalf("%s snippet=%q should mark redaction", finding.ID, finding.CodeSnippet)
		}
		for _, leaked := range []string{"hunter2secret", githubToken, "abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJ"} {
			if strings.Contains(finding.CodeSnippet, leaked) {
				t.Fatalf("%s snippet leaked secret fragment %q in %q", finding.ID, leaked, finding.CodeSnippet)
			}
		}
	}
}

func TestSeverityThresholdExcludesLowerRiskFindings(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	source := `console.debug("password:", password);
eval(userInput);
`
	if err := os.WriteFile(filepath.Join(work, "app.js"), []byte(source), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	err := run(ctx, rc, input{
		Path:              work,
		ScanMode:          "scan",
		SeverityThreshold: "high",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	output := decodeSecurityOutput(t, buf)
	if output.VulnerabilityCount == 0 {
		t.Fatal("expected high-threshold scan to retain critical eval finding")
	}
	for _, finding := range output.Vulnerabilities {
		if severityScore(finding.Severity) < severityScore("high") {
			t.Fatalf("finding below threshold survived: %+v", finding)
		}
	}
}

func TestFilterBySeverityGeneratedThresholds(t *testing.T) {
	severities := []string{"low", "medium", "high", "critical"}
	cfg := &quick.Config{MaxCount: 100}

	err := quick.Check(func(rawSeverities []uint8, rawThreshold uint8) bool {
		threshold := severities[int(rawThreshold)%len(severities)]
		vulns := make([]vulnerability, len(rawSeverities))
		for i, raw := range rawSeverities {
			vulns[i] = vulnerability{ID: severities[int(raw)%len(severities)], Severity: severities[int(raw)%len(severities)]}
		}

		got := filterBySeverity(vulns, threshold)
		if len(got) > len(vulns) {
			t.Logf("filter returned more findings than input: got=%d input=%d", len(got), len(vulns))
			return false
		}
		for _, finding := range got {
			if severityScore(finding.Severity) < severityScore(threshold) {
				t.Logf("severity %q survived threshold %q", finding.Severity, threshold)
				return false
			}
		}

		want := make([]vulnerability, 0, len(vulns))
		for _, finding := range vulns {
			if severityScore(finding.Severity) >= severityScore(threshold) {
				want = append(want, finding)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Logf("filter changed membership or order: got=%v want=%v", got, want)
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("severity filter property failed: %v", err)
	}
}

type securityOutput struct {
	VulnerabilityCount int             `json:"vulnerability_count"`
	Vulnerabilities    []vulnerability `json:"vulnerabilities"`
}

func decodeSecurityOutput(t *testing.T, stdout *bytes.Buffer) securityOutput {
	t.Helper()

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	dataBytes, err := json.Marshal(env["data"])
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var output securityOutput
	if err := json.Unmarshal(dataBytes, &output); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	return output
}

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, _ string) *skillmain.RunContext {
	t.Helper()
	state := t.TempDir()
	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   filepath.Join(state, "cas"),
			Jobs:  filepath.Join(state, "jobs"),
			Cache: filepath.Join(state, "cache"),
		},
	}
	rc, err := skillmain.BuildRunContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}
