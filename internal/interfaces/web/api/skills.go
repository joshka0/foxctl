package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

const (
	mcpDefaultPIDFile = "~/.foxctl/mcp-daemon.pid"
	mcpDefaultAddr    = ":8091"
)

// SkillResponse represents a skill in API responses.
type SkillResponse struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Tags        []string          `json:"tags"`
	Command     string            `json:"command"`
	Parameters  []skill.Parameter `json:"parameters"`
	Returns     []skill.Parameter `json:"returns,omitempty"`
	Help        *skill.Help       `json:"help,omitempty"`
	JSONSchema  map[string]any    `json:"json_schema,omitempty"`
}

type SkillsListResponse struct {
	Skills []SkillResponse `json:"skills"`
	Count  int             `json:"count"`
}

type SkillRunRequest struct {
	Skill string         `json:"skill"`
	Input map[string]any `json:"input"`
}

type SkillRunResponse struct {
	OK         bool            `json:"ok"`
	Skill      string          `json:"skill"`
	Output     json.RawMessage `json:"output"`
	Error      string          `json:"error"`
	DurationMS int64           `json:"duration_ms"`
}

type SkillManifestResponse struct {
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	Description  string             `json:"description"`
	Tags         []string           `json:"tags"`
	Command      string             `json:"command"`
	Parameters   []skill.Parameter  `json:"parameters"`
	Returns      []skill.Parameter  `json:"returns"`
	Help         *skill.Help        `json:"help"`
	Capabilities skill.Capabilities `json:"capabilities"`
	JSONSchema   map[string]any     `json:"json_schema"`
}

type MCPStatusResponse struct {
	Daemon   MCPDaemonStatus    `json:"daemon"`
	Backends []MCPBackendStatus `json:"backends"`
}

type MCPDaemonStatus struct {
	Running   bool           `json:"running"`
	PID       int            `json:"pid"`
	Addr      string         `json:"addr"`
	PIDFile   string         `json:"pid_file"`
	HealthURL string         `json:"health_url"`
	Health    map[string]any `json:"health"`
	Error     string         `json:"error"`
}

type MCPBackendStatus struct {
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
}

type MCPToolsResponse struct {
	Tools  []MCPToolInventory `json:"tools"`
	Count  int                `json:"count"`
	Source string             `json:"source"`
}

type MCPToolInventory struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	Description string         `json:"description"`
	Source      string         `json:"source"`
	Schema      map[string]any `json:"schema"`
}

// SkillsListHandler returns a handler for GET /api/skills.
// Lists all registered skills with their schemas.
func SkillsListHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Discover skills
		manifests, err := skill.Discover(cfg.Paths.Skills)
		if err != nil {
			log.Error().Err(err).Str("path", cfg.Paths.Skills).Msg("failed to discover skills")
			httpError(w, http.StatusInternalServerError, "failed to discover skills")
			return
		}

		// Convert to response format with JSON schemas
		skills := make([]SkillResponse, 0, len(manifests))
		for _, m := range manifests {
			resp := SkillResponse{
				Name:        m.Metadata.Name,
				Version:     m.Metadata.Version,
				Description: m.Metadata.Description,
				Tags:        m.Metadata.Tags,
				Command:     m.Signature.Command,
				Parameters:  m.Signature.Parameters,
				Returns:     m.Signature.Returns,
				Help:        m.Signature.Help,
				JSONSchema:  buildJSONSchema(m),
			}
			skills = append(skills, resp)
		}

		writeJSON(w, http.StatusOK, SkillsListResponse{
			Skills: skills,
			Count:  len(skills),
		})
	}
}

// SkillsRunHandler returns a handler for POST /api/skills/run.
//
// Index:
//
//	Purpose: Execute a skill and return its result
//	Flow: validate method → decode request → validate skill → run skill → emit event → respond
//	Related: NewSkillRunner, SkillRunner.Run, observability.Emit, readJSON
//	Keywords: skills/run, skill, input, output, duration_ms, skill.run, skill_name, ok
//
// [[protocol:http-skill-api]]
// [[domain:skill-execution-boundary]]
func SkillsRunHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	runner := NewSkillRunner(cfg)

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var req SkillRunRequest
		if err := readJSON(w, r, &req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid json")
			return
		}

		if req.Skill == "" {
			httpError(w, http.StatusBadRequest, "skill name required")
			return
		}

		log.Info().Str("skill", req.Skill).Msg("skill run requested")

		// Emit skill start event
		startTime := time.Now()
		skillEvent := observability.NewEvent("skill.run").
			WithComponent(observability.ComponentWeb).
			WithData("skill_name", req.Skill)

		// Execute skill
		result, err := runner.Run(r.Context(), req.Skill, req.Input)
		duration := time.Since(startTime)

		if err != nil {
			log.Error().Err(err).Str("skill", req.Skill).Msg("skill execution error")
			observability.Emit(r.Context(), skillEvent.Error(err, duration))
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Emit result event
		if result.Success {
			observability.Emit(r.Context(), skillEvent.Success(duration))
		} else {
			observability.Emit(r.Context(), skillEvent.
				WithData("error", result.Error).
				Error(nil, duration))
		}

		writeJSON(w, http.StatusOK, SkillRunResponse{
			OK:         result.Success,
			Skill:      req.Skill,
			Output:     result.Output,
			Error:      result.Error,
			DurationMS: result.Duration.Milliseconds(),
		})
	}
}

// buildJSONSchema converts skill parameters to OpenAI-compatible JSON Schema.
func buildJSONSchema(m skill.Manifest) map[string]any {
	schema := map[string]any{
		"type": "object",
	}

	if len(m.Signature.Parameters) == 0 {
		return schema
	}

	properties := make(map[string]any)
	var required []string

	for _, p := range m.Signature.Parameters {
		prop := parameterToSchema(p)
		properties[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}

	schema["properties"] = properties
	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// parameterToSchema converts a skill Parameter to JSON Schema format.
func parameterToSchema(p skill.Parameter) map[string]any {
	schema := make(map[string]any)

	// Map skill types to JSON Schema types
	switch p.Type {
	case "string":
		schema["type"] = "string"
	case "integer", "int":
		schema["type"] = "integer"
	case "number", "float":
		schema["type"] = "number"
	case "boolean", "bool":
		schema["type"] = "boolean"
	case "array":
		schema["type"] = "array"
		if p.Items != nil {
			schema["items"] = parameterToSchema(*p.Items)
		}
	case "object":
		schema["type"] = "object"
		if len(p.Properties) > 0 {
			props := make(map[string]any)
			for name, prop := range p.Properties {
				props[name] = parameterToSchema(prop)
			}
			schema["properties"] = props
		}
	default:
		schema["type"] = "string" // default fallback
	}

	if p.Description != "" {
		schema["description"] = p.Description
	}

	if len(p.Enum) > 0 {
		schema["enum"] = p.Enum
	}

	if p.Default != nil {
		schema["default"] = p.Default
	}

	return schema
}

// SkillDetailHandler returns a handler for GET /api/skills/manifest/{name}.
// Get detailed info about a specific skill without affecting /api/skills CRUD routes.
func SkillDetailHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		const prefix = "/api/skills/manifest/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			httpError(w, http.StatusBadRequest, "invalid path")
			return
		}
		skillName := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, prefix))
		if skillName == "" {
			httpError(w, http.StatusBadRequest, "missing skill name")
			return
		}

		// Discover and find the specific skill
		manifests, err := skill.Discover(cfg.Paths.Skills)
		if err != nil {
			log.Error().Err(err).Msg("failed to discover skills")
			httpError(w, http.StatusInternalServerError, "failed to discover skills")
			return
		}

		for _, m := range manifests {
			if m.Metadata.Name == skillName {
				// Return full manifest as JSON
				resp := SkillManifestResponse{
					Name:         m.Metadata.Name,
					Version:      m.Metadata.Version,
					Description:  m.Metadata.Description,
					Tags:         m.Metadata.Tags,
					Command:      m.Signature.Command,
					Parameters:   m.Signature.Parameters,
					Returns:      m.Signature.Returns,
					Help:         m.Signature.Help,
					Capabilities: m.Capabilities,
					JSONSchema:   buildJSONSchema(m),
				}
				writeJSON(w, http.StatusOK, resp)
				return
			}
		}

		httpError(w, http.StatusNotFound, "skill not found")
	}
}

// MCPStatusHandler returns lightweight read-only MCP daemon + backend status.
func MCPStatusHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		pidFile := expandUserPath(mcpDefaultPIDFile)
		pid, addr, ok := readMCPPIDFile(pidFile)
		if strings.TrimSpace(addr) == "" {
			addr = mcpDefaultAddr
		}
		healthURL := fmt.Sprintf("http://localhost%s/health", extractMCPPort(addr))

		running := false
		var health map[string]any
		var healthErr string
		if ok {
			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Get(healthURL)
			if err != nil {
				healthErr = err.Error()
			} else {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					running = true
					_ = json.NewDecoder(resp.Body).Decode(&health)
				} else {
					healthErr = fmt.Sprintf("health check failed: http %d", resp.StatusCode)
				}
			}
		}

		writeJSON(w, http.StatusOK, MCPStatusResponse{
			Daemon: MCPDaemonStatus{
				Running:   running,
				PID:       pid,
				Addr:      addr,
				PIDFile:   pidFile,
				HealthURL: healthURL,
				Health:    health,
				Error:     healthErr,
			},
			Backends: []MCPBackendStatus{
				{Name: "tavily", Configured: strings.TrimSpace(cfg.Search.TavilyAPIKey) != ""},
				{Name: "exa", Configured: strings.TrimSpace(cfg.Search.ExaAPIKey) != ""},
				{Name: "perplexity", Configured: strings.TrimSpace(cfg.Search.PerplexityAPIKey) != ""},
			},
		})
	}
}

// MCPToolsHandler returns a read-only skill-backed MCP tool inventory.
func MCPToolsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		manifests, err := skill.Discover(cfg.Paths.Skills)
		if err != nil {
			log.Error().Err(err).Msg("failed to discover skills for mcp tools facade")
			httpError(w, http.StatusInternalServerError, "failed to discover skills")
			return
		}

		tools := make([]MCPToolInventory, 0, len(manifests))
		for _, m := range manifests {
			tools = append(tools, MCPToolInventory{
				Name:        m.Signature.Command,
				DisplayName: m.Metadata.Name,
				Description: m.Metadata.Description,
				Source:      "skill",
				Schema:      buildJSONSchema(m),
			})
		}

		writeJSON(w, http.StatusOK, MCPToolsResponse{
			Tools:  tools,
			Count:  len(tools),
			Source: "skills",
		})
	}
}

func expandUserPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func extractMCPPort(addr string) string {
	if idx := strings.LastIndex(addr, ":"); idx >= 0 {
		return addr[idx:]
	}
	return addr
}

func readMCPPIDFile(path string) (pid int, addr string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", false
	}
	lines := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
	if len(lines) == 0 {
		return 0, "", false
	}
	parsedPID, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return 0, "", false
	}
	parsedAddr := ""
	if len(lines) > 1 {
		parsedAddr = strings.TrimSpace(lines[1])
	}
	return parsedPID, parsedAddr, true
}

// SkillsSchemaHandler returns OpenAI-compatible tool definitions.
// GET /api/skills/schema returns all skills as OpenAI function definitions.
func SkillsSchemaHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		manifests, err := skill.Discover(cfg.Paths.Skills)
		if err != nil {
			log.Error().Err(err).Msg("failed to discover skills")
			httpError(w, http.StatusInternalServerError, "failed to discover skills")
			return
		}

		// Build OpenAI-compatible function definitions
		tools := make([]map[string]any, 0, len(manifests))
		for _, m := range manifests {
			tool := map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        m.Signature.Command,
					"description": m.Metadata.Description,
					"parameters":  buildJSONSchema(m),
				},
			}
			tools = append(tools, tool)
		}

		// Return as raw JSON array for direct use
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(tools)
	}
}
