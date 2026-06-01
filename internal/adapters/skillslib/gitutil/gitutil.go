package gitutil

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
)

// PathValidator is the narrow interface ResolveRepoPath needs from a run context.
// Both run context variants expose a *policy.PathValidator that satisfies this
// interface.
type PathValidator interface {
	ValidatePath(path string) (string, error)
}

// RequireGit ensures git is available in PATH and returns its resolved path.
func RequireGit() (string, error) {
	gitPath, err := executil.RequireTool("git", "install git")
	if err != nil {
		return "", skillerr.Runtime(
			"git command not found",
			skillerr.WithCause(err),
			skillerr.WithHint("Install git and ensure it is available in PATH."),
		)
	}
	return gitPath, nil
}

// ResolveRepoPath validates and resolves a repo path using the given validator.
func ResolveRepoPath(v PathValidator, path string) (string, error) {
	if v == nil {
		return "", skillerr.Arg("path validator not configured")
	}
	if path == "" {
		path = "."
	}
	valid, err := v.ValidatePath(path)
	if err != nil {
		return "", skillerr.Arg(
			fmt.Sprintf("path validation failed for %q: %v", path, err),
			skillerr.WithHint("Provide a repo_path within the workspace or an allowed root."),
		)
	}
	return valid, nil
}

// CheckRepo verifies that repoPath is a git repository.
func CheckRepo(ctx context.Context, gitPath, repoPath string) error {
	cmd := exec.CommandContext(ctx, gitPath, "-C", repoPath, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return skillerr.Validation("not a git repository")
	}
	return nil
}
