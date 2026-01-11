package skillmain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/rs/zerolog"
)

// RunFunc is the skill's main function signature.
type RunFunc[I any] func(ctx context.Context, rc *RunContext, in I) error

// Main is the standard skill entrypoint with typed input.
//
// It handles:
//   - Loading config and .env files
//   - Parsing JSON input from stdin
//   - Validating input using go-playground/validator struct tags
//   - Trapping SIGINT/SIGTERM for graceful cancellation
//   - Emitting error envelope on failure
//
// Example usage:
//
//	func main() {
//	    skillmain.Main("code/symbols", run)
//	}
//
//	type Input struct {
//	    Path string `json:"path" validate:"required"`
//	}
//
//	func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
//	    // Business logic...
//	    return skillout.Emit(rc, "code/symbols", data)
//	}
func Main[I any](command string, run RunFunc[I]) {
	code := mainWithCode(command, run, os.Stdin, os.Stdout)
	os.Exit(code)
}

// mainWithCode is the internal implementation that returns an exit code.
// This allows testing without calling os.Exit.
func mainWithCode[I any](command string, run RunFunc[I], stdin io.Reader, stdout io.Writer) int {
	// Set up context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Load .env files
	config.LoadDotEnv()

	// Start observability span - one wide event per skill execution.
	// Note: runservice may also emit a wide event; we differentiate using subtype.
	// The span starts early so we capture config/parse/validation failures too.
	ctx, done, span := observability.StartSpan(ctx, observability.OpSkillRun,
		observability.WithSpanComponent(observability.ComponentSkill),
		observability.WithSpanCommand(command),
		observability.WithSpanSubtype("skillmain"),
	)
	var runErr error
	defer func() { done(runErr) }()

	// Load configuration
	cfg, err := config.Load(ctx)
	if err != nil {
		runErr = err
		emitError(stdout, command, skillerr.WrapRuntime("load config", err))
		return 1
	}

	// Build run context
	rc, err := buildRunContext(cfg, stdout)
	if err != nil {
		runErr = err
		emitError(stdout, command, skillerr.WrapRuntime("build context", err))
		return 1
	}
	defer errs.Ignore(rc.Close(), "close run context")

	// Enrich span with runtime-resolved context.
	span.WithWorkspace(rc.Workspace).WithSession(rc.SessionID, rc.AgentID)
	span.WithData("no_cas", rc.NoCAS)
	span.WithData("inline_kb", rc.InlineKB)

	// Parse input from stdin
	var input I
	if err := json.NewDecoder(stdin).Decode(&input); err != nil {
		if err == io.EOF {
			// Empty input is ok - use zero value
		} else {
			runErr = err
			emitError(stdout, command, skillerr.WrapParse("decode input", err))
			return 1
		}
	}

	// Validate input
	if err := rc.Validator.Struct(input); err != nil {
		runErr = err
		if validationErrs, ok := err.(validator.ValidationErrors); ok {
			emitError(stdout, command, formatValidationErrors(validationErrs))
			return 1
		}
		emitError(stdout, command, skillerr.WrapValidation("validate input", err))
		return 1
	}

	// Run the skill
	if err := run(ctx, rc, input); err != nil {
		runErr = err
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

// BuildRunContext creates a RunContext from configuration.
// This is useful for skills that need custom main() logic (e.g., flag parsing)
// but still want to use the standard RunContext and skillout.Emit.
func BuildRunContext(cfg config.Config, stdout io.Writer) (*RunContext, error) {
	return buildRunContext(cfg, stdout)
}

// buildRunContext creates a RunContext from configuration.
func buildRunContext(cfg config.Config, stdout io.Writer) (*RunContext, error) {
	// Initialize CAS store
	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return nil, fmt.Errorf("cas store: %w", err)
	}

	// Resolve workspace
	workspace := strings.TrimSpace(os.Getenv("AGENTCTL_WORKSPACE"))
	if workspace == "" {
		workspace, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve workspace: %w", err)
		}
	}

	// Build allowed roots for path validator
	var allowedRoots []string
	if cfg.Home != "" {
		allowedRoots = append(allowedRoots, cfg.Home)
	}
	if tmp := os.TempDir(); tmp != "" {
		allowedRoots = append(allowedRoots, tmp)
	}

	// Initialize path validator
	pathValidator, err := policy.NewPathValidator(workspace, allowedRoots)
	if err != nil {
		return nil, fmt.Errorf("path validator: %w", err)
	}

	// Resolve agent ID
	agentID := os.Getenv("AGENTCTL_AGENT_ID")
	if agentID == "" {
		agentID = "agentctl"
	}

	// Check for no-CAS mode
	noCAS := os.Getenv("AGENTCTL_NO_CAS") == "1"

	// Initialize validator
	v := validator.New(validator.WithRequiredStructEnabled())

	// Initialize logger
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().Timestamp().Logger()

	return &RunContext{
		Config:        cfg,
		CASStore:      store,
		Workspace:     workspace,
		SessionID:     resolveSessionIDWithFallback(workspace, cfg.Home),
		AgentID:       agentID,
		Logger:        logger,
		PathValidator: pathValidator,
		Validator:     v,
		Stdout:        stdout,
		Now:           time.Now,
		InlineKB:      cfg.InlineOutputKB,
		MaxPreview:    5,
		NoCAS:         noCAS,
	}, nil
}

// resolveSessionIDWithFallback returns the session ID from env vars or identity file.
func resolveSessionIDWithFallback(workspace, agentctlHome string) string {
	// Try environment variables first
	for _, key := range []string{
		"AGENTCTL_SESSION_ID",
		"CLAUDE_SESSION_ID",
		"OPENCODE_SESSION_ID",
		"CURSOR_SESSION_ID",
		"TERM_SESSION_ID",
	} {
		if sid := os.Getenv(key); sid != "" {
			return sid
		}
	}

	// Fall back to identity file
	if workspace != "" && agentctlHome != "" {
		im := sessions.NewIdentityManager(agentctlHome)
		if active, err := im.GetActive(workspace); err == nil && active != nil {
			return active.SessionID
		}
	}

	return ""
}

// emitError writes an error envelope to stdout.
func emitError(w io.Writer, command string, err *skillerr.Error) {
	env := envelope.Error(command, err.Code, err.Message, err.ToEnvelopeData())
	errs.Ignore(envelope.Write(w, env), "emit error envelope")
}

// formatValidationErrors converts validator errors to a skill error.
func formatValidationErrors(errs validator.ValidationErrors) *skillerr.Error {
	var fields []string
	for _, e := range errs {
		fields = append(fields, formatFieldError(e))
	}

	return skillerr.Validation(
		fmt.Sprintf("input validation failed: %s", strings.Join(fields, ", ")),
		skillerr.WithHint("Check the input fields and ensure all required values are provided"),
		skillerr.WithData("fields", fields),
	)
}

// formatFieldError formats a single field validation error.
func formatFieldError(e validator.FieldError) string {
	field := e.Field()
	tag := e.Tag()

	switch tag {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s", field, e.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, e.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, e.Param())
	case "email":
		return fmt.Sprintf("%s must be a valid email", field)
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	default:
		return fmt.Sprintf("%s failed %s validation", field, tag)
	}
}
