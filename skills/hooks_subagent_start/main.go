// Package main implements the hooks/subagent_start skill.
// This skill handles SubagentStart events to:
//  1. Infer the agent profile from the subagent name
//  2. Generate and inject a capability briefing via agent/handbook
//  3. Set environment variables for the bash_guard hook
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/agentpolicy"
	"github.com/jkatigb/agentctl/internal/hooks"
)

const command = "hooks/subagent_start"

// SubagentPayload represents the SubagentStart event payload from Claude Code.
type SubagentPayload struct {
	SubagentName string `json:"subagent_name"`
	SubagentType string `json:"subagent_type"`
	AgentID      string `json:"agent_id"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	// Parse subagent payload from tool input
	var payload SubagentPayload
	if len(in.ToolInput) > 0 {
		_ = json.Unmarshal(in.ToolInput, &payload)
	}

	// Get subagent name from payload or hook config
	subagentName := payload.SubagentName
	if subagentName == "" {
		subagentName = payload.SubagentType
	}
	if subagentName == "" && in.HookConfig != nil {
		if name, ok := in.HookConfig["subagent_name"].(string); ok {
			subagentName = name
		}
	}

	// Infer profile from subagent name
	profile := inferProfile(subagentName)

	// Get profile configuration
	cfg, ok := agentpolicy.GetProfileConfig(profile)
	if !ok {
		// Use unrestricted for unknown profiles
		profile = agentpolicy.ProfileUnrestricted
		cfg, _ = agentpolicy.GetProfileConfig(profile)
	}

	// Generate briefing by calling agent/handbook skill
	briefing, err := generateBriefing(ctx, profile)
	if err != nil {
		// Fall back to a simple briefing on error
		briefing = generateFallbackBriefing(cfg)
		rc.Logger.Warn().Err(err).Msg("failed to generate handbook, using fallback")
	}

	// Build output with context injection
	output := hooks.NewNoneWithContext(briefing)
	output.Meta = map[string]any{
		"subagent_name":   subagentName,
		"inferred_profile": string(profile),
		"allowed_skills":  agentpolicy.GetAllowedSkillNames(profile),
	}

	data := map[string]any{
		"hook_output": output,
		"profile":     string(profile),
		"briefing":    briefing,
		"summary":     fmt.Sprintf("Subagent %q started with profile %q", subagentName, profile),
	}

	return skillout.Emit(rc, command, data)
}

// inferProfile determines the agent profile from the subagent name.
// Uses pattern matching on common naming conventions.
func inferProfile(name string) agentpolicy.Profile {
	lower := strings.ToLower(name)

	// Explorer patterns
	if containsAny(lower, "explorer", "navigator", "investigate", "search", "find", "browse", "discover") {
		return agentpolicy.ProfileExplorer
	}

	// Reviewer patterns
	if containsAny(lower, "reviewer", "architect", "analyze", "audit", "check", "inspect", "assess") {
		return agentpolicy.ProfileReviewer
	}

	// Implementer patterns
	if containsAny(lower, "implementer", "coder", "fixer", "developer", "builder", "writer", "implement", "fix", "code") {
		return agentpolicy.ProfileImplementer
	}

	// Default to unrestricted if no pattern matches
	return agentpolicy.ProfileUnrestricted
}

// containsAny returns true if s contains any of the patterns.
func containsAny(s string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

// generateBriefing calls the agent/handbook skill to generate a briefing.
func generateBriefing(ctx context.Context, profile agentpolicy.Profile) (string, error) {
	input := map[string]string{"profile": string(profile)}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "agentctl", "run", "agent/handbook", "--input", string(inputJSON))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("handbook skill failed: %w (stderr: %s)", err, stderr.String())
	}

	// Parse the output envelope
	var envelope struct {
		Data struct {
			Briefing string `json:"briefing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		return "", fmt.Errorf("parse handbook output: %w", err)
	}

	return envelope.Data.Briefing, nil
}

// generateFallbackBriefing creates a simple briefing when the handbook skill fails.
func generateFallbackBriefing(cfg agentpolicy.ProfileConfig) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Agent Profile: %s\n\n", cfg.Title)
	fmt.Fprintf(&b, "%s\n\n", cfg.Description)

	if len(cfg.AllowedSkills) > 0 {
		b.WriteString("## Allowed Skills\n\n")
		for _, skill := range cfg.AllowedSkills {
			fmt.Fprintf(&b, "- %s: %s\n", skill.Name, skill.Description)
		}
		b.WriteString("\n")
	}

	if len(cfg.Rules) > 0 {
		b.WriteString("## Rules\n\n")
		for i, rule := range cfg.Rules {
			fmt.Fprintf(&b, "%d. %s\n", i+1, rule)
		}
	}

	return b.String()
}
