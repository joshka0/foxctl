// Package llm provides LLM-based task planning and decomposition.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PlanTask represents a single task in a generated plan.
type PlanTask struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ScopePath   string   `json:"scope_path,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"` // Titles of tasks this depends on
}

// PlanRequest contains the input for LLM-based planning.
type PlanRequest struct {
	Goal        string   `json:"goal"`
	Description string   `json:"description,omitempty"`
	ScopePaths  []string `json:"scope_paths,omitempty"`
	MaxTasks    int      `json:"max_tasks"`
	MaxDepth    int      `json:"max_depth"`
	Strategy    string   `json:"strategy"` // "auto", "epic", or "flat"
}

// PlanResult contains the generated plan from the LLM.
type PlanResult struct {
	Tasks      []PlanTask `json:"tasks"`
	Reasoning  string     `json:"reasoning,omitempty"`
	ModelUsed  string     `json:"model_used,omitempty"`
	TokensUsed int        `json:"tokens_used,omitempty"`
}

// Planner is the interface for LLM-based planning.
type Planner interface {
	// Plan generates a task decomposition from a goal.
	Plan(ctx context.Context, req PlanRequest) (*PlanResult, error)
}

// buildPrompt creates the planning prompt for the LLM.
func buildPrompt(req PlanRequest) string {
	var sb strings.Builder

	sb.WriteString("You are an expert software planning assistant. ")
	sb.WriteString("Decompose the following goal into a task graph that can be executed by multiple agents.\n\n")

	sb.WriteString("## Goal\n")
	sb.WriteString(req.Goal)
	sb.WriteString("\n\n")

	if req.Description != "" {
		sb.WriteString("## Additional Context\n")
		sb.WriteString(req.Description)
		sb.WriteString("\n\n")
	}

	if len(req.ScopePaths) > 0 {
		sb.WriteString("## Scope Paths (likely files/directories to touch)\n")
		for _, p := range req.ScopePaths {
			sb.WriteString("- ")
			sb.WriteString(p)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Constraints\n")
	sb.WriteString(fmt.Sprintf("- Maximum %d tasks\n", req.MaxTasks))
	sb.WriteString(fmt.Sprintf("- Maximum nesting depth: %d\n", req.MaxDepth))
	if req.Strategy != "" && req.Strategy != "auto" {
		sb.WriteString(fmt.Sprintf("- Strategy: %s\n", req.Strategy))
	}
	sb.WriteString("\n")

	sb.WriteString("## Output Format\n")
	sb.WriteString("Respond with a JSON object containing:\n")
	sb.WriteString("- `tasks`: array of task objects with `title`, `description`, `scope_path` (optional), `depends_on` (array of task titles this depends on)\n")
	sb.WriteString("- `reasoning`: brief explanation of the decomposition approach\n\n")

	sb.WriteString("Order tasks so that dependencies appear before dependents.\n")
	sb.WriteString("The first task should be the epic (high-level container).\n\n")

	sb.WriteString("Respond ONLY with valid JSON, no markdown formatting.\n")

	return sb.String()
}

// parseResponse extracts the plan from the LLM response.
func parseResponse(response string) (*PlanResult, error) {
	// Try to extract JSON from the response
	response = strings.TrimSpace(response)

	// Remove markdown code blocks if present
	if strings.HasPrefix(response, "```json") {
		response = strings.TrimPrefix(response, "```json")
		if idx := strings.Index(response, "```"); idx != -1 {
			response = response[:idx]
		}
	} else if strings.HasPrefix(response, "```") {
		response = strings.TrimPrefix(response, "```")
		if idx := strings.Index(response, "```"); idx != -1 {
			response = response[:idx]
		}
	}

	response = strings.TrimSpace(response)

	var result PlanResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response as JSON: %w\nResponse: %s", err, response)
	}

	if len(result.Tasks) == 0 {
		return nil, fmt.Errorf("LLM returned empty task list")
	}

	return &result, nil
}
