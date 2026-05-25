package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestReplaceSimpleLiteral(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "hello world\nhello universe\ngoodbye world\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:     "hello",
		Replacement: "hi",
		Paths:       []string{testFile},
		Literal:     true,
		DryRun:      false,
		MaxFiles:    100,
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
	if data["files_modified"].(float64) != 1 {
		t.Fatalf("expected 1 file modified, got %v", data["files_modified"])
	}

	// Verify file was actually modified
	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	expected := "hi world\nhi universe\ngoodbye world\n"
	if string(modified) != expected {
		t.Fatalf("expected %q, got %q", expected, string(modified))
	}
}

func TestReplaceCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "Hello world\nhELLO universe\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:         "hello",
		Replacement:     "hi",
		Paths:           []string{testFile},
		Literal:         false,
		CaseInsensitive: true,
		DryRun:          false,
		MaxFiles:        100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	expected := "hi world\nhi universe\n"
	if string(modified) != expected {
		t.Fatalf("expected %q, got %q", expected, string(modified))
	}
}

func TestReplaceWordBoundary(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "test testing tested\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:      "test",
		Replacement:  "exam",
		Paths:        []string{testFile},
		Literal:      false,
		WordBoundary: true,
		DryRun:       false,
		MaxFiles:     100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	// Only "test" should be replaced, not "testing" or "tested"
	expected := "exam testing tested\n"
	if string(modified) != expected {
		t.Fatalf("expected %q, got %q", expected, string(modified))
	}
}

func TestReplaceWithBackup(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "original content\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:      "original",
		Replacement:  "modified",
		Paths:        []string{testFile},
		Literal:      true,
		Backup:       true,
		BackupSuffix: ".backup",
		DryRun:       false,
		MaxFiles:     100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Verify backup was created
	backupPath := testFile + ".backup"
	backupContent, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup file: %v", err)
	}
	if string(backupContent) != content {
		t.Fatalf("backup should contain original content")
	}

	// Verify file was modified
	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	expected := "modified content\n"
	if string(modified) != expected {
		t.Fatalf("expected %q, got %q", expected, string(modified))
	}
}

func TestReplaceLineRange(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "line1 old\nline2 old\nline3 old\nline4 old\nline5 old\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:     "old",
		Replacement: "new",
		Paths:       []string{testFile},
		Literal:     true,
		LineRange:   &lineRange{Start: 2, End: 4},
		DryRun:      false,
		MaxFiles:    100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	// Only lines 2-4 should be modified
	expected := "line1 old\nline2 new\nline3 new\nline4 new\nline5 old\n"
	if string(modified) != expected {
		t.Fatalf("expected %q, got %q", expected, string(modified))
	}
}

func TestReplacePatternRange(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "before\nSTART\nold1\nold2\nEND\nafter\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:     "old",
		Replacement: "new",
		Paths:       []string{testFile},
		Literal:     true,
		LineRange:   &lineRange{StartPattern: "START", EndPattern: "END"},
		DryRun:      false,
		MaxFiles:    100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	// Only lines between START and END should be modified
	expected := "before\nSTART\nnew1\nnew2\nEND\nafter\n"
	if string(modified) != expected {
		t.Fatalf("expected %q, got %q", expected, string(modified))
	}
}

func TestReplaceMultipleOperations(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "foo bar baz\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Paths: []string{testFile},
		Operations: []operation{
			{Pattern: "foo", Replacement: "FOO", Literal: true},
			{Pattern: "bar", Replacement: "BAR", Literal: true},
			{Pattern: "baz", Replacement: "BAZ", Literal: true},
		},
		DryRun:   false,
		MaxFiles: 100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	expected := "FOO BAR BAZ\n"
	if string(modified) != expected {
		t.Fatalf("expected %q, got %q", expected, string(modified))
	}
}

func TestReplaceRejectsEmptyOperationPatternWithoutMutatingFile(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "abc\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	err := run(ctx, rc, input{
		Paths: []string{testFile},
		Operations: []operation{
			{Pattern: "", Replacement: "X", Literal: true},
		},
		MaxFiles: 100,
	})
	if err == nil {
		t.Fatal("expected empty operation pattern to be rejected")
	}

	got, readErr := os.ReadFile(testFile)
	if readErr != nil {
		t.Fatalf("read file: %v", readErr)
	}
	if string(got) != content {
		t.Fatalf("invalid operation mutated file: got %q want %q", string(got), content)
	}
}

func TestBuildReplacerPropertyRejectsWhitespaceOnlyPatterns(t *testing.T) {
	err := quick.Check(func(raw string, literal bool) bool {
		if strings.TrimSpace(raw) != "" {
			return true
		}
		_, err := buildReplacer(operation{Pattern: raw, Replacement: "x", Literal: literal}, false, false, false)
		return err != nil
	}, &quick.Config{MaxCount: 100})
	if err != nil {
		t.Fatalf("whitespace pattern property failed: %v", err)
	}
}

func FuzzBuildReplacerMaintainsReplacementInvariants(f *testing.F) {
	seeds := []struct {
		pattern         string
		replacement     string
		content         string
		literal         bool
		caseInsensitive bool
		wordBoundary    bool
		multiline       bool
	}{
		{pattern: "fox", replacement: "cat", content: "fox fox\n", literal: true},
		{pattern: "fox", replacement: "$0hound", content: "fox\nFOX\n", caseInsensitive: true},
		{pattern: `(?m)^name=(.*)$`, replacement: "name=$1_test", content: "name=fox\nother=true\n", multiline: true},
		{pattern: "id", replacement: "ID", content: "grid id identity\n", wordBoundary: true},
		{pattern: "", replacement: "x", content: "abc", literal: true},
		{pattern: "[", replacement: "x", content: "abc"},
	}
	for _, seed := range seeds {
		f.Add(seed.pattern, seed.replacement, seed.content, seed.literal, seed.caseInsensitive, seed.wordBoundary, seed.multiline)
	}

	f.Fuzz(func(t *testing.T, pattern, replacement, content string, literal, caseInsensitive, wordBoundary, multiline bool) {
		const (
			maxPatternLen     = 1024
			maxReplacementLen = 512
			maxContentLen     = 2048
			maxOutputGrowth   = 1 << 20
		)
		if len(pattern) > maxPatternLen || len(replacement) > maxReplacementLen || len(content) > maxContentLen {
			t.Skip("input too large for focused text replacement fuzzing")
		}

		r, err := buildReplacer(operation{
			Pattern:     pattern,
			Replacement: replacement,
			Literal:     literal,
		}, caseInsensitive, wordBoundary, multiline)
		if strings.TrimSpace(pattern) == "" {
			if err == nil {
				t.Fatalf("empty or whitespace-only pattern compiled successfully")
			}
			return
		}
		if err != nil {
			return
		}

		var wantModified string
		var wantCount int
		switch typed := r.(type) {
		case *literalReplacer:
			wantCount = strings.Count(content, pattern)
			if wantCount*len(replacement) > maxOutputGrowth {
				t.Skip("literal replacement output too large for focused fuzzing")
			}
			wantModified = strings.ReplaceAll(content, pattern, replacement)
		case *regexReplacer:
			wantCount = len(typed.pattern.FindAllStringIndex(content, -1))
			if wantCount*len(replacement) > maxOutputGrowth {
				t.Skip("regex replacement output too large for focused fuzzing")
			}
			wantModified = typed.pattern.ReplaceAllString(content, replacement)
		default:
			t.Fatalf("unexpected replacer type %T", r)
		}

		matched := r.Match(content)
		modified, count := r.Replace(content)
		if count != wantCount {
			t.Fatalf("replacement count = %d, want %d", count, wantCount)
		}
		if modified != wantModified {
			t.Fatalf("replacement output = %q, want %q", modified, wantModified)
		}
		if matched != (wantCount > 0) {
			t.Fatalf("match result = %v, want %v", matched, wantCount > 0)
		}
	})
}

func TestReplaceBinarySkip(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	// Create a binary file (with null bytes)
	binaryContent := []byte{0x00, 0x01, 0x02, 't', 'e', 's', 't', 0x00}
	testFile := filepath.Join(work, "test.bin")
	if err := os.WriteFile(testFile, binaryContent, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	skipBinary := true
	in := input{
		Pattern:     "test",
		Replacement: "exam",
		Paths:       []string{testFile},
		Literal:     true,
		SkipBinary:  &skipBinary,
		DryRun:      false,
		MaxFiles:    100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	data := env["data"].(map[string]any)
	if data["files_skipped"].(float64) != 1 {
		t.Fatalf("expected 1 file skipped, got %v", data["files_skipped"])
	}

	// Verify file was not modified
	unmodified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !bytes.Equal(unmodified, binaryContent) {
		t.Fatalf("binary file should not be modified")
	}
}

func TestReplacePreserveLineEndings(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	// Create file with Windows line endings
	content := "old line\r\nold again\r\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	preserveLineEndings := true
	in := input{
		Pattern:             "old",
		Replacement:         "new",
		Paths:               []string{testFile},
		Literal:             true,
		PreserveLineEndings: &preserveLineEndings,
		DryRun:              false,
		MaxFiles:            100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	// Should preserve CRLF line endings
	expected := "new line\r\nnew again\r\n"
	if string(modified) != expected {
		t.Fatalf("expected %q with CRLF, got %q", expected, string(modified))
	}
}

func TestReplaceDryRun(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "foo bar\nfoo baz\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:     "foo",
		Replacement: "qux",
		Paths:       []string{testFile},
		Literal:     true,
		DryRun:      true,
		MaxFiles:    100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	data := env["data"].(map[string]any)
	if data["dry_run"].(bool) != true {
		t.Fatalf("expected dry_run true")
	}

	// Verify file was NOT modified
	unchanged, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(unchanged) != content {
		t.Fatalf("expected file unchanged, got %q", string(unchanged))
	}
}

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, workspace string) *skillmain.RunContext {
	t.Helper()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
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
