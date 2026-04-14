package agentpolicy

import (
	"fmt"
	"strings"
	"unicode"
)

// AuthorizeBash checks if a bash command is allowed for the given profile.
//
// For restricted profiles (explorer, reviewer, implementer):
//   - Only "foxctl run <skill>" commands are allowed where <skill> is in the profile's allowlist
//   - All other bash commands are blocked
//
// For unrestricted profiles or empty profiles:
//   - All commands are allowed
func AuthorizeBash(profile Profile, command string) AuthorizationResult {
	// Unrestricted or empty profile = all commands allowed
	if !profile.IsRestricted() {
		return AuthorizationResult{
			Decision: DecisionAllow,
			Reason:   "unrestricted profile",
			Profile:  profile,
		}
	}

	// Parse the command
	parsed := ParseCommand(command)

	// If it's an foxctl run command, check if the skill is allowed
	if parsed.IsAgentctlRun {
		if IsSkillAllowed(profile, parsed.Skill) {
			return AuthorizationResult{
				Decision:    DecisionAllow,
				Reason:      fmt.Sprintf("skill %q is allowed for profile %q", parsed.Skill, profile),
				ParsedSkill: parsed.Skill,
				Profile:     profile,
			}
		}
		return AuthorizationResult{
			Decision:    DecisionBlock,
			Reason:      fmt.Sprintf("skill %q is not allowed for profile %q", parsed.Skill, profile),
			ParsedSkill: parsed.Skill,
			Profile:     profile,
		}
	}

	// Not an foxctl run command - block for restricted profiles
	return AuthorizationResult{
		Decision: DecisionBlock,
		Reason:   fmt.Sprintf("only foxctl run commands are allowed for profile %q", profile),
		Profile:  profile,
	}
}

// ParseCommand parses a bash command to determine if it's an foxctl run command
// and extracts the skill name if so.
//
// Handles:
//   - Simple: "foxctl run code/symbols --input '{}'"
//   - With env vars: "AGENTCTL_WORKSPACE=/foo foxctl run code/symbols"
//   - With path: "/usr/local/bin/foxctl run code/symbols"
//   - Quoted args: foxctl run 'code/symbols' --input '{}'
func ParseCommand(command string) ParsedCommand {
	result := ParsedCommand{
		RawCommand: command,
		EnvVars:    make(map[string]string),
	}

	// Tokenize the command
	tokens := tokenize(command)
	if len(tokens) == 0 {
		return result
	}

	// Find the command start (skip env var assignments)
	cmdStart := 0
	for i, token := range tokens {
		if isEnvAssignment(token) {
			key, value := parseEnvAssignment(token)
			result.EnvVars[key] = value
			continue
		}
		cmdStart = i
		break
	}

	remaining := tokens[cmdStart:]
	if len(remaining) < 2 {
		return result
	}

	// Check if it's foxctl (possibly with path)
	cmd := remaining[0]
	if !isAgentctlCommand(cmd) {
		return result
	}

	// Check if the next token is "run"
	if remaining[1] != "run" {
		return result
	}

	// Need at least one more token for the skill name
	if len(remaining) < 3 {
		return result
	}

	// Extract the skill name (third token)
	skill := remaining[2]

	// Remove surrounding quotes if present
	skill = trimQuotes(skill)

	// Validate skill format (should contain "/" or be a valid skill name)
	if skill == "" || strings.HasPrefix(skill, "-") {
		return result
	}

	result.IsAgentctlRun = true
	result.Skill = skill
	return result
}

// tokenize splits a command into tokens, handling quotes.
func tokenize(command string) []string {
	var tokens []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	escapeNext := false

	for _, r := range command {
		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}

		if r == '\\' && !inSingleQuote {
			escapeNext = true
			continue
		}

		if r == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			continue
		}

		if r == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			continue
		}

		if unicode.IsSpace(r) && !inSingleQuote && !inDoubleQuote {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// isEnvAssignment checks if a token is an environment variable assignment.
func isEnvAssignment(token string) bool {
	// Must contain = and start with a letter or underscore
	idx := strings.Index(token, "=")
	if idx <= 0 {
		return false
	}

	name := token[:idx]
	if len(name) == 0 {
		return false
	}

	first := rune(name[0])
	if !unicode.IsLetter(first) && first != '_' {
		return false
	}

	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}

	return true
}

// parseEnvAssignment extracts key and value from an env assignment.
func parseEnvAssignment(token string) (string, string) {
	idx := strings.Index(token, "=")
	if idx <= 0 {
		return "", ""
	}
	return token[:idx], token[idx+1:]
}

// isAgentctlCommand checks if a token is the foxctl command.
func isAgentctlCommand(token string) bool {
	// Direct match
	if token == "foxctl" {
		return true
	}

	// Check if it ends with /foxctl (path)
	if strings.HasSuffix(token, "/foxctl") {
		return true
	}

	return false
}

// trimQuotes removes surrounding single or double quotes from a string.
func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
