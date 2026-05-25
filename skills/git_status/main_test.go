package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/gitutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
)

func TestParseStatusOutput(t *testing.T) {
	output := `## main...origin/main
 M file1.go
A  file2.go
 D file3.go
?? untracked.go
`
	files, _, _ := parseStatusOutput(output)

	if countByStatus(files, "M") != 1 {
		t.Errorf("expected 1 modified, got %d", countByStatus(files, "M"))
	}
	if countByStatus(files, "A") != 1 {
		t.Errorf("expected 1 added, got %d", countByStatus(files, "A"))
	}
	if countByStatus(files, "D") != 1 {
		t.Errorf("expected 1 deleted, got %d", countByStatus(files, "D"))
	}
	if countByStatus(files, "?") != 1 {
		t.Errorf("expected 1 untracked, got %d", countByStatus(files, "?"))
	}

	if len(files) != 4 {
		t.Errorf("expected 4 files, got %d", len(files))
	}
}

func TestParseDiffStat(t *testing.T) {
	output := ` file1.go | 10 +++++-----
 file2.go |  5 +++++
 2 files changed, 10 insertions(+), 5 deletions(-)
`
	stats := parseDiffStat(output)

	if len(stats) != 2 {
		t.Errorf("expected 2 files changed, got %d", len(stats))
	}
	if stats["file1.go"]["additions"] != 5 {
		t.Errorf("expected 5 insertions for file1.go, got %d", stats["file1.go"]["additions"])
	}
	if stats["file1.go"]["deletions"] != 5 {
		t.Errorf("expected 5 deletions for file1.go, got %d", stats["file1.go"]["deletions"])
	}
	if stats["file2.go"]["additions"] != 5 {
		t.Errorf("expected 5 insertions for file2.go, got %d", stats["file2.go"]["additions"])
	}
}

func TestParseDiffNameOnly(t *testing.T) {
	output := `file1.go
dir/file2.go

`
	files := parseDiffNameOnly(output)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0] != "file1.go" || files[1] != "dir/file2.go" {
		t.Fatalf("unexpected files: %v", files)
	}
}

func TestValidateGitRevisionArgRejectsOptionLikeAndControlValues(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "head", value: "HEAD"},
		{name: "branch path", value: "feature/harden-tests"},
		{name: "revision expression", value: "HEAD~1"},
		{name: "option", value: "--output=/tmp/out", wantErr: true},
		{name: "short option", value: "-p", wantErr: true},
		{name: "blank", value: "  ", wantErr: true},
		{name: "newline", value: "HEAD\n--output=/tmp/out", wantErr: true},
		{name: "tab", value: "HEAD\t--output=/tmp/out", wantErr: true},
		{name: "escape", value: "HEAD\x1b--output=/tmp/out", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitRevisionArg(tt.value)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateGitRevisionArgPropertyRejectsOptionBlankAndControlValues(t *testing.T) {
	property := func(seed uint8, raw string) bool {
		value := invalidGitRevisionArg(seed, raw)
		if err := validateGitRevisionArg(value); err == nil {
			t.Logf("accepted invalid revision arg %q", value)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateGitRevisionArgPropertyAllowsGeneratedSafeValues(t *testing.T) {
	property := func(a, b, c uint8) bool {
		values := []string{
			fmt.Sprintf("feature/%c%c-%d", 'a'+a%26, 'a'+b%26, c),
			fmt.Sprintf("HEAD~%d", int(a%32)+1),
			fmt.Sprintf("v%d.%d.%d", a%10, b%10, c%10),
		}
		for _, value := range values {
			if err := validateGitRevisionArg(value); err != nil {
				t.Logf("rejected safe revision arg %q: %v", value, err)
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestGetDiffRejectsOptionLikeCommitBeforeGitCanWriteOutput(t *testing.T) {
	ctx := context.Background()
	gitPath := requireGitForIntegration(t)
	repo := initGitRepo(t, gitPath)
	outside := filepath.Join(t.TempDir(), "diff.out")

	_, err := getDiff(ctx, &skillmain.RunContext{MaxPreview: 1024}, gitPath, repo, input{Commit: "--output=" + outside})
	if err == nil {
		t.Fatal("expected option-like commit to be rejected")
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Fatalf("git option side effect created %s: %v", outside, statErr)
	}
}

func invalidGitRevisionArg(seed uint8, raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = "HEAD"
	}
	switch seed % 3 {
	case 0:
		return "-" + base
	case 1:
		return strings.Repeat(" ", int(seed%4)+1)
	default:
		controls := []rune{'\x00', '\n', '\r', '\t', '\x1b', '\x7f'}
		r := controls[int(seed)%len(controls)]
		return base + string(r) + "--output=/tmp/out"
	}
}

func TestGetLogRejectsOptionLikeCommitBeforeGitCanWriteOutput(t *testing.T) {
	ctx := context.Background()
	gitPath := requireGitForIntegration(t)
	repo := initGitRepo(t, gitPath)
	outside := filepath.Join(t.TempDir(), "log.out")

	_, err := getLog(ctx, gitPath, repo, input{Limit: 1, Commit: "--output=" + outside})
	if err == nil {
		t.Fatal("expected option-like commit to be rejected")
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Fatalf("git option side effect created %s: %v", outside, statErr)
	}
}

func requireGitForIntegration(t *testing.T) string {
	t.Helper()
	gitPath, err := gitutil.RequireGit()
	if err != nil {
		t.Skipf("git not available: %v", err)
	}
	return gitPath
}

func initGitRepo(t *testing.T, gitPath string) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, gitPath, repo, "init")
	runGit(t, gitPath, repo, "config", "user.email", "test@example.com")
	runGit(t, gitPath, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, gitPath, repo, "add", "file.txt")
	runGit(t, gitPath, repo, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("after\n"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}
	return repo
}

func runGit(t *testing.T, gitPath, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
