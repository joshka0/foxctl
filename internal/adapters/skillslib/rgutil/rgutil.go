package rgutil

import (
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/tooling/tools/ripgrep"
)

const DefaultMaxMatches = 10000

// DefaultExcludeGlobs are the default excludes for ripgrep-backed skills.
var DefaultExcludeGlobs = []string{
	".git",
	"node_modules",
	"vendor",
	"__pycache__",
	".godot",
}

// SearchInput captures common ripgrep options for skills.
type SearchInput struct {
	Pattern         string
	CaseInsensitive bool
	FixedStrings    bool
	Glob            []string
	GlobNot         []string
	MaxMatches      int
	ContextLines    int
	Hidden          bool
}

// Normalize applies default values for ripgrep searches.
func Normalize(in SearchInput) SearchInput {
	if in.MaxMatches <= 0 {
		in.MaxMatches = DefaultMaxMatches
	}
	if in.ContextLines < 0 {
		in.ContextLines = 0
	}
	return in
}

// BuildSearchOpts builds ripgrep options with default excludes when none are provided.
func BuildSearchOpts(in SearchInput, workspace, searchPath string, defaultExclude []string) ripgrep.SearchOpts {
	excludeGlobs := in.GlobNot
	if len(excludeGlobs) == 0 {
		if len(defaultExclude) > 0 {
			excludeGlobs = defaultExclude
		} else {
			excludeGlobs = DefaultExcludeGlobs
		}
	}

	return ripgrep.SearchOpts{
		Pattern:           in.Pattern,
		Path:              searchPath,
		WorkingDir:        workspace,
		CaseInsensitive:   in.CaseInsensitive,
		FixedStrings:      in.FixedStrings,
		ContextLines:      in.ContextLines,
		MaxMatches:        in.MaxMatches,
		MaxMatchesPerFile: in.MaxMatches,
		Hidden:            in.Hidden,
		IncludeGlobs:      in.Glob,
		ExcludeGlobs:      excludeGlobs,
	}
}

// RequireRipgrep ensures the ripgrep binary is available and returns a skill-friendly error when missing.
func RequireRipgrep() error {
	if _, err := executil.RequireTool("rg", "install ripgrep"); err != nil {
		return skillerr.Runtime(
			"ripgrep (rg) not found in PATH",
			skillerr.WithCause(err),
			skillerr.WithHint("Install ripgrep and ensure `rg` is available in PATH."),
		)
	}
	return nil
}
