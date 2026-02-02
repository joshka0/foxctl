package envelope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	canonicaljson "github.com/gibson042/canonicaljson-go"
)

const (
	// Version is the only supported envelope version for Core Profile v1.
	Version = 1
	// StatusOK indicates the command completed successfully.
	StatusOK = "ok"
	// StatusError indicates the command failed.
	StatusError = "error"
	// StatusProgress indicates the command is streaming progress (Agent Profile).
	StatusProgress = "progress"
)

// ErrValidation captures canonical envelope validation failures.
var (
	ErrValidation      = errors.New("envelope: validation failed")
	ErrInvalidVersion  = errors.New("envelope: invalid version")
	ErrInvalidStatus   = errors.New("envelope: invalid status")
	ErrMissingCommand  = errors.New("envelope: missing command")
	ErrMissingTS       = errors.New("envelope: missing timestamp")
	ErrMissingErrCode  = errors.New("envelope: missing error.code")
	ErrMissingErrMsg   = errors.New("envelope: missing error.message")
	ErrInvalidTS       = errors.New("envelope: invalid timestamp")
	ErrUnexpectedError = errors.New("envelope: unexpected error fields for ok status")
)

var now = func() time.Time {
	return time.Now().UTC()
}

// Envelope is the canonical wire format for agentctl commands.
type Envelope struct {
	Version int         `json:"version"`
	Status  string      `json:"status"`
	Command string      `json:"command"`
	Data    any         `json:"data,omitempty"`
	Meta    Meta        `json:"meta"`
	Error   ErrorFields `json:"error"`
}

// Meta captures timestamps and other metadata for responses.
type Meta struct {
	TS         string       `json:"ts"`
	DurationMS int64        `json:"duration_ms,omitempty"`
	Source     string       `json:"source,omitempty"`
	Runner     string       `json:"runner,omitempty"`
	Seq        *int         `json:"seq,omitempty"`
	Final      *bool        `json:"final,omitempty"`
	CASDigest  string       `json:"cas_digest,omitempty"`
	Memory     *MemoryRef   `json:"memory,omitempty"`
	JobID      string       `json:"job_id,omitempty"`
	SkillVer   string       `json:"skill_version,omitempty"`
	Workspace  string       `json:"workspace,omitempty"`
	CacheKey   string       `json:"cache_key,omitempty"`
	Partial    bool         `json:"partial,omitempty"`
	Profiles   []string     `json:"profiles,omitempty"`        // Agent Profile: e.g. ["core/v1", "agent/v1"]
	AgentID    string       `json:"agent_id,omitempty"`        // Agent Profile: agent identifier
	MailboxID  string       `json:"mailbox_id,omitempty"`      // Agent Profile: mailbox identifier
	CorrelID   string       `json:"correlation_id,omitempty"`  // Agent Profile: correlation ID for ask/reply
	ParentJob  string       `json:"parent_job_id,omitempty"`   // Agent Profile: parent job ID
	QuotaRem   *QuotaRemain `json:"quota_remaining,omitempty"` // Agent Profile: remaining quotas
}

// QuotaRemain captures remaining resource quotas for agent profile.
type QuotaRemain struct {
	CPUMS     int `json:"cpu_ms,omitempty"`
	MemoryMB  int `json:"memory_mb,omitempty"`
	NetworkMB int `json:"network_mb,omitempty"`
}

// MemoryRef describes a named memory reference included with an envelope.
type MemoryRef struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Workspace string `json:"workspace,omitempty"`
}

// CASHint provides user-friendly guidance for retrieving large outputs from CAS.
// This is included in the envelope data when output is truncated and stored in CAS.
type CASHint struct {
	// Digest is the CAS content address (e.g., "sha256:abc123...")
	Digest string `json:"digest"`
	// TotalBytes is the full size of the stored content
	TotalBytes int64 `json:"total_bytes"`
	// ContentType describes the content format (e.g., "application/json", "text/plain")
	ContentType string `json:"content_type,omitempty"`
	// PageCount is the number of pages if content supports pagination
	PageCount int `json:"page_count,omitempty"`
	// PageSize is the number of items per page
	PageSize int `json:"page_size,omitempty"`
	// ReadCommand is a shell command to retrieve the full content
	ReadCommand string `json:"read_command,omitempty"`
	// GetCommand is an alternative command for raw content retrieval
	GetCommand string `json:"get_command,omitempty"`
	// IsBinary indicates if the content is binary (non-text)
	IsBinary bool `json:"is_binary,omitempty"`
}

// ErrorFields captures error metadata when status != ok.
type ErrorFields struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Option mutates an envelope during construction.
type Option func(*Envelope)

// WithMeta overrides the meta section entirely.
func WithMeta(meta Meta) Option {
	return func(env *Envelope) {
		env.Meta = meta
	}
}

// WithMetaMutator allows targeted mutation of the meta block.
func WithMetaMutator(fn func(*Meta)) Option {
	return func(env *Envelope) {
		fn(&env.Meta)
	}
}

// WithData replaces the data payload after the default helper sets it.
func WithData(data any) Option {
	return func(env *Envelope) {
		env.Data = data
	}
}

// WithTimestamp sets the envelope timestamp explicitly (for testing/determinism).
func WithTimestamp(t time.Time) Option {
	return func(env *Envelope) {
		env.Meta.TS = t.Format(time.RFC3339)
	}
}

// OK returns a success envelope with the provided data payload.
func OK(command string, data any, opts ...Option) Envelope {
	env := Envelope{
		Version: Version,
		Status:  StatusOK,
		Command: command,
		Data:    data,
		Meta: Meta{
			TS: now().Format(time.RFC3339),
		},
	}
	for _, opt := range opts {
		opt(&env)
	}
	applyMetaDefaults(&env)
	return env
}

// Error returns an error envelope matching the canonical format.
func Error(command, code, message string, data any, opts ...Option) Envelope {
	env := Envelope{
		Version: Version,
		Status:  StatusError,
		Command: command,
		Data:    data,
		Meta: Meta{
			TS: now().Format(time.RFC3339),
		},
		Error: ErrorFields{
			Code:    code,
			Message: message,
		},
	}
	for _, opt := range opts {
		opt(&env)
	}
	applyMetaDefaults(&env)
	return env
}

func applyMetaDefaults(env *Envelope) {
	if env.Meta.TS == "" {
		env.Meta.TS = now().Format(time.RFC3339)
	}
}

// Validate ensures the envelope matches the Core Profile invariants.
func Validate(env Envelope) error {
	if env.Version != Version {
		return fmt.Errorf("%w: %w", ErrValidation, ErrInvalidVersion)
	}
	if strings.TrimSpace(env.Command) == "" {
		return fmt.Errorf("%w: %w", ErrValidation, ErrMissingCommand)
	}
	switch env.Status {
	case StatusOK:
		if env.Error.Code != "" || env.Error.Message != "" {
			return fmt.Errorf("%w: %w", ErrValidation, ErrUnexpectedError)
		}
	case StatusError:
		if env.Error.Code == "" {
			return fmt.Errorf("%w: %w", ErrValidation, ErrMissingErrCode)
		}
		if strings.TrimSpace(env.Error.Message) == "" {
			return fmt.Errorf("%w: %w", ErrValidation, ErrMissingErrMsg)
		}
	default:
		return fmt.Errorf("%w: %w", ErrValidation, ErrInvalidStatus)
	}
	ts := strings.TrimSpace(env.Meta.TS)
	if ts == "" {
		return fmt.Errorf("%w: %w", ErrValidation, ErrMissingTS)
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrValidation, ErrInvalidTS)
	}
	if parsed.Location() != time.UTC {
		return fmt.Errorf("%w: %w", ErrValidation, ErrInvalidTS)
	}
	return nil
}

// Write encodes the envelope to the provided writer.
func Write(w io.Writer, env Envelope) error {
	return NewWriter(w).Write(env)
}

// Writer streams envelopes to an io.Writer with canonical JSON settings.
type Writer struct {
	enc *json.Encoder
}

// NewWriter constructs a Writer bound to the supplied io.Writer.
func NewWriter(w io.Writer) *Writer {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &Writer{enc: enc}
}

// Write encodes a single envelope.
func (w *Writer) Write(env Envelope) error {
	return w.enc.Encode(env)
}

// Canonicalize returns RFC 8785 canonical JSON bytes for the provided input.
func Canonicalize(v any) ([]byte, error) {
	canon, err := canonicaljson.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonicalize: %w", err)
	}
	return canon, nil
}

// CanonicalString returns a UTF-8 string containing canonical JSON.
func CanonicalString(v any) (string, error) {
	canon, err := Canonicalize(v)
	if err != nil {
		return "", err
	}
	return string(canon), nil
}

// CanonicalDigest returns a sha256 digest (sha256:<hex>) of the canonical JSON.
func CanonicalDigest(v any) (string, error) {
	canon, err := Canonicalize(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
