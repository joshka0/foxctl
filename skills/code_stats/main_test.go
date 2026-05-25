package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestCodeStatsBasic(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a simple Go file
	goCode := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte(goCode), 0o644); err != nil {
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
		Path:        work,
		BreakdownBy: "language",
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Status != "ok" {
		t.Fatalf("expected ok status, got %v", env.Status)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data map")
	}
	statistics, ok := data["statistics"].(map[string]any)
	if !ok {
		t.Fatalf("expected statistics map")
	}
	if statistics["total_files"].(float64) != 1 {
		t.Fatalf("expected 1 file, got %v", statistics["total_files"])
	}
	if statistics["total_lines"].(float64) < 5 {
		t.Fatalf("expected at least 5 lines, got %v", statistics["total_lines"])
	}

	// Check languages breakdown
	languages, ok := statistics["languages"].(map[string]any)
	if !ok {
		t.Fatalf("expected languages map")
	}
	if languages["Go"] == nil {
		t.Fatalf("expected Go language to be detected")
	}
}

func TestCodeStatsMultipleLanguages(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create Go and Python files
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write go file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "script.py"), []byte("print('hello')\n"), 0o644); err != nil {
		t.Fatalf("write py file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Path:        work,
		BreakdownBy: "language",
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data map")
	}
	statistics, ok := data["statistics"].(map[string]any)
	if !ok {
		t.Fatalf("expected statistics map")
	}
	languages, ok := statistics["languages"].(map[string]any)
	if !ok {
		t.Fatalf("expected languages map")
	}

	if languages["Go"] == nil {
		t.Fatalf("expected Go to be detected")
	}
	if languages["Python"] == nil {
		t.Fatalf("expected Python to be detected")
	}
}

func TestCodeStatsRejectsInvalidBreakdownBy(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	work := t.TempDir()
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	err := run(ctx, rc, input{
		Path:        work,
		BreakdownBy: "owner",
	})
	if err == nil {
		t.Fatalf("expected invalid breakdown_by to fail")
	}
	if !skillerr.IsCode(err, skillerr.CodeArg) {
		t.Fatalf("expected EARG, got %v", err)
	}
	if !strings.Contains(err.Error(), "breakdown_by") {
		t.Fatalf("expected breakdown_by in error, got %v", err)
	}
}

func TestCountLinesSingleLineBlockCommentDoesNotLeakToNextLine(t *testing.T) {
	path := writeStatsTestFile(t, "main.go", "package main\n/* package comment */\nfunc main() {}\n")

	total, code, blank, comments, err := countLines(path, "Go")
	if err != nil {
		t.Fatalf("count lines: %v", err)
	}

	if total != 3 || code != 2 || blank != 0 || comments != 1 {
		t.Fatalf("expected total=3 code=2 blank=0 comments=1, got total=%d code=%d blank=%d comments=%d", total, code, blank, comments)
	}
}

func TestCountLinesMultilineBlockCommentDoesNotLeakAfterEnd(t *testing.T) {
	path := writeStatsTestFile(t, "main.go", "package main\n/*\npackage comment\n*/\nfunc main() {}\n")

	total, code, blank, comments, err := countLines(path, "Go")
	if err != nil {
		t.Fatalf("count lines: %v", err)
	}

	if total != 5 || code != 2 || blank != 0 || comments != 3 {
		t.Fatalf("expected total=5 code=2 blank=0 comments=3, got total=%d code=%d blank=%d comments=%d", total, code, blank, comments)
	}
}

func TestCountLinesInlineBlockCommentOnCodeLineCountsAsCode(t *testing.T) {
	path := writeStatsTestFile(t, "main.go", "package main\nconst before = 1 /* units */\n/* package comment */ const after = 2\n")

	total, code, blank, comments, err := countLines(path, "Go")
	if err != nil {
		t.Fatalf("count lines: %v", err)
	}

	if total != 3 || code != 3 || blank != 0 || comments != 0 {
		t.Fatalf("expected total=3 code=3 blank=0 comments=0, got total=%d code=%d blank=%d comments=%d", total, code, blank, comments)
	}
}

func TestCountLinesGeneratedClosedBlockCommentsPreserveFollowingCode(t *testing.T) {
	cfg := &quick.Config{MaxCount: 100}

	err := quick.Check(func(commentID uint16, valueID uint16) bool {
		content := fmt.Sprintf("package main\n/* generated comment %d */\nconst value%d = 1\n", commentID, valueID)
		path := writeStatsTestFile(t, "generated.go", content)

		total, code, blank, comments, err := countLines(path, "Go")
		if err != nil {
			t.Logf("count lines: %v", err)
			return false
		}
		if total != 3 || code != 2 || blank != 0 || comments != 1 {
			t.Logf("closed block comment should not affect following code: total=%d code=%d blank=%d comments=%d content=%q", total, code, blank, comments, content)
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("closed block comment property failed: %v", err)
	}
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

func writeStatsTestFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}
