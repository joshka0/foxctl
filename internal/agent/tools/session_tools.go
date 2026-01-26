package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/skillrun"
)

// registerSessionTools registers session retrieval tools.
func (r *Registry) registerSessionTools() error {
	// session.recall - semantic search over past sessions
	recallTool := dstools.NewFuncTool(
		"session.recall",
		"Search past coding sessions for relevant context. Returns summaries, learnings, gotchas, and decisions from previous work. Use this to find what was done before on similar tasks.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"query": {
					Type:        "string",
					Description: "Natural language query describing what you're looking for (e.g., 'authentication bug fixes', 'database migrations')",
					Required:    true,
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum number of sessions to return (default 5)",
				},
				"min_similarity": {
					Type:        "number",
					Description: "Minimum similarity threshold 0.0-1.0 (default 0.3)",
				},
			},
		},
		r.wrapWithTelemetry("session.recall", r.sessionRecall),
	)
	if err := r.tools.Register(recallTool); err != nil {
		return fmt.Errorf("register session.recall: %w", err)
	}

	return nil
}

// sessionRecallInput matches the session/recall skill input.
type sessionRecallInput struct {
	Query         string  `json:"query"`
	Limit         int     `json:"limit,omitempty"`
	MinSimilarity float64 `json:"min_similarity,omitempty"`
	Workspace     string  `json:"workspace,omitempty"`
}

// sessionRecall implements the session.recall tool by invoking the session/recall skill.
func (r *Registry) sessionRecall(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return errorResult("query is required"), nil
	}

	input := sessionRecallInput{
		Query:     query,
		Workspace: r.config.WorkspaceRoot,
	}

	if limit, ok := args["limit"].(float64); ok && limit > 0 {
		input.Limit = int(limit)
	} else {
		input.Limit = 5
	}

	// Default MinSimilarity to 0.3, override only if valid value provided
	input.MinSimilarity = 0.3
	if minSim, ok := args["min_similarity"].(float64); ok && minSim >= 0 && minSim <= 1.0 {
		input.MinSimilarity = minSim
	}

	inputBytes, err := json.Marshal(input)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal input: %v", err)), nil
	}

	resolver := skill.NewResolver(skill.WithSearchPaths(
		r.config.WorkspaceRoot+"/dist/skills",
		r.config.WorkspaceRoot+"/skills",
	))

	ctx = workspace.WithContext(ctx, r.config.WorkspaceRoot)

	var payload struct {
		Query   string `json:"query"`
		Matches []struct {
			SessionID    string   `json:"session_id"`
			ProjectName  string   `json:"project_name"`
			GitBranch    string   `json:"git_branch,omitempty"`
			Summary      string   `json:"summary"`
			Accomplished []string `json:"accomplished,omitempty"`
			Decisions    []string `json:"decisions,omitempty"`
			Gotchas      []string `json:"gotchas,omitempty"`
			UserInsights []string `json:"user_insights,omitempty"`
			Tags         []string `json:"tags,omitempty"`
			KeyFiles     []string `json:"key_files,omitempty"`
			Similarity   float64  `json:"similarity"`
			StartedAt    string   `json:"started_at,omitempty"`
		} `json:"matches"`
		Status  string `json:"status"`
		Message string `json:"message"`
	}

	_, err = skillrun.RunAndDecodeInto(ctx, resolver, "session/recall", inputBytes, skillrun.Options{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: r.config.WorkspaceRoot,
	}, &payload)
	if err != nil {
		if errors.Is(err, skill.ErrArtifactsMissing) {
			return errorResult("session/recall skill not found (ensure skill is installed)"), nil
		}
		var runErr skillrun.RunError
		if errors.As(err, &runErr) {
			errMsg := fmt.Sprintf("skill execution failed: %v", runErr.Err)
			if len(runErr.Stderr) > 0 {
				errMsg += fmt.Sprintf(" (stderr: %s)", string(runErr.Stderr))
			}
			return errorResult(errMsg), nil
		}
		return errorResult(fmt.Sprintf("skill error: %v", err)), nil
	}

	// Build simplified result for agent consumption
	matches := make([]map[string]any, 0, len(payload.Matches))
	for _, m := range payload.Matches {
		match := map[string]any{
			"session_id":   m.SessionID,
			"project":      m.ProjectName,
			"summary":      m.Summary,
			"similarity":   m.Similarity,
			"started_at":   m.StartedAt,
			"accomplished": m.Accomplished,
			"decisions":    m.Decisions,
			"gotchas":      m.Gotchas,
		}
		if len(m.KeyFiles) > 0 {
			match["key_files"] = m.KeyFiles
		}
		if len(m.Tags) > 0 {
			match["tags"] = m.Tags
		}
		matches = append(matches, match)
	}

	return successResult(map[string]any{
		"query":   payload.Query,
		"matches": matches,
		"count":   len(matches),
		"status":  payload.Status,
	}), nil
}
