package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/codeedit"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/diffutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

// applyDefaultsAndValidate applies defaults and validates required fields (mirrors run function).
func applyDefaultsAndValidate(in *input) error {
	if in.Path == "" {
		return fmt.Errorf("path is required")
	}
	if len(in.Edits) == 0 {
		return fmt.Errorf("edits is required")
	}
	if err := codeedit.ValidateEdits(in.Edits); err != nil {
		return err
	}

	if in.ContextLines <= 0 {
		in.ContextLines = 3
	}
	return nil
}

// parseInput is a test helper that parses JSON, applies defaults, and validates.
func parseInput(r io.Reader) (input, error) {
	in, err := skilltest.ParseInput[input](r)
	if err != nil {
		return in, err
	}
	if err := applyDefaultsAndValidate(&in); err != nil {
		return in, err
	}
	return in, nil
}

func TestGenerateUnifiedDiff(t *testing.T) {
	original := `line 1
line 2
line 3`

	modified := `line 1
modified line 2
line 3`

	diff, err := diffutil.UnifiedDiff("test.go", original, modified, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if diff == "" {
		t.Error("expected non-empty diff")
	}
	if !strings.Contains(diff, "-line 2") {
		t.Error("expected diff to show removed line")
	}
	if !strings.Contains(diff, "+modified line 2") {
		t.Error("expected diff to show added line")
	}
	if !strings.Contains(diff, "--- a/test.go") {
		t.Error("expected diff to have from-file header")
	}
	if !strings.Contains(diff, "+++ b/test.go") {
		t.Error("expected diff to have to-file header")
	}
}

func TestGenerateUnifiedDiffNoChanges(t *testing.T) {
	content := `line 1
line 2`

	diff, err := diffutil.UnifiedDiff("test.go", content, content, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if diff != "" {
		t.Errorf("expected empty diff for identical content, got: %s", diff)
	}
}

func TestParseInputValid(t *testing.T) {
	input := `{
		"path": "test.go",
		"edits": [
			{"type": "symbol", "symbol": "hello", "new_code": "func hello() {}"}
		],
		"dry_run": true,
		"context_lines": 5
	}`

	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if in.Path != "test.go" {
		t.Errorf("expected path='test.go', got %s", in.Path)
	}
	if len(in.Edits) != 1 {
		t.Errorf("expected 1 edit, got %d", len(in.Edits))
	}
	if !in.DryRun {
		t.Error("expected dry_run=true")
	}
	if in.ContextLines != 5 {
		t.Errorf("expected context_lines=5, got %d", in.ContextLines)
	}
}

func TestParseInputDefaults(t *testing.T) {
	input := `{
		"path": "test.go",
		"edits": [{"type": "lines", "start_line": 1, "end_line": 1, "new_code": "x"}]
	}`

	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if in.ContextLines != 3 {
		t.Errorf("expected default context_lines=3, got %d", in.ContextLines)
	}
}

func TestParseInputMissingPath(t *testing.T) {
	input := `{"edits": [{"type": "symbol"}]}`

	_, err := parseInput(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestParseInputNoEdits(t *testing.T) {
	input := `{"path": "test.go", "edits": []}`

	_, err := parseInput(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for empty edits")
	}
}

func TestRelativeTo(t *testing.T) {
	tests := []struct {
		base   string
		target string
		want   string
	}{
		{"/home/user/project", "/home/user/project/src/main.go", "src/main.go"},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got := pathutil.RelTo(tt.base, tt.target)
			if got != tt.want {
				t.Errorf("pathutil.RelTo(%q, %q) = %q, want %q", tt.base, tt.target, got, tt.want)
			}
		})
	}
}

func TestParseInputInvalidJSON(t *testing.T) {
	input := `{invalid json}`
	_, err := parseInput(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, workspace string) *skillmain.RunContext {
	t.Helper()
	t.Setenv("FOXCTL_WORKSPACE", workspace)
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
		t.Fatalf("new runner context: %v", err)
	}
	return rc
}

func TestRunSymbolEdit(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	testFile := filepath.Join(workspace, "test.go")
	content := `package main

func hello() {
	fmt.Println("Hello")
}

func goodbye() {
	fmt.Println("Goodbye")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: testFile,
		Edits: []edit{
			{
				Type:    "symbol",
				Symbol:  "hello",
				NewCode: "func hello() {\n\tfmt.Println(\"Hi there!\")\n}",
			},
		},
		DryRun:       true,
		ContextLines: 3,
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

	data := env["data"].(map[string]any)
	if data["dry_run"] != true {
		t.Error("expected dry_run=true")
	}
	if data["edit_count"].(float64) != 1 {
		t.Errorf("expected edit_count=1, got %v", data["edit_count"])
	}

	afterContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(afterContent) != content {
		t.Error("expected file unchanged in dry_run mode")
	}
}

func TestRunLinesEdit(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	testFile := filepath.Join(workspace, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\n"
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: testFile,
		Edits: []edit{
			{
				Type:      "lines",
				StartLine: 2,
				EndLine:   3,
				NewCode:   "new line 2\nnew line 3",
			},
		},
		DryRun: false,
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

	data := env["data"].(map[string]any)
	if data["edited"] != true {
		t.Error("expected edited=true")
	}

	afterContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(afterContent), "new line 2") {
		t.Error("expected file to contain 'new line 2'")
	}
}

func TestRunReplaceEdit(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	testFile := filepath.Join(workspace, "test.go")
	content := `package main

func greet() {
	fmt.Println("old value")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: testFile,
		Edits: []edit{
			{
				Type:    "replace",
				Search:  "old value",
				Replace: "new value",
			},
		},
		DryRun: false,
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

	afterContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(afterContent), "new value") {
		t.Error("expected file to contain 'new value'")
	}
	if strings.Contains(string(afterContent), "old value") {
		t.Error("expected 'old value' to be replaced")
	}
}

func TestRunUnknownEditType(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	testFile := filepath.Join(workspace, "test.txt")
	content := "test content"
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: testFile,
		Edits: []edit{
			{
				Type:    "unknown_type",
				NewCode: "something",
			},
		},
	}

	err := run(ctx, rc, in)
	if err == nil {
		t.Error("expected error for unknown edit type")
	}
	if !strings.Contains(err.Error(), "unknown edit type") {
		t.Errorf("expected 'unknown edit type' error, got: %v", err)
	}
}

func TestRunNoChanges(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	testFile := filepath.Join(workspace, "test.go")
	content := `package main

func hello() {
	fmt.Println("Hello")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: testFile,
		Edits: []edit{
			{
				Type:    "replace",
				Search:  "nonexistent",
				Replace: "replacement",
			},
		},
		DryRun: true,
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

	data := env["data"].(map[string]any)
	if data["edit_count"].(float64) != 0 {
		t.Errorf("expected edit_count=0, got %v", data["edit_count"])
	}
	if edited, ok := data["edited"].(bool); ok && edited {
		t.Errorf("expected edited=false, got true")
	}
}

func TestRunFileNotFound(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path: filepath.Join(workspace, "nonexistent.go"),
		Edits: []edit{
			{
				Type:    "symbol",
				Symbol:  "test",
				NewCode: "func test() {}",
			},
		},
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

	data := env["data"].(map[string]any)
	if data["error"] == nil || !strings.Contains(data["error"].(string), "read file") {
		t.Errorf("expected 'read file' error, got: %v", data["error"])
	}
}

func TestRunRestoreWritesCASContent(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	testFile := filepath.Join(workspace, "restore.txt")
	if err := os.WriteFile(testFile, []byte("current\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, workspace)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	restored := []byte("restored from backup\n")
	digest := putCASObject(t, ctx, rc, restored)
	if err := run(ctx, rc, input{
		Path:          testFile,
		RestoreDigest: digest,
	}); err != nil {
		t.Fatalf("run restore: %v", err)
	}

	after, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if !bytes.Equal(after, restored) {
		t.Fatalf("restored file = %q, want %q", string(after), string(restored))
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	data := env["data"].(map[string]any)
	if data["restored"] != true {
		t.Fatalf("expected restored=true, got %v", data["restored"])
	}
	if data["dry_run"] != false {
		t.Fatalf("expected dry_run=false, got %v", data["dry_run"])
	}
	if int(data["size"].(float64)) != len(restored) {
		t.Fatalf("size = %v, want %d", data["size"], len(restored))
	}
}

func TestRunRestoreDryRunGeneratedInputsDoNotWrite(t *testing.T) {
	prop := func(raw []byte) bool {
		ctx := context.Background()
		workspace := t.TempDir()
		testFile := filepath.Join(workspace, "restore.txt")
		current := shortBytes(raw)
		restored := append(append([]byte(nil), current...), []byte("\nrestored")...)
		if err := os.WriteFile(testFile, current, 0o644); err != nil {
			t.Logf("write test file: %v", err)
			return false
		}

		buf := &bytes.Buffer{}
		rc := newTestRunnerContext(t, buf, workspace)
		defer func() { errs.Ignore(rc.Close(), "cleanup") }()

		digest := putCASObject(t, ctx, rc, restored)
		if err := run(ctx, rc, input{
			Path:          testFile,
			RestoreDigest: digest,
			DryRun:        true,
		}); err != nil {
			t.Logf("run restore dry-run: %v", err)
			return false
		}

		after, err := os.ReadFile(testFile)
		if err != nil {
			t.Logf("read test file: %v", err)
			return false
		}
		if !bytes.Equal(after, current) {
			t.Logf("dry-run changed file: got %q want %q", string(after), string(current))
			return false
		}

		var env map[string]any
		if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
			t.Logf("unmarshal envelope: %v", err)
			return false
		}
		data := env["data"].(map[string]any)
		return data["restored"] == false && data["dry_run"] == true && int(data["size"].(float64)) == len(restored)
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}

func putCASObject(t *testing.T, ctx context.Context, rc *skillmain.RunContext, content []byte) string {
	t.Helper()
	obj, err := rc.CASStore.Put(ctx, bytes.NewReader(content), "text/plain", []string{"test-restore"})
	if err != nil {
		t.Fatalf("put CAS object: %v", err)
	}
	return obj.Digest
}

func shortBytes(raw []byte) []byte {
	if len(raw) == 0 {
		return []byte("current")
	}
	out := append([]byte(nil), raw...)
	if len(out) > 128 {
		out = out[:128]
	}
	return out
}
