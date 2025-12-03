// Package observability provides helpers for emitting observability events.
// Events are written as NDJSON to $AGENTCTL_OBS_DIR/events/<name>.ndjson.
// See docs/observability/events.md for the full spec.
package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// EnvObsDir is the environment variable for the observability directory.
const EnvObsDir = "AGENTCTL_OBS_DIR"

// obsDir caches the resolved observability directory.
var (
	obsDir     string
	obsDirOnce sync.Once
)

// getObsDir returns the configured observability directory, or empty if disabled.
func getObsDir() string {
	obsDirOnce.Do(func() {
		obsDir = os.Getenv(EnvObsDir)
	})
	return obsDir
}

// SetObsDirForTesting overrides the observability directory for testing.
// This should only be called from tests.
func SetObsDirForTesting(dir string) {
	obsDir = dir
	obsDirOnce.Do(func() {}) // mark as done so getObsDir uses our value
}

// WriteEvent appends an NDJSON-encoded event to $AGENTCTL_OBS_DIR/events/<name>.ndjson.
// If AGENTCTL_OBS_DIR is unset or empty, this is a no-op.
// Errors are logged but do not fail the caller.
func WriteEvent(_ context.Context, name string, v any) error {
	dir := getObsDir()
	if dir == "" {
		return nil // observability disabled
	}

	// Validate name to prevent directory traversal
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return nil // invalid name, silently ignore
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

// SweGrepEvent is the NDJSON observation for a single code/swe_grep run.
// See docs/observability/events.md §3.1 for field semantics.
type SweGrepEvent struct {
	Ts              time.Time `json:"ts"`
	Command         string    `json:"command"` // "code/swe_grep"
	WorkspaceID     string    `json:"workspace_id"`
	QuestionHash    string    `json:"question_hash"` // 8 hex chars
	Candidates      int       `json:"candidates"`
	FilesConsidered int       `json:"files_considered"`
	FilesRelevant   int       `json:"files_relevant"`
	SnippetsEmitted int       `json:"snippets_emitted"`
	HasArtifact     bool      `json:"has_artifact"`
	ArtifactKind    string    `json:"artifact_kind,omitempty"`
	DurationMS      int64     `json:"duration_ms,omitempty"`
	Source          string    `json:"source"` // "run" | "cache" | "job"
}

// NewSweGrepEvent creates a new SweGrepEvent with the current timestamp.
func NewSweGrepEvent(
	workspaceID, question string,
	candidates, filesConsidered, filesRelevant, snippetsEmitted int,
	hasArtifact bool,
	artifactKind string,
	durationMS int64,
	source string,
) SweGrepEvent {
	return SweGrepEvent{
		Ts:              time.Now().UTC(),
		Command:         "code/swe_grep",
		WorkspaceID:     workspaceID,
		QuestionHash:    HashQuestion(question),
		Candidates:      candidates,
		FilesConsidered: filesConsidered,
		FilesRelevant:   filesRelevant,
		SnippetsEmitted: snippetsEmitted,
		HasArtifact:     hasArtifact,
		ArtifactKind:    artifactKind,
		DurationMS:      durationMS,
		Source:          source,
	}
}

// WriteSweGrepEvent writes a SweGrepEvent to the observability stream.
func WriteSweGrepEvent(ctx context.Context, ev SweGrepEvent) error {
	return WriteEvent(ctx, "code_swe_grep", ev)
}
