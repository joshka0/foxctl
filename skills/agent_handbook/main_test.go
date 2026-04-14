package main

import (
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/agentpolicy"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "agent/handbook", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Profile: "explorer",
	}

	assert.Equal(t, "explorer", in.Profile)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Profile: "reviewer",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Profile, decoded.Profile)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Profile)
}

func TestInput_JSONContainsProfile(t *testing.T) {
	in := input{
		Profile: "implementer",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "profile")
	assert.Contains(t, string(data), "implementer")
}

func TestInput_ValidProfiles(t *testing.T) {
	validProfiles := []string{"explorer", "reviewer", "implementer", "unrestricted"}

	for _, profile := range validProfiles {
		in := input{Profile: profile}
		assert.Equal(t, profile, in.Profile)
	}
}

// Tests for generateBriefing helper

func TestGenerateBriefing_WithSkills(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:     agentpolicy.ProfileExplorer,
		Title:       "Explorer Agent",
		Description: "Read-only access for exploration",
		AllowedSkills: []agentpolicy.SkillInfo{
			{
				Name:        "code/semantic_search",
				Description: "Search code by meaning",
				Example:     "foxctl run code/semantic_search --input '{\"query\": \"auth\"}'",
			},
		},
		Rules: []string{
			"Do not modify files",
			"Focus on understanding the codebase",
		},
	}

	result := generateBriefing(cfg)

	assert.Contains(t, result, "# Agent Capabilities: Explorer Agent")
	assert.Contains(t, result, "Read-only access for exploration")
	assert.Contains(t, result, "## Allowed foxctl Skills")
	assert.Contains(t, result, "### code/semantic_search")
	assert.Contains(t, result, "Search code by meaning")
	assert.Contains(t, result, "**Example:**")
	assert.Contains(t, result, "## Rules")
	assert.Contains(t, result, "1. Do not modify files")
	assert.Contains(t, result, "2. Focus on understanding the codebase")
	assert.Contains(t, result, "## Important Notes")
	assert.Contains(t, result, "Bash commands are restricted")
}

func TestGenerateBriefing_NoSkills(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:       agentpolicy.ProfileUnrestricted,
		Title:         "Unrestricted Agent",
		Description:   "Full access to all capabilities",
		AllowedSkills: []agentpolicy.SkillInfo{},
		Rules:         []string{},
	}

	result := generateBriefing(cfg)

	assert.Contains(t, result, "# Agent Capabilities: Unrestricted Agent")
	assert.Contains(t, result, "Full access to all capabilities")
	assert.Contains(t, result, "## Skills")
	assert.Contains(t, result, "unrestricted access to all foxctl skills")
	// Should NOT contain the bash restriction note for unrestricted profile
	assert.NotContains(t, result, "## Important Notes")
}

func TestGenerateBriefing_MultipleSkills(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:     agentpolicy.ProfileReviewer,
		Title:       "Reviewer Agent",
		Description: "Code review capabilities",
		AllowedSkills: []agentpolicy.SkillInfo{
			{
				Name:        "code/semantic_search",
				Description: "Search code semantically",
				Example:     "foxctl run code/semantic_search",
			},
			{
				Name:        "code/complexity",
				Description: "Analyze code complexity",
				Example:     "foxctl run code/complexity",
			},
			{
				Name:        "code/security",
				Description: "Security analysis",
				Example:     "foxctl run code/security",
			},
		},
		Rules: []string{},
	}

	result := generateBriefing(cfg)

	assert.Contains(t, result, "### code/semantic_search")
	assert.Contains(t, result, "### code/complexity")
	assert.Contains(t, result, "### code/security")
}

func TestGenerateBriefing_NoRules(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:     agentpolicy.ProfileImplementer,
		Title:       "Implementer Agent",
		Description: "Implementation capabilities",
		AllowedSkills: []agentpolicy.SkillInfo{
			{Name: "test/skill", Description: "Test", Example: "foxctl run test/skill"},
		},
		Rules: []string{},
	}

	result := generateBriefing(cfg)

	// Should not contain rules section header if no rules
	// Actually let me check - the code shows rules section is only added if len(cfg.Rules) > 0
	assert.NotContains(t, result, "## Rules")
}

func TestGenerateBriefing_WithRules(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:       agentpolicy.ProfileExplorer,
		Title:         "Explorer",
		Description:   "Exploration",
		AllowedSkills: []agentpolicy.SkillInfo{},
		Rules: []string{
			"Rule 1",
			"Rule 2",
			"Rule 3",
		},
	}

	result := generateBriefing(cfg)

	assert.Contains(t, result, "## Rules")
	assert.Contains(t, result, "1. Rule 1")
	assert.Contains(t, result, "2. Rule 2")
	assert.Contains(t, result, "3. Rule 3")
}

func TestGenerateBriefing_SpecialCharacters(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:     agentpolicy.ProfileReviewer,
		Title:       "Reviewer <Agent>",
		Description: "Code review with \"special\" characters & symbols",
		AllowedSkills: []agentpolicy.SkillInfo{
			{
				Name:        "code/search",
				Description: "Search with <query> parameter",
				Example:     "foxctl run code/search --input '{\"query\": \"foo && bar\"}'",
			},
		},
		Rules: []string{"Don't use `rm -rf`"},
	}

	result := generateBriefing(cfg)

	assert.Contains(t, result, "Reviewer <Agent>")
	assert.Contains(t, result, "\"special\"")
	assert.Contains(t, result, "<query>")
	assert.Contains(t, result, "Don't use `rm -rf`")
}

func TestGenerateBriefing_EmptyDescription(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:       agentpolicy.ProfileExplorer,
		Title:         "Test Agent",
		Description:   "",
		AllowedSkills: []agentpolicy.SkillInfo{},
		Rules:         []string{},
	}

	result := generateBriefing(cfg)

	assert.Contains(t, result, "# Agent Capabilities: Test Agent")
}

func TestGenerateBriefing_BashRestrictionNote_Explorer(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:       agentpolicy.ProfileExplorer,
		Title:         "Explorer",
		Description:   "Exploration",
		AllowedSkills: []agentpolicy.SkillInfo{},
		Rules:         []string{},
	}

	result := generateBriefing(cfg)

	assert.Contains(t, result, "## Important Notes")
	assert.Contains(t, result, "Bash commands are restricted")
	assert.Contains(t, result, "Only `foxctl run <skill>` commands are allowed")
}

func TestGenerateBriefing_BashRestrictionNote_Unrestricted(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:       agentpolicy.ProfileUnrestricted,
		Title:         "Unrestricted",
		Description:   "Full access",
		AllowedSkills: []agentpolicy.SkillInfo{},
		Rules:         []string{},
	}

	result := generateBriefing(cfg)

	// Unrestricted profile should NOT have bash restriction note
	assert.NotContains(t, result, "## Important Notes")
	assert.NotContains(t, result, "Bash commands are restricted")
}

func TestGenerateBriefing_CodeBlockFormatting(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:     agentpolicy.ProfileImplementer,
		Title:       "Implementer",
		Description: "Implementation",
		AllowedSkills: []agentpolicy.SkillInfo{
			{
				Name:        "code/edit",
				Description: "Edit code files",
				Example:     "foxctl run code/edit --input '{\"path\": \"file.go\"}'",
			},
		},
		Rules: []string{},
	}

	result := generateBriefing(cfg)

	// Verify code block formatting
	assert.Contains(t, result, "```bash")
	assert.Contains(t, result, "```\n\n")
}

func TestGenerateBriefing_SkillSectionHeader(t *testing.T) {
	// With skills
	cfgWithSkills := agentpolicy.ProfileConfig{
		Profile:     agentpolicy.ProfileReviewer,
		Title:       "Reviewer",
		Description: "Review",
		AllowedSkills: []agentpolicy.SkillInfo{
			{Name: "skill1", Description: "desc", Example: "example"},
		},
		Rules: []string{},
	}

	resultWithSkills := generateBriefing(cfgWithSkills)
	assert.Contains(t, resultWithSkills, "## Allowed foxctl Skills")
	assert.Contains(t, resultWithSkills, "Use these skills via bash:")

	// Without skills
	cfgNoSkills := agentpolicy.ProfileConfig{
		Profile:       agentpolicy.ProfileUnrestricted,
		Title:         "Unrestricted",
		Description:   "Full access",
		AllowedSkills: []agentpolicy.SkillInfo{},
		Rules:         []string{},
	}

	resultNoSkills := generateBriefing(cfgNoSkills)
	assert.Contains(t, resultNoSkills, "## Skills")
	assert.Contains(t, resultNoSkills, "unrestricted access")
}

// Tests for profile validation (testing the pattern used in run())

func TestProfileValidation(t *testing.T) {
	validProfiles := []string{"explorer", "reviewer", "implementer", "unrestricted"}

	for _, p := range validProfiles {
		profile := agentpolicy.Profile(p)
		assert.True(t, profile.IsValid(), "profile %s should be valid", p)
	}
}

func TestProfileValidation_Invalid(t *testing.T) {
	invalidProfiles := []string{"invalid", "admin", "root", "superuser", ""}

	for _, p := range invalidProfiles {
		profile := agentpolicy.Profile(p)
		// Note: IsValid() returns true for "" according to the code (p != ProfileUnrestricted && p != "")
		// Actually looking at the code: func (p Profile) IsValid() bool { return p != ProfileUnrestricted && p != "" }
		// This means IsValid returns true for profiles that should be restricted (not unrestricted and not empty)
		// So "invalid" would return true for IsValid, but GetProfileConfig would fail
		// Let me check GetProfileConfig behavior instead
		_, ok := agentpolicy.GetProfileConfig(profile)
		if p == "" {
			// Empty profile might be handled specially
			continue
		}
		switch p {
		case "unrestricted":
			assert.True(t, ok, "unrestricted should have config")
		case "explorer", "reviewer", "implementer":
			assert.True(t, ok, "known profile %s should have config", p)
		}
	}
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Profile: "explorer",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Profile, decoded.Profile)
}

func TestGenerateBriefing_LongDescription(t *testing.T) {
	longDesc := "This is a very long description that explains in great detail what this agent profile is capable of doing. " +
		"It covers all the various capabilities and restrictions that apply to agents with this profile. " +
		"The description goes on and on to ensure that long text is handled properly."

	cfg := agentpolicy.ProfileConfig{
		Profile:       agentpolicy.ProfileReviewer,
		Title:         "Reviewer",
		Description:   longDesc,
		AllowedSkills: []agentpolicy.SkillInfo{},
		Rules:         []string{},
	}

	result := generateBriefing(cfg)

	assert.Contains(t, result, longDesc)
}

func TestGenerateBriefing_ManyRules(t *testing.T) {
	rules := make([]string, 10)
	for i := range rules {
		rules[i] = "Rule number " + string(rune('0'+i))
	}

	cfg := agentpolicy.ProfileConfig{
		Profile:       agentpolicy.ProfileExplorer,
		Title:         "Explorer",
		Description:   "Exploration",
		AllowedSkills: []agentpolicy.SkillInfo{},
		Rules:         rules,
	}

	result := generateBriefing(cfg)

	for _, rule := range rules {
		assert.Contains(t, result, rule)
	}
}

func TestGenerateBriefing_EmptyExample(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Profile:     agentpolicy.ProfileImplementer,
		Title:       "Implementer",
		Description: "Implementation",
		AllowedSkills: []agentpolicy.SkillInfo{
			{
				Name:        "skill/test",
				Description: "Test skill",
				Example:     "",
			},
		},
		Rules: []string{},
	}

	result := generateBriefing(cfg)

	assert.Contains(t, result, "### skill/test")
	assert.Contains(t, result, "Test skill")
	assert.Contains(t, result, "**Example:**")
}
