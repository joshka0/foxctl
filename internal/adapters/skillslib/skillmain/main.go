package skillmain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/execution/circuitbreaker"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/rs/zerolog"
)

// FlexString accepts both string and number JSON values, converting to string.
// Use this for fields like PR numbers that users might pass as either "123" or 123.
type FlexString string

// UnmarshalJSON implements json.Unmarshaler for FlexString.
func (f *FlexString) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexString(s)
		return nil
	}
	// Try number
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*f = FlexString(n.String())
		return nil
	}
	return fmt.Errorf("FlexString: expected string or number, got %s", string(data))
}

// String returns the underlying string value.
func (f FlexString) String() string {
	return string(f)
}

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

// Bootstrap loads .env/config and builds a RunContext for custom skill entrypoints.
func Bootstrap(ctx context.Context, stdout io.Writer) (*RunContext, error) {
	config.LoadDotEnv()
	workspacePath, err := resolveWorkspaceForConfig()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(ctx, config.WithWorkspacePath(workspacePath))
	if err != nil {
		return nil, err
	}
	return BuildRunContext(cfg, stdout)
}

// mainWithCode is the internal implementation that returns an exit code.
// mainWithCode runs a typed skill entrypoint using the provided stdin/stdout and returns an OS-style exit code.
// It loads environment and configuration, decodes and validates JSON input from stdin, enriches observability spans, executes the supplied run function, and writes any error envelopes to stdout.
// Returns 0 on success and a non-zero exit code when configuration, parsing, validation, or execution fail.
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
	workspacePath, err := resolveWorkspaceForConfig()
	if err != nil {
		runErr = err
		emitError(stdout, command, skillerr.WrapRuntime("resolve workspace", err))
		return 1
	}
	cfg, err := config.Load(ctx, config.WithWorkspacePath(workspacePath))
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
	decoder := json.NewDecoder(stdin)
	// Hook skills must be forward-compatible with new fields added to hooks.Input,
	// so only enforce strict field checking for non-hook skills.
	if !strings.HasPrefix(command, "hooks/") {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(&input); err != nil {
		if err == io.EOF {
			// Empty input is ok - use zero value
		} else {
			runErr = err
			// Provide helpful hint for unknown field errors
			hint := "Ensure the input is valid JSON; use --input with a JSON object or --input-file for larger payloads"
			if strings.Contains(err.Error(), "unknown field") {
				hint = "Unknown field in input; check field names match the skill's expected parameters (e.g., 'scope' not 'scopes')"
			}
			emitError(stdout, command, skillerr.WrapParse(
				"decode input",
				err,
				skillerr.WithHint(hint),
			))
			return 1
		}
	}

	// Validate input
	if err := rc.Validator.Struct(input); err != nil {
		runErr = err
		if validationErrs, ok := err.(validator.ValidationErrors); ok {
			emitError(stdout, command, formatValidationErrors(command, input, validationErrs))
			return 1
		}
		emitError(stdout, command, skillerr.WrapValidation("validate input", err))
		return 1
	}

	// Enrich span with input data for observability
	enrichSpanWithInput(span, input)

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

// ResolvePath validates a candidate path with the run context and returns workspace + resolved path.
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

// buildRunContext creates a RunContext from configuration.
func buildRunContext(cfg config.Config, stdout io.Writer) (*RunContext, error) {
	// Initialize CAS store
	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return nil, fmt.Errorf("cas store: %w", err)
	}

	// Resolve workspace
	workspacePath := workspace.Detect("")
	if workspacePath == "" {
		workspacePath, err = os.Getwd()
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
	// On macOS, /tmp symlinks to /private/tmp which differs from os.TempDir()
	// ($TMPDIR, typically /var/folders/.../T). Claude Code sandboxes create
	// dirs like /tmp/agentctl-* and /tmp/plan-build-*. Add only those specific
	// sandbox dirs rather than all of /tmp.
	for _, pattern := range []string{"/tmp/agentctl-*", "/tmp/plan-build-*"} {
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			allowedRoots = append(allowedRoots, matches...)
		}
	}

	// Initialize path validator
	pathValidator, err := policy.NewPathValidator(workspacePath, allowedRoots)
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
		Stores:        NewStoreProvider(cfg),
		Breakers:      circuitbreaker.NewManager(circuitbreaker.DefaultConfig()),
		Workspace:     workspacePath,
		SessionID:     resolveSessionIDWithFallback(workspacePath, cfg.Home),
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

func resolveWorkspaceForConfig() (string, error) {
	workspacePath := workspace.Detect("")
	if strings.TrimSpace(workspacePath) != "" {
		return workspacePath, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	return cwd, nil
}

// resolveSessionIDWithFallback obtains the session ID by checking known environment variables and falling back to the workspace identity manager.
// It checks environment variables in this priority: AGENTCTL_SESSION_ID, CLAUDE_SESSION_ID, OPENCODE_SESSION_ID, CURSOR_SESSION_ID, TERM_SESSION_ID.
// If none are set and both workspace and agentctlHome are provided, it queries the identity manager for the active session and returns its SessionID.
// Returns an empty string if no session ID can be determined.
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

// enrichSpanWithInput extracts key fields from the input and adds them to the span.
// enrichSpanWithInput adds selected input fields to the given observability span to improve debugging visibility.
// It marshals the input to JSON, extracts a predefined set of common keys, and attaches them to the span:
// - Scalar values are recorded as `input_<field>` (string values longer than 100 characters are truncated and suffixed with "...").
// - Slice/array values are recorded as `input_<field>_count` with the element count.
// If `input` is nil or cannot be marshaled/unmarshaled, the function returns without modifying the span.
func enrichSpanWithInput(span *observability.EventBuilder, input any) {
	if input == nil {
		return
	}

	// Convert input to a map to extract fields
	data, err := json.Marshal(input)
	if err != nil {
		return
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}

	// Sensitive fields - only log length, never raw content
	sensitiveFields := map[string]bool{
		"prompt":  true,
		"query":   true,
		"path":    true,
		"paths":   true,
		"file":    true,
		"files":   true,
		"pattern": true,
	}

	// Safe fields - can log values (with truncation)
	safeFields := []string{
		"scope",       // index scope
		"scopes",      // multiple scopes
		"limit",       // result limits
		"format",      // output format
		"action",      // todo/memory actions
		"workspace",   // workspace path
		"name",        // memory/entity names
		"type",        // memory/entity types
		"pr",          // PR number
		"branch",      // git branch
		"commit",      // git commit
		"symbol",      // code symbol
		"symbols",     // multiple symbols
		"model",       // LLM model
		"provider",    // LLM provider
		"errors_only", // error filtering
		"since",       // time filtering
	}

	// Log sensitive fields with length only
	for field := range sensitiveFields {
		if v, ok := m[field]; ok && v != nil {
			if s, ok := v.(string); ok {
				span.WithData("input_"+field+"_length", len(s))
			} else if arr, ok := v.([]any); ok {
				span.WithData("input_"+field+"_count", len(arr))
			}
		}
	}

	// Log safe fields with values (truncated if needed)
	for _, field := range safeFields {
		if v, ok := m[field]; ok && v != nil {
			// For strings, truncate if too long
			if s, ok := v.(string); ok && len(s) > 100 {
				v = s[:100] + "..."
			}
			// For arrays, just show count
			if arr, ok := v.([]any); ok {
				span.WithData("input_"+field+"_count", len(arr))
				continue
			}
			span.WithData("input_"+field, v)
		}
	}
}

// emitError writes an error envelope to w for the given command and skill error.
// It appends a usage hint to err when appropriate, constructs an envelope containing
// the command and error details, and attempts to write that envelope to w. Failures
// during writing are suppressed.
func emitError(w io.Writer, command string, err *skillerr.Error) {
	appendUsageHint(command, err)
	env := envelope.Error(command, err.Code, err.Message, err.ToEnvelopeData())
	errs.Ignore(envelope.Write(w, env), "emit error envelope")
}

func appendUsageHint(command string, err *skillerr.Error) {
	if err == nil {
		return
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	usage := fmt.Sprintf("For examples, run: agentctl run %s --examples", command)
	if err.Hint == "" {
		err.Hint = usage
		return
	}
	if !strings.Contains(err.Hint, "--examples") {
		err.Hint = strings.TrimSpace(err.Hint + " " + usage)
	}
}

// formatValidationErrors converts validator errors to a skill error.
func formatValidationErrors(command string, input any, errs validator.ValidationErrors) *skillerr.Error {
	var fields []string
	for _, e := range errs {
		fields = append(fields, formatFieldError(e, input))
	}

	return skillerr.Validation(
		fmt.Sprintf("input validation failed: %s", strings.Join(fields, ", ")),
		skillerr.WithHint(formatValidationHint(command)),
		skillerr.WithData("fields", fields),
	)
}

// formatFieldError formats a single field validation error.
func formatFieldError(e validator.FieldError, input any) string {
	field := formatFieldPath(e, input)
	if field == "" {
		field = e.Field()
	}
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

func formatValidationHint(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "Check the input fields and ensure all required values are provided"
	}
	return fmt.Sprintf("Check the input fields and ensure all required values are provided. For examples, run: agentctl run %s --examples", command)
}

func formatFieldPath(e validator.FieldError, input any) string {
	if input == nil {
		return ""
	}

	t := reflect.TypeOf(input)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return ""
	}

	path := e.StructNamespace()
	if path == "" {
		return ""
	}
	if name := t.Name(); name != "" {
		path = strings.TrimPrefix(path, name+".")
	}

	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return ""
	}

	var out []string
	for _, part := range parts {
		fieldName, indexSuffix := splitIndex(part)
		if fieldName == "" {
			continue
		}
		jsonName, nextType := jsonFieldName(t, fieldName)
		if jsonName == "" {
			jsonName = fieldName
		}
		out = append(out, jsonName+indexSuffix)
		t = nextType
	}

	return strings.Join(out, ".")
}

func splitIndex(part string) (string, string) {
	idx := strings.Index(part, "[")
	if idx == -1 {
		return part, ""
	}
	return part[:idx], part[idx:]
}

func jsonFieldName(t reflect.Type, fieldName string) (string, reflect.Type) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return "", nil
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
		for t != nil && t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
	}
	if t == nil || t.Kind() != reflect.Struct {
		return "", nil
	}

	field, ok := t.FieldByName(fieldName)
	if !ok {
		return "", nil
	}

	tag := field.Tag.Get("json")
	tagName := strings.Split(tag, ",")[0]
	if tagName == "-" {
		tagName = ""
	}

	nextType := field.Type
	for nextType != nil && nextType.Kind() == reflect.Pointer {
		nextType = nextType.Elem()
	}
	if nextType != nil && (nextType.Kind() == reflect.Slice || nextType.Kind() == reflect.Array) {
		nextType = nextType.Elem()
	}
	for nextType != nil && nextType.Kind() == reflect.Pointer {
		nextType = nextType.Elem()
	}

	return tagName, nextType
}
