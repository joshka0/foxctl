package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBuildPrompt(t *testing.T) {
	req := PlanRequest{
		Goal:        "Implement user authentication",
		Description: "Add OAuth2 support with Google and GitHub providers",
		ScopePaths:  []string{"cmd/auth", "internal/auth"},
		MaxTasks:    10,
		MaxDepth:    2,
		Strategy:    "epic",
	}

	prompt := buildPrompt(req)

	// Check prompt contains key elements
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !contains(prompt, req.Goal) {
		t.Error("prompt should contain goal")
	}
	if !contains(prompt, req.Description) {
		t.Error("prompt should contain description")
	}
	if !contains(prompt, "cmd/auth") {
		t.Error("prompt should contain scope paths")
	}
	if !contains(prompt, "10") {
		t.Error("prompt should contain max tasks")
	}
}

func TestParseResponse_ValidJSON(t *testing.T) {
	response := `{
		"tasks": [
			{"title": "Epic: User Auth", "description": "Main auth epic"},
			{"title": "Add OAuth2 client", "description": "Setup OAuth2", "depends_on": ["Epic: User Auth"]},
			{"title": "Add Google provider", "description": "Google OAuth", "scope_path": "internal/auth/google", "depends_on": ["Add OAuth2 client"]}
		],
		"reasoning": "Decomposed into OAuth setup then provider implementation"
	}`

	result, err := parseResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(result.Tasks))
	}
	if result.Tasks[0].Title != "Epic: User Auth" {
		t.Errorf("expected first task to be epic, got %s", result.Tasks[0].Title)
	}
	if result.Tasks[2].ScopePath != "internal/auth/google" {
		t.Errorf("expected scope path on task 3, got %s", result.Tasks[2].ScopePath)
	}
	if len(result.Tasks[1].DependsOn) != 1 || result.Tasks[1].DependsOn[0] != "Epic: User Auth" {
		t.Errorf("expected dependency on Epic: User Auth")
	}
}

func TestParseResponse_MarkdownCodeBlock(t *testing.T) {
	response := "```json\n" + `{
		"tasks": [{"title": "Test Task", "description": "Testing"}],
		"reasoning": "Simple test"
	}` + "\n```"

	result, err := parseResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(result.Tasks))
	}
}

func TestParseResponse_EmptyTasks(t *testing.T) {
	response := `{"tasks": [], "reasoning": "Nothing to do"}`

	_, err := parseResponse(response)
	if err == nil {
		t.Error("expected error for empty tasks")
	}
}

// configFromEnv builds a ProviderConfig from environment variables.
// This is only used in tests - the imperative shell.
func configFromEnv() ProviderConfig {
	return ProviderConfig{
		CerebrasAPIKey:   os.Getenv("CEREBRAS_API_KEY"),
		OpenRouterAPIKey: os.Getenv("OPENROUTER_API_KEY"),
		OpenRouterModel:  os.Getenv("OPENROUTER_MODEL"),
		GroqAPIKey:       os.Getenv("GROQ_API_KEY"),
		OpenAIAPIKey:     os.Getenv("OPENAI_API_KEY"),
	}
}

func TestAutoPlanner_NoAPIKey(t *testing.T) {
	// Test with empty config - no API keys set
	cfg := ProviderConfig{}

	planner := AutoPlannerFromConfig(cfg)
	if planner != nil {
		t.Error("expected nil planner when no API keys are set")
	}

	if IsLLMPlanningAvailableFromConfig(cfg) {
		t.Error("expected LLM planning to be unavailable")
	}
}

// TestOpenAIPlanner_Integration is an integration test that requires an API key.
// Set AGENTCTL_ENABLE_LIVE_LLM_TESTS=1 and one of OPENROUTER_API_KEY, GROQ_API_KEY, or OPENAI_API_KEY to run this test.
func TestOpenAIPlanner_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping LLM integration test in -short mode")
	}
	if os.Getenv("AGENTCTL_ENABLE_LIVE_LLM_TESTS") != "1" {
		t.Skip("Skipping LLM integration test: set AGENTCTL_ENABLE_LIVE_LLM_TESTS=1 to enable")
	}

	cfg := configFromEnv()
	if !IsLLMPlanningAvailableFromConfig(cfg) {
		t.Skip("Skipping LLM integration test: no API key configured (set GROQ_API_KEY or OPENAI_API_KEY)")
	}

	planner := AutoPlannerFromConfig(cfg)
	if planner == nil {
		t.Fatal("expected planner to be available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req := PlanRequest{
		Goal:        "Add a simple health check endpoint",
		Description: "Create a /health endpoint that returns 200 OK with version info",
		ScopePaths:  []string{"cmd/server"},
		MaxTasks:    5,
		MaxDepth:    2,
		Strategy:    "flat",
	}

	result, err := planner.Plan(ctx, req)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	t.Logf("LLM Provider: %s", planner.Provider())
	t.Logf("Model: %s", result.ModelUsed)
	t.Logf("Tokens: %d", result.TokensUsed)
	t.Logf("Reasoning: %s", result.Reasoning)
	t.Logf("Generated %d tasks:", len(result.Tasks))

	for i, task := range result.Tasks {
		t.Logf("  %d. %s", i+1, task.Title)
		if task.Description != "" {
			t.Logf("     %s", task.Description)
		}
		if len(task.DependsOn) > 0 {
			t.Logf("     Depends on: %v", task.DependsOn)
		}
	}

	if len(result.Tasks) == 0 {
		t.Error("expected at least one task")
	}
	if result.ModelUsed == "" {
		t.Error("expected model name in result")
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
