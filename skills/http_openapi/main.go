// Package main implements the http/openapi skill for invoking OpenAPI 3.x operations.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	openapiauth "github.com/jkatigb/agentctl/internal/openapi/auth"
	"github.com/jkatigb/agentctl/internal/openapi/builder"
	"github.com/jkatigb/agentctl/internal/openapi/client"
	"github.com/jkatigb/agentctl/internal/openapi/loader"
	"github.com/jkatigb/agentctl/internal/openapi/retry"
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
	Retry       *RetryConfig       `json:"retry"`
	DryRun      bool               `json:"dry_run"`
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
		fail("http/openapi", "ECONFIG", err)
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
		return fmt.Errorf("spec is required")
	}
	if in.OperationID == "" {
		return fmt.Errorf("operationId is required")
	}

	// Initialize memory store if memory: references are used
	var memStore storage.MemoryStore
	if strings.HasPrefix(in.Spec, "memory:") {
		var err error
		memStore, err = memory.Open(ctx, rc.Config.Paths.Memory, rc.Config.Paths.CAS)
		if err != nil {
			return &client.Error{
				Code:    "ERUNTIME",
				Message: fmt.Sprintf("failed to open memory store: %v", err),
				Err:     err,
			}
		}
		defer errs.Ignore(memStore.Close(), "close memory store")
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
		return &client.Error{
			Code:    "EOPENAPI",
			Message: fmt.Sprintf("failed to load spec %q: %v", in.Spec, err),
			Err:     err,
		}
	}

	// Build request from operation
	bldr := builder.New(spec)
	builtReq, err := bldr.Build(in.OperationID, in.Params)
	if err != nil {
		return &client.Error{
			Code:    "EARG",
			Message: fmt.Sprintf("failed to build request: %v", err),
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

	// Execute request with retry
	httpClient := client.New(rc.Config, rc.CASStore)
	retryer := createRetryer(in.Retry)

	var response *client.Response
	_, err = retryer.Execute(ctx, func() (*http.Response, error) {
		// Execute the HTTP request
		resp, err := httpClient.Execute(ctx, req)
		if err != nil {
			return nil, err
		}
		response = resp

		// Return nil http.Response since we already captured what we need
		// The retry logic will check the status code from the response
		return nil, nil
	})

	if err != nil {
		return err
	}

	// If we got a response, process it
	if response != nil {
		return emitResponse(rc, response)
	}

	return fmt.Errorf("no response received")
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

func emitResponse(rc *runner.RunnerContext, resp *client.Response) error {
	summary := map[string]any{
		"status_code": resp.StatusCode,
		"headers":     secrets.RedactHeaders(resp.Headers),
	}

	data := map[string]any{
		"summary": summary,
	}

	// If response has artifact (large response), include digest and preview
	if resp.Artifact != nil {
		data["artifact"] = resp.Digest
		data["kind"] = resp.ContentType
		data["size_bytes"] = resp.Size

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

func createRetryer(cfg *RetryConfig) *retry.Retryer {
	retryConfig := retry.Config{
		MaxAttempts:  5,
		InitialDelay: 250 * time.Millisecond,
		MaxDelay:     8 * time.Second,
		Multiplier:   2.0,
	}

	if cfg != nil {
		if cfg.MaxAttempts > 0 {
			retryConfig.MaxAttempts = cfg.MaxAttempts
		}
		if cfg.BaseMS > 0 {
			retryConfig.InitialDelay = time.Duration(cfg.BaseMS) * time.Millisecond
		}
		if cfg.MaxMS > 0 {
			retryConfig.MaxDelay = time.Duration(cfg.MaxMS) * time.Millisecond
		}
		if cfg.Factor > 0 {
			retryConfig.Multiplier = cfg.Factor
		}
	}

	return retry.New(retryConfig)
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
			return "Authentication failed. Check your credentials (token, API key, or username/password)."
		}
		if statusCode == 403 {
			return "Authorization failed. You may not have permission to access this resource."
		}
		return "Authentication error. Verify your credentials are correct and not expired."
	case "EOPENAPI":
		return "Failed to load or parse OpenAPI specification. Verify the spec path or memory reference is correct."
	case "EARG":
		return "Invalid parameters. Check that all required parameters are provided and have correct types."
	case "ERATELIMIT":
		return "Rate limit exceeded. Wait before retrying, or reduce request frequency."
	case "ERUNTIME":
		if statusCode >= 500 {
			return "Server error. The API may be experiencing issues. Try again later."
		}
		return "Request failed. Check network connectivity and API availability."
	default:
		return "An error occurred. Check the error message for details."
	}
}
