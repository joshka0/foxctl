package transcriptpipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/sessionkit/claudejsonl"
	"github.com/joshka0/foxctl/internal/context/sessionkit/codexjsonl"
)

// DreamSourceProvider identifies a configured transcript source root.
type DreamSourceProvider string

const (
	DreamSourceProviderClaude DreamSourceProvider = "claude"
	DreamSourceProviderCodex  DreamSourceProvider = "codex"
	DreamSourceProviderPi     DreamSourceProvider = "pi"
	DreamSourceProviderHermes DreamSourceProvider = "hermes"
)

// DreamSourceStability describes whether a transcript file is quiet enough to process.
type DreamSourceStability string

const (
	DreamSourceStable        DreamSourceStability = "stable"
	DreamSourceChanging      DreamSourceStability = "changing"
	DreamSourceFutureMTime   DreamSourceStability = "future_mtime"
	DreamSourceInvalidStat   DreamSourceStability = "invalid_stat"
	DreamSourceOutsideRoot   DreamSourceStability = "outside_root"
	DreamSourceInvalidSource DreamSourceStability = "invalid_source"
)

var (
	ErrDreamSourceInvalidRoot = errors.New("transcriptpipeline: invalid dream source root")
	ErrDreamSourceInvalidFile = errors.New("transcriptpipeline: invalid dream source file")
)

// DreamSourceRoot is one explicitly configured source root. Pi and Hermes roots are
// represented here even though transcript parsing can land later.
type DreamSourceRoot struct {
	Provider      DreamSourceProvider
	RootPath      string
	WorkspacePath string
}

// DreamSourceFile is observed filesystem metadata for one candidate transcript.
type DreamSourceFile struct {
	Provider      DreamSourceProvider
	RootPath      string
	SourcePath    string
	SessionID     string
	WorkspacePath string
	Size          int64
	ModTime       time.Time
}

// DreamSourceCandidate is the deterministic source model consumed by dream workers.
type DreamSourceCandidate struct {
	Provider      DreamSourceProvider
	RootPath      string
	SourcePath    string
	SessionID     string
	WorkspacePath string
	Size          int64
	ModTime       time.Time
	Fingerprint   string
	Stability     DreamSourceStability
}

// Stable reports whether the source has passed the configured quiet period.
func (c DreamSourceCandidate) Stable() bool {
	return c.Stability == DreamSourceStable
}

// BuildDreamSourceCandidates turns configured roots plus stat metadata into a
// deduped, sorted candidate list without reading files or touching storage.
func BuildDreamSourceCandidates(roots []DreamSourceRoot, files []DreamSourceFile, now time.Time, quietPeriod time.Duration) ([]DreamSourceCandidate, error) {
	rootByKey := make(map[dreamSourceRootKey]DreamSourceRoot, len(roots))
	for _, root := range roots {
		normalized, err := normalizeDreamSourceRoot(root)
		if err != nil {
			return nil, err
		}
		rootByKey[dreamSourceRootKey{Provider: normalized.Provider, RootPath: normalized.RootPath}] = normalized
	}

	byIdentity := make(map[string]DreamSourceCandidate, len(files))
	for _, file := range files {
		candidate, err := buildDreamSourceCandidate(rootByKey, file, now, quietPeriod)
		if err != nil {
			return nil, err
		}
		key := dreamSourceIdentity(candidate)
		if existing, ok := byIdentity[key]; ok {
			byIdentity[key] = pickDeterministicDreamSource(existing, candidate)
			continue
		}
		byIdentity[key] = candidate
	}

	out := make([]DreamSourceCandidate, 0, len(byIdentity))
	for _, candidate := range byIdentity {
		out = append(out, candidate)
	}
	sortDreamSourceCandidates(out)
	return out, nil
}

// CodexDreamSourceFiles adapts the existing Codex session locator into the pure
// dream source model.
func CodexDreamSourceFiles(root DreamSourceRoot) ([]DreamSourceFile, error) {
	normalized, err := normalizeDreamSourceRoot(root)
	if err != nil {
		return nil, err
	}
	if normalized.Provider != DreamSourceProviderCodex {
		return nil, fmt.Errorf("%w: codex adapter requires provider %q", ErrDreamSourceInvalidRoot, DreamSourceProviderCodex)
	}
	sessions, err := codexjsonl.ListSessionFiles(normalized.RootPath)
	if err != nil {
		return nil, err
	}
	files := make([]DreamSourceFile, 0, len(sessions))
	for _, session := range sessions {
		files = append(files, DreamSourceFile{
			Provider:      normalized.Provider,
			RootPath:      normalized.RootPath,
			SourcePath:    session.Path,
			SessionID:     session.ID,
			WorkspacePath: normalized.WorkspacePath,
			Size:          session.Size,
			ModTime:       session.ModTime,
		})
	}
	return files, nil
}

// ClaudeDreamSourceFiles adapts the existing Claude workspace locator into the
// pure dream source model.
func ClaudeDreamSourceFiles(root DreamSourceRoot) ([]DreamSourceFile, error) {
	normalized, err := normalizeDreamSourceRoot(root)
	if err != nil {
		return nil, err
	}
	if normalized.Provider != DreamSourceProviderClaude {
		return nil, fmt.Errorf("%w: claude adapter requires provider %q", ErrDreamSourceInvalidRoot, DreamSourceProviderClaude)
	}
	pairs := claudejsonl.LocateAllSessionJSONLs(normalized.WorkspacePath)
	files := make([]DreamSourceFile, 0, len(pairs))
	for _, pair := range pairs {
		files = append(files, DreamSourceFile{
			Provider:      normalized.Provider,
			RootPath:      normalized.RootPath,
			SourcePath:    pair.Path,
			SessionID:     pair.SessionID,
			WorkspacePath: normalized.WorkspacePath,
		})
	}
	return files, nil
}

type dreamSourceRootKey struct {
	Provider DreamSourceProvider
	RootPath string
}

func normalizeDreamSourceRoot(root DreamSourceRoot) (DreamSourceRoot, error) {
	provider := DreamSourceProvider(strings.ToLower(strings.TrimSpace(string(root.Provider))))
	switch provider {
	case DreamSourceProviderClaude, DreamSourceProviderCodex, DreamSourceProviderPi, DreamSourceProviderHermes:
	default:
		return DreamSourceRoot{}, fmt.Errorf("%w: unsupported provider %q", ErrDreamSourceInvalidRoot, root.Provider)
	}
	rootPath := cleanDreamPath(root.RootPath)
	if rootPath == "" {
		return DreamSourceRoot{}, fmt.Errorf("%w: root path is required for provider %q", ErrDreamSourceInvalidRoot, provider)
	}
	return DreamSourceRoot{
		Provider:      provider,
		RootPath:      rootPath,
		WorkspacePath: cleanDreamPath(root.WorkspacePath),
	}, nil
}

func buildDreamSourceCandidate(roots map[dreamSourceRootKey]DreamSourceRoot, file DreamSourceFile, now time.Time, quietPeriod time.Duration) (DreamSourceCandidate, error) {
	provider := DreamSourceProvider(strings.ToLower(strings.TrimSpace(string(file.Provider))))
	rootPath := cleanDreamPath(file.RootPath)
	sourcePath := cleanDreamPath(file.SourcePath)
	if provider == "" || rootPath == "" || sourcePath == "" {
		return DreamSourceCandidate{}, fmt.Errorf("%w: provider, root_path, and source_path are required", ErrDreamSourceInvalidFile)
	}
	root, ok := roots[dreamSourceRootKey{Provider: provider, RootPath: rootPath}]
	if !ok {
		return DreamSourceCandidate{}, fmt.Errorf("%w: unconfigured root %s:%s", ErrDreamSourceInvalidFile, provider, rootPath)
	}
	sessionID := strings.TrimSpace(file.SessionID)
	if sessionID == "" {
		sessionID = dreamSessionIDFromPath(provider, sourcePath)
	}
	candidate := DreamSourceCandidate{
		Provider:      provider,
		RootPath:      root.RootPath,
		SourcePath:    sourcePath,
		SessionID:     sessionID,
		WorkspacePath: firstNonEmpty(cleanDreamPath(file.WorkspacePath), root.WorkspacePath),
		Size:          file.Size,
		ModTime:       file.ModTime.UTC(),
		Stability:     dreamSourceStability(root.RootPath, sourcePath, file.Size, file.ModTime.UTC(), now.UTC(), quietPeriod),
	}
	if strings.TrimSpace(candidate.SessionID) == "" {
		candidate.Stability = DreamSourceInvalidSource
	}
	candidate.Fingerprint = dreamSourceFingerprint(candidate)
	return candidate, nil
}

func dreamSourceStability(rootPath, sourcePath string, size int64, modTime, now time.Time, quietPeriod time.Duration) DreamSourceStability {
	if !dreamPathInsideRoot(rootPath, sourcePath) {
		return DreamSourceOutsideRoot
	}
	if size < 0 || modTime.IsZero() {
		return DreamSourceInvalidStat
	}
	if modTime.After(now) {
		return DreamSourceFutureMTime
	}
	if quietPeriod < 0 {
		quietPeriod = 0
	}
	if now.Sub(modTime) < quietPeriod {
		return DreamSourceChanging
	}
	return DreamSourceStable
}

func dreamPathInsideRoot(rootPath, sourcePath string) bool {
	rel, err := filepath.Rel(rootPath, sourcePath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != "" && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func dreamSessionIDFromPath(provider DreamSourceProvider, path string) string {
	switch provider {
	case DreamSourceProviderCodex:
		return strings.TrimSpace(codexjsonl.SessionIDFromFilename(path))
	case DreamSourceProviderClaude, DreamSourceProviderPi, DreamSourceProviderHermes:
		return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	default:
		return ""
	}
}

func dreamSourceFingerprint(candidate DreamSourceCandidate) string {
	h := sha256.New()
	fields := []string{
		string(candidate.Provider),
		candidate.RootPath,
		candidate.SourcePath,
		candidate.SessionID,
		candidate.WorkspacePath,
		fmt.Sprintf("%d", candidate.Size),
		fmt.Sprintf("%d", candidate.ModTime.UnixNano()),
	}
	for _, field := range fields {
		_, _ = h.Write([]byte(field))
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func dreamSourceIdentity(candidate DreamSourceCandidate) string {
	return strings.Join([]string{
		string(candidate.Provider),
		candidate.SourcePath,
	}, "\x00")
}

func pickDeterministicDreamSource(a, b DreamSourceCandidate) DreamSourceCandidate {
	if dreamSourceSortKey(b) < dreamSourceSortKey(a) {
		return b
	}
	return a
}

func sortDreamSourceCandidates(candidates []DreamSourceCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return dreamSourceSortKey(candidates[i]) < dreamSourceSortKey(candidates[j])
	})
}

func dreamSourceSortKey(candidate DreamSourceCandidate) string {
	return strings.Join([]string{
		string(candidate.Provider),
		candidate.WorkspacePath,
		candidate.SessionID,
		candidate.SourcePath,
		fmt.Sprintf("%020d", candidate.Size),
		fmt.Sprintf("%020d", candidate.ModTime.UnixNano()),
	}, "\x00")
}

func cleanDreamPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}
