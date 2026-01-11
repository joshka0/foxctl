package agentpolicy

import "slices"

// SkillInfo contains metadata about an allowed skill.
type SkillInfo struct {
	// Name is the skill name (e.g., "code/semantic_search").
	Name string

	// Description is a brief description of the skill.
	Description string

	// Example is an example invocation.
	Example string
}

// ProfileConfig contains the configuration for a profile.
type ProfileConfig struct {
	// Profile is the profile identifier.
	Profile Profile

	// Title is the human-readable title.
	Title string

	// Description is a brief description of the profile capabilities.
	Description string

	// AllowedSkills is the list of skills this profile can run.
	AllowedSkills []SkillInfo

	// Rules are guidelines for agents with this profile.
	Rules []string
}

// explorerSkills are the skills available to the explorer profile.
var explorerSkills = []SkillInfo{
	{
		Name:        "code/semantic_search",
		Description: "Search code using vector embeddings for semantic similarity",
		Example:     `agentctl run code/semantic_search --input '{"query":"auth middleware"}'`,
	},
	{
		Name:        "code/swe_grep",
		Description: "Smart code retrieval with context expansion",
		Example:     `agentctl run code/swe_grep --input '{"question":"Where is auth enforced?"}'`,
	},
	{
		Name:        "code/context_ripgrep",
		Description: "Search and return full function bodies containing matches",
		Example:     `agentctl run code/context_ripgrep --input '{"pattern":"handleAuth","path":"."}'`,
	},
	{
		Name:        "code/symbols",
		Description: "Extract functions, types, and variables from files",
		Example:     `agentctl run code/symbols --input '{"path":"internal/auth"}'`,
	},
	{
		Name:        "fs/read",
		Description: "Read file contents with line ranges",
		Example:     `agentctl run fs/read --input '{"path":"main.go"}'`,
	},
	{
		Name:        "fs/find",
		Description: "Find files by glob pattern",
		Example:     `agentctl run fs/find --input '{"pattern":"**/*.go"}'`,
	},
	{
		Name:        "text/ripgrep",
		Description: "Fast regex search across files",
		Example:     `agentctl run text/ripgrep --input '{"pattern":"TODO","path":"."}'`,
	},
	{
		Name:        "session/recall",
		Description: "Search past session context",
		Example:     `agentctl run session/recall --input '{"query":"auth implementation"}'`,
	},
	{
		Name:        "memory/query",
		Description: "Query stored memories and gotchas",
		Example:     `agentctl run memory/query --input '{"query":"database gotchas"}'`,
	},
}

// reviewerSkills are additional skills for the reviewer profile (on top of explorer).
var reviewerSkills = []SkillInfo{
	{
		Name:        "code/complexity",
		Description: "Analyze cyclomatic complexity and hotspots",
		Example:     `agentctl run code/complexity --input '{"path":"internal/auth"}'`,
	},
	{
		Name:        "code/security",
		Description: "Scan for security vulnerabilities",
		Example:     `agentctl run code/security --input '{"path":"."}'`,
	},
	{
		Name:        "code/imports",
		Description: "Analyze import dependencies",
		Example:     `agentctl run code/imports --input '{"path":"internal/auth"}'`,
	},
	{
		Name:        "lsp/gopls",
		Description: "Go LSP: definitions, references, hover",
		Example:     `agentctl run lsp/gopls --input '{"method":"definition","path":"main.go","line":10,"column":5}'`,
	},
	{
		Name:        "git/status",
		Description: "Show git working tree status",
		Example:     `agentctl run git/status --input '{}'`,
	},
}

// implementerSkills are additional skills for the implementer profile (on top of reviewer).
var implementerSkills = []SkillInfo{
	{
		Name:        "test/run",
		Description: "Run tests with coverage",
		Example:     `agentctl run test/run --input '{"path":"./..."}'`,
	},
	{
		Name:        "code/smart_write",
		Description: "Symbol-based editing with diff preview",
		Example:     `agentctl run code/smart_write --input '{"path":"main.go","symbol":"handleAuth","content":"..."}'`,
	},
}

// explorerRules are the rules for the explorer profile.
var explorerRules = []string{
	"Do not propose code changes - your role is investigation only",
	"Keep output focused: 3-8 key files, 3-10 key symbols",
	"Use agentctl skills for retrieval, not raw grep/cat",
	"Include file:line references in findings",
}

// reviewerRules are the rules for the reviewer profile.
var reviewerRules = []string{
	"Do not propose code changes - your role is analysis only",
	"Format output as: Summary, Critical Issues, Warnings, Suggestions",
	"Use complexity and security scans to support findings",
	"Cite specific lines and functions in issues",
}

// implementerRules are the rules for the implementer profile.
var implementerRules = []string{
	"Use agentctl run test/run for testing (not raw go test)",
	"Keep diffs minimal and focused on the task",
	"Verify changes compile before marking complete",
}

// DefaultProfiles returns the default profile configurations.
func DefaultProfiles() map[Profile]ProfileConfig {
	return map[Profile]ProfileConfig{
		ProfileExplorer: {
			Profile:       ProfileExplorer,
			Title:         "Explorer",
			Description:   "Read-only codebase explorer for investigation and reconnaissance",
			AllowedSkills: explorerSkills,
			Rules:         explorerRules,
		},
		ProfileReviewer: {
			Profile:       ProfileReviewer,
			Title:         "Reviewer",
			Description:   "Code reviewer and analyzer (read-only)",
			AllowedSkills: append(append([]SkillInfo{}, explorerSkills...), reviewerSkills...),
			Rules:         reviewerRules,
		},
		ProfileImplementer: {
			Profile:     ProfileImplementer,
			Title:       "Implementer",
			Description: "Code implementer with write access",
			AllowedSkills: append(
				append(append([]SkillInfo{}, explorerSkills...), reviewerSkills...),
				implementerSkills...,
			),
			Rules: implementerRules,
		},
		ProfileUnrestricted: {
			Profile:       ProfileUnrestricted,
			Title:         "Unrestricted",
			Description:   "Full access to all agentctl skills",
			AllowedSkills: nil, // nil means all skills allowed
			Rules:         nil,
		},
	}
}

// GetProfileConfig returns the configuration for a profile.
func GetProfileConfig(profile Profile) (ProfileConfig, bool) {
	profiles := DefaultProfiles()
	cfg, ok := profiles[profile]
	return cfg, ok
}

// GetAllowedSkillNames returns the list of skill names allowed for a profile.
// Returns nil for ProfileUnrestricted (meaning all skills allowed).
func GetAllowedSkillNames(profile Profile) []string {
	if profile == ProfileUnrestricted {
		return nil
	}

	cfg, ok := GetProfileConfig(profile)
	if !ok {
		return []string{} // Unknown profile = no skills allowed
	}

	names := make([]string, len(cfg.AllowedSkills))
	for i, skill := range cfg.AllowedSkills {
		names[i] = skill.Name
	}
	return names
}

// IsSkillAllowed returns true if the skill is allowed for the profile.
func IsSkillAllowed(profile Profile, skill string) bool {
	if profile == ProfileUnrestricted || profile == "" {
		return true
	}

	allowedSkills := GetAllowedSkillNames(profile)
	if allowedSkills == nil {
		return true // nil means all allowed
	}

	return slices.Contains(allowedSkills, skill)
}
