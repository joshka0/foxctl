package chatadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/api"
)

// Bridge connects chat adapter commands to agentctl skills and APIs.
type Bridge struct {
	runner    *api.SkillRunner
	cfg       config.Config
	daemonURL string
}

// NewBridge creates a Bridge with an explicit SkillRunner for dependency injection.
// daemonURL is the base URL for the agent daemon API (e.g. "http://localhost:8090").
func NewBridge(runner *api.SkillRunner, cfg config.Config, daemonURL string) *Bridge {
	if daemonURL == "" {
		daemonURL = "http://localhost:8090"
	}
	return &Bridge{
		runner:    runner,
		cfg:       cfg,
		daemonURL: daemonURL,
	}
}

// NewDefaultBridge creates a Bridge with a default SkillRunner.
func NewDefaultBridge(cfg config.Config, daemonURL string) *Bridge {
	return NewBridge(api.NewSkillRunner(cfg), cfg, daemonURL)
}

// skillRoute maps a command name to its skill name and input builder.
type skillRoute struct {
	skill      string
	buildInput func(opts map[string]any) map[string]any
}

// routes maps slash command names to skill invocations.
var routes = map[string]skillRoute{
	"search": {
		skill: "code/semantic_search",
		buildInput: func(opts map[string]any) map[string]any {
			return map[string]any{
				"query":  opts["query"],
				"limit":  10,
				"format": "tree",
			}
		},
	},
	"todo": {
		skill: "todo/manage",
		buildInput: func(opts map[string]any) map[string]any {
			input := map[string]any{
				"action": opts["action"],
			}
			if title, ok := opts["title"]; ok {
				input["title"] = title
			}
			if id, ok := opts["id"]; ok {
				input["id"] = id
			}
			return input
		},
	},
	"memory": {
		skill: "memory/query",
		buildInput: func(opts map[string]any) map[string]any {
			return map[string]any{
				"query": opts["query"],
			}
		},
	},
	"logs": {
		skill: "obs/logs",
		buildInput: func(opts map[string]any) map[string]any {
			input := map[string]any{}
			if errOnly, ok := opts["errors_only"]; ok {
				input["errors_only"] = errOnly
			}
			return input
		},
	},
}

// HandleCommand dispatches a CommandEvent to the appropriate skill or API.
func (b *Bridge) HandleCommand(ctx context.Context, evt CommandEvent) error {
	observability.Emit(ctx, observability.NewEvent("discord.command_received").
		WithComponent("discord").
		WithCommand(evt.Command).
		WithData("user", evt.User.Username).
		Success(0))

	// Check for agent commands first (use daemon HTTP API, not skills)
	switch evt.Command {
	case "agent-spawn":
		return b.handleAgentSpawn(ctx, evt)
	case "agent-list":
		return b.handleAgentList(ctx, evt)
	}

	// Look up skill route
	route, ok := routes[evt.Command]
	if !ok {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return evt.Respond(fmt.Sprintf("Unknown command: `/%s`. Available: /search, /todo, /memory, /logs, /agent-spawn, /agent-list", evt.Command), nil)
	}

	input := route.buildInput(evt.Options)
	result, err := b.runner.Run(ctx, route.skill, input)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("discord.skill_failed").
			WithComponent("discord").
			WithCommand(route.skill).
			Error(err, 0))
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return evt.Respond(fmt.Sprintf("Error running `%s`: %s\nHint: verify the skill is installed and the daemon is running.", route.skill, err.Error()), nil)
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if !result.Success {
		return evt.Respond(fmt.Sprintf("Skill `%s` failed: %s\nHint: check skill logs with `/logs` or verify input.", route.skill, result.Error), nil)
	}

	// Format output as a message
	content := formatSkillOutput(route.skill, result.Output)
	return evt.Respond(content, nil)
}

// formatSkillOutput converts skill JSON output into a Discord-friendly message.
func formatSkillOutput(skillName string, output json.RawMessage) string {
	if len(output) == 0 {
		return fmt.Sprintf("`%s` completed (no output)", skillName)
	}

	// Try to pretty-print as indented JSON
	var parsed any
	if err := json.Unmarshal(output, &parsed); err == nil {
		// For tree-formatted search results, extract the tree string
		if m, ok := parsed.(map[string]any); ok {
			if tree, ok := m["tree"].(string); ok && tree != "" {
				return truncate(fmt.Sprintf("**%s**\n```\n%s\n```", skillName, tree), 1900)
			}
			if items, ok := m["items"].([]any); ok {
				return formatItems(skillName, items)
			}
			if results, ok := m["results"].([]any); ok {
				return formatItems(skillName, results)
			}
		}

		// Fallback: JSON code block
		pretty, _ := json.MarshalIndent(parsed, "", "  ")
		return truncate(fmt.Sprintf("**%s**\n```json\n%s\n```", skillName, string(pretty)), 1900)
	}

	// Raw string fallback
	return truncate(fmt.Sprintf("**%s**\n```\n%s\n```", skillName, string(output)), 1900)
}

// formatItems formats a list of result items.
func formatItems(skillName string, items []any) string {
	if len(items) == 0 {
		return fmt.Sprintf("**%s**: no results", skillName)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**%s** (%d results)\n", skillName, len(items)))
	for i, item := range items {
		if i >= 15 {
			sb.WriteString(fmt.Sprintf("... and %d more\n", len(items)-15))
			break
		}
		if m, ok := item.(map[string]any); ok {
			// Try common fields
			if name, ok := m["name"].(string); ok {
				sb.WriteString(fmt.Sprintf("- **%s**", name))
				if summary, ok := m["summary"].(string); ok {
					sb.WriteString(fmt.Sprintf(": %s", summary))
				}
				sb.WriteString("\n")
				continue
			}
			if path, ok := m["path"].(string); ok {
				sb.WriteString(fmt.Sprintf("- `%s`", path))
				if score, ok := m["score"].(float64); ok {
					sb.WriteString(fmt.Sprintf(" (%.2f)", score))
				}
				sb.WriteString("\n")
				continue
			}
		}
		line, _ := json.Marshal(item)
		sb.WriteString(fmt.Sprintf("- %s\n", string(line)))
	}
	return truncate(sb.String(), 1900)
}

// truncate ensures the string does not exceed maxLen runes.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// handleAgentSpawn spawns an agent via the daemon HTTP API.
func (b *Bridge) handleAgentSpawn(ctx context.Context, evt CommandEvent) error {
	role, _ := evt.Options["role"].(string)
	prompt, _ := evt.Options["prompt"].(string)

	if role == "" || prompt == "" {
		return evt.Respond("Both `role` and `prompt` are required.", nil)
	}

	payload, err := json.Marshal(map[string]any{
		"role":           role,
		"prompt":         prompt,
		"exec_mode":      "autonomous",
		"max_iterations": 10,
	})
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to build request: %s", err), nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.daemonURL+"/api/agents/spawn", strings.NewReader(string(payload)))
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s\nHint: check daemon URL format.", err), nil)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Daemon unreachable: %s\nHint: ensure the daemon is running (`agentctl web serve`).", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return evt.Respond(fmt.Sprintf("Agent spawn failed (HTTP %d)\nHint: check daemon logs with `/logs`.", resp.StatusCode), nil)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return evt.Respond("Agent spawned, but couldn't parse response.\nHint: check daemon API version.", nil)
	}

	sessionID, _ := result["session_id"].(string)
	return evt.Respond(fmt.Sprintf("Agent spawned (role: `%s`, session: `%s`)", role, sessionID), nil)
}

// handleAgentList lists running agents via the daemon HTTP API.
func (b *Bridge) handleAgentList(ctx context.Context, evt CommandEvent) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.daemonURL+"/api/agents", nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s\nHint: check daemon URL format.", err), nil)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Daemon unreachable: %s\nHint: ensure the daemon is running (`agentctl web serve`).", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return evt.Respond(fmt.Sprintf("Agent list failed (HTTP %d): %s\nHint: check daemon logs.", resp.StatusCode, string(body)), nil)
	}

	var agents []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return evt.Respond("Failed to parse agent list.\nHint: check daemon API and network.", nil)
	}

	if len(agents) == 0 {
		return evt.Respond("No agents running. Use `/agent-spawn` to start one.", nil)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Agents** (%d)\n", len(agents)))
	for _, a := range agents {
		name, _ := a["name"].(string)
		role, _ := a["role"].(string)
		status, _ := a["status"].(string)
		sb.WriteString(fmt.Sprintf("- **%s** (%s) — %s\n", name, role, status))
	}
	return evt.Respond(sb.String(), nil)
}
