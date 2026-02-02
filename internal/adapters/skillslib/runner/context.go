package runner

import (
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// RunnerContext is an alias for skillmain.RunContext.
// Deprecated: Use skillmain.RunContext directly.
type RunnerContext = skillmain.RunContext

// Artifact is an alias for skillmain.Artifact.
// Deprecated: Use skillmain.Artifact directly.
type Artifact = skillmain.Artifact

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

// NewRunnerContext initializes a RunContext from configuration.
// Deprecated: Use skillmain.BuildRunContext directly.
func NewRunnerContext(cfg config.Config, stdout io.Writer) (*RunnerContext, error) {
	return skillmain.BuildRunContext(cfg, stdout)
}

// Emit writes an OK envelope with the given command and data.
// Deprecated: Use skillout.Emit directly.
func Emit(rc *RunnerContext, command string, data any, _ string, meta envelope.Meta) error {
	env := envelope.OK(command, data, envelope.WithMeta(meta))
	return envelope.Write(rc.Stdout, env)
}

// PersistJSON marshals value to JSON and stores it in CAS.
// Deprecated: Use skillmain.PersistJSON directly.
var PersistJSON = skillmain.PersistJSON

// PersistBuffer streams the provided buffer into CAS.
// Deprecated: Use skillmain.PersistBuffer directly.
var PersistBuffer = skillmain.PersistBuffer

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

// BuildCASHint creates a user-friendly CAS hint for the given artifact.
// It provides commands to retrieve the full content and metadata about the stored data.
//
// Parameters:
//   - artifact: the stored CAS artifact with digest, size, and content type
//   - linesPerPage: number of lines per page for pagination (converted to bytes internally)
//
// The function converts linesPerPage to bytes (~80 bytes per line heuristic) for the
// --page-size flag. Pagination uses --page and --page-size (bytes), not line-based limits.
func BuildCASHint(artifact Artifact, linesPerPage int) envelope.CASHint {
	return skillout.BuildCASHint(artifact, linesPerPage)
}
