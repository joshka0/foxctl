package changes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/langutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/intelligence/indexing"
	refscope "github.com/joshka0/foxctl/internal/intelligence/refactor/scope"
	refsnapshot "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshot"
	refsnapshotstore "github.com/joshka0/foxctl/internal/intelligence/refactor/snapshotstore"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

type SinceKind string

const (
	SinceKindGitRef   SinceKind = "git_ref"
	SinceKindSnapshot SinceKind = "snapshot"
)

// SinceInfo describes the baseline used for a changes comparison.
type SinceInfo struct {
	Kind       SinceKind `json:"kind"`
	Value      string    `json:"value"`
	GitRef     string    `json:"git_ref,omitempty"`
	SnapshotID string    `json:"snapshot_id,omitempty"`
	GitHeadSHA string    `json:"git_head_sha,omitempty"`
}

// FileChange is the refactor-facing file change projection.
type FileChange struct {
	Path         string              `json:"path"`
	Language     string              `json:"language,omitempty"`
	ChangeKind   indexing.ChangeKind `json:"change_kind"`
	PreviousHash string              `json:"previous_hash,omitempty"`
	CurrentHash  string              `json:"current_hash,omitempty"`
}

// SymbolChange is the refactor-facing symbol change projection.
type SymbolChange struct {
	Path       string              `json:"path"`
	SymbolID   string              `json:"symbol_id"`
	Name       string              `json:"name"`
	Kind       string              `json:"kind,omitempty"`
	ChangeKind indexing.ChangeKind `json:"change_kind"`
	Hash       string              `json:"hash,omitempty"`
}

// Summary captures bounded counts for a changes result.
type Summary struct {
	FileCount       int            `json:"file_count"`
	SymbolCount     int            `json:"symbol_count"`
	ChangeKinds     map[string]int `json:"change_kinds,omitempty"`
	LimitedByFiles  bool           `json:"limited_by_files,omitempty"`
	LimitedBySymbol bool           `json:"limited_by_symbols,omitempty"`
}

// Result is the bounded output payload for refactor changes.
type Result struct {
	Since   SinceInfo      `json:"since"`
	Summary Summary        `json:"summary"`
	Files   []FileChange   `json:"files"`
	Symbols []SymbolChange `json:"symbols"`
}

// BuildError reports a user-correctable changes request failure.
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

// Options configures a refactor changes computation.
type Options struct {
	Scope        refscope.Scope
	IncludeTests bool
	Since        string
	MaxFiles     int
	MaxSymbols   int
}

// Build computes changed files and symbols for a scope against a git ref or snapshot baseline.
func Build(ctx context.Context, storageRoot, casRoot string, now time.Time, opts Options) (Result, error) {
	since := strings.TrimSpace(opts.Since)
	if since == "" {
		return Result{}, &BuildError{
			Message: "--since is required",
			Hint:    "Pass a git ref like HEAD~5 or a refactor snapshot id like refsnap-1775039485123.",
		}
	}

	if opts.MaxFiles <= 0 {
		opts.MaxFiles = 200
	}
	if opts.MaxSymbols <= 0 {
		opts.MaxSymbols = 200
	}

	currentSnapshot, err := refsnapshot.Builder{}.Build(ctx, refsnapshot.Input{
		SnapshotID:   refsnapshot.GenerateID(now.UTC()),
		CreatedAt:    now.UTC(),
		Scope:        opts.Scope,
		IncludeTests: opts.IncludeTests,
	})
	if err != nil {
		return Result{}, err
	}

	switch {
	case strings.HasPrefix(since, "refsnap-"):
		return buildSnapshotChanges(ctx, storageRoot, casRoot, since, currentSnapshot, opts.MaxFiles, opts.MaxSymbols)
	default:
		return buildGitChanges(ctx, opts.Scope, opts.IncludeTests, since, currentSnapshot, opts.MaxFiles, opts.MaxSymbols)
	}
}

func buildSnapshotChanges(ctx context.Context, storageRoot, casRoot, snapshotID string, current refsnapshot.Payload, maxFiles, maxSymbols int) (Result, error) {
	store, err := refsnapshotstore.Open(ctx, storageRoot)
	if err != nil {
		return Result{}, fmt.Errorf("open refactor snapshot store: %w", err)
	}
	defer store.Close()

	record, err := store.Get(ctx, snapshotID)
	if err != nil {
		return Result{}, &BuildError{
			Message: fmt.Sprintf("snapshot %q not found", snapshotID),
			Hint:    "Create a snapshot first with `foxctl refactor snapshot ...`, then re-run `refactor changes --since <snapshot-id>`.",
		}
	}

	previous, err := readSnapshotArtifact(ctx, casRoot, record.ArtifactDigest)
	if err != nil {
		return Result{}, fmt.Errorf("read snapshot artifact: %w", err)
	}

	files, symbols, summary := diffSnapshots(previous, current, maxFiles, maxSymbols)
	return Result{
		Since: SinceInfo{
			Kind:       SinceKindSnapshot,
			Value:      snapshotID,
			SnapshotID: snapshotID,
			GitHeadSHA: record.GitHeadSHA,
		},
		Summary: summary,
		Files:   files,
		Symbols: symbols,
	}, nil
}

func buildGitChanges(ctx context.Context, scope refscope.Scope, includeTests bool, gitRef string, current refsnapshot.Payload, maxFiles, maxSymbols int) (Result, error) {
	fileChanges, err := collectGitFileChanges(ctx, scope, includeTests, gitRef)
	if err != nil {
		return Result{}, fmt.Errorf("collect git changes: %w", err)
	}

	currentFiles := map[string]refsnapshot.FileSnapshot{}
	for _, file := range current.Files {
		currentFiles[file.Path] = file
	}
	currentSymbolsByPath := map[string][]refsnapshot.SymbolSnapshot{}
	for _, symbol := range current.Symbols {
		currentSymbolsByPath[symbol.Path] = append(currentSymbolsByPath[symbol.Path], symbol)
	}

	sort.Slice(fileChanges, func(i, j int) bool {
		return fileChanges[i].Path < fileChanges[j].Path
	})

	files := make([]FileChange, 0, min(len(fileChanges), maxFiles))
	symbols := make([]SymbolChange, 0, maxSymbols)
	changeKinds := map[string]int{}
	totalSymbols := 0
	for _, change := range fileChanges {
		changeKinds[string(change.ChangeKind)]++
		fileRecord := FileChange{
			Path:       change.Path,
			Language:   change.Language,
			ChangeKind: change.ChangeKind,
		}
		if file, ok := currentFiles[change.Path]; ok {
			fileRecord.CurrentHash = file.Hash
			if fileRecord.Language == "" {
				fileRecord.Language = file.Language
			}
		}
		if len(files) < maxFiles {
			files = append(files, fileRecord)
		}

		if change.ChangeKind == indexing.ChangeKindDeleted {
			continue
		}
		for _, symbol := range currentSymbolsByPath[change.Path] {
			totalSymbols++
			if len(symbols) >= maxSymbols {
				break
			}
			symbols = append(symbols, SymbolChange{
				Path:       symbol.Path,
				SymbolID:   symbol.SymbolID,
				Name:       symbol.Name,
				Kind:       string(symbol.Kind),
				ChangeKind: normalizeSymbolChangeKind(change.ChangeKind),
				Hash:       symbol.Hash,
			})
		}
	}

	return Result{
		Since: SinceInfo{
			Kind:   SinceKindGitRef,
			Value:  gitRef,
			GitRef: gitRef,
		},
		Summary: Summary{
			FileCount:       len(fileChanges),
			SymbolCount:     totalSymbols,
			ChangeKinds:     changeKinds,
			LimitedByFiles:  len(fileChanges) > maxFiles,
			LimitedBySymbol: totalSymbols > maxSymbols,
		},
		Files:   files,
		Symbols: symbols,
	}, nil
}

func readSnapshotArtifact(ctx context.Context, casRoot, digest string) (refsnapshot.Payload, error) {
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return refsnapshot.Payload{}, err
	}
	defer store.Close()
	rc, _, err := store.Get(ctx, strings.TrimSpace(digest))
	if err != nil {
		return refsnapshot.Payload{}, err
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return refsnapshot.Payload{}, err
	}
	var payload refsnapshot.Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		return refsnapshot.Payload{}, err
	}
	return payload, nil
}

func diffSnapshots(previous, current refsnapshot.Payload, maxFiles, maxSymbols int) ([]FileChange, []SymbolChange, Summary) {
	fileDiff := diffSnapshotFiles(previous.Files, current.Files, maxFiles)
	symbolDiff := diffSnapshotSymbols(previous.Symbols, current.Symbols, maxSymbols, fileDiff.changedPaths)
	return fileDiff.files, symbolDiff.symbols, Summary{
		FileCount:       fileDiff.total,
		SymbolCount:     symbolDiff.total,
		ChangeKinds:     fileDiff.changeKinds,
		LimitedByFiles:  fileDiff.total > maxFiles,
		LimitedBySymbol: symbolDiff.total > maxSymbols,
	}
}

type snapshotFileDiff struct {
	files        []FileChange
	total        int
	changeKinds  map[string]int
	changedPaths map[string]indexing.ChangeKind
}

func diffSnapshotFiles(previous, current []refsnapshot.FileSnapshot, maxFiles int) snapshotFileDiff {
	prevByPath := fileSnapshotsByPath(previous)
	currByPath := fileSnapshotsByPath(current)
	allPaths := sortedSnapshotKeys(prevByPath, currByPath)

	files := make([]FileChange, 0, min(len(allPaths), maxFiles))
	changeKinds := map[string]int{}
	changedPaths := map[string]indexing.ChangeKind{}
	totalFiles := 0

	for _, path := range allPaths {
		prev, hasPrev := prevByPath[path]
		curr, hasCurr := currByPath[path]
		kind, ok := snapshotFileChangeKind(prev, hasPrev, curr, hasCurr)
		if !ok {
			continue
		}
		totalFiles++
		changeKinds[string(kind)]++
		changedPaths[path] = kind
		if len(files) < maxFiles {
			files = append(files, FileChange{
				Path:         path,
				Language:     firstNonEmpty(curr.Language, prev.Language),
				ChangeKind:   kind,
				PreviousHash: prev.Hash,
				CurrentHash:  curr.Hash,
			})
		}
	}

	return snapshotFileDiff{
		files:        files,
		total:        totalFiles,
		changeKinds:  changeKinds,
		changedPaths: changedPaths,
	}
}

func fileSnapshotsByPath(files []refsnapshot.FileSnapshot) map[string]refsnapshot.FileSnapshot {
	out := make(map[string]refsnapshot.FileSnapshot, len(files))
	for _, file := range files {
		out[file.Path] = file
	}
	return out
}

func snapshotFileChangeKind(previous refsnapshot.FileSnapshot, hasPrevious bool, current refsnapshot.FileSnapshot, hasCurrent bool) (indexing.ChangeKind, bool) {
	switch {
	case hasPrevious && !hasCurrent:
		return indexing.ChangeKindDeleted, true
	case !hasPrevious && hasCurrent:
		return indexing.ChangeKindAdded, true
	case hasPrevious && hasCurrent && previous.Hash != current.Hash:
		return indexing.ChangeKindModified, true
	default:
		return "", false
	}
}

type snapshotSymbolDiff struct {
	symbols []SymbolChange
	total   int
}

func diffSnapshotSymbols(previous, current []refsnapshot.SymbolSnapshot, maxSymbols int, changedPaths map[string]indexing.ChangeKind) snapshotSymbolDiff {
	prevByID := symbolSnapshotsByID(previous)
	currByID := symbolSnapshotsByID(current)
	allSymbolIDs := sortedSnapshotKeys(prevByID, currByID)

	symbols := make([]SymbolChange, 0, min(len(allSymbolIDs), maxSymbols))
	totalSymbols := 0
	for _, id := range allSymbolIDs {
		prev, hasPrev := prevByID[id]
		curr, hasCurr := currByID[id]
		path, name, hash, kind, ok := snapshotSymbolChange(prev, hasPrev, curr, hasCurr)
		if !ok {
			continue
		}
		if kind == indexing.ChangeKindModified {
			if _, exists := changedPaths[path]; !exists {
				changedPaths[path] = indexing.ChangeKindModified
			}
		}
		totalSymbols++
		if len(symbols) < maxSymbols {
			symbols = append(symbols, SymbolChange{
				Path:       path,
				SymbolID:   id,
				Name:       name,
				Kind:       string(firstSymbolKind(curr, prev)),
				ChangeKind: kind,
				Hash:       hash,
			})
		}
	}
	return snapshotSymbolDiff{
		symbols: symbols,
		total:   totalSymbols,
	}
}

func symbolSnapshotsByID(symbols []refsnapshot.SymbolSnapshot) map[string]refsnapshot.SymbolSnapshot {
	out := make(map[string]refsnapshot.SymbolSnapshot, len(symbols))
	for _, symbol := range symbols {
		out[symbol.SymbolID] = symbol
	}
	return out
}

func snapshotSymbolChange(previous refsnapshot.SymbolSnapshot, hasPrevious bool, current refsnapshot.SymbolSnapshot, hasCurrent bool) (path, name, hash string, kind indexing.ChangeKind, ok bool) {
	switch {
	case hasPrevious && !hasCurrent:
		return previous.Path, previous.Name, previous.Hash, indexing.ChangeKindDeleted, true
	case !hasPrevious && hasCurrent:
		return current.Path, current.Name, current.Hash, indexing.ChangeKindAdded, true
	case hasPrevious && hasCurrent && previous.Hash != current.Hash:
		return current.Path, current.Name, current.Hash, indexing.ChangeKindModified, true
	default:
		return "", "", "", "", false
	}
}

func sortedSnapshotKeys[V any](left, right map[string]V) []string {
	keys := make([]string, 0, len(left)+len(right))
	seen := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range right {
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func collectGitFileChanges(ctx context.Context, scope refscope.Scope, includeTests bool, sinceRef string) ([]indexing.FileChange, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", scope.RepoRoot, "diff", "--name-status", strings.TrimSpace(sinceRef))
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff %q failed: %w (stderr: %s)", sinceRef, err, strings.TrimSpace(stderr.String()))
	}

	out := parseGitNameStatusChanges(stdout.String(), scope, includeTests)
	untracked, err := collectUntrackedFiles(ctx, scope, includeTests)
	if err != nil {
		return nil, err
	}
	return mergeGitFileChanges(out, untracked), nil
}

func collectUntrackedFiles(ctx context.Context, scope refscope.Scope, includeTests bool) ([]indexing.FileChange, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", scope.RepoRoot, "ls-files", "--others", "--exclude-standard")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return parseUntrackedFileChanges(stdout.String(), scope, includeTests), nil
}

func parseGitNameStatusChanges(output string, scope refscope.Scope, includeTests bool) []indexing.FileChange {
	scopePath := normalizedScopePath(scope)
	out := make([]indexing.FileChange, 0, 64)
	for _, line := range strings.Split(output, "\n") {
		rawChanges, ok := parseGitNameStatusLine(line)
		if !ok {
			continue
		}
		for _, rawChange := range rawChanges {
			change, ok := buildScopedChange(rawChange.path, rawChange.kind, scope, scopePath, includeTests)
			if !ok {
				continue
			}
			out = append(out, change)
		}
	}
	return out
}

func parseUntrackedFileChanges(output string, scope refscope.Scope, includeTests bool) []indexing.FileChange {
	scopePath := normalizedScopePath(scope)
	out := make([]indexing.FileChange, 0, 16)
	for _, line := range strings.Split(output, "\n") {
		if change, ok := buildScopedChange(line, indexing.ChangeKindAdded, scope, scopePath, includeTests); ok {
			out = append(out, change)
		}
	}
	return out
}

type rawNameStatusChange struct {
	path string
	kind indexing.ChangeKind
}

func parseGitNameStatusLine(line string) ([]rawNameStatusChange, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, false
	}
	parts := strings.Split(line, "\t")
	if len(parts) < 2 {
		return nil, false
	}
	status := strings.TrimSpace(parts[0])
	switch {
	case strings.HasPrefix(status, "R"), strings.HasPrefix(status, "C"):
		if len(parts) < 3 {
			return nil, false
		}
		return []rawNameStatusChange{
			{path: parts[1], kind: indexing.ChangeKindDeleted},
			{path: parts[2], kind: indexing.ChangeKindAdded},
		}, true
	case status == "A":
		return []rawNameStatusChange{{path: parts[1], kind: indexing.ChangeKindAdded}}, true
	case status == "D":
		return []rawNameStatusChange{{path: parts[1], kind: indexing.ChangeKindDeleted}}, true
	default:
		return []rawNameStatusChange{{path: parts[1], kind: indexing.ChangeKindModified}}, true
	}
}

func mergeGitFileChanges(diffChanges, untrackedChanges []indexing.FileChange) []indexing.FileChange {
	out := make([]indexing.FileChange, 0, len(diffChanges)+len(untrackedChanges))
	out = append(out, diffChanges...)
	return appendUniqueChanges(out, untrackedChanges)
}

func appendUniqueChanges(existing, candidate []indexing.FileChange) []indexing.FileChange {
	seen := make(map[string]struct{}, len(existing))
	for _, change := range existing {
		seen[change.Path] = struct{}{}
	}
	for _, change := range candidate {
		if _, ok := seen[change.Path]; ok {
			continue
		}
		seen[change.Path] = struct{}{}
		existing = append(existing, change)
	}
	return existing
}

func buildScopedChange(path string, kind indexing.ChangeKind, scope refscope.Scope, scopePath string, includeTests bool) (indexing.FileChange, bool) {
	path = pathutil.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return indexing.FileChange{}, false
	}
	if !pathInScope(path, scopePath, scope.IsDir) {
		return indexing.FileChange{}, false
	}
	if !includeTests && fsutil.IsTestFile(filepath.Base(path)) {
		return indexing.FileChange{}, false
	}
	lang := langutil.DetectAllowedWithHint(scope.Language, path, langutil.CommonCodeLanguages)
	if lang == "" {
		return indexing.FileChange{}, false
	}
	return indexing.FileChange{
		Path:       path,
		Language:   lang,
		ChangeKind: kind,
	}, true
}

func normalizedScopePath(scope refscope.Scope) string {
	path := pathutil.ToSlash(strings.TrimSpace(scope.Path))
	if path == "" {
		return "."
	}
	return path
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

func normalizeSymbolChangeKind(kind indexing.ChangeKind) indexing.ChangeKind {
	switch kind {
	case indexing.ChangeKindAdded:
		return indexing.ChangeKindAdded
	default:
		return indexing.ChangeKindModified
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstSymbolKind(values ...refsnapshot.SymbolSnapshot) string {
	for _, value := range values {
		if strings.TrimSpace(string(value.Kind)) != "" {
			return string(value.Kind)
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
