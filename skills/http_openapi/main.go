// Package main implements a minimal OpenAPI dry-run planner.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	openapiauth "github.com/jkatigb/agentctl/internal/openapi/auth"
	"github.com/jkatigb/agentctl/internal/secrets"
	"github.com/jkatigb/agentctl/internal/skillslib"
)

type input struct {
	BaseURL string             `json:"base_url"`
	Path    string             `json:"path"`
	Method  string             `json:"method"`
	Query   map[string]string  `json:"query"`
	Headers map[string]string  `json:"headers"`
	Body    any                `json:"body"`
	DryRun  bool               `json:"dry_run"`
	Auth    openapiauth.Config `json:"auth"`
}

func main() {
	cfg, err := config.Load(context.Background())
	if err != nil {
		fail("http/openapi", "ECONFIG", err)
	}
	rc, err := skillslib.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("http/openapi", "ERUNTIME", err)
	}
	defer func() { _ = rc.Close() }()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("http/openapi", "EARG", fmt.Errorf("decode input: %w", err))
	}
	if err := run(rc, in); err != nil {
		fail("http/openapi", "ERUNTIME", err)
	}
}

func run(rc *skillslib.RunnerContext, in input) error {
	if in.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	if in.Path == "" {
		return fmt.Errorf("path is required")
	}
	method := strings.ToUpper(in.Method)
	if method == "" {
		method = "GET"
	}
	u, err := url.Parse(in.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}
	rel, err := url.Parse(in.Path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	resolved := u.ResolveReference(rel)
	q := resolved.Query()
	for k, v := range in.Query {
		q.Set(k, v)
	}
	resolved.RawQuery = q.Encode()

	req, err := http.NewRequest(method, resolved.String(), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}
	if err := openapiauth.Apply(req, in.Auth); err != nil {
		return err
	}
	resolved = req.URL
	headers := map[string]string{}
	for k, vals := range req.Header {
		headers[k] = strings.Join(vals, ", ")
	}
	headers = secrets.RedactHeaders(headers)

	plan := map[string]any{
		"method":  method,
		"url":     resolved.String(),
		"headers": headers,
		"body":    in.Body,
	}
	data := map[string]any{
		"request_plan": plan,
		"dry_run":      true,
	}
	return rc.Emit("http/openapi", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	_ = envelope.Write(os.Stdout, env)
	os.Exit(1)
}
