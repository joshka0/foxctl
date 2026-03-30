package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Detect returns the workspace path using the standard fallback chain:
// 1. AGENTCTL_WORKSPACE env var (highest priority - set by agentctl for sandboxes)
// 2. CLAUDE_PROJECT_DIR env var (set by Claude Code)
// 3. Walk up from start looking for .git or .agentctl markers
// 4. Return start directory if no markers found
//
// Pass empty string for start to use current working directory.
func Detect(start string) string {
	// 1. AGENTCTL_WORKSPACE has highest priority (handles sandbox scenarios)
	if ws := os.Getenv("AGENTCTL_WORKSPACE"); ws != "" {
		return ws
	}

	// 2. CLAUDE_PROJECT_DIR (set by Claude Code)
	if projDir := os.Getenv("CLAUDE_PROJECT_DIR"); projDir != "" {
		return projDir
	}

	// 3. Walk up looking for markers
	dir := start
	if dir == "" {
		if cwd, err := os.Getwd(); err == nil {
			dir = cwd
		}
	}
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	candidate := dir
	for {
		if hasMarker(candidate, ".agentctl") || hasMarker(candidate, ".git") {
			return candidate
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
		candidate = parent
	}
	return dir
}

// Normalize cleans the workspace path for display/persistence.
func Normalize(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// FamilyPath returns a stable repo-family root for a workspace path.
// For normal repositories this is the detected workspace root itself.
// For git worktrees, this resolves to the main repository root so related
// worktrees share one family path even without a configured remote.
func FamilyPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	clean := Normalize(strings.TrimSpace(path))
	if !pathExists(clean) {
		if candidate := familyPathFromWorktreeConvention(clean); candidate != "" {
			return candidate
		}
		return clean
	}
	root := Normalize(Detect(clean))
	if root == "" {
		return ""
	}
	gitMeta := filepath.Join(root, ".git")
	info, err := os.Stat(gitMeta)
	if err != nil {
		return root
	}
	if info.IsDir() {
		return root
	}
	data, err := os.ReadFile(gitMeta)
	if err != nil {
		return root
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(strings.ToLower(line), "gitdir:") {
		return root
	}
	gitdir := strings.TrimSpace(line[len("gitdir:"):])
	if gitdir == "" {
		return root
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(root, gitdir)
	}
	gitdir = filepath.Clean(gitdir)
	marker := string(filepath.Separator) + ".git" + string(filepath.Separator) + "worktrees" + string(filepath.Separator)
	idx := strings.LastIndex(gitdir, marker)
	if idx < 0 {
		return root
	}
	family := filepath.Clean(gitdir[:idx])
	if family == "" {
		return root
	}
	return family
}

func familyPathFromWorktreeConvention(path string) string {
	parent := filepath.Dir(path)
	base := filepath.Base(parent)
	if !strings.HasSuffix(base, "-worktrees") {
		return ""
	}
	repoName := strings.TrimSuffix(base, "-worktrees")
	if repoName == "" {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(parent), repoName)
	if !pathExists(candidate) {
		return ""
	}
	return Normalize(candidate)
}

// PathIdentity returns a stable identifier derived from a workspace path.
// This is used when a repo identity is unavailable.
func PathIdentity(path string) string {
	if path == "" {
		return ""
	}
	clean := Normalize(path)
	h := sha256.Sum256([]byte(clean))
	return "ws-" + hex.EncodeToString(h[:8])
}

// LooksLikeID reports whether s already resembles a workspace identifier.
//
// This is used to avoid accidentally hashing IDs as if they were paths.
//
// Accepted forms:
//   - "default" (legacy workspace)
//   - 32 hex characters (RepoIdentity)
//   - "ws-" + 16 hex characters (PathIdentity)
//   - "ws-" + <opaque> (custom/workflow IDs; must not contain path separators)
func LooksLikeID(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if s == "default" {
		return true
	}
	if len(s) == 32 && isHex(s) {
		return true
	}
	if strings.HasPrefix(s, "ws-") && len(s) == len("ws-")+16 && isHex(s[len("ws-"):]) {
		return true
	}
	// Treat "ws-" prefixed opaque values as IDs too, as long as they are not
	// filesystem paths. This keeps test/workflow IDs like "ws-golden" stable,
	// while still allowing CanonicalID to hash real paths.
	if strings.HasPrefix(s, "ws-") && len(s) > len("ws-") && !strings.ContainsAny(s, `/\\`) {
		return true
	}
	return false
}

// CanonicalID returns a stable workspace identifier for input.
//
// If input already looks like a workspace ID, it is returned unchanged.
//
// Otherwise, CanonicalID treats input as a filesystem path only when it appears
// to be one (contains path separators, "~", ".", "./", "../", or is absolute),
// or when it exists on disk. This avoids accidentally hashing workflow IDs like
// "test-workspace" while still canonicalizing real workspace paths like "." or
// "/Users/...".
func CanonicalID(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if LooksLikeID(input) {
		return input
	}

	// Path-like selectors should be canonicalized to stable IDs.
	if looksLikePathSelector(input) || pathExists(input) {
		return ID(input)
	}

	// Otherwise treat it as a stable custom workspace ID.
	return input
}

func looksLikePathSelector(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "~") {
		return true
	}
	// Explicit relative path forms.
	if s == "." || s == ".." || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return true
	}
	// Any path separators imply it's a path selector.
	if strings.ContainsAny(s, `/\\`) {
		return true
	}
	// Absolute paths (including Windows drives) are path selectors.
	if filepath.IsAbs(s) {
		return true
	}
	return false
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		isDigit := c >= '0' && c <= '9'
		isLower := c >= 'a' && c <= 'f'
		isUpper := c >= 'A' && c <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}

// ID returns the preferred workspace identifier for a path.
// It uses the repo identity when available, falling back to a path-derived ID.
func ID(path string) string {
	if path == "" {
		return ""
	}
	family := FamilyPath(path)
	if family == "" {
		family = path
	}
	if repo := RepoIdentity(family); repo != "" {
		return repo
	}
	return PathIdentity(family)
}

func hasMarker(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return false
	}
	// .agentctl must be a directory, but .git can be either a directory
	// (normal repo) or a file (git worktree pointing to main repo)
	if name == ".git" {
		return true // exists as file or directory
	}
	return info.IsDir()
}

// WorkspaceInfo contains both the workspace path and repository identity.
type WorkspaceInfo struct {
	// Path is the filesystem path to the workspace root.
	Path string
	// FamilyPath is the canonical repo-family root. For worktrees this points at
	// the main repository checkout rather than the individual worktree path.
	FamilyPath string
	// RepoIdentity is a stable identifier for the git repository.
	// For worktrees, this returns the same identity since they share the same remote.
	// Empty if not in a git repo or no remote configured.
	RepoIdentity string
}

// DetectWithIdentity returns both the workspace path and repo identity.
// This is useful for scenarios where you need a stable identifier across worktrees.
func DetectWithIdentity(start string) WorkspaceInfo {
	path := Detect(start)
	return WorkspaceInfo{
		Path:         path,
		FamilyPath:   FamilyPath(path),
		RepoIdentity: RepoIdentity(FamilyPath(path)),
	}
}

// RepoIdentity returns a stable identifier for the git repository at the given path.
// For worktrees, this returns the same identity since they share the same remote origin.
// Returns empty string if not in a git repo or no remote origin configured.
//
// The identity is a SHA256 hash of the normalized git remote origin URL,
// providing a consistent identifier regardless of:
// - SSH vs HTTPS URL format
// - .git suffix presence
// - Worktree vs main checkout
func RepoIdentity(workspace string) string {
	if workspace == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	remoteURL := ""
	for _, remote := range []string{"origin", "upstream"} {
		if u := gitRemoteURL(ctx, workspace, remote); u != "" {
			remoteURL = u
			break
		}
	}
	if remoteURL == "" {
		remotes := gitRemoteNames(ctx, workspace)
		sort.Strings(remotes)
		for _, remote := range remotes {
			remote = strings.TrimSpace(remote)
			if remote == "" || remote == "origin" || remote == "upstream" {
				continue
			}
			if u := gitRemoteURL(ctx, workspace, remote); u != "" {
				remoteURL = u
				break
			}
		}
	}
	if remoteURL == "" {
		return "" // Not a git repo or no configured remotes.
	}

	// Normalize the URL to handle SSH vs HTTPS and .git suffix variations
	normalized := normalizeGitURL(remoteURL)

	// Hash the normalized URL for a stable identifier
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:16]) // 32 hex chars
}

// normalizeGitURL converts git URLs to a canonical form for consistent hashing.
// Handles:
// - git@github.com:owner/repo.git -> github.com/owner/repo
// - https://github.com/owner/repo.git -> github.com/owner/repo
// - https://github.com/owner/repo -> github.com/owner/repo
func normalizeGitURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Prefer URL parsing when a scheme is present (https/ssh/git/file).
	if strings.Contains(raw, "://") {
		if parsed, err := neturl.Parse(raw); err == nil && parsed.Scheme != "" {
			scheme := strings.ToLower(parsed.Scheme)
			switch scheme {
			case "http", "https", "ssh", "git":
				host := strings.ToLower(parsed.Hostname())
				path := strings.Trim(parsed.Path, "/")
				path = strings.TrimSuffix(path, ".git")
				path = strings.TrimSuffix(path, "/")
				if host == "" {
					break
				}
				if path == "" {
					return host
				}
				return host + "/" + path
			case "file":
				trimmed := strings.TrimRight(raw, "/")
				return strings.TrimSuffix(trimmed, ".git")
			}
		}
	}

	trimmed := strings.TrimRight(raw, "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")

	// Handle SCP-like SSH: [user@]host:path
	// Examples:
	//   git@github.com:owner/repo.git -> github.com/owner/repo
	//   joshka@gitlab.com:group/sub/repo -> gitlab.com/group/sub/repo
	if i := strings.Index(trimmed, ":"); i > 0 && !strings.Contains(trimmed[:i], "/") {
		hostPart := trimmed[:i]
		pathPart := trimmed[i+1:]
		if at := strings.LastIndex(hostPart, "@"); at >= 0 {
			hostPart = hostPart[at+1:]
		}
		hostPart = strings.ToLower(hostPart)
		pathPart = strings.TrimLeft(pathPart, "/")
		if hostPart != "" && pathPart != "" {
			return hostPart + "/" + pathPart
		}
		if hostPart != "" {
			return hostPart
		}
	}

	// Unknown/opaque format; return suffix-only normalization.
	return trimmed
}

func gitRemoteURL(ctx context.Context, dir, remote string) string {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", remote)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitRemoteNames(ctx context.Context, dir string) []string {
	cmd := exec.CommandContext(ctx, "git", "remote")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(string(out), "\n")
	names := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		names = append(names, line)
	}
	return names
}
