package snapshot

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/fsutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/langutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/pathutil"
	symindex "github.com/jkatigb/agentctl/internal/intelligence/indexing/symbol"
	refscope "github.com/jkatigb/agentctl/internal/intelligence/refactor/scope"
	refstatus "github.com/jkatigb/agentctl/internal/intelligence/refactor/status"
	platformsymbol "github.com/jkatigb/agentctl/internal/platform/symbolutil"
)

// BuildError reports a user-correctable snapshot build failure.
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

// Summary captures high-level scope counts.
type Summary struct {
	FileCount   int `json:"file_count"`
	SymbolCount int `json:"symbol_count"`
	LineCount   int `json:"line_count"`
}

// GitSnapshot captures the git head used for the snapshot.
type GitSnapshot struct {
	HeadSHA string `json:"head_sha,omitempty"`
}

// RepoIndexSnapshot captures the repoindex metadata relevant to the snapshot.
type RepoIndexSnapshot struct {
	HeadSHA       string    `json:"head_sha,omitempty"`
	IndexedAt     time.Time `json:"indexed_at,omitempty"`
	SchemaVersion int       `json:"schema_version,omitempty"`
}

// FileSnapshot captures per-file metadata in a refactor snapshot artifact.
type FileSnapshot struct {
	Path        string `json:"path"`
	Language    string `json:"language"`
	LineCount   int    `json:"line_count"`
	Hash        string `json:"hash"`
	SymbolCount int    `json:"symbol_count"`
	Package     string `json:"package,omitempty"`
}

// SymbolSnapshot captures stable symbol metadata in a refactor snapshot artifact.
type SymbolSnapshot struct {
	Path      string        `json:"path"`
	SymbolID  string        `json:"symbol_id"`
	Name      string        `json:"name"`
	Kind      symindex.Kind `json:"kind"`
	Hash      string        `json:"hash,omitempty"`
	LineStart int           `json:"line_start,omitempty"`
	LineEnd   int           `json:"line_end,omitempty"`
	Signature string        `json:"signature,omitempty"`
}

// Payload is the full persisted refactor snapshot artifact payload.
type Payload struct {
	SnapshotID string            `json:"snapshot_id"`
	CreatedAt  time.Time         `json:"created_at"`
	Mode       refstatus.Mode    `json:"mode"`
	Scope      refscope.Scope    `json:"scope"`
	Git        GitSnapshot       `json:"git"`
	RepoIndex  RepoIndexSnapshot `json:"repo_index"`
	Summary    Summary           `json:"summary"`
	Files      []FileSnapshot    `json:"files"`
	Symbols    []SymbolSnapshot  `json:"symbols"`
}

// Input is the shared build contract for a refactor snapshot.
type Input struct {
	SnapshotID   string
	CreatedAt    time.Time
	Scope        refscope.Scope
	Status       refstatus.Status
	IncludeTests bool
}

// Builder constructs deterministic refactor snapshot payloads.
type Builder struct {
	Registry *symindex.ExtractorRegistry
}

// GenerateID returns a refactor snapshot identifier for the provided timestamp.
func GenerateID(now time.Time) string {
	return fmt.Sprintf("refsnap-%d", now.UTC().UnixMilli())
}

// Build constructs a deterministic snapshot payload for the provided scope and status.
func (b Builder) Build(ctx context.Context, in Input) (Payload, error) {
	if strings.TrimSpace(in.SnapshotID) == "" {
		return Payload{}, fmt.Errorf("snapshot id is required")
	}
	if in.CreatedAt.IsZero() {
		return Payload{}, fmt.Errorf("created_at is required")
	}
	if strings.TrimSpace(in.Scope.Workspace) == "" || strings.TrimSpace(in.Scope.Absolute) == "" {
		return Payload{}, fmt.Errorf("resolved scope is required")
	}

	registry := b.Registry
	if registry == nil {
		registry = symindex.DefaultRegistry()
	}

	files, err := collectFiles(in.Scope, in.IncludeTests)
	if err != nil {
		return Payload{}, err
	}
	if len(files) == 0 {
		return Payload{}, &BuildError{
			Message: "no supported source files found for snapshot scope",
			Hint:    "Point the snapshot command at a source file or directory that matches the resolved language.",
		}
	}

	payload := Payload{
		SnapshotID: in.SnapshotID,
		CreatedAt:  in.CreatedAt.UTC(),
		Mode:       in.Status.Mode,
		Scope:      in.Scope,
		Git: GitSnapshot{
			HeadSHA: strings.TrimSpace(in.Status.Git.HeadSHA),
		},
		RepoIndex: RepoIndexSnapshot{
			HeadSHA:       strings.TrimSpace(in.Status.RepoIndex.Meta.HeadSHA),
			IndexedAt:     in.Status.RepoIndex.Meta.IndexedAt.UTC(),
			SchemaVersion: in.Status.RepoIndex.Meta.SchemaVersion,
		},
	}

	for _, absPath := range files {
		if err := ctx.Err(); err != nil {
			return Payload{}, err
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			return Payload{}, fmt.Errorf("read %s: %w", absPath, err)
		}
		if fsutil.IsBinaryContent(content) {
			continue
		}

		relPath := pathutil.RelTo(in.Scope.Workspace, absPath)
		lang := langutil.DetectAllowedWithHint(in.Scope.Language, absPath, langutil.CommonCodeLanguages)
		if lang == "" {
			continue
		}

		symbols := extractSymbols(ctx, registry, lang, relPath, content)
		payload.Files = append(payload.Files, FileSnapshot{
			Path:        relPath,
			Language:    lang,
			LineCount:   countLines(content),
			Hash:        symindex.ComputeDigest(content),
			SymbolCount: len(symbols),
			Package:     platformsymbol.DeriveSymbolPackage(relPath, lang),
		})
		payload.Summary.FileCount++
		payload.Summary.SymbolCount += len(symbols)
		payload.Summary.LineCount += countLines(content)

		for _, symbol := range symbols {
			payload.Symbols = append(payload.Symbols, SymbolSnapshot{
				Path:      relPath,
				SymbolID:  symbol.EffectiveID(),
				Name:      strings.TrimSpace(symbol.Name),
				Kind:      symbol.Kind,
				Hash:      strings.TrimSpace(symbol.BodyDigest),
				LineStart: symbol.StartLine,
				LineEnd:   symbol.EndLine,
				Signature: strings.TrimSpace(symbol.Signature),
			})
		}
	}

	sort.Slice(payload.Files, func(i, j int) bool {
		return payload.Files[i].Path < payload.Files[j].Path
	})
	sort.Slice(payload.Symbols, func(i, j int) bool {
		if payload.Symbols[i].Path != payload.Symbols[j].Path {
			return payload.Symbols[i].Path < payload.Symbols[j].Path
		}
		if payload.Symbols[i].LineStart != payload.Symbols[j].LineStart {
			return payload.Symbols[i].LineStart < payload.Symbols[j].LineStart
		}
		if payload.Symbols[i].Name != payload.Symbols[j].Name {
			return payload.Symbols[i].Name < payload.Symbols[j].Name
		}
		return payload.Symbols[i].SymbolID < payload.Symbols[j].SymbolID
	})

	return payload, nil
}

func collectFiles(scope refscope.Scope, includeTests bool) ([]string, error) {
	if !scope.IsDir {
		if langutil.DetectAllowedWithHint(scope.Language, scope.Absolute, langutil.CommonCodeLanguages) == "" {
			return nil, nil
		}
		return []string{scope.Absolute}, nil
	}

	files := make([]string, 0, 64)
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
		if !includeTests && fsutil.IsTestFile(d.Name()) {
			return nil
		}
		if langutil.DetectAllowedWithHint(scope.Language, path, langutil.CommonCodeLanguages) == "" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk scope: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func extractSymbols(ctx context.Context, registry *symindex.ExtractorRegistry, lang, relPath string, content []byte) []symindex.Symbol {
	if registry == nil {
		return nil
	}
	extractor := registry.Get(lang)
	if extractor == nil {
		return nil
	}
	symbols, err := extractor.Extract(ctx, relPath, content)
	if err != nil {
		return nil
	}
	return symbols
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	lines := 1
	for _, b := range content {
		if b == '\n' {
			lines++
		}
	}
	return lines
}
