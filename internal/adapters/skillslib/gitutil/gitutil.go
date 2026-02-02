package gitutil

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
)

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

// ResolveRepoPath validates and resolves a repo path using the run context.
func ResolveRepoPath(rc *skillmain.RunContext, path string) (string, error) {
	if path == "" {
		path = "."
	}
	valid, err := rc.PathValidator.ValidatePath(path)
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
