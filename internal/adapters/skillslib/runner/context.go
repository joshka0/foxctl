// Package runner provides helpers for building skill runners.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// RunnerContext bundles dependencies skill binaries need.
//
//nolint:revive
type RunnerContext struct {
	Config        config.Config
	CASStore      *cas.Store
	PathValidator *policy.PathValidator
	InlineKB      int
	Stdout        io.Writer
	Now           func() time.Time
	MaxPreview    int
	SessionID     string // AI coding tool session ID (tool-agnostic)
	AgentID       string // Agent identifier (default: agentctl)
	Workspace     string // Current workspace path
}

// ResolveSessionID returns the session ID from environment variables.
// Priority: AGENTCTL_SESSION_ID > CLAUDE_SESSION_ID > OPENCODE_SESSION_ID >
// CURSOR_SESSION_ID > TERM_SESSION_ID. Returns empty string if none set.
func ResolveSessionID() string {
	// Tool-agnostic session ID (highest priority)
	if sid := os.Getenv("AGENTCTL_SESSION_ID"); sid != "" {
		return sid
	}
	// Claude Code session ID
	if sid := os.Getenv("CLAUDE_SESSION_ID"); sid != "" {
		return sid
	}
	// OpenCode session ID (if they add one in future)
	if sid := os.Getenv("OPENCODE_SESSION_ID"); sid != "" {
		return sid
	}
	// Cursor session ID (if they add one in future)
	if sid := os.Getenv("CURSOR_SESSION_ID"); sid != "" {
		return sid
	}
	// Terminal session ID fallback
	if sid := os.Getenv("TERM_SESSION_ID"); sid != "" {
		return sid
	}
	return ""
}

// ResolveSessionIDWithFallback returns the session ID, falling back to identity file.
// Priority: env vars (via ResolveSessionID) > identity file for workspace.
// This is useful when env vars aren't available (e.g., hooks, shell edge-cases).
func ResolveSessionIDWithFallback(workspace, agentctlHome string) string {
	// First try environment variables
	if sid := ResolveSessionID(); sid != "" {
		return sid
	}

	// Fall back to identity file for this workspace
	if workspace != "" && agentctlHome != "" {
		im := sessions.NewIdentityManager(agentctlHome)
		if active, err := im.GetActive(workspace); err == nil && active != nil {
			return active.SessionID
		}
	}

	return ""
}

// NewRunnerContext initializes a RunnerContext from configuration.
func NewRunnerContext(cfg config.Config, stdout io.Writer) (*RunnerContext, error) {
	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return nil, fmt.Errorf("skillslib: cas store: %w", err)
	}

	workspace := strings.TrimSpace(os.Getenv("AGENTCTL_WORKSPACE"))
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("skillslib: resolve workspace: %w", err)
		}
	}

	var allowedRoots []string
	if cfg.Home != "" {
		allowedRoots = append(allowedRoots, cfg.Home)
	}
	if tmp := os.TempDir(); tmp != "" {
		allowedRoots = append(allowedRoots, tmp)
	}

	pathValidator, err := policy.NewPathValidator(workspace, allowedRoots)
	if err != nil {
		return nil, fmt.Errorf("skillslib: path validator: %w", err)
	}

	agentID := os.Getenv("AGENTCTL_AGENT_ID")
	if agentID == "" {
		agentID = "agentctl"
	}

	return &RunnerContext{
		Config:        cfg,
		CASStore:      store,
		PathValidator: pathValidator,
		InlineKB:      cfg.InlineOutputKB,
		Stdout:        stdout,
		Now:           time.Now,
		MaxPreview:    5,
		SessionID:     ResolveSessionIDWithFallback(workspace, cfg.Home),
		AgentID:       agentID,
		Workspace:     workspace,
	}, nil
}

// Close releases resources held by the runner context.
func (rc *RunnerContext) Close() error {
	// CAS store currently has no Close; placeholder for future resources.
	return nil
}

// Emit OK envelope with automatic CAS wrapping for large payloads.
// The third parameter (contentType) is currently unused and reserved for future use.
func (rc *RunnerContext) Emit(command string, data any, _ string, meta envelope.Meta) error {
	env := envelope.OK(command, data, envelope.WithMeta(meta))
	return envelope.Write(rc.Stdout, env)
}

// Artifact describes a stored CAS object.
type Artifact struct {
	Digest string
	Size   int64
	Kind   string
}

// PersistJSON marshals value to JSON and stores it in CAS.
func PersistJSON(ctx context.Context, rc *RunnerContext, value any, tags ...string) (Artifact, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return Artifact{}, fmt.Errorf("skillslib: marshal json: %w", err)
	}
	return PersistBuffer(ctx, rc, bytes.NewBuffer(payload), "application/json", tags...)
}

// PersistBuffer streams the provided buffer into CAS.
func PersistBuffer(ctx context.Context, rc *RunnerContext, buf *bytes.Buffer, kind string, tags ...string) (Artifact, error) {
	if buf == nil {
		return Artifact{}, fmt.Errorf("skillslib: persist buffer: nil buffer")
	}
	obj, err := rc.CASStore.Put(ctx, bytes.NewReader(buf.Bytes()), kind, tags)
	if err != nil {
		return Artifact{}, fmt.Errorf("skillslib: cas put: %w", err)
	}
	return Artifact{Digest: obj.Digest, Size: obj.Size, Kind: obj.Kind}, nil
}

func artifactDigest(data any) string {
	if data == nil {
		return ""
	}
	switch v := data.(type) {
	case map[string]any:
		return extractArtifact(v)
	default:
		val := reflect.ValueOf(data)
		if val.Kind() == reflect.Map && val.Type().Key().Kind() == reflect.String {
			iter := val.MapRange()
			for iter.Next() {
				if iter.Key().String() == "artifact" {
					if s, ok := iter.Value().Interface().(string); ok && strings.HasPrefix(s, "sha256:") {
						return s
					}
				}
			}
		}
	}
	return ""
}

func extractArtifact(m map[string]any) string {
	val, ok := m["artifact"]
	if !ok {
		return ""
	}
	if digest, ok := val.(string); ok && strings.HasPrefix(digest, "sha256:") {
		return digest
	}
	return ""
}
