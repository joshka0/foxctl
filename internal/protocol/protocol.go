// Package protocol provides centralized wire-level protocol semantics for agentctl.
// It wraps the internal/domain/envelope package and provides canonical error codes,
// helper functions for building and writing envelopes, and validation utilities.
package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

// ErrorCode represents a canonical error code from Core Profile v1.
type ErrorCode string

const (
	// ErrorCodeEARG indicates invalid arguments or validation errors.
	ErrorCodeEARG ErrorCode = "EARG"
	// ErrorCodeEOpenAPI indicates OpenAPI spec parsing or validation errors.
	ErrorCodeEOpenAPI ErrorCode = "EOPENAPI"
	// ErrorCodeEAuth indicates authentication or credential problems.
	ErrorCodeEAuth ErrorCode = "EAUTH"
	// ErrorCodeEPagination indicates pagination-related failures.
	ErrorCodeEPagination ErrorCode = "EPAGINATION"
	// ErrorCodeERateLimit indicates rate limit or backoff exhaustion.
	ErrorCodeERateLimit ErrorCode = "ERATELIMIT"
	// ErrorCodeERuntime indicates runtime or generic execution failure (HTTP 5xx, runner crash, etc.).
	ErrorCodeERuntime ErrorCode = "ERUNTIME"
	// ErrorCodeEOutputTooLarge indicates output exceeded capture limits.
	ErrorCodeEOutputTooLarge ErrorCode = "EOUTPUT_TOO_LARGE"
	// ErrorCodeEPolicy indicates capability or policy violations.
	ErrorCodeEPolicy ErrorCode = "EPOLICY"
	// ErrorCodeENotFound indicates resource not found.
	ErrorCodeENotFound ErrorCode = "ENOTFOUND"
	// ErrorCodeETimeout indicates operation timeout.
	ErrorCodeETimeout ErrorCode = "ETIMEOUT"
	// ErrorCodeESkillDown indicates skill unavailability.
	ErrorCodeESkillDown ErrorCode = "ESKILLDOWN"
	// ErrorCodeEParse indicates JSON parse or invalid UTF-8 errors.
	ErrorCodeEParse ErrorCode = "EPARSE"
	// ErrorCodeEEnvelope indicates invalid or malformed envelope.
	ErrorCodeEEnvelope ErrorCode = "EENVELOPE"
	// ErrorCodeEIO indicates filesystem or I/O errors.
	ErrorCodeEIO ErrorCode = "EIO"
	// ErrorCodeECanceled indicates job was canceled by user.
	ErrorCodeECanceled ErrorCode = "ECANCELED"
)

// IsRetryable returns true if the error code indicates a transient failure
// that may succeed on retry.
func IsRetryable(code ErrorCode) bool {
	switch code {
	case ErrorCodeERuntime, ErrorCodeERateLimit, ErrorCodeETimeout:
		return true
	default:
		return false
	}
}

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

// WithMeta applies a custom function to mutate the meta section.
func WithMeta(fn func(*envelope.Meta)) Option {
	return func(env *envelope.Envelope) {
		fn(&env.Meta)
	}
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
// This wraps envelope.Validate and may be extended with protocol-level checks.
func Validate(env envelope.Envelope) error {
	if err := envelope.Validate(env); err != nil {
		return fmt.Errorf("protocol: %w", err)
	}
	return nil
}

// Write writes an envelope to w as JSON, ensuring validation passes.
// If validation fails, returns an error without writing.
func Write(w io.Writer, env envelope.Envelope) error {
	if err := Validate(env); err != nil {
		return fmt.Errorf("write envelope (cmd=%s, status=%s): %w", env.Command, env.Status, err)
	}
	return envelope.Write(w, env)
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
