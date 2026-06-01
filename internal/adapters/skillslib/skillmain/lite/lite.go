// Package lite provides a lightweight skill entrypoint for store-less skills.
//
// It omits the heavy dependencies of the full skillmain package: no storage
// stores, no circuit breaker manager, no CAS store, no observability spans,
// and no zerolog logger. Skills that only need envelope I/O, config loading,
// path validation, and input validation can use this package to avoid pulling
// in the intelligence and runtime monolith.
//
// Direct-import invariant: this package must not directly import any package
// under internal/storage, internal/runtime, internal/intelligence, or
// internal/context.
package lite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillcas"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/domain/policy"
	"github.com/joshka0/foxctl/internal/platform/workspace"
)

// RunContext bundles lightweight dependencies for skill execution.
// It is a subset of skillmain.RunContext with all store/runtime/intelligence
// fields removed.
type RunContext struct {
	// Config is the loaded lite configuration.
	Config LiteConfig

	// Workspace is the current workspace path.
	Workspace string

	// SessionID is the AI coding tool session ID (tool-agnostic).
	// Resolved from environment variables only (no storage fallback).
	SessionID string

	// AgentID is the agent identifier (default: foxctl).
	AgentID string

	// PathValidator validates file paths against allowed roots.
	PathValidator *policy.PathValidator

	// Validator is the struct validator for input validation.
	Validator *validator.Validate

	// Stdout is the output writer (usually os.Stdout).
	Stdout io.Writer

	// CASWriter stores large outputs when injected by a custom entrypoint.
	CASWriter skillcas.Writer

	// Now returns the current time (injectable for testing).
	Now func() time.Time

	// InlineKB is the maximum inline output size in KB.
	InlineKB int

	// NoCAS disables CAS truncation when true.
	NoCAS bool
}

// InlineLimit returns the maximum inline output size in bytes.
func (rc *RunContext) InlineLimit() int {
	return rc.InlineKB * 1024
}

// OutputWriter returns the envelope output writer for capability-based helpers.
func (rc *RunContext) OutputWriter() io.Writer {
	return rc.Stdout
}

// ShouldTruncate returns true if data exceeds the inline limit and NoCAS is false.
func (rc *RunContext) ShouldTruncate(dataSize int) bool {
	if rc.NoCAS {
		return false
	}
	inlineLimit := rc.InlineLimit()
	return inlineLimit > 0 && dataSize > inlineLimit
}

// ShouldStoreCAS returns true when CAS output persistence is configured and injected.
func (rc *RunContext) ShouldStoreCAS() bool {
	return rc != nil && !rc.NoCAS && rc.Config.CAS.Store && rc.CASWriter != nil
}

// CASExposePolicy returns the backend-neutral CAS expose policy.
func (rc *RunContext) CASExposePolicy() skillcas.ExposePolicy {
	return skillcas.ExposePolicy(rc.Config.CAS.Expose)
}

// PutArtifact stores content through the injected CAS writer.
func (rc *RunContext) PutArtifact(ctx context.Context, r io.Reader, kind string, tags []string) (skillcas.Artifact, error) {
	if rc == nil || rc.CASWriter == nil {
		return skillcas.Artifact{}, fmt.Errorf("lite: cas writer not configured")
	}
	return rc.CASWriter.PutArtifact(ctx, r, kind, tags)
}

// Close is present for parity with the full skillmain.RunContext.
func (rc *RunContext) Close() error {
	return nil
}

// RunFunc is the skill's main function signature for the lite path.
type RunFunc[I any] func(ctx context.Context, rc *RunContext, in I) error

// Main is the lightweight skill entrypoint with typed input.
//
// It handles:
//   - Loading config and .env files
//   - Parsing JSON input from stdin
//   - Validating input using go-playground/validator struct tags
//   - Trapping SIGINT/SIGTERM for graceful cancellation
//   - Emitting error envelope on failure
//
// Unlike skillmain.Main, it does not initialize stores, CAS, circuit breakers,
// or observability spans.
func Main[I any](command string, run RunFunc[I]) {
	code := mainWithCode(command, run, os.Stdin, os.Stdout)
	os.Exit(code)
}

// Bootstrap loads .env/config and builds a RunContext for custom
// skill entrypoints (e.g., when a skill needs flag parsing before stdin).
func Bootstrap(ctx context.Context, stdout io.Writer) (*RunContext, error) {
	_ = ctx
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	return BuildRunContext(cfg, stdout)
}

func mainWithCode[I any](command string, run RunFunc[I], stdin io.Reader, stdout io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := LoadConfig()
	if err != nil {
		emitError(stdout, command, skillerr.WrapRuntime("load config", err))
		return 1
	}

	rc, err := BuildRunContext(cfg, stdout)
	if err != nil {
		emitError(stdout, command, skillerr.WrapRuntime("build context", err))
		return 1
	}

	var input I
	decoder := json.NewDecoder(stdin)
	// Hook skills must be forward-compatible with new fields.
	if !strings.HasPrefix(command, "hooks/") {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(&input); err != nil {
		if err == io.EOF {
			// Empty input is ok; use zero value.
		} else {
			hint := "Ensure the input is valid JSON; use --input with a JSON object or --input-file for larger payloads"
			if strings.Contains(err.Error(), "unknown field") {
				hint = "Unknown field in input; check field names match the skill's expected parameters (e.g., 'scope' not 'scopes')"
			}
			emitError(stdout, command, skillerr.WrapParse("decode input", err, skillerr.WithHint(hint)))
			return 1
		}
	}

	if err := rc.Validator.Struct(input); err != nil {
		if validationErrs, ok := err.(validator.ValidationErrors); ok {
			emitError(stdout, command, formatValidationErrors(command, input, validationErrs))
			return 1
		}
		emitError(stdout, command, skillerr.WrapValidation("validate input", err))
		return 1
	}

	if err := run(ctx, rc, input); err != nil {
		var skillErr *skillerr.Error
		if errors.As(err, &skillErr) {
			emitError(stdout, command, skillErr)
		} else {
			emitError(stdout, command, skillerr.WrapRuntime("execute", err))
		}
		return 1
	}

	return 0
}

// BuildRunContext creates a RunContext from a LiteConfig.
func BuildRunContext(cfg LiteConfig, stdout io.Writer) (*RunContext, error) {
	workspacePath := workspace.Detect("")
	if workspacePath == "" {
		var err error
		workspacePath, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve workspace: %w", err)
		}
	}

	var allowedRoots []string
	if cfg.Home != "" {
		allowedRoots = append(allowedRoots, cfg.Home)
	}
	if tmp := os.TempDir(); tmp != "" {
		allowedRoots = append(allowedRoots, tmp)
	}
	// On macOS, /tmp symlinks to /private/tmp which differs from os.TempDir().
	// Claude Code sandboxes create dirs like /tmp/foxctl-* and /tmp/plan-build-*.
	for _, pattern := range []string{"/tmp/foxctl-*", "/tmp/plan-build-*"} {
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil || info == nil || !info.IsDir() {
					continue
				}
				allowedRoots = append(allowedRoots, match)
			}
		}
	}

	pathValidator, err := policy.NewPathValidator(workspacePath, allowedRoots)
	if err != nil {
		return nil, fmt.Errorf("path validator: %w", err)
	}

	agentID := os.Getenv("FOXCTL_AGENT_ID")
	if agentID == "" {
		agentID = "foxctl"
	}

	return &RunContext{
		Config:        cfg,
		Workspace:     workspacePath,
		SessionID:     resolveSessionID(workspacePath, cfg.Home),
		AgentID:       agentID,
		PathValidator: pathValidator,
		Validator:     validator.New(validator.WithRequiredStructEnabled()),
		Stdout:        stdout,
		Now:           time.Now,
		InlineKB:      cfg.InlineOutputKB,
		NoCAS:         envBool("FOXCTL_NO_CAS", false),
	}, nil
}

// resolveSessionID obtains the session ID from environment variables only.
// The full skillmain falls back to sessions.NewIdentityManager (storage), but
// the lite package avoids that dependency.
func resolveSessionID(workspace, foxctlHome string) string {
	_ = workspace
	_ = foxctlHome
	for _, key := range []string{
		"FOXCTL_SESSION_ID",
		"CLAUDE_SESSION_ID",
		"OPENCODE_SESSION_ID",
		"CURSOR_SESSION_ID",
		"TERM_SESSION_ID",
	} {
		if sid := os.Getenv(key); sid != "" {
			return sid
		}
	}
	return ""
}

// ResolvePath validates a candidate path with the run context and returns
// workspace + resolved path.
func ResolvePath(rc *RunContext, candidate string) (string, string, error) {
	if rc == nil || rc.PathValidator == nil {
		return "", "", skillerr.Arg("path validator not configured")
	}

	workspace := rc.PathValidator.Workspace()
	if strings.TrimSpace(candidate) == "" {
		return workspace, workspace, nil
	}

	resolved, err := ValidatePath(rc, candidate)
	if err != nil {
		return "", "", err
	}
	return workspace, resolved, nil
}
