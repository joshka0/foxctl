package status

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/fsutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/langutil"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
)

type Mode string

const (
	ModeIndexBacked Mode = "index_backed"
	ModeParserOnly  Mode = "parser_only"
)

const (
	ReasonRepoIndexMissing          = "repoindex_missing"
	ReasonRepoIndexOpenFailed       = "repoindex_open_failed"
	ReasonRepoIndexMetaUnavailable  = "repoindex_meta_unavailable"
	ReasonRepoIndexStatsUnavailable = "repoindex_stats_unavailable"
	ReasonRepoIndexSchemaMismatch   = "repoindex_schema_mismatch"
	ReasonRepoIndexHeadMismatch     = "repoindex_head_mismatch"
	ReasonGitHeadUnavailable        = "git_head_unavailable"
	ReasonScopeLanguageNotIndexed   = "scope_language_not_indexed"
	ReasonScopePathNotIndexed       = "scope_path_not_indexed"
)

const coverageSampleLimit = 5

type GitStatus struct {
	Available bool   `json:"available"`
	HeadSHA   string `json:"head_sha,omitempty"`
}

type ScopeCoverage struct {
	DiscoveredFileCount   int      `json:"discovered_file_count"`
	IndexedFileCount      int      `json:"indexed_file_count"`
	MatchedFileCount      int      `json:"matched_file_count"`
	MissingFileCount      int      `json:"missing_file_count"`
	ExtraIndexedFileCount int      `json:"extra_indexed_file_count"`
	MissingFilesSample    []string `json:"missing_files_sample,omitempty"`
}

type RepoIndexStatus struct {
	Available    bool                `json:"available"`
	StorePath    string              `json:"store_path,omitempty"`
	Meta         repoindex.IndexMeta `json:"meta,omitempty"`
	Stats        repoindex.Stats     `json:"stats,omitempty"`
	Languages    []string            `json:"languages,omitempty"`
	ScopeCovered bool                `json:"scope_covered"`
	Coverage     ScopeCoverage       `json:"coverage"`
}

type Status struct {
	Scope     refscope.Scope  `json:"scope"`
	Mode      Mode            `json:"mode"`
	Reasons   []string        `json:"reasons,omitempty"`
	Git       GitStatus       `json:"git"`
	RepoIndex RepoIndexStatus `json:"repo_index"`
}

func Evaluate(ctx context.Context, storageRoot string, scope refscope.Scope) Status {
	scope = rebaseToIndexedWorkspace(ctx, storageRoot, scope)
	out := Status{
		Scope:   scope,
		Mode:    ModeParserOnly,
		Reasons: make([]string, 0, 4),
	}

	headSHA := resolveGitHead(ctx, scope.RepoRoot)
	if strings.TrimSpace(headSHA) != "" {
		out.Git = GitStatus{Available: true, HeadSHA: headSHA}
	} else {
		out.Reasons = append(out.Reasons, ReasonGitHeadUnavailable)
	}

	store, err := repoindex.Open(ctx, storageRoot, scope.Workspace)
	if err != nil {
		out.Reasons = append(out.Reasons, ReasonRepoIndexOpenFailed)
		return out
	}
	defer store.Close()

	out.RepoIndex.StorePath = store.Path()

	meta, err := store.GetMeta(ctx)
	if err != nil {
		out.Reasons = append(out.Reasons, ReasonRepoIndexMetaUnavailable)
		return out
	}
	stats, err := store.Stats(ctx)
	if err != nil {
		out.Reasons = append(out.Reasons, ReasonRepoIndexStatsUnavailable)
		return out
	}

	out.RepoIndex.Meta = meta
	out.RepoIndex.Stats = stats
	out.RepoIndex.Languages = append([]string(nil), meta.Languages...)

	if meta.IndexedAt.IsZero() || strings.TrimSpace(meta.RepoRoot) == "" {
		out.RepoIndex.Available = false
		out.Reasons = append(out.Reasons, ReasonRepoIndexMissing)
		return out
	}
	out.RepoIndex.Available = true

	if meta.SchemaVersion != repoindex.SchemaVersion() {
		out.Reasons = append(out.Reasons, ReasonRepoIndexSchemaMismatch)
	}
	if out.Git.Available && strings.TrimSpace(meta.HeadSHA) != "" && meta.HeadSHA != out.Git.HeadSHA {
		out.Reasons = append(out.Reasons, ReasonRepoIndexHeadMismatch)
	}
	if !languageIndexed(scope.Language, meta.Languages) {
		out.Reasons = append(out.Reasons, ReasonScopeLanguageNotIndexed)
	}
	if coverage, err := scopeCoverage(ctx, store, scope); err == nil {
		out.RepoIndex.Coverage = coverage
		out.RepoIndex.ScopeCovered = coverageComplete(coverage)
		if !out.RepoIndex.ScopeCovered {
			out.Reasons = append(out.Reasons, ReasonScopePathNotIndexed)
		}
	} else {
		out.Reasons = append(out.Reasons, ReasonScopePathNotIndexed)
	}

	if len(out.Reasons) == 0 {
		out.Mode = ModeIndexBacked
	}
	return out
}

func rebaseToIndexedWorkspace(ctx context.Context, storageRoot string, scope refscope.Scope) refscope.Scope {
	current := strings.TrimSpace(scope.Workspace)
	target := strings.TrimSpace(scope.Absolute)
	if current == "" || target == "" {
		return scope
	}

	absWorkspace, err := filepath.Abs(current)
	if err != nil {
		return scope
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return scope
	}

	start := target
	if !scope.IsDir {
		start = filepath.Dir(target)
	}
	for candidate := start; candidate != ""; candidate = filepath.Dir(candidate) {
		if rel, err := filepath.Rel(absWorkspace, candidate); err != nil || relPathEscapesWorkspace(rel) {
			break
		}
		exists, err := repoindex.StoreExists(storageRoot, candidate)
		if err != nil || !exists {
			if filepath.Clean(candidate) == filepath.Clean(absWorkspace) {
				break
			}
			parent := filepath.Dir(candidate)
			if parent == candidate {
				break
			}
			continue
		}
		store, err := repoindex.Open(ctx, storageRoot, candidate)
		if err != nil {
			if filepath.Clean(candidate) == filepath.Clean(absWorkspace) {
				break
			}
			parent := filepath.Dir(candidate)
			if parent == candidate {
				break
			}
			continue
		}
		meta, err := store.GetMeta(ctx)
		_ = store.Close()
		if err == nil && !meta.IndexedAt.IsZero() && strings.TrimSpace(meta.RepoRoot) != "" && languageIndexed(scope.Language, meta.Languages) {
			scope.Workspace = filepath.Clean(candidate)
			scope.RepoRoot = filepath.Clean(candidate)
			scope.Path = workspaceRelativePath(scope.Workspace, scope.Absolute)
			return scope
		}
		if filepath.Clean(candidate) == filepath.Clean(absWorkspace) {
			break
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
	}
	return scope
}

func resolveGitHead(ctx context.Context, repoRoot string) string {
	if strings.TrimSpace(repoRoot) == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func languageIndexed(language string, languages []string) bool {
	if strings.TrimSpace(language) == "" {
		return false
	}
	want := normalizeIndexedLanguage(language)
	for _, indexed := range languages {
		if normalizeIndexedLanguage(indexed) == want {
			return true
		}
	}
	return false
}

func normalizeIndexedLanguage(language string) string {
	switch strings.TrimSpace(language) {
	case "javascript":
		return "typescript"
	default:
		return strings.TrimSpace(language)
	}
}

func scopeCoverage(ctx context.Context, store *repoindex.Store, scope refscope.Scope) (ScopeCoverage, error) {
	indexedFiles, err := store.ListFilesInScope(ctx, scope.Path, scope.IsDir)
	if err != nil {
		return ScopeCoverage{}, err
	}
	actualFiles, err := collectScopeFiles(scope)
	if err != nil {
		return ScopeCoverage{}, err
	}
	indexedSet := make(map[string]struct{}, len(indexedFiles))
	for _, file := range indexedFiles {
		indexedSet[file] = struct{}{}
	}
	coverage := ScopeCoverage{
		DiscoveredFileCount: len(actualFiles),
		IndexedFileCount:    len(indexedSet),
	}
	missing := make([]string, 0)
	for _, file := range actualFiles {
		if _, ok := indexedSet[file]; ok {
			coverage.MatchedFileCount++
			continue
		}
		missing = append(missing, file)
	}
	coverage.MissingFileCount = len(missing)
	if coverage.IndexedFileCount > coverage.MatchedFileCount {
		coverage.ExtraIndexedFileCount = coverage.IndexedFileCount - coverage.MatchedFileCount
	}
	if len(missing) > coverageSampleLimit {
		missing = missing[:coverageSampleLimit]
	}
	coverage.MissingFilesSample = append([]string(nil), missing...)
	return coverage, nil
}

func coverageComplete(coverage ScopeCoverage) bool {
	return coverage.DiscoveredFileCount > 0 && coverage.MissingFileCount == 0
}

func collectScopeFiles(scope refscope.Scope) ([]string, error) {
	if !scope.IsDir {
		if !scopeFileEligible(scope.Language, scope.Absolute) {
			return nil, nil
		}
		return []string{filepath.ToSlash(strings.TrimSpace(scope.Path))}, nil
	}

	files := make([]string, 0, 32)
	err := filepath.WalkDir(scope.Absolute, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if fsutil.ShouldSkipHiddenOrCommon(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !scope.IncludeTests && fsutil.IsTestFile(d.Name()) {
			return nil
		}
		if !scopeFileEligible(scope.Language, path) {
			return nil
		}
		rel, err := filepath.Rel(scope.Workspace, path)
		if err != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func workspaceRelativePath(workspace, absPath string) string {
	rel, err := filepath.Rel(workspace, absPath)
	if err != nil || rel == "" {
		return filepath.ToSlash(filepath.Clean(absPath))
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

func relPathEscapesWorkspace(rel string) bool {
	if strings.TrimSpace(rel) == "" {
		return false
	}
	rel = filepath.Clean(rel)
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func scopeFileEligible(language, path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return langutil.DetectAllowedWithHint(language, path, langutil.CommonCodeLanguages) != ""
}
