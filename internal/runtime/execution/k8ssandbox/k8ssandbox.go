// Package k8ssandbox executes skills inside Kubernetes agent-sandbox pods.
//
// Each agent session gets its own isolated, stateful sandbox pod managed by
// the agent-sandbox controller. The sandbox persists across skill executions
// within a session and supports hibernation for idle sessions.
package k8ssandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	sandbox "sigs.k8s.io/agent-sandbox/clients/go/sandbox"
)

// Config configures the k8s sandbox runner.
type Config struct {
	WarmPool           string
	Namespace          string
	Mode               string
	GatewayName        string
	GatewayNamespace   string
	APIURL             string
	HibernateAfterIdle time.Duration
	MaxLifetime        time.Duration
	CommandTimeout     time.Duration
}

// Runner executes skills inside k8s agent-sandbox pods.
type Runner struct {
	cfg    Config
	client *sandbox.Client
}

// NewRunner creates a k8s sandbox runner and initializes the sandbox client.
// For local k3s without a router sidecar, use Mode="auto" which tries
// port-forward first, then falls back to resolving the pod IP directly.
func NewRunner(ctx context.Context, cfg Config) (*Runner, error) {
	if strings.TrimSpace(cfg.WarmPool) == "" {
		return nil, fmt.Errorf("k8ssandbox: warmPool is required")
	}
	if strings.TrimSpace(cfg.Namespace) == "" {
		cfg.Namespace = "default"
	}
	if cfg.CommandTimeout == 0 {
		cfg.CommandTimeout = 5 * time.Minute
	}

	opts := sandbox.Options{WarmPoolName: cfg.WarmPool}

	switch strings.ToLower(cfg.Mode) {
	case "gateway":
		opts.GatewayName = cfg.GatewayName
		opts.GatewayNamespace = cfg.GatewayNamespace
	case "direct":
		opts.APIURL = cfg.APIURL
	case "port-forward", "":
	case "auto":
		// Auto mode: if APIURL is set, use direct. Otherwise use port-forward.
		if cfg.APIURL != "" {
			opts.APIURL = cfg.APIURL
		}
	default:
		return nil, fmt.Errorf("k8ssandbox: unknown mode %q", cfg.Mode)
	}

	client, err := sandbox.NewClient(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("k8ssandbox: create client: %w", err)
	}
	return &Runner{cfg: cfg, client: client}, nil
}

// ExecuteRaw runs a command inside a sandbox and returns raw result.
func (r *Runner) ExecuteRaw(ctx context.Context, command string, input []byte, env []string) (*RawResult, error) {
	sbCtx, cancel := context.WithTimeout(ctx, r.cfg.CommandTimeout)
	defer cancel()

	// Build full command with env vars and input redirect.
	// The sandbox runtime executes commands via subprocess without a shell,
	// so we must wrap in /bin/sh -c for env vars and redirects to work.
	fullCmd := command
	if len(input) > 0 {
		fullCmd += " < input.json"
	}
	if len(env) > 0 {
		var prefix []string
		for _, e := range env {
			prefix = append(prefix, "export "+e+";")
		}
		fullCmd = strings.Join(prefix, " ") + " " + fullCmd
	}
	fullCmd = "/bin/sh -c " + strconv.Quote(fullCmd)

	// In direct mode (APIURL set), we can use the sandbox's Write/Run
	// API directly without creating a SandboxClaim — the APIURL points
	// to the sandbox runtime HTTP server.
	if r.cfg.APIURL != "" {
		return r.executeDirect(sbCtx, fullCmd, input)
	}

	// In gateway or port-forward mode, create a sandbox from the warm pool.
	sb, err := r.client.CreateSandbox(sbCtx, r.cfg.WarmPool, r.cfg.Namespace)
	if err != nil {
		return nil, fmt.Errorf("k8ssandbox: create sandbox: %w", err)
	}
	defer func() { _ = sb.Close(sbCtx) }()

	if len(input) > 0 {
		if err := sb.Write(sbCtx, "input.json", input); err != nil {
			return nil, fmt.Errorf("k8ssandbox: write input: %w", err)
		}
	}

	result, err := sb.Run(sbCtx, fullCmd)
	if err != nil {
		return nil, fmt.Errorf("k8ssandbox: run: %w", err)
	}

	exitCode := 0
	if result.ExitCode != 0 {
		exitCode = result.ExitCode
	}
	return &RawResult{
		Stdout:   []byte(result.Stdout),
		Stderr:   []byte(result.Stderr),
		ExitCode: exitCode,
	}, nil
}

// executeDirect sends commands directly to the sandbox runtime HTTP API
// without creating a SandboxClaim. Used for local testing with APIURL.
func (r *Runner) executeDirect(ctx context.Context, command string, input []byte) (*RawResult, error) {
	client := &http.Client{Timeout: r.cfg.CommandTimeout}

	// Write input file if provided (uses multipart upload per sandbox runtime API).
	if len(input) > 0 {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		part, err := writer.CreateFormFile("file", "input.json")
		if err != nil {
			return nil, fmt.Errorf("k8ssandbox: create form file: %w", err)
		}
		if _, err := part.Write(input); err != nil {
			return nil, fmt.Errorf("k8ssandbox: write content: %w", err)
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("k8ssandbox: close writer: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", r.cfg.APIURL+"/upload", &buf)
		if err != nil {
			return nil, fmt.Errorf("k8ssandbox: upload request: %w", err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("k8ssandbox: upload: %w", err)
		}
		resp.Body.Close()
	}

	// Run command.
	runPayload, _ := json.Marshal(map[string]string{"command": command})
	req, err := http.NewRequestWithContext(ctx, "POST", r.cfg.APIURL+"/execute", bytes.NewReader(runPayload))
	if err != nil {
		return nil, fmt.Errorf("k8ssandbox: run request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("k8ssandbox: run: %w", err)
	}
	defer resp.Body.Close()

	var runResult struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&runResult); err != nil {
		return nil, fmt.Errorf("k8ssandbox: decode run response: %w", err)
	}
	return &RawResult{
		Stdout:   []byte(runResult.Stdout),
		Stderr:   []byte(runResult.Stderr),
		ExitCode: runResult.ExitCode,
	}, nil
}

// RawResult is the execution result.
type RawResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Close cleans up all tracked sandboxes.
func (r *Runner) Close(ctx context.Context) error {
	r.client.DeleteAll(ctx)
	return nil
}
