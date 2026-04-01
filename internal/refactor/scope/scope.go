package scope

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	skillfs "github.com/jkatigb/agentctl/internal/adapters/skillslib/fsutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/langutil"
)

// Input defines the shared scope input for refactor commands.
type Input struct {
	Workspace    string
	Path         string
	Language     string
	IncludeTests bool
}

// Scope is the canonical resolved refactor scope.
type Scope struct {
	Workspace string   `json:"workspace"`
	RepoRoot  string   `json:"repo_root"`
	Path      string   `json:"path"`
	Absolute  string   `json:"absolute_path"`
	Mode      string   `json:"mode"`
	Language  string   `json:"language"`
	Detected  []string `json:"detected,omitempty"`
	IsDir     bool     `json:"is_dir"`
}

// ResolveError is a typed validation error for scope resolution.
type ResolveError struct {
	Message string
	Hint    string
}

func (e *ResolveError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Resolve resolves a refactor scope from workspace-relative input.
func Resolve(in Input) (Scope, error) {
	workspace := strings.TrimSpace(in.Workspace)
	if workspace == "" {
		workspace = "."
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return Scope{}, fmt.Errorf("resolve workspace: %w", err)
	}

	candidate := strings.TrimSpace(in.Path)
	if candidate == "" {
		candidate = "."
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absWorkspace, candidate)
	}
	absPath, err := filepath.Abs(candidate)
	if err != nil {
		return Scope{}, fmt.Errorf("resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return Scope{}, fmt.Errorf("stat path: %w", err)
	}
	return ResolveResolvedPath(absWorkspace, absPath, info, in.Language, in.IncludeTests)
}

// ResolveResolvedPath resolves a refactor scope from an already resolved workspace/path.
func ResolveResolvedPath(workspace, absPath string, info fs.FileInfo, language string, includeTests bool) (Scope, error) {
	workspace = strings.TrimSpace(workspace)
	absPath = strings.TrimSpace(absPath)
	if workspace == "" {
		return Scope{}, fmt.Errorf("workspace is required")
	}
	if absPath == "" {
		return Scope{}, fmt.Errorf("absolute path is required")
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return Scope{}, fmt.Errorf("resolve workspace: %w", err)
	}
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return Scope{}, fmt.Errorf("resolve path: %w", err)
	}
	if info == nil {
		info, err = os.Stat(absPath)
		if err != nil {
			return Scope{}, fmt.Errorf("stat path: %w", err)
		}
	}
	if rel, err := filepath.Rel(absWorkspace, absPath); err != nil || relPathEscapesWorkspace(rel) {
		return Scope{}, &ResolveError{
			Message: fmt.Sprintf("path %q escapes workspace %q", absPath, absWorkspace),
			Hint:    "Provide a file or directory inside the target workspace.",
		}
	}

	scope := Scope{
		Workspace: absWorkspace,
		RepoRoot:  absWorkspace,
		Path:      workspaceRelativePath(absWorkspace, absPath),
		Absolute:  absPath,
		IsDir:     info.IsDir(),
	}

	if strings.TrimSpace(language) == "" {
		language = "auto"
	}
	if strings.TrimSpace(language) != "" && language != "auto" {
		scope.Mode = "explicit"
		scope.Language = language
		scope.Detected = []string{language}
		return scope, nil
	}

	if !info.IsDir() {
		lang := langutil.DetectAllowed(absPath, langutil.CommonCodeLanguages)
		if lang == "" {
			return Scope{}, &ResolveError{
				Message: "unsupported file type",
				Hint:    "Pass a supported source file or specify --language for a supported code family.",
			}
		}
		scope.Mode = "auto_file"
		scope.Language = lang
		scope.Detected = []string{lang}
		return scope, nil
	}

	detected, err := discoverLanguages(absPath, includeTests)
	if err != nil {
		return Scope{}, err
	}
	switch len(detected) {
	case 0:
		return Scope{}, &ResolveError{
			Message: "no supported source files found",
			Hint:    "Point the refactor command at a source directory or set --language to the language you want to analyze.",
		}
	case 1:
		scope.Mode = "auto_directory_single_language"
		scope.Language = detected[0]
		scope.Detected = detected
		return scope, nil
	default:
		return Scope{}, &ResolveError{
			Message: fmt.Sprintf("multiple languages detected in %s: %s", absPath, strings.Join(detected, ", ")),
			Hint:    "Refactor commands are intentionally single-language per run. Re-run with --language set to one language.",
		}
	}
}

func discoverLanguages(dir string, includeTests bool) ([]string, error) {
	found := make(map[string]struct{})
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if skillfs.ShouldSkipHiddenOrCommon(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !includeTests && skillfs.IsTestFile(d.Name()) {
			return nil
		}
		lang := langutil.DetectAllowed(path, langutil.CommonCodeLanguages)
		if lang != "" {
			found[lang] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}
	out := make([]string, 0, len(found))
	for lang := range found {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out, nil
}

func workspaceRelativePath(workspace, absPath string) string {
	rel, err := filepath.Rel(workspace, absPath)
	if err != nil || rel == "" {
		return filepath.Clean(absPath)
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
