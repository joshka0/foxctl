// Package main implements the git/worktree skill.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/gitutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const command = "git/worktree"

// input defines the input parameters for git/worktree operations.
type input struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	RepoPath  string `json:"repo_path"`
	Force     bool   `json:"force"`
	NewBranch bool   `json:"new_branch"`
}

// worktree represents a git worktree with metadata.
type worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	Bare   bool   `json:"bare"`
}

// main is the skill entry point for git/worktree.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates git worktree operations with path validation and repository management.
//
// Index:
//
//	Purpose: Execute git worktree operations (list, add, remove, prune) with path validation
//	Keywords: git/worktree, worktree_management, git_operations, repository_management
//	Related: listWorktrees, addWorktree, removeWorktree, pruneWorktrees, parseWorktreeList
//	Flow: validate input → resolve repo → check git → dispatch operation → emit results
//	Resources: git command execution; file system access; repository management
//	Events: git-worktree-list, git-worktree-add, git-worktree-remove, git-worktree-prune
//	OutputFields: operation, worktree_count, worktrees, path, branch
//
// [[domain:git-operations]]
// [[protocol:skill-dispatch]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	in.Operation = oputil.DefaultOp(in.Operation, "list")
	if in.RepoPath == "" {
		in.RepoPath = "."
	}

	// Validate and resolve repository path
	repoPath, err := gitutil.ResolveRepoPath(rc, in.RepoPath)
	if err != nil {
		return err
	}

	// Check if git is available
	gitPath, err := gitutil.RequireGit()
	if err != nil {
		return err
	}

	// Dispatch operation
	data, err := oputil.NewSwitch(in.Operation).
		Case("list", func() (map[string]any, error) { return listWorktrees(ctx, gitPath, repoPath) }).
		Case("add", func() (map[string]any, error) { return addWorktree(ctx, rc, gitPath, repoPath, in) }).
		Case("remove", func() (map[string]any, error) { return removeWorktree(ctx, rc, gitPath, repoPath, in) }).
		Case("prune", func() (map[string]any, error) { return pruneWorktrees(ctx, gitPath, repoPath) }).
		Run()
	if err != nil {
		return err
	}

	return skillout.Emit(rc, command, data)
}

// listWorktrees retrieves and parses all worktrees in the repository.
func listWorktrees(ctx context.Context, gitPath, repoPath string) (map[string]any, error) {
	result := executil.Run(ctx, "", gitPath, "-C", repoPath, "worktree", "list", "--porcelain")
	if result.Err != nil {
		return nil, skillerr.Runtimef("git worktree list failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	worktrees := parseWorktreeList(string(result.Stdout))

	return map[string]any{
		"operation":      "list",
		"worktree_count": len(worktrees),
		"worktrees":      worktrees,
	}, nil
}

// parseWorktreeList parses git worktree list porcelain output into structured data.
func parseWorktreeList(output string) []worktree {
	lines := strings.Split(output, "\n")
	var worktrees []worktree
	var current worktree

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = worktree{}
			}
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "HEAD ") {
			current.Commit = strings.TrimPrefix(line, "HEAD ")
		} else if strings.HasPrefix(line, "branch ") {
			current.Branch = strings.TrimPrefix(line, "branch ")
		} else if line == "bare" {
			current.Bare = true
		}
	}

	// Add the last worktree if exists
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}

// addWorktree creates a new git worktree with optional branch creation.
func addWorktree(ctx context.Context, rc *skillmain.RunContext, gitPath, repoPath string, in input) (map[string]any, error) {
	if in.Path == "" {
		return nil, skillerr.Arg(
			"path is required for add operation",
			skillerr.WithHint("Provide path when operation is add."),
		)
	}

	// Validate the target path
	targetPath, err := skillmain.ValidatePath(rc, in.Path, skillmain.WithPathMessage("invalid target path"))
	if err != nil {
		return nil, err
	}

	args := []string{"-C", repoPath, "worktree", "add"}

	if in.NewBranch && in.Branch != "" {
		args = append(args, "-b", in.Branch)
	}

	args = append(args, targetPath)

	if !in.NewBranch && in.Branch != "" {
		args = append(args, in.Branch)
	}

	result := executil.Run(ctx, "", gitPath, args...)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git worktree add failed: %s", string(result.Stderr))
	}

	return map[string]any{
		"operation": "add",
		"path":      targetPath,
		"branch":    in.Branch,
		"message":   fmt.Sprintf("Worktree added at %s", targetPath),
	}, nil
}

// removeWorktree removes an existing git worktree with optional force flag.
func removeWorktree(ctx context.Context, rc *skillmain.RunContext, gitPath, repoPath string, in input) (map[string]any, error) {
	if in.Path == "" {
		return nil, skillerr.Arg(
			"path is required for remove operation",
			skillerr.WithHint("Provide path when operation is remove."),
		)
	}

	// Validate the target path
	targetPath, err := skillmain.ValidatePath(rc, in.Path, skillmain.WithPathMessage("invalid target path"))
	if err != nil {
		return nil, err
	}

	args := []string{"-C", repoPath, "worktree", "remove"}

	if in.Force {
		args = append(args, "--force")
	}

	args = append(args, targetPath)

	result := executil.Run(ctx, "", gitPath, args...)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git worktree remove failed: %s", string(result.Stderr))
	}

	return map[string]any{
		"operation": "remove",
		"path":      targetPath,
		"message":   fmt.Sprintf("Worktree removed from %s", targetPath),
	}, nil
}

// pruneWorktrees removes stale worktree references from the repository.
func pruneWorktrees(ctx context.Context, gitPath, repoPath string) (map[string]any, error) {
	result := executil.Run(ctx, "", gitPath, "-C", repoPath, "worktree", "prune", "-v")
	output := append([]byte{}, result.Stdout...)
	output = append(output, result.Stderr...)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git worktree prune failed: %s", string(output))
	}

	message := strings.TrimSpace(string(output))
	if message == "" {
		message = "No stale worktrees to prune"
	}

	return map[string]any{
		"operation": "prune",
		"message":   message,
	}, nil
}
