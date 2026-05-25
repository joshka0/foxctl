package gitutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/policy"
)

func TestResolveRepoPathRequiresPathValidator(t *testing.T) {
	tests := []struct {
		name string
		rc   *skillmain.RunContext
	}{
		{name: "nil context", rc: nil},
		{name: "nil validator", rc: &skillmain.RunContext{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ResolveRepoPath(tt.rc, "."); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestResolveRepoPathUsesWorkspaceDefaultAndRejectsEscapes(t *testing.T) {
	workspace := t.TempDir()
	rc := &skillmain.RunContext{PathValidator: testPathValidator(t, workspace)}

	got, err := ResolveRepoPath(rc, "")
	if err != nil {
		t.Fatalf("ResolveRepoPath empty: %v", err)
	}
	if got != workspace {
		t.Fatalf("default repo path=%q want workspace %q", got, workspace)
	}

	if _, err := ResolveRepoPath(rc, filepath.Join(workspace, "..", "outside")); err == nil {
		t.Fatal("expected escaping path to be rejected")
	}
}

func TestCheckRepoUsesRealGitRepositoryBoundary(t *testing.T) {
	gitPath, err := RequireGit()
	if err != nil {
		t.Skipf("git not available: %v", err)
	}
	repo := t.TempDir()
	runGit(t, gitPath, repo, "init")

	if err := CheckRepo(context.Background(), gitPath, repo); err != nil {
		t.Fatalf("CheckRepo initialized repo: %v", err)
	}

	notRepo := t.TempDir()
	if err := CheckRepo(context.Background(), gitPath, notRepo); err == nil {
		t.Fatal("expected non-repo to be rejected")
	}
}

func testPathValidator(t *testing.T, workspace string) *policy.PathValidator {
	t.Helper()
	pv, err := policy.NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("path validator: %v", err)
	}
	return pv
}

func runGit(t *testing.T, gitPath, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
