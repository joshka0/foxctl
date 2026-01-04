// Package main implements the http/openapi skill for invoking OpenAPI 3.x operations.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	openapiauth "github.com/jkatigb/agentctl/internal/openapi/auth"
	"github.com/jkatigb/agentctl/internal/openapi/builder"
	"github.com/jkatigb/agentctl/internal/openapi/client"
	"github.com/jkatigb/agentctl/internal/openapi/loader"
	"github.com/jkatigb/agentctl/internal/openapi/pagination"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/secrets"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// Input matches the OpenAPI skill specification.
type Input struct {
	Spec        string             `json:"spec"`
	OperationID string             `json:"operationId"`
	Params      builder.Params     `json:"params"`
	Auth        openapiauth.Config `json:"auth"`
	Paging      *PagingConfig      `json:"paging"`
	Retry       *RetryConfig       `json:"retry"`
	DryRun      bool               `json:"dry_run"`
}

// PagingConfig configures pagination behavior.
type PagingConfig struct {
	Strategy     string `json:"strategy"`
	MaxPages     int    `json:"max_pages"`
	MaxItems     int    `json:"max_items"`
	CursorField  string `json:"cursor_field"`
	CursorParam  string `json:"cursor_param"`
	OffsetParam  string `json:"offset_param"`
	LimitParam   string `json:"limit_param"`
	PageParam    string `json:"page_param"`
	PerPageParam string `json:"per_page_param"`
}

// RetryConfig configures retry behavior.
type RetryConfig struct {
	BaseMS      int     `json:"base_ms"`
	Factor      float64 `json:"factor"`
	MaxAttempts int     `json:"max_attempts"`
	MaxMS       int     `json:"max_ms"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("http/openapi", "ERUNTIME", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("http/openapi", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("http/openapi", "EARG", fmt.Errorf("decode input: %w", err))
	}
	if err := run(ctx, rc, in); err != nil {
		if openapiErr, ok := err.(*client.Error); ok {
			failWithHint("http/openapi", openapiErr.Code, openapiErr.Message, openapiErr.Response)
		}
		fail("http/openapi", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in Input) error {
	// Validate input
	if in.Spec == "" {
		return &client.Error{
			Code:    "EARG",
			Message: "spec is required (provide a spec path, URL, or memory: reference)",
		}
	}
	if in.OperationID == "" {
		return &client.Error{
			Code:    "EARG",
			Message: "operationId is required (use 'agentctl openapi describe <spec>' to list available operations)",
		}
	}

	// Initialize memory store if memory: references are used
	var memStore storage.MemoryStore
	if strings.HasPrefix(in.Spec, "memory:") {
		var err error
		memStore, err = memory.Open(ctx, rc.Config.Storage.Root, rc.Config.Paths.CAS)
		if err != nil {
			return &client.Error{
				Code:    "ERUNTIME",
				Message: fmt.Sprintf("failed to open memory store: %v", err),
				Err:     err,
			}
		}
		defer func() { errs.Ignore(memStore.Close(), "close memory store") }()
	}

	// Load OpenAPI spec
	workspace := os.Getenv("AGENTCTL_WORKSPACE")
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("determine workspace: %w", err)
		}
	}

	ldr := loader.New(rc.CASStore, memStore, loader.WithWorkspace(workspace))
	spec, err := ldr.Load(ctx, in.Spec)
	if err != nil {
		hint := "Verify the spec path exists and is a valid OpenAPI 3.0+ specification"
		if strings.HasPrefix(in.Spec, "memory:") {
			hint = fmt.Sprintf("Memory reference not found. Use 'agentctl openapi import <spec> --as %s' to import it first", strings.TrimPrefix(in.Spec, "memory:"))
		} else if strings.HasPrefix(in.Spec, "http://") || strings.HasPrefix(in.Spec, "https://") {
			hint = "Check the URL is accessible and returns a valid OpenAPI specification"
		}
		return &client.Error{
			Code:    "EOPENAPI",
			Message: fmt.Sprintf("failed to load spec %q: %v. %s", in.Spec, err, hint),
			Err:     err,
		}
	}

	// Check if operation exists and provide helpful error
	if _, err := spec.GetOperation(in.OperationID); err != nil {
		available := suggestOperations(spec, in.OperationID)
		hint := fmt.Sprintf("Operation %q not found. Available operations: %s. Use 'agentctl openapi describe %s' to list all operations",
			in.OperationID, available, in.Spec)
		return &client.Error{
			Code:    "EOPENAPI",
			Message: hint,
			Err:     fmt.Errorf("operation not found: %s", in.OperationID),
		}
	}

	// Build request from operation
	bldr := builder.New(spec)
	builtReq, err := bldr.Build(in.OperationID, in.Params)
	if err != nil {
		// Enhance error message with suggestions
		hint := generateBuildHint(err, spec, in.OperationID)
		return &client.Error{
			Code:    "EARG",
			Message: fmt.Sprintf("failed to build request: %v. %s", err, hint),
			Err:     err,
		}
	}

	// Convert to http.Request
	req, err := builtReq.ToHTTPRequest()
	if err != nil {
		return &client.Error{
			Code:    "EARG",
			Message: fmt.Sprintf("failed to create HTTP request: %v", err),
			Err:     err,
		}
	}

	// Apply authentication
	if err := openapiauth.Apply(req, in.Auth); err != nil {
		return &client.Error{
			Code:    "EAUTH",
			Message: fmt.Sprintf("failed to apply authentication: %v", err),
			Err:     err,
		}
	}

	// If dry-run, return request plan
	if in.DryRun {
		return emitDryRun(rc, builtReq, in)
	}

	// Execute request with optional pagination
	httpClient := client.New(rc.Config, rc.CASStore)

	// Check if pagination is needed
	if in.Paging != nil && in.Paging.Strategy != "none" {
		return executeWithPagination(ctx, rc, req, httpClient, in.Paging)
	}

	// Single request execution (retry logic is handled internally by client)
	response, err := httpClient.Execute(ctx, req)
	if err != nil {
		return err
	}

	return emitResponse(rc, response, nil)
}

func executeWithPagination(ctx context.Context, rc *runner.RunnerContext, req *http.Request, httpClient *client.Client, pagingCfg *PagingConfig) error {
	// Create paginator
	paginator, err := pagination.New(pagination.Config{
		Strategy:     pagination.Strategy(pagingCfg.Strategy),
		MaxPages:     pagingCfg.MaxPages,
		MaxRecords:   pagingCfg.MaxItems,
		CursorField:  pagingCfg.CursorField,
		CursorParam:  pagingCfg.CursorParam,
		OffsetParam:  pagingCfg.OffsetParam,
		LimitParam:   pagingCfg.LimitParam,
		PageParam:    pagingCfg.PageParam,
		PerPageParam: pagingCfg.PerPageParam,
	})
	if err != nil {
		return &client.Error{
			Code:    "EPAGINATION",
			Message: fmt.Sprintf("failed to create paginator: %v", err),
			Err:     err,
		}
	}

	var allResponses []*client.Response
	var allBodies []any
	currentReq := req

	for {
		// Execute current page (retry logic handled internally by client)
		pageResponse, err := httpClient.Execute(ctx, currentReq)
		if err != nil {
			// Return partial results if we have any
			if len(allResponses) > 0 {
				aggregated := aggregateResponses(allBodies)
				combined := allResponses[0]
				combined.Body = aggregated
				partialSummary := paginator.Summary()
				return emitResponse(rc, combined, &partialSummary)
			}
			return err
		}

		if pageResponse == nil {
			break
		}

		// Store the response
		allResponses = append(allResponses, pageResponse)

		// Extract body for aggregation
		if pageResponse.Body != nil {
			allBodies = append(allBodies, pageResponse.Body)
		}

		// Create pagination response with defensive body conversion
		var bodyBytes []byte
		switch v := pageResponse.Body.(type) {
		case []byte:
			bodyBytes = v
		case string:
			bodyBytes = []byte(v)
		case nil:
			bodyBytes = nil
		default:
			// Best-effort JSON encoding for non-byte types
			if b, err := json.Marshal(v); err == nil {
				bodyBytes = b
			} else {
				return &client.Error{
					Code:    "EPAGINATION",
					Message: fmt.Sprintf("failed to convert response body to bytes for pagination: %v", err),
					Err:     err,
				}
			}
		}

		pagResp := &pagination.Response{
			Request: currentReq,
			Headers: convertHeaders(pageResponse.Headers),
			Body:    bodyBytes,
		}

		// Check if we should continue
		nextReq, done, err := paginator.ShouldContinue(pagResp)
		if err != nil {
			return &client.Error{
				Code:    "EPAGINATION",
				Message: fmt.Sprintf("pagination error: %v", err),
				Err:     err,
			}
		}

		if done {
			break
		}

		if nextReq == nil {
			break
		}

		currentReq = nextReq
	}

	// Aggregate all responses
	if len(allResponses) == 0 {
		return &client.Error{
			Code:    "EPAGINATION",
			Message: "no pages fetched (check page size/offset or upstream pagination response)",
		}
	}

	// Get pagination summary
	summary := paginator.Summary()

	// Aggregate bodies
	aggregated := aggregateResponses(allBodies)

	// Create combined response with first page metadata
	combined := allResponses[0]
	combined.Body = aggregated

	return emitResponse(rc, combined, &summary)
}

func convertHeaders(headers map[string]string) http.Header {
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return h
}

func aggregateResponses(bodies []any) any {
	if len(bodies) == 0 {
		return nil
	}

	if len(bodies) == 1 {
		return bodies[0]
	}

	// Check if all bodies are arrays
	var allArrays [][]any
	for _, body := range bodies {
		if arr, ok := body.([]any); ok {
			allArrays = append(allArrays, arr)
		} else {
			// Not all arrays, return as-is with page metadata
			return map[string]any{
				"pages": bodies,
			}
		}
	}

	// Concatenate all arrays
	if len(allArrays) == len(bodies) {
		var combined []any
		for _, arr := range allArrays {
			combined = append(combined, arr...)
		}
		return combined
	}

	return bodies
}

func emitDryRun(rc *runner.RunnerContext, req *builder.Request, in Input) error {
	// Redact sensitive headers
	headers := secrets.RedactHeaders(req.Headers)

	plan := map[string]any{
		"method":  req.Method,
		"url":     req.URL,
		"headers": headers,
		"body":    nil,
	}
	if len(req.Body) > 0 {
		var bodyJSON any
		if err := json.Unmarshal(req.Body, &bodyJSON); err == nil {
			plan["body"] = bodyJSON
		} else {
			plan["body"] = string(req.Body)
		}
	}

	retryConfig := map[string]any{
		"base_ms":      250,
		"factor":       2.0,
		"max_attempts": 5,
		"max_ms":       8000,
	}
	if in.Retry != nil {
		if in.Retry.BaseMS > 0 {
			retryConfig["base_ms"] = in.Retry.BaseMS
		}
		if in.Retry.Factor > 0 {
			retryConfig["factor"] = in.Retry.Factor
		}
		if in.Retry.MaxAttempts > 0 {
			retryConfig["max_attempts"] = in.Retry.MaxAttempts
		}
		if in.Retry.MaxMS > 0 {
			retryConfig["max_ms"] = in.Retry.MaxMS
		}
	}

	data := map[string]any{
		"summary": map[string]any{
			"request_plan": plan,
			"retry_config": retryConfig,
		},
	}

	return rc.Emit("http/openapi", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func emitResponse(rc *runner.RunnerContext, resp *client.Response, pagingSummary *pagination.Summary) error {
	summary := map[string]any{
		"status_code": resp.StatusCode,
		"headers":     secrets.RedactHeaders(resp.Headers),
	}

	// Add pagination summary if present
	if pagingSummary != nil {
		summary["pagination"] = map[string]any{
			"strategy":    string(pagingSummary.Strategy),
			"total_pages": pagingSummary.TotalPages,
			"total_items": pagingSummary.TotalItems,
			"has_more":    pagingSummary.HasMore,
			"truncated":   pagingSummary.Truncated,
		}
		if pagingSummary.CursorFinal != "" {
			summary["pagination"].(map[string]any)["cursor_final"] = pagingSummary.CursorFinal
		}
	}

	data := map[string]any{
		"summary": summary,
	}

	// If response has artifact (large response), include digest and preview
	if resp.Artifact != nil {
		data["artifact"] = resp.Digest
		summary["kind"] = resp.ContentType
		summary["size_bytes"] = resp.Size

		if resp.RecordCount > 0 {
			summary["record_count"] = resp.RecordCount
		}
		if resp.Preview.FirstKeys != nil || resp.Preview.SampleRecord != nil {
			summary["preview"] = resp.Preview
		}
	} else {
		// Inline response
		data["body"] = resp.Body
	}

	return rc.Emit("http/openapi", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit http/openapi failure")
	os.Exit(1)
}

func failWithHint(command, code, message string, resp *client.Response) {
	data := map[string]any{}
	if resp != nil {
		data["summary"] = map[string]any{
			"status_code": resp.StatusCode,
			"headers":     secrets.RedactHeaders(resp.Headers),
		}
		data["hint"] = generateHint(code, resp.StatusCode)
	}
	env := envelope.Error(command, code, message, data)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit http/openapi failure")
	os.Exit(1)
}

func generateHint(code string, statusCode int) string {
	switch code {
	case "EAUTH":
		if statusCode == 401 {
			return "Authentication failed. Check your credentials (token, API key, or username/password). Set environment variables like AGENTCTL_BEARER_TOKEN or pass credentials in the auth parameter."
		}
		if statusCode == 403 {
			return "Authorization failed. You may not have permission to access this resource. Verify your API key has the required scopes."
		}
		return "Authentication error. Verify your credentials are correct and not expired."
	case "EOPENAPI":
		return "Failed to load or parse OpenAPI specification. Verify the spec path or memory reference is correct. Use 'agentctl openapi validate <spec>' to check for errors."
	case "EARG":
		return "Invalid parameters. Check that all required parameters are provided and have correct types. Use 'agentctl openapi describe <spec>' to see parameter requirements."
	case "ERATELIMIT":
		return "Rate limit exceeded. Wait before retrying, or reduce request frequency. Check the X-RateLimit-Reset header for when limits reset."
	case "ERUNTIME":
		if statusCode >= 500 {
			return "Server error. The API may be experiencing issues. Try again later or check the API status page."
		}
		return "Request failed. Check network connectivity and API availability. Use --dry_run to validate the request first."
	case "EPAGINATION":
		return "Pagination error. Try specifying the strategy manually with --paging.strategy or check the API documentation for pagination format."
	default:
		return "An error occurred. Check the error message for details. Use --dry_run to preview the request."
	}
}

func generateBuildHint(err error, spec *loader.Spec, operationID string) string {
	errMsg := err.Error()

	// Missing required parameter
	if strings.Contains(errMsg, "missing required path parameter") {
		op, err := spec.GetOperation(operationID)
		if err == nil && op != nil {
			var requiredParams []string
			for _, param := range op.Parameters {
				if param.Value != nil && param.Value.Required && param.Value.In == "path" {
					requiredParams = append(requiredParams, param.Value.Name)
				}
			}
			if len(requiredParams) > 0 {
				return fmt.Sprintf("Required path parameters: %s. Example: {\"params\": {\"path\": {\"%s\": \"value\"}}}",
					strings.Join(requiredParams, ", "), requiredParams[0])
			}
		}
	}

	// Parameter not found in template
	if strings.Contains(errMsg, "not found in path template") {
		return "The parameter name does not match the path template. Check the OpenAPI spec for correct parameter names."
	}

	// Unresolved parameters
	if strings.Contains(errMsg, "unresolved path parameters") {
		return "Some path parameters were not provided. Check that all {param} placeholders have corresponding values."
	}

	return "Review the operation parameters in the OpenAPI spec or use 'agentctl openapi describe <spec>' for details."
}

func suggestOperations(spec *loader.Spec, attempted string) string {
	if spec == nil || len(spec.Operations) == 0 {
		return "none"
	}

	// Get all operation IDs
	var ids []string
	for id := range spec.Operations {
		ids = append(ids, id)
	}

	// If there are many operations, just show first few
	if len(ids) > 5 {
		// Try to find similar operations (fuzzy matching)
		var similar []string
		lower := strings.ToLower(attempted)
		for _, id := range ids {
			if strings.Contains(strings.ToLower(id), lower) || strings.Contains(lower, strings.ToLower(id)) {
				similar = append(similar, id)
				if len(similar) >= 5 {
					break
				}
			}
		}
		if len(similar) > 0 {
			return strings.Join(similar, ", ") + "..."
		}
		// Just show first 5
		return strings.Join(ids[:5], ", ") + fmt.Sprintf(" (and %d more)", len(ids)-5)
	}

	return strings.Join(ids, ", ")
}
