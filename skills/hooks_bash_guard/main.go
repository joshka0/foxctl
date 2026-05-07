// Package main implements the hooks/bash_guard skill.
// This skill enforces agent profile restrictions on bash commands.
//
// For restricted profiles (explorer, reviewer, implementer):
//   - Only "foxctl run <skill>" commands are allowed where <skill> is in the profile's allowlist
//   - All other bash commands are blocked
//
// Profile is determined from:
//  1. FOXCTL_AGENT_PROFILE environment variable
//  2. Hook config's "profile" field
//  3. Default: unrestricted (all commands allowed)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/hookutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/runtime/agentpolicy"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
)

const command = "hooks/bash_guard"

// main is the skill entry point for hooks/bash_guard.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates bash command authorization with profile-based restrictions and sed rewriting.
//
// Index:
//
//	Purpose: Enforce agent profile restrictions on bash commands with automatic sed-to-context_grep rewriting
//	Keywords: hooks/bash_guard, bash_authorization, profile_restrictions, command_rewriting, sed_to_grep
//	Related: extractCommand, resolveProfile, rewriteSedToContextGrep, emitApprove, emitBlock
//	Flow: extract command → resolve profile → attempt sed rewrite → authorize command → emit decision
//	Resources: agentpolicy.Profile, agentpolicy.AuthorizeBash
//	Events: bash-authorized, bash-blocked, sed-rewritten
//	OutputFields: decision, profile, parsed_skill, rewritten_from
//
// [[invariant:profile-restricted-bash]]
// [[risk:unauthorized-command-execution]]
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

	// Rewrite sed range reads to code/context_grep when possible.
	if rewritten, sedInfo, ok := rewriteSedToContextGrep(bashCommand); ok {
		result := agentpolicy.AuthorizeBash(profile, rewritten)
		if result.Decision == agentpolicy.DecisionBlock {
			return emitBlock(rc, result, profile)
		}
		meta := map[string]any{
			"profile":        string(profile),
			"parsed_skill":   result.ParsedSkill,
			"rewritten_from": bashCommand,
			"file_path":      sedInfo.FilePath,
			"line_start":     sedInfo.LineStart,
			"line_end":       sedInfo.LineEnd,
		}
		updatedInput, err := buildUpdatedToolInput(rewritten)
		if err != nil {
			return emitApprove(rc, fmt.Sprintf("failed to build updated tool input: %v", err), meta)
		}
		return emitApproveWithUpdate(rc, "rewrote sed range to code/context_grep", meta, updatedInput)
	}

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
//
// Index:
//
//	Purpose: Determine agent profile from environment or config
//	Keywords: profile_resolution, environment_variable, hook_config
//	Related: run
//	Flow: check environment variable → check hook config → default to unrestricted
//	Resources: FOXCTL_AGENT_PROFILE env var, hook config
//	Events: profile-resolved
//	OutputFields: profile
//
// [[domain:agent-policy]]
func resolveProfile(in hooks.Input) agentpolicy.Profile {
	// Check environment variable first
	if envProfile := os.Getenv("FOXCTL_AGENT_PROFILE"); envProfile != "" {
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

var (
	sedRangeRe      = regexp.MustCompile(`(?i)sed\s+-n\s+['"]?\s*(\d+)\s*,\s*(\d+)\s*p\s*['"]?`)
	sedRangeTokenRe = regexp.MustCompile(`(?i)^\s*\d+\s*,\s*\d+\s*p\s*$`)
)

type sedRange struct {
	FilePath  string
	LineStart int
	LineEnd   int
}

// rewriteSedToContextGrep attempts to rewrite sed range commands to code/context_grep.
//
// Index:
//
//	Purpose: Rewrite sed range commands to code/context_grep
//	Keywords: sed_rewriting, context_grep
//	Related: run
//	Flow: split command → parse sed range → extract file path → build context grep command
//	Resources: regex patterns for sed parsing
//	Events: sed-rewritten
//	OutputFields: rewritten, file_path, line_start, line_end
//
// [[domain:command-rewriting]]
func rewriteSedToContextGrep(command string) (string, sedRange, bool) {
	segments := strings.Split(command, "|")
	for idx, segment := range segments {
		segment = strings.TrimSpace(segment)
		lineStart, lineEnd, ok := parseSedRange(segment)
		if !ok {
			continue
		}

		filePath := extractSedFile(segment)
		if filePath == "" && idx > 0 {
			filePath = extractCatFile(strings.TrimSpace(segments[idx-1]))
		}
		if filePath == "" || filePath == "-" {
			return "", sedRange{}, false
		}

		// Validate file path before building command
		if !isValidFilePath(filePath) {
			return "", sedRange{}, false
		}

		info := sedRange{
			FilePath:  filePath,
			LineStart: lineStart,
			LineEnd:   lineEnd,
		}
		rewritten, err := buildContextGrepCommand(info)
		if err != nil {
			return "", sedRange{}, false
		}
		return rewritten, info, true
	}
	return "", sedRange{}, false
}

// parseSedRange extracts line range from sed command segment.
func parseSedRange(segment string) (int, int, bool) {
	matches := sedRangeRe.FindStringSubmatch(segment)
	if len(matches) != 3 {
		return 0, 0, false
	}
	lineStart, err := strconv.Atoi(matches[1])
	if err != nil || lineStart <= 0 {
		return 0, 0, false
	}
	lineEnd, err := strconv.Atoi(matches[2])
	if err != nil || lineEnd < lineStart {
		return 0, 0, false
	}
	return lineStart, lineEnd, true
}

// extractSedFile extracts file path from sed command segment.
func extractSedFile(segment string) string {
	tokens := tokenizeCommand(segment)
	for i, token := range tokens {
		if token == "<" && i+1 < len(tokens) {
			return trimQuotes(tokens[i+1])
		}
	}

	rangeIndex := -1
	for i, token := range tokens {
		if sedRangeTokenRe.MatchString(token) {
			rangeIndex = i
			break
		}
	}
	if rangeIndex == -1 {
		return ""
	}
	for i := rangeIndex + 1; i < len(tokens); i++ {
		token := tokens[i]
		if token == "" {
			continue
		}
		if token == "|" {
			break
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		return trimQuotes(token)
	}
	return ""
}

// extractCatFile extracts file path from cat command segment.
func extractCatFile(segment string) string {
	tokens := tokenizeCommand(segment)
	for i, token := range tokens {
		if token != "cat" {
			continue
		}
		for j := i + 1; j < len(tokens); j++ {
			next := tokens[j]
			if next == "" {
				continue
			}
			if next == "<" && j+1 < len(tokens) {
				return trimQuotes(tokens[j+1])
			}
			if strings.HasPrefix(next, "-") {
				continue
			}
			return trimQuotes(next)
		}
	}
	return ""
}

// buildContextGrepCommand builds a code/context_grep command from sed range info.
func buildContextGrepCommand(info sedRange) (string, error) {
	input := map[string]any{
		"mode":       "line",
		"file_path":  info.FilePath,
		"line_start": info.LineStart,
		"line_end":   info.LineEnd,
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("foxctl run code/context_grep --input %s", shellQuote(string(payload))), nil
}

// buildUpdatedToolInput creates updated tool input for rewritten commands.
func buildUpdatedToolInput(command string) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		return nil, err
	}
	return payload, nil
}

// shellQuote properly quotes a string for shell usage.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.Contains(value, "'") {
		return "'" + value + "'"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// isValidFilePath validates that a file path is safe to use in a command.
func isValidFilePath(path string) bool {
	// Reject null bytes
	if strings.ContainsRune(path, 0) {
		return false
	}
	// Reject newlines (could break command structure)
	if strings.ContainsAny(path, "\n\r") {
		return false
	}
	// Reject backticks (command substitution)
	if strings.Contains(path, "`") {
		return false
	}
	// Reject $( (command substitution)
	if strings.Contains(path, "$(") {
		return false
	}
	return true
}

// tokenizeCommand splits a command string into tokens respecting quotes.
func tokenizeCommand(command string) []string {
	var tokens []string
	var buf strings.Builder
	var quote rune
	escaped := false

	for _, r := range command {
		if escaped {
			buf.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote == 0 {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			buf.WriteRune(r)
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			if buf.Len() > 0 {
				tokens = append(tokens, buf.String())
				buf.Reset()
			}
		default:
			buf.WriteRune(r)
		}
	}
	if buf.Len() > 0 {
		tokens = append(tokens, buf.String())
	}
	return tokens
}

// trimQuotes removes surrounding quotes from a string.
func trimQuotes(value string) string {
	if len(value) < 2 {
		return value
	}
	if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
		return value[1 : len(value)-1]
	}
	return value
}

// emitApprove emits an approve hook output.
func emitApprove(rc *skillmain.RunContext, reason string, meta map[string]any) error {
	output := hooks.NewApprove(reason, meta)
	return emitOutput(rc, output)
}

// emitApproveWithUpdate emits an approve hook output with updated tool input.
func emitApproveWithUpdate(rc *skillmain.RunContext, reason string, meta map[string]any, updatedInput json.RawMessage) error {
	output := hooks.NewApprove(reason, meta)
	output.UpdatedToolInput = updatedInput
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
		contextBuilder.WriteString("Only `foxctl run <skill>` commands are allowed for restricted profiles.\n\n")
	}

	if len(allowedSkills) > 0 {
		contextBuilder.WriteString("### Allowed Skills\n\n")
		for _, skill := range allowedSkills {
			fmt.Fprintf(&contextBuilder, "- `%s`\n", skill)
		}
		contextBuilder.WriteString("\n")

		contextBuilder.WriteString("### Example Usage\n\n")
		contextBuilder.WriteString("```bash\n")
		fmt.Fprintf(&contextBuilder, "foxctl run %s --input '{\"query\":\"...\"}'\n", allowedSkills[0])
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
	return hookutil.EmitOutput(rc, command, output, nil)
}
