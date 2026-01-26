package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

// OpenAPISpec generates an OpenAPI 3.0.3 specification from skill manifests.
// Only skills with openapi.enabled: true are included.
// Routes are generated based on openapi.methods configuration.
// Output is deterministic - manifests are sorted by command name.
func OpenAPISpec(manifests []skill.Manifest, serverURL string) map[string]any {
	paths := make(map[string]any)

	// Sort manifests by command name for deterministic output
	sortedManifests := make([]skill.Manifest, len(manifests))
	copy(sortedManifests, manifests)
	sort.Slice(sortedManifests, func(i, j int) bool {
		return sortedManifests[i].Signature.Command < sortedManifests[j].Signature.Command
	})

	for _, m := range sortedManifests {
		// Skip skills without OpenAPI enabled
		if m.OpenAPI == nil || !m.OpenAPI.Enabled {
			continue
		}

		basePath := "/api/skills/" + m.Signature.Command
		if m.OpenAPI.BasePath != "" {
			basePath = m.OpenAPI.BasePath
		}

		operationBase := strings.ReplaceAll(m.Signature.Command, "/", "_")
		idParam := m.OpenAPI.GetIDParam()

		// Common response definitions
		successResponse := map[string]any{
			"description": "Skill executed successfully",
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": buildReturnsSchema(m),
				},
			},
		}
		errorResponses := map[string]any{
			"400": map[string]any{
				"description": "Invalid input",
				"content":     map[string]any{"application/json": map[string]any{"schema": errorSchema()}},
			},
			"404": map[string]any{
				"description": "Resource not found",
				"content":     map[string]any{"application/json": map[string]any{"schema": errorSchema()}},
			},
			"500": map[string]any{
				"description": "Skill execution failed",
				"content":     map[string]any{"application/json": map[string]any{"schema": errorSchema()}},
			},
		}

		tags := make([]string, len(m.Metadata.Tags))
		copy(tags, m.Metadata.Tags)
		if len(tags) == 0 {
			if idx := strings.Index(m.Signature.Command, "/"); idx > 0 {
				tags = []string{m.Signature.Command[:idx]}
			}
		}
		// Sort tags for deterministic output
		sort.Strings(tags)

		// Build operations based on configured methods
		collectionOps := make(map[string]any)
		resourceOps := make(map[string]any)

		// Sort method keys for deterministic iteration
		methodKeys := make([]string, 0, len(m.OpenAPI.Methods))
		for method := range m.OpenAPI.Methods {
			methodKeys = append(methodKeys, method)
		}
		sort.Strings(methodKeys)

		for _, method := range methodKeys {
			opValue := m.OpenAPI.Methods[method]
			methodLower := strings.ToLower(method)

			switch methodLower {
			case "get":
				// GET on collection = list
				collectionOps["get"] = map[string]any{
					"operationId": operationBase + "_list",
					"summary":     "List " + m.Metadata.Name,
					"description": "Maps to operation=" + opValue,
					"tags":        tags,
					"parameters":  buildQueryParams(m),
					"responses":   mergeResponses(successResponse, errorResponses),
				}
				// GET on resource = get by ID (if different operation exists)
				if getOp := m.OpenAPI.Methods["GET_ID"]; getOp != "" {
					resourceOps["get"] = buildResourceOp(operationBase+"_get", "Get "+m.Metadata.Name+" by ID",
						"Maps to operation="+getOp, tags, idParam, nil, successResponse, errorResponses)
				}

			case "get_id":
				// Explicit GET with ID (skip, handled above or standalone)
				resourceOps["get"] = buildResourceOp(operationBase+"_get", "Get "+m.Metadata.Name+" by ID",
					"Maps to operation="+opValue, tags, idParam, nil, successResponse, errorResponses)

			case "post":
				collectionOps["post"] = map[string]any{
					"operationId": operationBase + "_create",
					"summary":     "Create " + m.Metadata.Name,
					"description": buildOperationDescription(m) + "\n\nMaps to operation=" + opValue,
					"tags":        tags,
					"requestBody": map[string]any{
						"required": hasRequiredParams(m),
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": buildJSONSchema(m),
							},
						},
					},
					"responses": mergeResponses(successResponse, errorResponses),
				}

			case "put":
				resourceOps["put"] = buildResourceOp(operationBase+"_update", "Update "+m.Metadata.Name,
					"Maps to operation="+opValue, tags, idParam, buildJSONSchema(m), successResponse, errorResponses)

			case "patch":
				resourceOps["patch"] = buildResourceOp(operationBase+"_patch", "Patch "+m.Metadata.Name,
					"Maps to operation="+opValue, tags, idParam, buildJSONSchema(m), successResponse, errorResponses)

			case "delete":
				resourceOps["delete"] = buildResourceOp(operationBase+"_delete", "Delete "+m.Metadata.Name,
					"Maps to operation="+opValue, tags, idParam, nil, successResponse, errorResponses)
			}
		}

		// Add paths if they have operations
		if len(collectionOps) > 0 {
			paths[basePath] = collectionOps
		}
		if len(resourceOps) > 0 {
			idPath := basePath + "/{" + idParam + "}"
			paths[idPath] = resourceOps
		}
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "agentctl Skills API",
			"description": "REST API for agentctl skills. Only skills with openapi.enabled: true in their skill.yaml are exposed.",
			"version":     "1.0.0",
		},
		"servers": []map[string]any{
			{"url": serverURL, "description": "agentctl server"},
		},
		"paths": paths,
	}
}

// buildResourceOp creates an OpenAPI operation for a resource endpoint (with ID param).
func buildResourceOp(opID, summary, description string, tags []string, idParam string, schema map[string]any, success, errors map[string]any) map[string]any {
	op := map[string]any{
		"operationId": opID,
		"summary":     summary,
		"description": description,
		"tags":        tags,
		"parameters": []map[string]any{
			{
				"name":        idParam,
				"in":          "path",
				"required":    true,
				"description": "Resource identifier",
				"schema":      map[string]any{"type": "string"},
			},
		},
		"responses": mergeResponses(success, errors),
	}
	if schema != nil {
		op["requestBody"] = map[string]any{
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": schema,
				},
			},
		}
	}
	return op
}

// buildQueryParams converts non-required skill parameters to OpenAPI query parameters.
func buildQueryParams(m skill.Manifest) []map[string]any {
	var params []map[string]any
	for _, p := range m.Signature.Parameters {
		// Skip complex types for query params
		if p.Type == "object" || p.Type == "array" {
			continue
		}
		param := map[string]any{
			"name":        p.Name,
			"in":          "query",
			"required":    false,
			"description": p.Description,
			"schema":      parameterToSchema(p),
		}
		params = append(params, param)
	}
	return params
}

// mergeResponses combines success and error responses.
func mergeResponses(success map[string]any, errors map[string]any) map[string]any {
	result := map[string]any{"200": success}
	for k, v := range errors {
		result[k] = v
	}
	return result
}

// buildOperationDescription creates a detailed description from help text.
func buildOperationDescription(m skill.Manifest) string {
	if m.Signature.Help == nil {
		return m.Metadata.Description
	}
	if m.Signature.Help.Long != "" {
		return m.Signature.Help.Long
	}
	if m.Signature.Help.Short != "" {
		return m.Signature.Help.Short
	}
	return m.Metadata.Description
}

// hasRequiredParams checks if any parameters are required.
func hasRequiredParams(m skill.Manifest) bool {
	for _, p := range m.Signature.Parameters {
		if p.Required {
			return true
		}
	}
	return false
}

// buildReturnsSchema converts skill returns to JSON Schema.
func buildReturnsSchema(m skill.Manifest) map[string]any {
	if len(m.Signature.Returns) == 0 {
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ok": map[string]any{
					"type":        "boolean",
					"description": "Whether the skill executed successfully",
				},
				"output": map[string]any{
					"type":        "object",
					"description": "Skill output data",
				},
				"duration_ms": map[string]any{
					"type":        "integer",
					"description": "Execution time in milliseconds",
				},
			},
		}
	}

	properties := make(map[string]any)
	for _, r := range m.Signature.Returns {
		properties[r.Name] = parameterToSchema(r)
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok": map[string]any{
				"type": "boolean",
			},
			"skill": map[string]any{
				"type": "string",
			},
			"output": map[string]any{
				"type":       "object",
				"properties": properties,
			},
			"duration_ms": map[string]any{
				"type": "integer",
			},
		},
	}
}

// errorSchema returns the standard error response schema.
func errorSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ok": map[string]any{
				"type":    "boolean",
				"example": false,
			},
			"error": map[string]any{
				"type":        "string",
				"description": "Error message",
			},
		},
		"required": []string{"ok", "error"},
	}
}

// OpenAPIHandler returns a handler for GET /api/openapi.json.
func OpenAPIHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		// Discover skills
		manifests, err := skill.Discover(cfg.Paths.Skills)
		if err != nil {
			log.Error().Err(err).Msg("failed to discover skills for OpenAPI spec")
			httpError(w, http.StatusInternalServerError, "failed to discover skills")
			return
		}

		// Determine server URL from request
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		// Validate X-Forwarded-Proto header against allowlist
		if fwdProto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); fwdProto != "" {
			switch fwdProto {
			case "http", "https":
				scheme = fwdProto
			// Ignore invalid values - keep original scheme
			}
		}
		serverURL := scheme + "://" + r.Host

		spec := OpenAPISpec(manifests, serverURL)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(spec)
	}
}

// SwaggerUIHandler serves an embedded Swagger UI page.
func SwaggerUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(swaggerUIHTML))
	}
}

const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>agentctl API - Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    body { margin: 0; padding: 0; }
    .swagger-ui .topbar { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = () => {
      SwaggerUIBundle({
        url: '/api/openapi.json',
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [SwaggerUIBundle.presets.apis],
        layout: "BaseLayout"
      });
    };
  </script>
</body>
</html>`

// SkillsCRUDHandler handles HTTP methods for /api/skills/{command...}
// Only skills with openapi.enabled: true accept REST calls.
// HTTP methods are mapped to operations based on openapi.methods config.
func SkillsCRUDHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	runner := NewSkillRunner(cfg)

	return func(w http.ResponseWriter, r *http.Request) {
		// Extract skill name and optional ID from path
		const prefix = "/api/skills/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			httpError(w, http.StatusBadRequest, "invalid path")
			return
		}
		pathAfterPrefix := strings.TrimPrefix(r.URL.Path, prefix)

		// Skip reserved endpoints
		switch pathAfterPrefix {
		case "", "run", "schema":
			httpError(w, http.StatusBadRequest, "use specific endpoint")
			return
		}

		// Parse skill name (first two segments) and optional ID (third segment)
		parts := strings.Split(pathAfterPrefix, "/")
		var skillName, resourceID string
		if len(parts) >= 2 {
			skillName = parts[0] + "/" + parts[1]
			if len(parts) >= 3 {
				resourceID = strings.Join(parts[2:], "/")
			}
		} else {
			skillName = pathAfterPrefix
		}

		// Resolve skill to check OpenAPI config
		handle, err := runner.Resolve(skillName)
		if err != nil {
			log.Error().Err(err).Str("skill", skillName).Msg("skill not found")
			httpError(w, http.StatusNotFound, err.Error())
			return
		}

		// Check if skill has OpenAPI enabled
		openAPICfg := handle.Manifest.OpenAPI
		if openAPICfg == nil || !openAPICfg.Enabled {
			httpError(w, http.StatusForbidden, "skill does not have REST API enabled")
			return
		}

		// Determine the method key for lookup
		methodKey := r.Method
		if resourceID != "" && r.Method == http.MethodGet {
			// Try GET_ID first, fall back to GET
			if openAPICfg.Methods["GET_ID"] != "" {
				methodKey = "GET_ID"
			}
		}

		// Check if this method is supported
		operation := openAPICfg.OperationForMethod(methodKey)
		if operation == "" {
			// Try without _ID suffix
			operation = openAPICfg.OperationForMethod(r.Method)
		}
		if operation == "" {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed for this skill")
			return
		}

		// Build input from body (for POST/PUT/PATCH) or empty
		var input map[string]any
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			if err := readJSON(r, &input); err != nil {
				// Return 400 Bad Request for JSON parse errors
				httpError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
				return
			}
			if input == nil {
				input = make(map[string]any)
			}
		} else {
			input = make(map[string]any)
		}

		// Merge query parameters into input
		for key, values := range r.URL.Query() {
			if len(values) == 1 {
				input[key] = values[0]
			} else {
				input[key] = values
			}
		}

		// Validate resourceID is required for resource-scoped operations (PUT/PATCH/DELETE or GET_ID)
		isResourceScoped := r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete || methodKey == "GET_ID"
		if isResourceScoped && resourceID == "" {
			httpError(w, http.StatusBadRequest, "resource ID required for this operation")
			return
		}

		// Always set operation from manifest config - clients must not override
		if operation != "true" {
			input["operation"] = operation
		}

		// Add resource ID if present
		if resourceID != "" {
			idParam := openAPICfg.GetIDParam()
			input[idParam] = resourceID
		}

		log.Info().
			Str("method", r.Method).
			Str("skill", skillName).
			Str("operation", operation).
			Str("id", resourceID).
			Msg("skill REST request")

		// Execute skill
		result, err := runner.Run(r.Context(), skillName, input)
		if err != nil {
			log.Error().Err(err).Str("skill", skillName).Msg("skill execution error")
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":            result.Success,
			"skill":         skillName,
			"skill_version": handle.Manifest.Metadata.Version,
			"output":        result.Output,
			"error":         result.Error,
			"duration_ms":   result.Duration.Milliseconds(),
		})
	}
}
