package status

import (
	"context"
	"os/exec"
	"strings"

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
)

type GitStatus struct {
	Available bool   `json:"available"`
	HeadSHA   string `json:"head_sha,omitempty"`
}

type RepoIndexStatus struct {
	Available bool                `json:"available"`
	StorePath string              `json:"store_path,omitempty"`
	Meta      repoindex.IndexMeta `json:"meta,omitempty"`
	Stats     repoindex.Stats     `json:"stats,omitempty"`
	Languages []string            `json:"languages,omitempty"`
}

type Status struct {
	Scope     refscope.Scope  `json:"scope"`
	Mode      Mode            `json:"mode"`
	Reasons   []string        `json:"reasons,omitempty"`
	Git       GitStatus       `json:"git"`
	RepoIndex RepoIndexStatus `json:"repo_index"`
}

func Evaluate(ctx context.Context, storageRoot string, scope refscope.Scope) Status {
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

	if len(out.Reasons) == 0 {
		out.Mode = ModeIndexBacked
	}
	return out
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
