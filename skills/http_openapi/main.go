// Package main implements the http/openapi skill for invoking OpenAPI 3.x operations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	openapiauth "github.com/joshka0/foxctl/internal/interfaces/openapi/auth"
	"github.com/joshka0/foxctl/internal/interfaces/openapi/builder"
	"github.com/joshka0/foxctl/internal/interfaces/openapi/client"
	"github.com/joshka0/foxctl/internal/interfaces/openapi/loader"
	"github.com/joshka0/foxctl/internal/interfaces/openapi/pagination"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/platform/secrets"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

// Input defines the parameters for OpenAPI operation execution.
type Input struct {
	Spec        string             `json:"spec"`
	OperationID string             `json:"operationId"`
	Params      builder.Params     `json:"params"`
	Auth        openapiauth.Config `json:"auth"`
	Paging      *PagingConfig      `json:"paging"`
	Retry       *RetryConfig       `json:"retry"`
	DryRun      bool               `json:"dry_run"`
}

// PagingConfig configures pagination behavior for OpenAPI operations.
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

// RetryConfig configures retry behavior for HTTP requests.
type RetryConfig struct {
	BaseMS      int     `json:"base_ms"`
	Factor      float64 `json:"factor"`
	MaxAttempts int     `json:"max_attempts"`
	MaxMS       int     `json:"max_ms"`
}

const command = "http/openapi"

// main is the skill entry point for http/openapi.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates OpenAPI operation execution with pagination, retry, and authentication support.
//
// Index:
// - Purpose: Invoke OpenAPI 3.x operations with authentication, pagination, retry, and dry-run capabilities
// - Flow: validate input → load spec → build request → apply auth → execute with pagination/retry → emit response
// - SideEffects: HTTP requests; memory store usage; secret redaction; artifact storage for large responses
// - FailureModes: spec loading failures, operation not found, authentication errors, HTTP failures
// - Observability: emits request/response details, pagination summaries, retry attempts, and error hints
// - Related: executeWithPagination, emitDryRun, emitResponse, wrapOpenAPIError, suggestOperations
// - Keywords: http/openapi, openapi_execution, http_client, pagination, authentication, retry_logic
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate input
	if in.Spec == "" {
		return skillerr.Arg(
			"spec is required",
			skillerr.WithHint("Provide a spec path, URL, or memory: reference."),
		)
	}
	if in.OperationID == "" {
		return skillerr.Arg(
			"operationId is required",
			skillerr.WithHint("Use `foxctl openapi describe <spec>` to list available operations."),
		)
	}

	// Initialize memory store if memory: references are used
	var memStore storage.MemoryStore
	if strings.HasPrefix(in.Spec, "memory:") {
		var err error
		memStore, err = memory.OpenWithConfig(ctx, rc.Config)
		if err != nil {
			return skillerr.WrapIO("failed to open memory store", err)
		}
		defer func() { errs.Ignore(memStore.Close(), "close memory store") }()
	}

	// Load OpenAPI spec
	workspace := workspaceutil.ResolveID("", rc.Workspace)

	ldr := loader.New(rc.CASStore, memStore, loader.WithWorkspace(workspace))
	spec, err := ldr.Load(ctx, in.Spec)
	if err != nil {
		hint := "Verify the spec path exists and is a valid OpenAPI 3.0+ specification"
		if strings.HasPrefix(in.Spec, "memory:") {
			hint = fmt.Sprintf("Memory reference not found. Use 'foxctl openapi import <spec> --as %s' to import it first", strings.TrimPrefix(in.Spec, "memory:"))
		} else if strings.HasPrefix(in.Spec, "http://") || strings.HasPrefix(in.Spec, "https://") {
			hint = "Check the URL is accessible and returns a valid OpenAPI specification"
		}
		return skillerr.Runtime(
			fmt.Sprintf("failed to load spec %q: %v", in.Spec, err),
			skillerr.WithCause(err),
			skillerr.WithHint(hint),
		)
	}

	// Check if operation exists and provide helpful error
	if _, err := spec.GetOperation(in.OperationID); err != nil {
		available := suggestOperations(spec, in.OperationID)
		hint := fmt.Sprintf("Operation %q not found. Available operations: %s. Use 'foxctl openapi describe %s' to list all operations",
			in.OperationID, available, in.Spec)
		return skillerr.NotFound(hint, skillerr.WithCause(fmt.Errorf("operation not found: %s", in.OperationID)))
	}

	// Build request from operation
	bldr := builder.New(spec)
	builtReq, err := bldr.Build(in.OperationID, in.Params)
	if err != nil {
		// Enhance error message with suggestions
		hint := generateBuildHint(err, spec, in.OperationID)
		return skillerr.Arg(
			fmt.Sprintf("failed to build request: %v", err),
			skillerr.WithCause(err),
			skillerr.WithHint(hint),
		)
	}

	// Convert to http.Request
	req, err := builtReq.ToHTTPRequest()
	if err != nil {
		return skillerr.Arg(
			fmt.Sprintf("failed to create HTTP request: %v", err),
			skillerr.WithCause(err),
		)
	}

	// Apply authentication
	if err := openapiauth.Apply(req, in.Auth); err != nil {
		return skillerr.Auth(
			fmt.Sprintf("failed to apply authentication: %v", err),
			skillerr.WithCause(err),
		)
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
		return wrapOpenAPIError(err)
	}

	return emitResponse(rc, response, nil)
}

// executeWithPagination handles paginated OpenAPI operations with response aggregation.
func executeWithPagination(ctx context.Context, rc *skillmain.RunContext, req *http.Request, httpClient *client.Client, pagingCfg *PagingConfig) error {
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
		return skillerr.Arg(
			fmt.Sprintf("failed to create paginator: %v", err),
			skillerr.WithCause(err),
		)
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
			return wrapOpenAPIError(err)
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
				return skillerr.WrapRuntime("failed to convert response body to bytes for pagination", err)
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
			return skillerr.WrapRuntime("pagination error", err)
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
		return skillerr.Runtime(
			"no pages fetched",
			skillerr.WithHint("Check page size/offset or upstream pagination response."),
		)
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

// wrapOpenAPIError converts client errors to appropriate skill error types.
func wrapOpenAPIError(err error) error {
	if err == nil {
		return nil
	}
	var openErr *client.Error
	if !errors.As(err, &openErr) {
		return err
	}
	message := openErr.Message
	if message == "" && openErr.Err != nil {
		message = openErr.Err.Error()
	}
	switch openErr.Code {
	case "EARG":
		return skillerr.Arg(message, skillerr.WithCause(openErr.Err))
	case "EAUTH":
		return skillerr.Auth(message, skillerr.WithCause(openErr.Err))
	default:
		return skillerr.Runtime(message, skillerr.WithCause(openErr.Err))
	}
}

// convertHeaders converts string map to http.Header format.
func convertHeaders(headers map[string]string) http.Header {
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return h
}

// aggregateResponses combines multiple paginated responses into a single result.
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

// emitDryRun outputs the request plan without executing the HTTP request.
func emitDryRun(rc *skillmain.RunContext, req *builder.Request, in Input) error {
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

	return skillout.Emit(rc, command, data)
}

// emitResponse outputs the HTTP response with optional pagination summary.
func emitResponse(rc *skillmain.RunContext, resp *client.Response, pagingSummary *pagination.Summary) error {
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

	return skillout.Emit(rc, command, data)
}

// generateHint provides helpful error messages based on error codes and status.
func generateHint(code string, statusCode int) string {
	switch code {
	case "EAUTH":
		if statusCode == 401 {
			return "Authentication failed. Check your credentials (token, API key, or username/password). Set environment variables like FOXCTL_BEARER_TOKEN or pass credentials in the auth parameter."
		}
		if statusCode == 403 {
			return "Authorization failed. You may not have permission to access this resource. Verify your API key has the required scopes."
		}
		return "Authentication error. Verify your credentials are correct and not expired."
	case "EOPENAPI":
		return "Failed to load or parse OpenAPI specification. Verify the spec path or memory reference is correct. Use 'foxctl openapi validate <spec>' to check for errors."
	case "EARG":
		return "Invalid parameters. Check that all required parameters are provided and have correct types. Use 'foxctl openapi describe <spec>' to see parameter requirements."
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

// generateBuildHint provides specific hints for request building errors.
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

	return "Review the operation parameters in the OpenAPI spec or use 'foxctl openapi describe <spec>' for details."
}

// suggestOperations suggests similar operation IDs when a requested operation is not found.
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
