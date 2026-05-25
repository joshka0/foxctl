package transcriptpipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/sessionkit/codexjsonl"
)

// DreamSourceProvider identifies a configured transcript source family.
type DreamSourceProvider string

const (
	DreamSourceProviderClaude DreamSourceProvider = "claude"
	DreamSourceProviderCodex  DreamSourceProvider = "codex"
	DreamSourceProviderPi     DreamSourceProvider = "pi"
	DreamSourceProviderHermes DreamSourceProvider = "hermes"
)

// DreamSourceStabilityStatus describes whether a file stayed unchanged while inspected.
type DreamSourceStabilityStatus string

const (
	DreamSourceStable      DreamSourceStabilityStatus = "stable"
	DreamSourceUnstable    DreamSourceStabilityStatus = "unstable"
	DreamSourceRootOnly    DreamSourceStabilityStatus = "root_only"
	DreamSourceInvalidRoot DreamSourceStabilityStatus = "invalid_root"
)

// DreamSourceRoot is one explicit transcript source root from configuration.
type DreamSourceRoot struct {
	Provider      DreamSourceProvider `json:"provider"`
	RootPath      string              `json:"root_path"`
	WorkspaceHint string              `json:"workspace_hint,omitempty"`
}

// DreamSourceCandidate is the typed discovery model for transcript dream sources.
type DreamSourceCandidate struct {
	Provider        DreamSourceProvider        `json:"provider"`
	Path            string                     `json:"path,omitempty"`
	SessionID       string                     `json:"session_id,omitempty"`
	WorkspaceHint   string                     `json:"workspace_hint,omitempty"`
	WorkspacePath   string                     `json:"workspace_path,omitempty"`
	Size            int64                      `json:"size,omitempty"`
	ModTime         time.Time                  `json:"mtime,omitempty"`
	Fingerprint     string                     `json:"fingerprint,omitempty"`
	Digest          string                     `json:"digest,omitempty"`
	StabilityStatus DreamSourceStabilityStatus `json:"stability_status"`
	Root            DreamSourceRoot            `json:"root"`
	Error           string                     `json:"error,omitempty"`
}

// DiscoverDreamSourceCandidates discovers transcript source candidates from explicit roots.
func DiscoverDreamSourceCandidates(roots []DreamSourceRoot) []DreamSourceCandidate {
	candidates, _ := DiscoverDreamSourceCandidatesContext(context.Background(), roots)
	return candidates
}

func DiscoverDreamSourceCandidatesContext(ctx context.Context, roots []DreamSourceRoot) ([]DreamSourceCandidate, error) {
	candidates := make([]DreamSourceCandidate, 0, len(roots))
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return candidates, err
		}
		root = normalizeDreamSourceRoot(root)
		if root.RootPath == "" || !dirExists(root.RootPath) {
			candidates = append(candidates, invalidDreamSourceRoot(root))
			continue
		}

		switch root.Provider {
		case DreamSourceProviderCodex:
			found, err := discoverCodexDreamSources(ctx, root)
			candidates = append(candidates, found...)
			if err != nil {
				return candidates, err
			}
		case DreamSourceProviderClaude:
			found, err := discoverClaudeDreamSources(ctx, root)
			candidates = append(candidates, found...)
			if err != nil {
				return candidates, err
			}
		case DreamSourceProviderPi, DreamSourceProviderHermes:
			candidates = append(candidates, rootOnlyDreamSource(root))
		default:
			candidates = append(candidates, invalidDreamSourceRoot(root))
		}
	}
	sortDreamSourceCandidates(candidates)
	return candidates, nil
}

func discoverCodexDreamSources(ctx context.Context, root DreamSourceRoot) ([]DreamSourceCandidate, error) {
	files, err := codexjsonl.ListSessionFiles(root.RootPath)
	if err != nil {
		return []DreamSourceCandidate{invalidDreamSourceRootWithError(root, err)}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]DreamSourceCandidate, 0, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		candidate := dreamSourceCandidateFromFile(ctx, root, file.Path, file.ID, root.WorkspaceHint)
		if candidate.Size == 0 && file.Size != 0 {
			candidate.Size = file.Size
		}
		if candidate.ModTime.IsZero() && !file.ModTime.IsZero() {
			candidate.ModTime = file.ModTime.UTC()
		}
		out = append(out, candidate)
	}
	return out, nil
}

func discoverClaudeDreamSources(ctx context.Context, root DreamSourceRoot) ([]DreamSourceCandidate, error) {
	files, err := listClaudeDreamSessionFiles(ctx, root.RootPath)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return []DreamSourceCandidate{invalidDreamSourceRootWithError(root, err)}, nil
	}
	out := make([]DreamSourceCandidate, 0, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		out = append(out, dreamSourceCandidateFromFile(ctx, root, file.Path, file.SessionID, root.WorkspaceHint))
	}
	return out, nil
}

type claudeDreamSessionFile struct {
	Path      string
	SessionID string
}

func listClaudeDreamSessionFiles(ctx context.Context, root string) ([]claudeDreamSessionFile, error) {
	files := make([]claudeDreamSessionFile, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		files = append(files, claudeDreamSessionFile{
			Path:      path,
			SessionID: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func dreamSourceCandidateFromFile(ctx context.Context, root DreamSourceRoot, path, sessionID, workspaceHint string) DreamSourceCandidate {
	before, err := os.Stat(path)
	if err != nil {
		return DreamSourceCandidate{
			Provider:        root.Provider,
			Path:            path,
			SessionID:       strings.TrimSpace(sessionID),
			WorkspaceHint:   strings.TrimSpace(workspaceHint),
			StabilityStatus: DreamSourceUnstable,
			Root:            root,
			Error:           err.Error(),
		}
	}

	digest, readErr := fileSHA256Context(ctx, path)
	after, statErr := os.Stat(path)
	status := DreamSourceStable
	if readErr != nil || statErr != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		status = DreamSourceUnstable
	}
	errText := ""
	if readErr != nil {
		errText = readErr.Error()
	} else if statErr != nil {
		errText = statErr.Error()
	}

	size := before.Size()
	mtime := before.ModTime().UTC()
	if statErr == nil {
		size = after.Size()
		mtime = after.ModTime().UTC()
	}

	return DreamSourceCandidate{
		Provider:        root.Provider,
		Path:            path,
		SessionID:       strings.TrimSpace(sessionID),
		WorkspaceHint:   strings.TrimSpace(workspaceHint),
		WorkspacePath:   strings.TrimSpace(workspaceHint),
		Size:            size,
		ModTime:         mtime,
		Fingerprint:     dreamSourceFingerprint(root.Provider, path, size, mtime, digest),
		Digest:          digest,
		StabilityStatus: status,
		Root:            root,
		Error:           errText,
	}
}

func fileSHA256Context(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	hash := sha256.New()
	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, err := hash.Write(buf[:n]); err != nil {
				return "", err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func dreamSourceFingerprint(provider DreamSourceProvider, path string, size int64, mtime time.Time, digest string) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{
		string(provider),
		filepath.Clean(path),
		fmt.Sprintf("%d", size),
		mtime.UTC().Format(time.RFC3339Nano),
		digest,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func rootOnlyDreamSource(root DreamSourceRoot) DreamSourceCandidate {
	return DreamSourceCandidate{
		Provider:        root.Provider,
		WorkspaceHint:   root.WorkspaceHint,
		WorkspacePath:   root.WorkspaceHint,
		StabilityStatus: DreamSourceRootOnly,
		Root:            root,
	}
}

func invalidDreamSourceRoot(root DreamSourceRoot) DreamSourceCandidate {
	return invalidDreamSourceRootWithError(root, nil)
}

func invalidDreamSourceRootWithError(root DreamSourceRoot, err error) DreamSourceCandidate {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return DreamSourceCandidate{
		Provider:        root.Provider,
		WorkspaceHint:   root.WorkspaceHint,
		WorkspacePath:   root.WorkspaceHint,
		StabilityStatus: DreamSourceInvalidRoot,
		Root:            root,
		Error:           msg,
	}
}

func normalizeDreamSourceRoot(root DreamSourceRoot) DreamSourceRoot {
	root.Provider = DreamSourceProvider(strings.ToLower(strings.TrimSpace(string(root.Provider))))
	root.RootPath = strings.TrimSpace(root.RootPath)
	root.WorkspaceHint = strings.TrimSpace(root.WorkspaceHint)
	return root
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func sortDreamSourceCandidates(candidates []DreamSourceCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		keys := [][2]string{
			{string(left.Provider), string(right.Provider)},
			{left.Root.RootPath, right.Root.RootPath},
			{left.WorkspaceHint, right.WorkspaceHint},
			{left.Path, right.Path},
			{left.SessionID, right.SessionID},
		}
		for _, key := range keys {
			if key[0] == key[1] {
				continue
			}
			return key[0] < key[1]
		}
		return left.Fingerprint < right.Fingerprint
	})
}
