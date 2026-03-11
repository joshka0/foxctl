package memoryflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/hooks/lifecycle"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/todosync"
)

type Dependencies struct {
	Config   config.Config
	RunSkill lifecycle.SkillRunner
}

type ClaudeTodo = todosync.ClaudeTodo

func NewDependencies(cfg config.Config, life lifecycle.Dependencies) Dependencies {
	return Dependencies{Config: cfg, RunSkill: life.RunSkill}
}

type DetectorRequest struct {
	Prompt string
}

type DetectorResponse struct {
	Decision string `json:"decision"`
	Context  string `json:"context,omitempty"`
}

type RecallPayload struct {
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
	} `json:"tool_input"`
}

type RecallRequest struct {
	Workspace string
	Payload   RecallPayload
}

type RecallResponse struct {
	Decision string `json:"decision"`
	Context  string `json:"context,omitempty"`
	FilePath string `json:"file_path,omitempty"`
}

type LifecyclePayload struct {
	ToolName  string `json:"tool_name,omitempty"`
	ToolInput struct {
		FilePath  string       `json:"file_path,omitempty"`
		Path      string       `json:"path,omitempty"`
		OldString string       `json:"old_string,omitempty"`
		NewString string       `json:"new_string,omitempty"`
		Content   string       `json:"content,omitempty"`
		Operation string       `json:"operation,omitempty"`
		Name      string       `json:"name,omitempty"`
		Todos     []ClaudeTodo `json:"todos,omitempty"`
	} `json:"tool_input"`
}

type LifecycleRequest struct {
	Workspace string
	Payload   LifecyclePayload
}

type LifecycleResponse struct {
	Decision string `json:"decision"`
	Context  string `json:"context,omitempty"`
}

func DetectPrompt(req DetectorRequest) DetectorResponse {
	prompt := strings.ToLower(strings.TrimSpace(req.Prompt))
	response := DetectorResponse{Decision: "approve"}
	if prompt == "" {
		return response
	}

	if matchesAny(prompt,
		`how did (we|i)`, `where did (we|i)`, `what was the`, `when did (we|i)`,
		`didn.?t (we|i) already`, `like (we|i) did before`, `as we discussed`,
		`previously`, `earlier (we|i)`, `last time (we|i)`, `remember when (we|i)`,
		`similar to (before|what we)`, `we (already|once) (did|had|tried)`, `do you remember`,
	) {
		response.Context = "**Recall hint:** Try these to find past context:\n" +
			"- `agentctl memory get --query \"<keywords>\"` - search memories\n" +
			"- `agentctl run code/semantic_search --input '{\"query\": \"<question>\"}'` - semantic code search\n" +
			"- `agentctl run session/recall --input '{\"query\": \"<question>\"}'` - search past sessions"
		return response
	}

	if matchesAny(prompt,
		`let.?s make sure`, `let.?s not forget to`, `make sure (we|to)`, `we need to make sure`, `don.?t forget to`, `we should make sure`,
		`we need to`, `we should`, `we must`, `we have to`,
		`todo:`, `fixme:`, `hack:`, `xxx:`,
		`follow up on`, `action item`, `next step`, `before we `,
	) {
		response.Context = "**Todo hint:** Capture this as a task:\n" +
			"- `bin/agentctl todo add --title \"<task>\" --description \"<details>\"`\n" +
			"- Or use TodoWrite tool to track this"
		return response
	}

	memoryType := detectMemoryType(prompt)
	if memoryType != "" {
		response.Context = "**Memory hint:** User wants to save a " + memoryType + ". Use `/remember` skill to:\n" +
			"1. Store to agentctl memory\n2. Append to CLAUDE.md under Gotchas section"
	}
	return response
}

func RecallFile(ctx context.Context, deps Dependencies, req RecallRequest) (RecallResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return RecallResponse{}, fmt.Errorf("detect workspace")
	}
	response := RecallResponse{Decision: "approve"}
	filePath := strings.TrimSpace(req.Payload.ToolInput.FilePath)
	if filePath == "" {
		return response, nil
	}
	response.FilePath = filePath
	filename := filepath.Base(filePath)
	query := strings.TrimSuffix(filename, filepath.Ext(filename))
	if strings.TrimSpace(query) == "" {
		return response, nil
	}

	store, err := memory.OpenWithConfig(ctx, deps.Config)
	if err != nil {
		return response, err
	}
	defer store.Close()
	results, err := store.Search(ctx, target, query, 5)
	if err != nil || len(results) == 0 {
		return response, nil
	}
	parts := make([]string, 0, 3)
	for i, item := range results {
		if i >= 3 {
			break
		}
		kind := strings.TrimSpace(item.Entry.Type)
		if kind == "" {
			kind = "note"
		}
		label := kind
		if len(label) > 4 {
			label = label[:4]
		}
		summary := strings.TrimSpace(item.Entry.Summary)
		if summary == "" {
			summary = item.Entry.Name
		}
		summary = trimFilePrefix(summary, query)
		parts = append(parts, "["+label+"] "+summary)
	}
	if len(parts) == 0 {
		return response, nil
	}
	response.Context = "`" + filename + "`: " + strings.Join(parts, " | ")
	return response, nil
}

func HandleLifecycle(ctx context.Context, deps Dependencies, req LifecycleRequest) (LifecycleResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return LifecycleResponse{}, fmt.Errorf("detect workspace")
	}
	response := LifecycleResponse{Decision: "approve"}
	toolName := strings.TrimSpace(req.Payload.ToolName)
	switch toolName {
	case "TodoWrite":
		if envEnabled("AGENTCTL_MEMORY_PROMPT_DISABLED") {
			return response, nil
		}
		completed := make([]string, 0)
		for _, todo := range req.Payload.ToolInput.Todos {
			if todo.Status == "completed" {
				completed = append(completed, strings.TrimSpace(todo.Content))
			}
		}
		if len(completed) == 0 {
			return response, nil
		}
		if len(completed) == 1 {
			response.Context = "**Memory prompt:** Task completed: \"" + completed[0] + "\"\n\n" +
				"If you learned something useful or encountered a gotcha, save it:\n" +
				"`agentctl memory put --name \"gotcha-<topic>\" --type gotcha --summary \"<learning>\"`"
		} else {
			response.Context = fmt.Sprintf("**Memory prompt:** Completed %d tasks.\n\nIf you learned something useful or encountered gotchas, save them:\n`agentctl memory put --name \"gotcha-<topic>\" --type gotcha --summary \"<learning>\"`", len(completed))
		}
		return response, nil
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		if envEnabledInverse("AGENTCTL_MEMORY_CAPTURE", true) {
			if err := captureEditMemory(ctx, deps, target, toolName, req.Payload); err != nil {
				return response, err
			}
		}
		if envEnabledInverse("AGENTCTL_MEMORY_EMBED", true) && (strings.TrimSpace(deps.Config.Embedding.APIKey) != "" || strings.TrimSpace(deps.Config.Embedding.VoyageAPIKey) != "" || strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) != "" || strings.TrimSpace(deps.Config.Embedding.Provider) != "") {
			_ = refreshMemoryEmbedding(ctx, deps, target, req.Payload)
		}
		return response, nil
	default:
		if strings.Contains(strings.ToLower(toolName), "memory") && envEnabledInverse("AGENTCTL_MEMORY_EMBED", true) {
			if op := strings.TrimSpace(req.Payload.ToolInput.Operation); op == "set" || op == "append" {
				_ = refreshNamedMemory(ctx, deps, target, strings.TrimSpace(req.Payload.ToolInput.Name))
			}
		}
		return response, nil
	}
}

func captureEditMemory(ctx context.Context, deps Dependencies, workspacePath, toolName string, payload LifecyclePayload) error {
	filePath := strings.TrimSpace(payload.ToolInput.FilePath)
	if filePath == "" {
		filePath = strings.TrimSpace(payload.ToolInput.Path)
	}
	if filePath == "" {
		return nil
	}
	relPath := strings.TrimPrefix(filepath.ToSlash(filePath), filepath.ToSlash(workspacePath)+"/")
	changeType, changeSummary := summarizeEditChange(toolName, payload)
	store, err := memory.OpenWithConfig(ctx, deps.Config)
	if err != nil {
		return err
	}
	defer store.Close()
	envelopeBytes, _ := json.Marshal(map[string]any{
		"version": 1,
		"status":  "ok",
		"command": "memory/edit",
		"data": map[string]any{
			"file":        relPath,
			"change_type": changeType,
			"summary":     changeSummary,
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
		},
	})
	_, err = store.Save(ctx, storage.NamedEntry{
		Name:      "edit:" + relPath,
		Type:      "edit",
		Workspace: workspacePath,
		Summary:   changeType + " " + relPath + ": " + changeSummary,
		Result:    envelopeBytes,
	})
	return err
}

func refreshMemoryEmbedding(ctx context.Context, deps Dependencies, workspacePath string, payload LifecyclePayload) error {
	filePath := strings.TrimSpace(payload.ToolInput.FilePath)
	if filePath == "" {
		filePath = strings.TrimSpace(payload.ToolInput.Path)
	}
	if filePath == "" {
		return nil
	}
	relPath := strings.TrimPrefix(filepath.ToSlash(filePath), filepath.ToSlash(workspacePath)+"/")
	return refreshNamedMemory(ctx, deps, workspacePath, "edit:"+relPath)
}

func refreshNamedMemory(ctx context.Context, deps Dependencies, workspacePath, name string) error {
	if deps.RunSkill == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	var out any
	return deps.RunSkill(ctx, "embedding/refresh", map[string]any{
		"scope":     "memory",
		"name":      name,
		"workspace": workspacePath,
	}, workspacePath, &out)
}

func summarizeEditChange(toolName string, payload LifecyclePayload) (string, string) {
	switch toolName {
	case "Edit":
		oldStr := truncate(payload.ToolInput.OldString, 50)
		newStr := truncate(payload.ToolInput.NewString, 50)
		return "edit", fmt.Sprintf("replaced '%s...' with '%s...'", oldStr, newStr)
	case "Write":
		return "write", fmt.Sprintf("wrote %d chars", len(payload.ToolInput.Content))
	default:
		return strings.ToLower(toolName), "modified"
	}
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func envEnabledInverse(name string, defaultOn bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if value == "" {
		return defaultOn
	}
	return value != "0" && value != "false" && value != "no" && value != "off"
}

func detectMemoryType(prompt string) string {
	switch {
	case matchesAny(prompt, `(remember|note|save).*(this|that)`, `don.?t forget`, `do not forget`, `please don.?t`, `please do not`, `for future reference`, `(^|[[:space:]])remember([[:space:]]|$)`):
		return "context"
	case matchesAny(prompt, `^(gotcha|learned|learning|tricky):`, `the trick is`, `the key is`, `turns out`, `watch out for`, `be careful`, `keep in mind`, `til:`, `today i learned`, `pro tip`, `tip:`):
		return "gotcha"
	case matchesAny(prompt, `^(decision|decided|choosing):`):
		return "decision"
	case matchesAny(prompt, `^(pattern|approach|solution):`):
		return "pattern"
	default:
		return ""
	}
}

func matchesAny(prompt string, patterns ...string) bool {
	for _, pattern := range patterns {
		if ok, _ := regexp.MatchString(pattern, prompt); ok {
			return true
		}
	}
	return false
}

func trimFilePrefix(summary, filenameNoExt string) string {
	re := regexp.MustCompile(`(?i)^` + regexp.QuoteMeta(filenameNoExt) + `(\\.\w+)?:\s*`)
	return re.ReplaceAllString(summary, "")
}
