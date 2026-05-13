package hot

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/langutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	refscope "github.com/joshka0/foxctl/internal/intelligence/refactor/scope"
	refsnapshot "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshot"
	refsnapshotstore "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshotstore"
)

type SinceKind string

const (
	SinceKindGitRef   SinceKind = "git_ref"
	SinceKindSnapshot SinceKind = "snapshot"
)

// BuildError reports a user-correctable hot-file request failure.
type BuildError struct {
	Message string
	Hint    string
}

func (e *BuildError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// SinceInfo describes the baseline used for the hot ranking.
type SinceInfo struct {
	Kind       SinceKind `json:"kind"`
	Value      string    `json:"value"`
	GitRef     string    `json:"git_ref,omitempty"`
	SnapshotID string    `json:"snapshot_id,omitempty"`
	GitHeadSHA string    `json:"git_head_sha,omitempty"`
}

// FileHotspot is a churn-ranked file within the scoped path.
type FileHotspot struct {
	Path        string    `json:"path"`
	Language    string    `json:"language,omitempty"`
	TouchCount  int       `json:"touch_count"`
	Score       float64   `json:"score"`
	LastTouched time.Time `json:"last_touched_at,omitempty"`
	LineCount   int       `json:"line_count,omitempty"`
	SymbolCount int       `json:"symbol_count,omitempty"`
	CurrentHash string    `json:"current_hash,omitempty"`
}

// Summary captures bounded counts for a hot ranking.
type Summary struct {
	FileCount      int  `json:"file_count"`
	LimitedResults bool `json:"limited_by_results,omitempty"`
}

// Result is the refactor hot output payload.
type Result struct {
	Since   SinceInfo     `json:"since"`
	Summary Summary       `json:"summary"`
	Files   []FileHotspot `json:"files"`
}

// Options configures a refactor hot computation.
type Options struct {
	Scope        refscope.Scope
	IncludeTests bool
	Since        string
	MaxResults   int
	HalfLifeDays int
	Now          time.Time
}

// Build computes a file-level hot ranking for a scoped path.
func Build(ctx context.Context, storageRoot string, opts Options) (Result, error) {
	since := strings.TrimSpace(opts.Since)
	if since == "" {
		return Result{}, &BuildError{
			Message: "--since is required",
			Hint:    "Pass a git ref like HEAD~20 or a refactor snapshot id like refsnap-1775039485123.",
		}
	}
	if opts.MaxResults <= 0 {
		opts.MaxResults = 20
	}
	if opts.HalfLifeDays <= 0 {
		opts.HalfLifeDays = 90
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}

	sinceInfo, gitBase, err := resolveSince(ctx, storageRoot, since)
	if err != nil {
		return Result{}, err
	}

	currentSnapshot, err := refsnapshot.Builder{}.Build(ctx, refsnapshot.Input{
		SnapshotID:   refsnapshot.GenerateID(opts.Now),
		CreatedAt:    opts.Now,
		Scope:        opts.Scope,
		IncludeTests: opts.IncludeTests,
	})
	if err != nil {
		return Result{}, err
	}
	currentFiles := make(map[string]refsnapshot.FileSnapshot, len(currentSnapshot.Files))
	for _, file := range currentSnapshot.Files {
		currentFiles[file.Path] = file
	}

	fileScores, err := collectHotFiles(ctx, opts.Scope, opts.IncludeTests, gitBase, opts.HalfLifeDays, opts.Now)
	if err != nil {
		return Result{}, err
	}

	files := make([]FileHotspot, 0, len(fileScores))
	for _, item := range fileScores {
		hot := FileHotspot{
			Path:        item.Path,
			Language:    item.Language,
			TouchCount:  item.TouchCount,
			Score:       item.Score,
			LastTouched: item.LastTouched,
		}
		if file, ok := currentFiles[item.Path]; ok {
			hot.LineCount = file.LineCount
			hot.SymbolCount = file.SymbolCount
			hot.CurrentHash = file.Hash
			if hot.Language == "" {
				hot.Language = file.Language
			}
		}
		files = append(files, hot)
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Score != files[j].Score {
			return files[i].Score > files[j].Score
		}
		if files[i].TouchCount != files[j].TouchCount {
			return files[i].TouchCount > files[j].TouchCount
		}
		return files[i].Path < files[j].Path
	})

	limited := false
	total := len(files)
	if len(files) > opts.MaxResults {
		files = files[:opts.MaxResults]
		limited = true
	}

	return Result{
		Since: sinceInfo,
		Summary: Summary{
			FileCount:      total,
			LimitedResults: limited,
		},
		Files: files,
	}, nil
}

type fileScore struct {
	Path        string
	Language    string
	TouchCount  int
	Score       float64
	LastTouched time.Time
}

func resolveSince(ctx context.Context, storageRoot, since string) (SinceInfo, string, error) {
	if strings.HasPrefix(since, "refsnap-") {
		store, err := refsnapshotstore.Open(ctx, storageRoot)
		if err != nil {
			return SinceInfo{}, "", fmt.Errorf("open refactor snapshot store: %w", err)
		}
		defer store.Close()
		record, err := store.Get(ctx, since)
		if err != nil {
			return SinceInfo{}, "", &BuildError{
				Message: fmt.Sprintf("snapshot %q not found", since),
				Hint:    "Create a snapshot first with `foxctl refactor snapshot ...`, then re-run `refactor hot --since <snapshot-id>`.",
			}
		}
		if strings.TrimSpace(record.GitHeadSHA) == "" {
			return SinceInfo{}, "", &BuildError{
				Message: fmt.Sprintf("snapshot %q has no git baseline", since),
				Hint:    "Use a snapshot captured from a git-backed workspace, or pass a git ref directly.",
			}
		}
		return SinceInfo{
			Kind:       SinceKindSnapshot,
			Value:      since,
			SnapshotID: since,
			GitHeadSHA: record.GitHeadSHA,
		}, record.GitHeadSHA, nil
	}
	return SinceInfo{
		Kind:   SinceKindGitRef,
		Value:  since,
		GitRef: since,
	}, since, nil
}

func collectHotFiles(ctx context.Context, scope refscope.Scope, includeTests bool, gitBase string, halfLifeDays int, now time.Time) ([]fileScore, error) {
	args := []string{"-C", scope.RepoRoot, "log", "--format=%H%x1f%ct", "--name-only", fmt.Sprintf("%s..HEAD", strings.TrimSpace(gitBase)), "--"}
	if path := scopeGitPath(scope); path != "" {
		args = append(args, path)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, &BuildError{
			Message: fmt.Sprintf("git log since %q failed", gitBase),
			Hint:    fmt.Sprintf("Verify that %q is a valid git ref or snapshot baseline. stderr: %s", gitBase, strings.TrimSpace(stderr.String())),
		}
	}

	return parseHotLogScores(stdout.String(), scope, includeTests, halfLifeDays, now), nil
}

func parseHotLogScores(logOutput string, scope refscope.Scope, includeTests bool, halfLifeDays int, now time.Time) []fileScore {
	scopePath := normalizedScopePath(scope)
	scores := map[string]*fileScore{}
	var currentTime time.Time
	for _, raw := range strings.Split(logOutput, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if ts, isCommitLine := parseHotCommitTimestamp(line); isCommitLine {
			currentTime = ts
			continue
		}
		path, lang, ok := filterHotPath(line, scopePath, scope.IsDir, includeTests, scope.Language)
		if !ok {
			continue
		}
		addHotScore(scores, path, lang, currentTime, now, halfLifeDays)
	}
	return rankHotFileScores(scores)
}

func parseHotCommitTimestamp(line string) (time.Time, bool) {
	if !strings.Contains(line, "\x1f") {
		return time.Time{}, false
	}
	parts := strings.SplitN(line, "\x1f", 2)
	if len(parts) != 2 {
		return time.Time{}, true
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return time.Time{}, true
	}
	return time.Unix(sec, 0).UTC(), true
}

func filterHotPath(rawPath, scopePath string, isDir, includeTests bool, languageHint string) (string, string, bool) {
	path := pathutil.ToSlash(strings.TrimSpace(rawPath))
	if path == "" || !pathInScope(path, scopePath, isDir) {
		return "", "", false
	}
	if !includeTests && fsutil.IsTestFile(filepath.Base(path)) {
		return "", "", false
	}
	lang := langutil.DetectAllowedWithHint(languageHint, path, langutil.CommonCodeLanguages)
	if lang == "" {
		return "", "", false
	}
	return path, lang, true
}

func addHotScore(scores map[string]*fileScore, path, language string, touchedAt, now time.Time, halfLifeDays int) {
	weight := recencyWeight(touchedAt, now, halfLifeDays)
	item := scores[path]
	if item == nil {
		item = &fileScore{Path: path, Language: language}
		scores[path] = item
	}
	item.TouchCount++
	item.Score += weight
	if touchedAt.After(item.LastTouched) {
		item.LastTouched = touchedAt
	}
}

func rankHotFileScores(scores map[string]*fileScore) []fileScore {
	out := make([]fileScore, 0, len(scores))
	for _, item := range scores {
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].TouchCount != out[j].TouchCount {
			return out[i].TouchCount > out[j].TouchCount
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func recencyWeight(ts, now time.Time, halfLifeDays int) float64 {
	if ts.IsZero() || halfLifeDays <= 0 {
		return 1
	}
	ageDays := now.Sub(ts).Hours() / 24
	if ageDays <= 0 {
		return 1
	}
	return math.Pow(0.5, ageDays/float64(halfLifeDays))
}

func normalizedScopePath(scope refscope.Scope) string {
	path := pathutil.ToSlash(strings.TrimSpace(scope.Path))
	if path == "" {
		return "."
	}
	return path
}

func scopeGitPath(scope refscope.Scope) string {
	if scope.Path == "." || strings.TrimSpace(scope.Path) == "" {
		return ""
	}
	return pathutil.ToSlash(strings.TrimSpace(scope.Path))
}

func pathInScope(path, scopePath string, isDir bool) bool {
	if scopePath == "." {
		return true
	}
	if !isDir {
		return path == scopePath
	}
	return path == scopePath || strings.HasPrefix(path, scopePath+"/")
}
