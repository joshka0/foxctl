// Package main implements the git/worktree skill.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const command = "git/worktree"

type input struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	RepoPath  string `json:"repo_path"`
	Force     bool   `json:"force"`
	NewBranch bool   `json:"new_branch"`
}

type worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	Bare   bool   `json:"bare"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	in.Operation = oputil.DefaultOp(in.Operation, "list")
	if in.RepoPath == "" {
		in.RepoPath = "."
	}

	// Validate and resolve repository path
	repoPath, err := resolveRepoPath(rc, in.RepoPath)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}

	// Check if git is available
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git command not found: %w", err)
	}

	// Dispatch operation
	data, err := oputil.NewSwitch(in.Operation).
		Case("list", func() (map[string]any, error) { return listWorktrees(ctx, repoPath) }).
		Case("add", func() (map[string]any, error) { return addWorktree(ctx, rc, repoPath, in) }).
		Case("remove", func() (map[string]any, error) { return removeWorktree(ctx, rc, repoPath, in) }).
		Case("prune", func() (map[string]any, error) { return pruneWorktrees(ctx, repoPath) }).
		Run()
	if err != nil {
		return err
	}

	return skillout.Emit(rc, command, data)
}

func resolveRepoPath(rc *skillmain.RunContext, path string) (string, error) {
	if path == "" {
		path = "."
	}
	valid, err := rc.PathValidator.ValidatePath(path)
	if err != nil {
		return "", fmt.Errorf("path validation failed: %w", err)
	}
	return valid, nil
}

func listWorktrees(ctx context.Context, repoPath string) (map[string]any, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git worktree list failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("git worktree list failed: %w", err)
	}

	worktrees := parseWorktreeList(string(output))

	return map[string]any{
		"operation":      "list",
		"worktree_count": len(worktrees),
		"worktrees":      worktrees,
	}, nil
}

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

func addWorktree(ctx context.Context, rc *skillmain.RunContext, repoPath string, in input) (map[string]any, error) {
	if in.Path == "" {
		return nil, fmt.Errorf("path is required for add operation")
	}

	// Validate the target path
	targetPath, err := rc.PathValidator.ValidatePath(in.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid target path: %w", err)
	}

	args := []string{"-C", repoPath, "worktree", "add"}

	if in.NewBranch && in.Branch != "" {
		args = append(args, "-b", in.Branch)
	}

	args = append(args, targetPath)

	if !in.NewBranch && in.Branch != "" {
		args = append(args, in.Branch)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree add failed: %s", stderr.String())
	}

	return map[string]any{
		"operation": "add",
		"path":      targetPath,
		"branch":    in.Branch,
		"message":   fmt.Sprintf("Worktree added at %s", targetPath),
	}, nil
}

func removeWorktree(ctx context.Context, rc *skillmain.RunContext, repoPath string, in input) (map[string]any, error) {
	if in.Path == "" {
		return nil, fmt.Errorf("path is required for remove operation")
	}

	// Validate the target path
	targetPath, err := rc.PathValidator.ValidatePath(in.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid target path: %w", err)
	}

	args := []string{"-C", repoPath, "worktree", "remove"}

	if in.Force {
		args = append(args, "--force")
	}

	args = append(args, targetPath)

	cmd := exec.CommandContext(ctx, "git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree remove failed: %s", stderr.String())
	}

	return map[string]any{
		"operation": "remove",
		"path":      targetPath,
		"message":   fmt.Sprintf("Worktree removed from %s", targetPath),
	}, nil
}

func pruneWorktrees(ctx context.Context, repoPath string) (map[string]any, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "prune", "-v")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git worktree prune failed: %s", string(output))
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

