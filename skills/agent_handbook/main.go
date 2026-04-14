// Package main implements the agent/handbook skill.
// Generates capability briefings for agent profiles with allowed skills and rules.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/runtime/agentpolicy"
)

const command = "agent/handbook"

// input defines the parameters for agent/handbook operations.
type input struct {
	Profile string `json:"profile" validate:"required"`
}

// main is the skill entry point for agent/handbook.
func main() {
	skillmain.Main(command, run)
}

// run generates an agent handbook briefing for the specified profile.
//
// Index:
// - Purpose: Generate capability briefings for agent profiles with allowed skills and rules
// - Flow: validate profile → get profile config → generate briefing markdown → emit response
// - SideEffects: None (read-only operation)
// - FailureModes: invalid profile name, missing profile configuration
// - Observability: emits profile/title/description/briefing/allowed_skills/rules/summary
// - Related: agentpolicy.Profile, agentpolicy.GetProfileConfig, generateBriefing, skillout.Emit
// - Keywords: agent/handbook, profile, allowed_skills, briefing, rules, agentpolicy.Profile
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate and parse profile
	profile := agentpolicy.Profile(in.Profile)
	if !profile.IsValid() {
		return fmt.Errorf("invalid profile: %q (valid: explorer, reviewer, implementer, unrestricted)", in.Profile)
	}

	// Get profile configuration
	cfg, ok := agentpolicy.GetProfileConfig(profile)
	if !ok {
		return fmt.Errorf("profile configuration not found: %q", profile)
	}

	// Generate briefing markdown
	briefing := generateBriefing(cfg)

	// Extract skill names for JSON output
	allowedSkills := make([]string, len(cfg.AllowedSkills))
	for i, skill := range cfg.AllowedSkills {
		allowedSkills[i] = skill.Name
	}

	data := map[string]any{
		"profile":        string(profile),
		"title":          cfg.Title,
		"description":    cfg.Description,
		"briefing":       briefing,
		"allowed_skills": allowedSkills,
		"rules":          cfg.Rules,
		"summary":        fmt.Sprintf("Generated %s handbook with %d skills", cfg.Title, len(cfg.AllowedSkills)),
	}

	return skillout.Emit(rc, command, data)
}

// generateBriefing creates a markdown capability briefing for an agent profile.
func generateBriefing(cfg agentpolicy.ProfileConfig) string {
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "# Agent Capabilities: %s\n\n", cfg.Title)
	fmt.Fprintf(&b, "%s\n\n", cfg.Description)

	// Allowed skills section
	if len(cfg.AllowedSkills) > 0 {
		b.WriteString("## Allowed agentctl Skills\n\n")
		b.WriteString("Use these skills via bash:\n\n")

		for _, skill := range cfg.AllowedSkills {
			fmt.Fprintf(&b, "### %s\n\n", skill.Name)
			fmt.Fprintf(&b, "%s\n\n", skill.Description)
			fmt.Fprintf(&b, "**Example:**\n```bash\n%s\n```\n\n", skill.Example)
		}
	} else {
		b.WriteString("## Skills\n\n")
		b.WriteString("This profile has unrestricted access to all agentctl skills.\n\n")
	}

	// Rules section
	if len(cfg.Rules) > 0 {
		b.WriteString("## Rules\n\n")
		for i, rule := range cfg.Rules {
			fmt.Fprintf(&b, "%d. %s\n", i+1, rule)
		}
		b.WriteString("\n")
	}

	// Bash guard note
	if cfg.Profile != agentpolicy.ProfileUnrestricted {
		b.WriteString("## Important Notes\n\n")
		b.WriteString("- **Bash commands are restricted**: Only `agentctl run <skill>` commands are allowed\n")
		b.WriteString("- Any attempt to run other bash commands (e.g., `rm`, `cat`, `curl`) will be blocked\n")
		b.WriteString("- Use the skills above for all file reading, searching, and code analysis\n\n")
	}

	return b.String()
}
