// Package main implements the hooks/bash_guard skill.
// This skill enforces agent profile restrictions on bash commands.
//
// For restricted profiles (explorer, reviewer, implementer):
//   - Only "agentctl run <skill>" commands are allowed where <skill> is in the profile's allowlist
//   - All other bash commands are blocked
//
// Profile is determined from:
//  1. AGENTCTL_AGENT_PROFILE environment variable
//  2. Hook config's "profile" field
//  3. Default: unrestricted (all commands allowed)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/agentpolicy"
	"github.com/jkatigb/agentctl/internal/hooks"
)

const command = "hooks/bash_guard"

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	// Only process Bash tool calls
	if in.ToolName != "Bash" {
		return emitApprove(rc, "not a Bash tool call", nil)
	}

	// Extract the bash command from tool input
	bashCommand, err := extractCommand(in.ToolInput)
	if err != nil {
		return emitApprove(rc, fmt.Sprintf("failed to extract command: %v", err), nil)
	}

	if bashCommand == "" {
		return emitApprove(rc, "empty command", nil)
	}

	// Resolve profile
	profile := resolveProfile(in)

	// Authorize the command
	result := agentpolicy.AuthorizeBash(profile, bashCommand)

	// Return hook decision
	if result.Decision == agentpolicy.DecisionBlock {
		return emitBlock(rc, result, profile)
	}

	return emitApprove(rc, result.Reason, map[string]any{
		"profile":      string(profile),
		"parsed_skill": result.ParsedSkill,
	})
}

// extractCommand extracts the "command" field from the tool input.
func extractCommand(toolInput json.RawMessage) (string, error) {
	if len(toolInput) == 0 {
		return "", nil
	}

	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return "", err
	}
	return input.Command, nil
}

// resolveProfile determines the agent profile from environment or config.
// Priority:
//  1. AGENTCTL_AGENT_PROFILE environment variable
//  2. Hook config's "profile" field
//  3. Default: unrestricted
func resolveProfile(in hooks.Input) agentpolicy.Profile {
	// Check environment variable first
	if envProfile := os.Getenv("AGENTCTL_AGENT_PROFILE"); envProfile != "" {
		profile := agentpolicy.Profile(envProfile)
		if profile.IsValid() {
			return profile
		}
	}

	// Check hook config
	if in.HookConfig != nil {
		if profileStr, ok := in.HookConfig["profile"].(string); ok && profileStr != "" {
			profile := agentpolicy.Profile(profileStr)
			if profile.IsValid() {
				return profile
			}
		}
	}

	// Default to unrestricted
	return agentpolicy.ProfileUnrestricted
}

// emitApprove emits an approve hook output.
func emitApprove(rc *skillmain.RunContext, reason string, meta map[string]any) error {
	output := hooks.NewApprove(reason, meta)
	return emitOutput(rc, output)
}

// emitBlock emits a block hook output with helpful context.
func emitBlock(rc *skillmain.RunContext, result agentpolicy.AuthorizationResult, profile agentpolicy.Profile) error {
	// Build context with allowed skills for this profile
	allowedSkills := agentpolicy.GetAllowedSkillNames(profile)
	var contextBuilder strings.Builder

	contextBuilder.WriteString("## Bash Command Blocked\n\n")
	fmt.Fprintf(&contextBuilder, "**Profile:** %s\n", profile)
	fmt.Fprintf(&contextBuilder, "**Reason:** %s\n\n", result.Reason)

	if result.ParsedSkill != "" {
		fmt.Fprintf(&contextBuilder, "Skill `%s` is not in the allowlist for profile `%s`.\n\n", result.ParsedSkill, profile)
	} else {
		contextBuilder.WriteString("Only `agentctl run <skill>` commands are allowed for restricted profiles.\n\n")
	}

	if len(allowedSkills) > 0 {
		contextBuilder.WriteString("### Allowed Skills\n\n")
		for _, skill := range allowedSkills {
			fmt.Fprintf(&contextBuilder, "- `%s`\n", skill)
		}
		contextBuilder.WriteString("\n")

		contextBuilder.WriteString("### Example Usage\n\n")
		contextBuilder.WriteString("```bash\n")
		fmt.Fprintf(&contextBuilder, "agentctl run %s --input '{\"query\":\"...\"}'\n", allowedSkills[0])
		contextBuilder.WriteString("```\n")
	}

	output := hooks.NewBlockWithContext(result.Reason, contextBuilder.String())
	output.Meta = map[string]any{
		"profile":      string(profile),
		"parsed_skill": result.ParsedSkill,
	}
	return emitOutput(rc, output)
}

// emitOutput writes the hook output to the skill output.
func emitOutput(rc *skillmain.RunContext, output hooks.Output) error {
	data := map[string]any{
		"hook_output": output,
	}
	return skillout.Emit(rc, command, data)
}
