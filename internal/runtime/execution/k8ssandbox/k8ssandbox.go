// Package k8ssandbox executes skills inside Kubernetes agent-sandbox pods.
package k8ssandbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/execution"
	sandbox "sigs.k8s.io/agent-sandbox/clients/go/sandbox"
)

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

type Runner struct {
	cfg    Config
	client *sandbox.Client
}

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

	opts := sandbox.Options{}
	switch strings.ToLower(cfg.Mode) {
	case "gateway":
		opts.GatewayName = cfg.GatewayName
		opts.GatewayNamespace = cfg.GatewayNamespace
	case "direct":
		opts.APIURL = cfg.APIURL
	case "port-forward", "":
	default:
		return nil, fmt.Errorf("k8ssandbox: unknown mode %q", cfg.Mode)
	}

	client, err := sandbox.NewClient(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("k8ssandbox: create client: %w", err)
	}
	return &Runner{cfg: cfg, client: client}, nil
}

func (r *Runner) Execute(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
	sbCtx, cancel := context.WithTimeout(ctx, r.cfg.CommandTimeout)
	defer cancel()

	sb, err := r.client.CreateSandbox(sbCtx, r.cfg.WarmPool, r.cfg.Namespace)
	if err != nil {
		return nil, fmt.Errorf("k8ssandbox: create sandbox: %w", err)
	}
	defer func() { _ = sb.Close(sbCtx) }()

	if len(opts.Input) > 0 {
		if err := sb.Write(sbCtx, "input.json", opts.Input); err != nil {
			return nil, fmt.Errorf("k8ssandbox: write input: %w", err)
		}
	}

	command := r.buildCommand(opts)
	result, err := sb.Run(sbCtx, command)
	if err != nil {
		return nil, fmt.Errorf("k8ssandbox: run: %w", err)
	}

	exitCode := 0
	if result.ExitCode != 0 {
		exitCode = result.ExitCode
	}
	return &execution.Result{
		Stdout:   []byte(result.Stdout),
		Stderr:   []byte(result.Stderr),
		ExitCode: exitCode,
	}, nil
}

func (r *Runner) buildCommand(opts execution.ExecuteOptions) string {
	var parts []string
	for _, env := range opts.ExtraEnv {
		parts = append(parts, "export "+env+";")
	}
	binary := opts.ArtifactPath
	if binary == "" {
		binary = "/usr/local/bin/foxctl-skill"
	}
	parts = append(parts, binary)
	if len(opts.Input) > 0 {
		parts = append(parts, "< input.json")
	}
	return strings.Join(parts, " ")
}

func (r *Runner) Close(ctx context.Context) error {
	r.client.DeleteAll(ctx)
	return nil
}
