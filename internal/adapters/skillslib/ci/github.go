package ci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/platform/env"
)

// NormalizeOwnerRepo splits a combined owner/repo input and trims values.
func NormalizeOwnerRepo(owner, repo string) (string, string) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)

	if repo != "" && strings.Contains(repo, "/") {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) == 2 {
			if owner == "" {
				owner = parts[0]
			}
			repo = parts[1]
		}
	}

	return owner, repo
}

// ApplyRepoEnv fills missing owner/repo from environment variables.
func ApplyRepoEnv(owner, repo string) (string, string) {
	if owner == "" {
		owner = env.GetString("GITHUB_OWNER")
	}
	if repo == "" {
		repo = env.GetString("GITHUB_REPO")
	}
	return owner, repo
}

// DetectRepo attempts to derive owner/repo from git remote origin.
func DetectRepo(ctx context.Context) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	// Try running from FOXCTL_WORKSPACE if set (fallback for MCP/daemon mode)
	if ws := os.Getenv("FOXCTL_WORKSPACE"); ws != "" {
		cmd.Dir = ws
	}
	out, err := cmd.Output()
	if err != nil {
		return "", "", skillerr.WrapRuntime("detect repo", err)
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", "", skillerr.Runtime("empty remote url")
	}

	if strings.HasPrefix(url, "git@") {
		parts := strings.SplitN(url, ":", 2)
		if len(parts) != 2 {
			return "", "", skillerr.Validationf("unexpected ssh remote format: %s", url)
		}
		path := strings.TrimSuffix(parts[1], ".git")
		sub := strings.SplitN(path, "/", 2)
		if len(sub) != 2 {
			return "", "", skillerr.Validationf("unexpected ssh path format: %s", path)
		}
		return sub[0], sub[1], nil
	}

	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		parts := strings.Split(url, "/")
		if len(parts) < 2 {
			return "", "", skillerr.Validationf("unexpected https remote format: %s", url)
		}
		owner := parts[len(parts)-2]
		repo := strings.TrimSuffix(parts[len(parts)-1], ".git")
		return owner, repo, nil
	}

	return "", "", skillerr.Validationf("unsupported remote url format: %s", url)
}

// ResolveOwnerRepo applies normalization, env fallbacks, and git auto-detection.
func ResolveOwnerRepo(ctx context.Context, owner, repo string) (string, string, error) {
	owner, repo = NormalizeOwnerRepo(owner, repo)
	owner, repo = ApplyRepoEnv(owner, repo)

	if owner == "" || repo == "" {
		detectedOwner, detectedRepo, err := DetectRepo(ctx)
		if err == nil {
			if owner == "" {
				owner = detectedOwner
			}
			if repo == "" {
				repo = detectedRepo
			}
		}
	}

	if owner == "" || repo == "" {
		return "", "", skillerr.Arg(
			"repository owner and name are required",
			skillerr.WithHint("Set owner/repo, GITHUB_OWNER/GITHUB_REPO, or run in a git repository with origin set."),
		)
	}

	return owner, repo, nil
}

// ResolveToken returns a GitHub API token from env or gh auth.
func ResolveToken(ctx context.Context) (string, error) {
	token := env.GetString("GITHUB_TOKEN")
	if token != "" {
		return token, nil
	}

	if executil.HasTool("gh") {
		cmd := exec.CommandContext(ctx, "gh", "auth", "token")
		out, err := cmd.Output()
		if err == nil {
			candidate := strings.TrimSpace(string(out))
			if candidate != "" {
				return candidate, nil
			}
		}
	}

	return "", skillerr.Auth("GitHub token is required", skillerr.WithHint("Set GITHUB_TOKEN or configure gh auth token."))
}

// ResolvePRNumber maps a PR number or branch name to a PR number.
func ResolvePRNumber(owner, repo, prRef, token string) (int, error) {
	if prNum, err := strconv.Atoi(prRef); err == nil {
		return prNum, nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	url := githubPullsByHeadURL(owner, repo, prRef)

	var prs []struct {
		Number int `json:"number"`
	}
	if err := GitHubGET(client, token, url, &prs); err != nil {
		return 0, err
	}
	if len(prs) == 0 {
		return 0, skillerr.NotFoundf("no PR found for branch %q", prRef)
	}
	return prs[0].Number, nil
}

func githubPullsByHeadURL(owner, repo, branch string) string {
	query := url.Values{}
	query.Set("head", owner+":"+branch)
	query.Set("state", "all")
	return fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/pulls?%s",
		url.PathEscape(owner),
		url.PathEscape(repo),
		query.Encode(),
	)
}

// GitHubGET performs a GitHub API GET request and decodes JSON into v.
func GitHubGET(client *http.Client, token, url string, v any) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return skillerr.WrapRuntime("create request", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return skillerr.WrapRuntime("send request", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024)) //nolint:errcheck
		return skillerr.Runtimef("GitHub API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return skillerr.WrapParse("decode response", err)
	}
	return nil
}
