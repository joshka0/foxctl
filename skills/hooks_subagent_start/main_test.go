package main

import (
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/agentpolicy"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "hooks/subagent_start", command)
}

// Tests for SubagentPayload structure

func TestSubagentPayload_AllFields(t *testing.T) {
	payload := SubagentPayload{
		SubagentName: "explorer",
		SubagentType: "Explore",
		AgentID:      "agent-123",
	}

	assert.Equal(t, "explorer", payload.SubagentName)
	assert.Equal(t, "Explore", payload.SubagentType)
	assert.Equal(t, "agent-123", payload.AgentID)
}

func TestSubagentPayload_JSONSerialization(t *testing.T) {
	payload := SubagentPayload{
		SubagentName: "test-agent",
		SubagentType: "Test",
		AgentID:      "agent-456",
	}

	data, err := json.Marshal(payload)
	assert.NoError(t, err)

	var decoded SubagentPayload
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, payload.SubagentName, decoded.SubagentName)
	assert.Equal(t, payload.SubagentType, decoded.SubagentType)
	assert.Equal(t, payload.AgentID, decoded.AgentID)
}

func TestSubagentPayload_EmptyFields(t *testing.T) {
	payload := SubagentPayload{}

	assert.Empty(t, payload.SubagentName)
	assert.Empty(t, payload.SubagentType)
	assert.Empty(t, payload.AgentID)
}

// Tests for inferProfile helper

func TestInferProfile_ExplorerPatterns(t *testing.T) {
	patterns := []string{
		"explorer",
		"code-explorer",
		"Navigator",
		"file-navigator",
		"investigate-issue",
		"search-agent",
		"find-files",
		"browse-codebase",
		"discover-patterns",
	}

	for _, name := range patterns {
		t.Run(name, func(t *testing.T) {
			result := inferProfile(name)
			assert.Equal(t, agentpolicy.ProfileExplorer, result)
		})
	}
}

func TestInferProfile_ReviewerPatterns(t *testing.T) {
	patterns := []string{
		"reviewer",
		"code-reviewer",
		"Architect",
		"solution-architect",
		"analyze-code",
		"audit-files",
		"check-quality",
		"inspect-module",
		"assess-risk",
	}

	for _, name := range patterns {
		t.Run(name, func(t *testing.T) {
			result := inferProfile(name)
			assert.Equal(t, agentpolicy.ProfileReviewer, result)
		})
	}
}

func TestInferProfile_ImplementerPatterns(t *testing.T) {
	patterns := []string{
		"implementer",
		"task-implementer",
		"Coder",
		"python-coder",
		"bug-fixer",
		"Developer",
		"frontend-developer",
		"Builder",
		"feature-builder",
		"Writer",
		"code-writer",
		"implement-feature",
		"fix-bug",
		"code-changes",
	}

	for _, name := range patterns {
		t.Run(name, func(t *testing.T) {
			result := inferProfile(name)
			assert.Equal(t, agentpolicy.ProfileImplementer, result)
		})
	}
}

func TestInferProfile_UnrestrictedDefault(t *testing.T) {
	patterns := []string{
		"custom-agent",
		"my-agent",
		"unknown",
		"random-name",
		"",
	}

	for _, name := range patterns {
		t.Run(name, func(t *testing.T) {
			result := inferProfile(name)
			assert.Equal(t, agentpolicy.ProfileUnrestricted, result)
		})
	}
}

func TestInferProfile_CaseInsensitive(t *testing.T) {
	assert.Equal(t, agentpolicy.ProfileExplorer, inferProfile("EXPLORER"))
	assert.Equal(t, agentpolicy.ProfileExplorer, inferProfile("Explorer"))
	assert.Equal(t, agentpolicy.ProfileExplorer, inferProfile("eXpLoReR"))
}

func TestInferProfile_PartialMatch(t *testing.T) {
	// Should match even if pattern is part of larger name
	assert.Equal(t, agentpolicy.ProfileExplorer, inferProfile("my-explorer-agent"))
	assert.Equal(t, agentpolicy.ProfileReviewer, inferProfile("code-reviewer-v2"))
	assert.Equal(t, agentpolicy.ProfileImplementer, inferProfile("fast-coder-bot"))
}

// Tests for containsAny helper

func TestContainsAny_SingleMatch(t *testing.T) {
	result := containsAny("hello world", "hello")
	assert.True(t, result)
}

func TestContainsAny_MultiplePatterns(t *testing.T) {
	result := containsAny("hello world", "foo", "world", "bar")
	assert.True(t, result)
}

func TestContainsAny_NoMatch(t *testing.T) {
	result := containsAny("hello world", "foo", "bar", "baz")
	assert.False(t, result)
}

func TestContainsAny_EmptyString(t *testing.T) {
	result := containsAny("", "foo", "bar")
	assert.False(t, result)
}

func TestContainsAny_EmptyPatterns(t *testing.T) {
	result := containsAny("hello world")
	assert.False(t, result)
}

func TestContainsAny_PartialMatch(t *testing.T) {
	result := containsAny("explorer-agent", "explore")
	assert.True(t, result)
}

func TestContainsAny_FirstPatternMatch(t *testing.T) {
	// Should return true on first match
	result := containsAny("hello world", "hello", "world")
	assert.True(t, result)
}

// Tests for generateFallbackBriefing helper

func TestGenerateFallbackBriefing_Basic(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Title:       "Test Profile",
		Description: "A test profile description",
	}

	result := generateFallbackBriefing(cfg)

	assert.Contains(t, result, "Test Profile")
	assert.Contains(t, result, "A test profile description")
}

func TestGenerateFallbackBriefing_WithSkills(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Title:       "Test Profile",
		Description: "Description",
		AllowedSkills: []agentpolicy.SkillInfo{
			{Name: "skill/one", Description: "First skill"},
			{Name: "skill/two", Description: "Second skill"},
		},
	}

	result := generateFallbackBriefing(cfg)

	assert.Contains(t, result, "Allowed Skills")
	assert.Contains(t, result, "skill/one")
	assert.Contains(t, result, "First skill")
	assert.Contains(t, result, "skill/two")
	assert.Contains(t, result, "Second skill")
}

func TestGenerateFallbackBriefing_WithRules(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Title:       "Test Profile",
		Description: "Description",
		Rules: []string{
			"Rule one",
			"Rule two",
			"Rule three",
		},
	}

	result := generateFallbackBriefing(cfg)

	assert.Contains(t, result, "Rules")
	assert.Contains(t, result, "1. Rule one")
	assert.Contains(t, result, "2. Rule two")
	assert.Contains(t, result, "3. Rule three")
}

func TestGenerateFallbackBriefing_NoSkillsOrRules(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Title:       "Simple Profile",
		Description: "Just a description",
	}

	result := generateFallbackBriefing(cfg)

	assert.Contains(t, result, "Simple Profile")
	assert.NotContains(t, result, "Allowed Skills")
	assert.NotContains(t, result, "Rules")
}

func TestGenerateFallbackBriefing_Formatting(t *testing.T) {
	cfg := agentpolicy.ProfileConfig{
		Title:         "Test",
		Description:   "Description",
		AllowedSkills: []agentpolicy.SkillInfo{{Name: "skill", Description: "desc"}},
		Rules:         []string{"Rule"},
	}

	result := generateFallbackBriefing(cfg)

	// Check markdown formatting
	assert.Contains(t, result, "# Agent Profile:")
	assert.Contains(t, result, "## Allowed Skills")
	assert.Contains(t, result, "## Rules")
	assert.Contains(t, result, "-") // List items for skills
}

// Tests for agentpolicy.Profile values

func TestProfileValues(t *testing.T) {
	assert.Equal(t, agentpolicy.Profile("explorer"), agentpolicy.ProfileExplorer)
	assert.Equal(t, agentpolicy.Profile("reviewer"), agentpolicy.ProfileReviewer)
	assert.Equal(t, agentpolicy.Profile("implementer"), agentpolicy.ProfileImplementer)
	assert.Equal(t, agentpolicy.Profile("unrestricted"), agentpolicy.ProfileUnrestricted)
}

// Tests for agentpolicy.GetProfileConfig

func TestGetProfileConfig_ValidProfiles(t *testing.T) {
	profiles := []agentpolicy.Profile{
		agentpolicy.ProfileExplorer,
		agentpolicy.ProfileReviewer,
		agentpolicy.ProfileImplementer,
		agentpolicy.ProfileUnrestricted,
	}

	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			cfg, ok := agentpolicy.GetProfileConfig(profile)
			assert.True(t, ok)
			assert.NotEmpty(t, cfg.Title)
			assert.NotEmpty(t, cfg.Description)
		})
	}
}

func TestGetProfileConfig_InvalidProfile(t *testing.T) {
	_, ok := agentpolicy.GetProfileConfig(agentpolicy.Profile("invalid"))
	assert.False(t, ok)
}

// Tests for agentpolicy.GetAllowedSkillNames

func TestGetAllowedSkillNames_Explorer(t *testing.T) {
	names := agentpolicy.GetAllowedSkillNames(agentpolicy.ProfileExplorer)
	assert.NotEmpty(t, names)
}

func TestGetAllowedSkillNames_Unrestricted(t *testing.T) {
	names := agentpolicy.GetAllowedSkillNames(agentpolicy.ProfileUnrestricted)
	// Unrestricted may have empty or full list
	_ = names
}

// Tests for edge cases

func TestInferProfile_SpecialCharacters(t *testing.T) {
	result := inferProfile("explorer@v2.0#latest")
	assert.Equal(t, agentpolicy.ProfileExplorer, result)
}

func TestInferProfile_Whitespace(t *testing.T) {
	result := inferProfile("  explorer  ")
	assert.Equal(t, agentpolicy.ProfileExplorer, result)
}

func TestContainsAny_SpecialChars(t *testing.T) {
	result := containsAny("hello-world_test", "world")
	assert.True(t, result)
}

func TestSubagentPayload_FromJSON(t *testing.T) {
	jsonStr := `{"subagent_name":"test","subagent_type":"Test","agent_id":"123"}`

	var payload SubagentPayload
	err := json.Unmarshal([]byte(jsonStr), &payload)
	assert.NoError(t, err)

	assert.Equal(t, "test", payload.SubagentName)
	assert.Equal(t, "Test", payload.SubagentType)
	assert.Equal(t, "123", payload.AgentID)
}

func TestSubagentPayload_PartialJSON(t *testing.T) {
	jsonStr := `{"subagent_name":"test"}`

	var payload SubagentPayload
	err := json.Unmarshal([]byte(jsonStr), &payload)
	assert.NoError(t, err)

	assert.Equal(t, "test", payload.SubagentName)
	assert.Empty(t, payload.SubagentType)
	assert.Empty(t, payload.AgentID)
}
