//nolint:forbidigo // This IS the logging infrastructure - zerolog/stderr usage is intentional
package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// EnvObsDir is the environment variable for the observability directory.
const EnvObsDir = "FOXCTL_OBS_DIR"

// obsDir caches the resolved observability directory.
var (
	obsDir    string
	obsDirSet bool
	obsDirMu  sync.RWMutex
)

// getObsDir returns the configured observability directory, or empty if disabled.
func getObsDir() string {
	obsDirMu.RLock()
	if obsDirSet {
		dir := obsDir
		obsDirMu.RUnlock()
		return dir
	}
	obsDirMu.RUnlock()

	// Upgrade to write lock for initialization
	obsDirMu.Lock()
	defer obsDirMu.Unlock()

	// Double-check after acquiring write lock
	if obsDirSet {
		return obsDir
	}

	obsDir = os.Getenv(EnvObsDir)
	obsDirSet = true
	return obsDir
}

// SetObsDirForTesting overrides the observability directory for testing.
// This should only be called from tests.
func SetObsDirForTesting(dir string) {
	obsDirMu.Lock()
	defer obsDirMu.Unlock()
	obsDir = dir
	obsDirSet = true
}

// WriteEvent appends an NDJSON-encoded event to $FOXCTL_OBS_DIR/events/<name>.ndjson.
// If FOXCTL_OBS_DIR is unset or empty, this is a no-op.
// Errors are logged and returned to the caller; callers may choose to ignore them
// since observability is typically best-effort.
// The function ignores context cancellation to avoid dropping terminal events.
//
// Index:
// - Purpose: Persist a structured observability event as NDJSON
// - Flow: resolve observability dir → validate name → ensure dir → append JSON line
// - SideEffects: creates directories; appends to NDJSON file
// - FailureModes: invalid name ignored, mkdir errors, file open/encode errors
// - Observability: emits stderr logs on write failures
// - Related: getObsDir, logWriteError
// - Keywords: ndjson, observability_dir, events, write_event
func WriteEvent(_ context.Context, name string, v any) error {
	dir := getObsDir()
	if dir == "" {
		return nil // observability disabled
	}

	// Validate name to prevent directory traversal.
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		logWriteError("validate", name, errors.New("invalid event name"))
		return nil
	}

	eventsDir := filepath.Join(dir, "events")
	filePath := filepath.Join(eventsDir, name+".ndjson")

	// Ensure events directory exists
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		logWriteError("mkdir", name, err)
		return err
	}

	// Open file for append
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logWriteError("open", name, err)
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			logWriteError("close", name, cerr)
		}
	}()

	// Encode as JSON + newline
	enc := json.NewEncoder(f)
	if err := enc.Encode(v); err != nil {
		logWriteError("encode", name, err)
		return err
	}

	return nil
}

// logWriteError logs an observability write error to stderr.
// We log but don't fail the caller since observability is best-effort.
func logWriteError(op, name string, err error) {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	log.Warn().
		Str("component", "observability").
		Str("op", op).
		Str("event_name", name).
		Err(err).
		Msg("event write failed")
}

// HashQuestion returns a truncated SHA-256 hash of the question.
// The result is the first 8 hex characters (4 bytes) of the hash.
// This provides enough entropy for correlation without being identifying.
func HashQuestion(q string) string {
	sum := sha256.Sum256([]byte(q))
	return hex.EncodeToString(sum[:4])
}

// SweGrepEvent is the NDJSON observation for a single code/snippet_extract run.
// See docs/observability/events.md §3.1 for field semantics.
type SweGrepEvent struct {
	Ts              time.Time `json:"ts"`
	Command         string    `json:"command"` // "code/snippet_extract"
	WorkspaceID     string    `json:"workspace_id"`
	QuestionHash    string    `json:"question_hash"` // 8 hex chars
	Candidates      int       `json:"candidates"`
	FilesConsidered int       `json:"files_considered"`
	FilesRelevant   int       `json:"files_relevant"`
	SnippetsEmitted int       `json:"snippets_emitted"`
	HasArtifact     bool      `json:"has_artifact"`
	DurationMS      int64     `json:"duration_ms,omitempty"`
	Source          string    `json:"source"` // "run" | "cache" | "job"
}

// NewSweGrepEvent creates a new SweGrepEvent with the current timestamp.
func NewSweGrepEvent(
	workspaceID, question string,
	candidates, filesConsidered, filesRelevant, snippetsEmitted int,
	hasArtifact bool,
	durationMS int64,
	source string,
) SweGrepEvent {
	return SweGrepEvent{
		Ts:              time.Now().UTC(),
		Command:         "code/snippet_extract",
		WorkspaceID:     workspaceID,
		QuestionHash:    HashQuestion(question),
		Candidates:      candidates,
		FilesConsidered: filesConsidered,
		FilesRelevant:   filesRelevant,
		SnippetsEmitted: snippetsEmitted,
		HasArtifact:     hasArtifact,
		DurationMS:      durationMS,
		Source:          source,
	}
}

// WriteSweGrepEvent writes a SweGrepEvent to the observability stream.
func WriteSweGrepEvent(ctx context.Context, ev SweGrepEvent) error {
	return WriteEvent(ctx, "code_swe_grep", ev)
}

// RepoIndexEvent captures observability for repo index queries.
type RepoIndexEvent struct {
	Ts          time.Time `json:"ts"`
	Command     string    `json:"command"`
	WorkspaceID string    `json:"workspace_id"`
	Source      string    `json:"source,omitempty"`
	QueryHash   string    `json:"query_hash,omitempty"`
	NodeID      string    `json:"node_id,omitempty"`
	SeedCount   int       `json:"seed_count,omitempty"`
	EdgeTypes   []string  `json:"edge_types,omitempty"`
	Direction   string    `json:"direction,omitempty"`
	Depth       int       `json:"depth,omitempty"`
	Budget      int       `json:"budget,omitempty"`
	PerNodeCap  int       `json:"per_node_cap,omitempty"`
	ResultCount int       `json:"result_count,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	Model       string    `json:"model,omitempty"`
	StopReason  string    `json:"stop_reason,omitempty"`
	ToolCalls   int       `json:"tool_calls,omitempty"`
	DurationMS  int64     `json:"duration_ms,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// NewRepoIndexEvent initializes a repo index observability event.
func NewRepoIndexEvent(command, workspaceID, source string) RepoIndexEvent {
	return RepoIndexEvent{
		Ts:          time.Now().UTC(),
		Command:     command,
		WorkspaceID: workspaceID,
		Source:      source,
	}
}

// WriteRepoIndexEvent writes a RepoIndexEvent to the observability stream.
func WriteRepoIndexEvent(ctx context.Context, ev RepoIndexEvent) error {
	return WriteEvent(ctx, "repo_index", ev)
}
