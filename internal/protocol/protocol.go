// Package protocol provides centralized wire-level protocol semantics for agentctl.
// It wraps the internal/domain/envelope package and provides canonical error codes,
// helper functions for building and writing envelopes, and validation utilities.
package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

// Option is a function that modifies an envelope.
type Option func(*envelope.Envelope)

// WithSource sets the meta.source field.
func WithSource(source string) Option {
	return func(env *envelope.Envelope) {
		env.Meta.Source = source
	}
}

// WithWorkspace sets the meta.workspace field.
func WithWorkspace(path string) Option {
	return func(env *envelope.Envelope) {
		env.Meta.Workspace = path
	}
}

// WithJobID sets the meta.job_id field.
func WithJobID(id string) Option {
	return func(env *envelope.Envelope) {
		env.Meta.JobID = id
	}
}

// WithSkillVersion sets the meta.skill_version field.
func WithSkillVersion(v string) Option {
	return func(env *envelope.Envelope) {
		env.Meta.SkillVer = v
	}
}

// WithCacheKey sets the meta.cache_key field.
func WithCacheKey(key string) Option {
	return func(env *envelope.Envelope) {
		env.Meta.CacheKey = key
	}
}

// WithCASDigest sets the meta.cas_digest field.
func WithCASDigest(d string) Option {
	return func(env *envelope.Envelope) {
		env.Meta.CASDigest = d
	}
}

// WithMemoryRef sets the meta.memory field.
func WithMemoryRef(ref *envelope.MemoryRef) Option {
	return func(env *envelope.Envelope) {
		env.Meta.Memory = ref
	}
}

// WithMetaMutator applies a custom function to mutate the meta section.
func WithMetaMutator(fn func(*envelope.Meta)) Option {
	return func(env *envelope.Envelope) {
		if fn != nil {
			fn(&env.Meta)
		}
	}
}

// WithMeta is kept for backward compatibility. Prefer WithMetaMutator.
func WithMeta(fn func(*envelope.Meta)) Option {
	return WithMetaMutator(fn)
}

// WithData replaces the data payload.
func WithData(data any) Option {
	return func(env *envelope.Envelope) {
		env.Data = data
	}
}

// WithDuration sets the meta.duration_ms field.
func WithDuration(ms int64) Option {
	return func(env *envelope.Envelope) {
		env.Meta.DurationMS = ms
	}
}

// WithRunner sets the meta.runner field.
func WithRunner(runner string) Option {
	return func(env *envelope.Envelope) {
		env.Meta.Runner = runner
	}
}

// OK builds a success envelope with the given command and data.
// It ensures:
// - version is set to envelope.Version
// - status is StatusOK
// - meta.ts is non-empty
// Options can be used to customize meta fields.
func OK(command string, data any, opts ...Option) envelope.Envelope {
	env := envelope.OK(command, data)
	for _, opt := range opts {
		opt(&env)
	}
	return env
}

// Error builds an error envelope with the given code and message.
// It ensures:
// - status == StatusError
// - error.code is non-empty
// - meta.ts is set
// Options can be used to customize meta fields.
func Error(command string, code ErrorCode, message string, data any, opts ...Option) envelope.Envelope {
	env := envelope.Error(command, string(code), message, data)
	for _, opt := range opts {
		opt(&env)
	}
	return env
}

// Validate ensures the envelope conforms to Core Profile v1 invariants.
// This wraps envelope.Validate and extends it with protocol-level checks.
func Validate(env envelope.Envelope) error {
	// First run base envelope validation
	if err := envelope.Validate(env); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}

	// Protocol-level validation extensions
	if err := validateCASDigest(env); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}

	if err := validateCacheMetadata(env); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}

	if err := validateMemoryMetadata(env); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}

	if err := validateErrorStatusCode(env); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}

	return nil
}

// validateCASDigest ensures that if data contains an artifact field,
// meta.cas_digest matches it.
func validateCASDigest(env envelope.Envelope) error {
	artifactStr, ok, err := extractArtifactValue(env.Data)
	if err != nil {
		return fmt.Errorf("extract artifact: %w", err)
	}
	if !ok {
		return nil
	}

	// If we have an artifact, meta.cas_digest must match
	if !strings.HasPrefix(artifactStr, "sha256:") {
		return fmt.Errorf("artifact field must use sha256: prefix, got: %s", artifactStr)
	}

	if env.Meta.CASDigest == "" {
		return fmt.Errorf("data.artifact is set but meta.cas_digest is empty")
	}

	if env.Meta.CASDigest != artifactStr {
		return fmt.Errorf("meta.cas_digest (%s) does not match data.artifact (%s)",
			env.Meta.CASDigest, artifactStr)
	}

	return nil
}

// validateCacheMetadata ensures cache-related metadata is consistent.
func validateCacheMetadata(env envelope.Envelope) error {
	// If source is cache, cache_key should be set
	if env.Meta.Source == "cache" && strings.TrimSpace(env.Meta.CacheKey) == "" {
		return fmt.Errorf("meta.source is 'cache' but meta.cache_key is empty")
	}

	return nil
}

func validateMemoryMetadata(env envelope.Envelope) error {
	if env.Meta.Source != "memory" {
		return nil
	}

	if env.Meta.Memory == nil {
		return fmt.Errorf("meta.source is 'memory' but meta.memory is nil")
	}

	if strings.TrimSpace(env.Meta.Memory.Name) == "" {
		return fmt.Errorf("meta.source is 'memory' but meta.memory.name is empty")
	}

	if strings.TrimSpace(env.Meta.Memory.Type) == "" {
		return fmt.Errorf("meta.source is 'memory' but meta.memory.type is empty")
	}

	return nil
}

func extractArtifactValue(data any) (string, bool, error) {
	if data == nil {
		return "", false, nil
	}

	if raw, ok := data.(json.RawMessage); ok {
		if len(raw) == 0 {
			return "", false, nil
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return "", false, fmt.Errorf("decode artifact raw message: %w", err)
		}
		if artifact, ok := decoded["artifact"].(string); ok && artifact != "" {
			return artifact, true, nil
		}
		return "", false, nil
	}

	rv := reflect.ValueOf(data)
	for rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return "", false, nil
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return "", false, nil
		}
		return extractArtifactValue(rv.Elem().Interface())
	case reflect.Map:
		for _, key := range rv.MapKeys() {
			if key.Kind() != reflect.String || key.String() != "artifact" {
				continue
			}
			val := rv.MapIndex(key)
			if !val.IsValid() {
				return "", false, nil
			}
			if val.Kind() == reflect.Interface || val.Kind() == reflect.Pointer {
				if val.IsNil() {
					return "", false, nil
				}
				val = val.Elem()
			}
			if !val.CanInterface() {
				continue
			}
			if artifact, ok := val.Interface().(string); ok && artifact != "" {
				return artifact, true, nil
			}
			if raw, ok := val.Interface().(json.RawMessage); ok {
				return extractArtifactValue(raw)
			}
		}
	case reflect.Struct:
		field := rv.FieldByName("Artifact")
		if field.IsValid() && field.Kind() == reflect.String {
			artifact := field.String()
			if artifact != "" {
				return artifact, true, nil
			}
		}
	}

	return "", false, nil
}

// validateErrorStatusCode checks if error envelopes have valid status codes
// in their data.summary.status_code field.
func validateErrorStatusCode(env envelope.Envelope) error {
	if env.Status != envelope.StatusError {
		return nil
	}

	code, ok := extractStatusCode(env.Data)
	if !ok {
		return nil
	}

	// For error envelopes, status_code should be in error range (400-599)
	if code < 400 || code >= 600 {
		return fmt.Errorf("error envelope has data.summary.status_code=%d, expected 400-599", code)
	}

	return nil
}

func extractStatusCode(data any) (int, bool) {
	switch v := data.(type) {
	case map[string]any:
		return statusCodeFromSummary(v["summary"])
	case map[string]string:
		if summary, ok := v["summary"]; ok {
			return parseStatusCodeValue(summary)
		}
	case ErrorData:
		return statusCodeFromSummary(v.Summary)
	case *ErrorData:
		if v != nil {
			return statusCodeFromSummary(v.Summary)
		}
	case HTTPErrorData:
		return statusCodeFromSummary(v.Summary)
	case *HTTPErrorData:
		if v != nil {
			return statusCodeFromSummary(v.Summary)
		}
	}
	return 0, false
}

func statusCodeFromSummary(summary any) (int, bool) {
	if summary == nil {
		return 0, false
	}

	switch v := summary.(type) {
	case map[string]any:
		return parseStatusCodeValue(v["status_code"])
	case map[string]string:
		if value, ok := v["status_code"]; ok {
			return parseStatusCodeValue(value)
		}
	case HTTPSummary:
		return v.StatusCode, true
	case *HTTPSummary:
		if v != nil {
			return v.StatusCode, true
		}
	}

	return 0, false
}

func parseStatusCodeValue(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, false
		}
		if code, err := strconv.Atoi(s); err == nil {
			return code, true
		}
	default:
		return 0, false
	}
	return 0, false
}

// Write writes an envelope to w as JSON, ensuring validation passes.
// If validation fails, returns an error without writing.
func Write(w io.Writer, env envelope.Envelope) error {
	if err := Validate(env); err != nil {
		return fmt.Errorf("write envelope (cmd=%s, status=%s): %w", env.Command, env.Status, err)
	}
	return envelope.NewWriter(w).Write(env)
}

// MustWrite validates and writes an envelope, returning any encountered error.
// It is a convenience alias for Write and exists to mirror future variations.
func MustWrite(w io.Writer, env envelope.Envelope) error {
	return Write(w, env)
}

// WriteOK builds, validates, and writes an OK envelope.
func WriteOK(w io.Writer, cmd string, data any, opts ...Option) error {
	env := OK(cmd, data, opts...)
	return Write(w, env)
}

// WriteError builds, validates, and writes an error envelope.
func WriteError(w io.Writer, cmd string, code ErrorCode, msg string, data any, opts ...Option) error {
	env := Error(cmd, code, msg, data, opts...)
	return Write(w, env)
}

// AnnotateRun annotates an envelope with run metadata.
// It sets meta.source="run", meta.workspace, and meta.skill_version.
func AnnotateRun(env envelope.Envelope, workspace, skillVersion string) envelope.Envelope {
	env.Meta.Source = "run"
	if workspace != "" {
		env.Meta.Workspace = workspace
	}
	if skillVersion != "" {
		env.Meta.SkillVer = skillVersion
	}
	return env
}

// AnnotateCacheHit annotates an envelope with cache hit metadata.
// It sets meta.source="cache", meta.cache_key, meta.workspace, and meta.skill_version.
// Returns an error if the envelope cannot be annotated.
func AnnotateCacheHit(env envelope.Envelope, cacheKey, workspace, skillVersion string) (envelope.Envelope, error) {
	env.Meta.Source = "cache"
	env.Meta.CacheKey = cacheKey
	if workspace != "" {
		env.Meta.Workspace = workspace
	}
	if skillVersion != "" {
		env.Meta.SkillVer = skillVersion
	}
	if err := Validate(env); err != nil {
		return env, fmt.Errorf("annotate cache hit: %w", err)
	}
	return env, nil
}

// AnnotateCacheHitBytes is a convenience function that unmarshals JSON bytes,
// annotates the envelope, and marshals it back.
func AnnotateCacheHitBytes(result []byte, cacheKey, workspace, skillVersion string) ([]byte, error) {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err != nil {
		return nil, fmt.Errorf("annotate cache hit: parse envelope: %w", err)
	}
	annotated, err := AnnotateCacheHit(env, cacheKey, workspace, skillVersion)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(annotated)
	if err != nil {
		return nil, fmt.Errorf("annotate cache hit: encode envelope: %w", err)
	}
	return data, nil
}

// AnnotateRunBytes is a convenience function that unmarshals JSON bytes,
// annotates the envelope for a run, and marshals it back.
func AnnotateRunBytes(result []byte, workspace, skillVersion string) []byte {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err != nil {
		return result
	}
	annotated := AnnotateRun(env, workspace, skillVersion)
	data, err := json.Marshal(annotated)
	if err != nil {
		return result
	}
	return data
}

// SummarizeForMemory returns a short string suitable for memory summaries.
// Format: "command (workspace)" or just "command" if no workspace.
func SummarizeForMemory(env envelope.Envelope) string {
	if env.Meta.Workspace != "" {
		return fmt.Sprintf("%s (%s)", env.Command, filepath.Base(env.Meta.Workspace))
	}
	return env.Command
}

// SummarizeForMemoryBytes is a convenience function that unmarshals JSON bytes
// and returns a summary string.
func SummarizeForMemoryBytes(result []byte) string {
	var env envelope.Envelope
	if err := json.Unmarshal(result, &env); err == nil {
		return SummarizeForMemory(env)
	}
	return ""
}

// MustValidate validates an envelope and panics on error.
// Use sparingly, typically only in tests.
func MustValidate(env envelope.Envelope) {
	if err := Validate(env); err != nil {
		panic(fmt.Sprintf("envelope validation failed: %v", err))
	}
}

// IsOK returns true if the envelope status is "ok".
func IsOK(env envelope.Envelope) bool {
	return env.Status == envelope.StatusOK
}

// IsError returns true if the envelope status is "error".
func IsError(env envelope.Envelope) bool {
	return env.Status == envelope.StatusError
}

// GetErrorCode extracts the error code from an error envelope.
// Returns empty string if status is not error.
func GetErrorCode(env envelope.Envelope) ErrorCode {
	if !IsError(env) {
		return ""
	}
	return ErrorCode(strings.TrimSpace(env.Error.Code))
}

// ErrorData represents common data structure for error envelopes.
type ErrorData struct {
	Summary map[string]any `json:"summary,omitempty"`
	Detail  string         `json:"detail,omitempty"`
	Hint    string         `json:"hint,omitempty"`
	Context map[string]any `json:"context,omitempty"`
}

// ValidationErrorData represents data for validation/argument errors.
type ValidationErrorData struct {
	Field   string         `json:"field,omitempty"`
	Value   any            `json:"value,omitempty"`
	Reason  string         `json:"reason,omitempty"`
	Hint    string         `json:"hint,omitempty"`
	Context map[string]any `json:"context,omitempty"`
}

// HTTPErrorData represents data for HTTP-related errors.
type HTTPErrorData struct {
	Summary    HTTPSummary `json:"summary"`
	Body       any         `json:"body,omitempty"`
	Hint       string      `json:"hint,omitempty"`
	RequestID  string      `json:"request_id,omitempty"`
	RetryAfter string      `json:"retry_after,omitempty"`
}

// HTTPSummary contains HTTP response summary information.
type HTTPSummary struct {
	StatusCode int               `json:"status_code"`
	Method     string            `json:"method,omitempty"`
	URL        string            `json:"url,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

// ErrorWithData builds an error envelope with structured ErrorData.
func ErrorWithData(command string, code ErrorCode, message string, data ErrorData, opts ...Option) envelope.Envelope {
	return Error(command, code, message, data, opts...)
}

// ValidationError builds an error envelope for validation/argument errors.
func ValidationError(command string, message string, data ValidationErrorData, opts ...Option) envelope.Envelope {
	return Error(command, ErrorCodeEARG, message, data, opts...)
}

// HTTPError builds an error envelope for HTTP-related errors.
// The error code is automatically determined based on the status code.
func HTTPError(command string, message string, data HTTPErrorData, opts ...Option) envelope.Envelope {
	code := httpStatusToErrorCode(data.Summary.StatusCode)
	return Error(command, code, message, data, opts...)
}

// AuthError builds an error envelope for authentication failures.
func AuthError(command string, message string, hint string, opts ...Option) envelope.Envelope {
	data := ErrorData{
		Hint: hint,
	}
	return Error(command, ErrorCodeEAuth, message, data, opts...)
}

// NotFoundError builds an error envelope for resource not found errors.
func NotFoundError(command string, resource string, identifier string, opts ...Option) envelope.Envelope {
	data := ErrorData{
		Detail: fmt.Sprintf("%s not found: %s", resource, identifier),
		Context: map[string]any{
			"resource":   resource,
			"identifier": identifier,
		},
	}
	message := fmt.Sprintf("%s not found", resource)
	return Error(command, ErrorCodeENotFound, message, data, opts...)
}

// TimeoutError builds an error envelope for timeout errors.
func TimeoutError(command string, operation string, duration string, opts ...Option) envelope.Envelope {
	data := ErrorData{
		Detail: fmt.Sprintf("operation '%s' timed out after %s", operation, duration),
		Context: map[string]any{
			"operation": operation,
			"duration":  duration,
		},
	}
	message := fmt.Sprintf("operation timed out: %s", operation)
	return Error(command, ErrorCodeETimeout, message, data, opts...)
}

// RateLimitError builds an error envelope for rate limit errors.
func RateLimitError(command string, message string, retryAfter string, opts ...Option) envelope.Envelope {
	data := ErrorData{
		Hint: fmt.Sprintf("retry after %s", retryAfter),
		Context: map[string]any{
			"retry_after": retryAfter,
		},
	}
	return Error(command, ErrorCodeERateLimit, message, data, opts...)
}

// PolicyError builds an error envelope for policy violation errors.
func PolicyError(command string, policyName string, reason string, opts ...Option) envelope.Envelope {
	data := ErrorData{
		Detail: reason,
		Context: map[string]any{
			"policy": policyName,
			"reason": reason,
		},
	}
	message := fmt.Sprintf("policy violation: %s", policyName)
	return Error(command, ErrorCodeEPolicy, message, data, opts...)
}

// httpStatusToErrorCode maps HTTP status codes to error codes.
func httpStatusToErrorCode(statusCode int) ErrorCode {
	switch {
	case statusCode == 401 || statusCode == 403:
		return ErrorCodeEAuth
	case statusCode == 404:
		return ErrorCodeENotFound
	case statusCode == 408:
		return ErrorCodeETimeout
	case statusCode == 429:
		return ErrorCodeERateLimit
	case statusCode >= 400 && statusCode < 500:
		return ErrorCodeEARG
	case statusCode >= 500:
		return ErrorCodeERuntime
	default:
		return ErrorCodeERuntime
	}
}
