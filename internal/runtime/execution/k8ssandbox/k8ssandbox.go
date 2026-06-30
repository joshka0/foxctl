// Package k8ssandbox executes skills inside Kubernetes agent-sandbox pods.
//
// Each agent session gets its own isolated, stateful sandbox pod managed by
// the agent-sandbox controller. The sandbox persists across skill executions
// within a session and supports hibernation for idle sessions.
package k8ssandbox

import (
	"context"
	"fmt"
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
