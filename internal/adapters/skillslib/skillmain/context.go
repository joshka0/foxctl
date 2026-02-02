package skillmain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/rs/zerolog"
)

// RunContext bundles dependencies for skill execution.
type RunContext struct {
	// Config is the loaded agentctl configuration.
	Config config.Config

	// CASStore is the content-addressable storage for large outputs.
	CASStore *cas.Store

	// Workspace is the current workspace path.
	Workspace string

	// SessionID is the AI coding tool session ID (tool-agnostic).
	SessionID string

	// AgentID is the agent identifier (default: agentctl).
	AgentID string

	// Logger is a structured logger for the skill.
	Logger zerolog.Logger

	// PathValidator validates file paths against allowed roots.
	PathValidator *policy.PathValidator

	// Validator is the struct validator for input validation.
	Validator *validator.Validate

	// Stdout is the output writer (usually os.Stdout).
	Stdout io.Writer

	// Now returns the current time (injectable for testing).
	Now func() time.Time

	// InlineKB is the maximum inline output size in KB.
	InlineKB int

	// MaxPreview is the maximum number of preview items.
	MaxPreview int

	// NoCAS disables CAS truncation when true.
	NoCAS bool
}

// Close releases resources held by the run context.
func (rc *RunContext) Close() error {
	if rc.CASStore != nil {
		return rc.CASStore.Close()
	}
	return nil
}

// ShouldTruncate returns true if the data size exceeds the inline limit and NoCAS is not set.
func (rc *RunContext) ShouldTruncate(dataSize int) bool {
	if rc.NoCAS {
		return false
	}
	inlineLimit := rc.InlineKB * 1024
	return inlineLimit > 0 && dataSize > inlineLimit
}

// InlineLimit returns the maximum inline output size in bytes.
func (rc *RunContext) InlineLimit() int {
	return rc.InlineKB * 1024
}

// CASStore returns true if CAS storage is enabled.
func (rc *RunContext) ShouldStoreCAS() bool {
	return !rc.NoCAS && rc.Config.CAS.Store
}

// ExposePolicy returns the CAS expose policy from config.
func (rc *RunContext) ExposePolicy() config.ExposePolicy {
	return rc.Config.CAS.Expose
}

// Artifact describes a stored CAS object.
type Artifact struct {
	Digest string
	Size   int64
	Kind   string
}

// PersistJSON marshals value to JSON and stores it in CAS.
func PersistJSON(ctx context.Context, rc *RunContext, value any, tags ...string) (Artifact, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return Artifact{}, fmt.Errorf("skillmain: marshal json: %w", err)
	}
	return PersistBuffer(ctx, rc, bytes.NewBuffer(payload), "application/json", tags...)
}

// PersistBuffer streams the provided buffer into CAS.
func PersistBuffer(ctx context.Context, rc *RunContext, buf *bytes.Buffer, kind string, tags ...string) (Artifact, error) {
	if buf == nil {
		return Artifact{}, fmt.Errorf("skillmain: persist buffer: nil buffer")
	}
	obj, err := rc.CASStore.Put(ctx, bytes.NewReader(buf.Bytes()), kind, tags)
	if err != nil {
		return Artifact{}, fmt.Errorf("skillmain: cas put: %w", err)
	}
	return Artifact{Digest: obj.Digest, Size: obj.Size, Kind: obj.Kind}, nil
}
