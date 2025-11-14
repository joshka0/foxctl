// Package storage defines shared interfaces and data structures for agentctl stores.
package storage

import (
	"context"
	"io"
	"time"

	jobtypes "github.com/jkatigb/agentctl/internal/jobs/types"
)

// Store is the minimal interface implemented by all storage providers.
type Store interface {
	Close() error
}

// CacheEntry represents the persisted data for a cache item.
type CacheEntry struct {
	CacheKey     string
	SkillName    string
	SkillVersion string
	Workspace    string
	Result       []byte
	Digests      []string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastAccessed time.Time
	HitCount     int
}

// CacheStats captures high-level cache metadata.
type CacheStats struct {
	Entries int64
	Path    string
	TTL     time.Duration
}

// CacheStore manages cached run results.
type CacheStore interface {
	Store
	Get(ctx context.Context, key string) (CacheEntry, bool, error)
	Put(ctx context.Context, entry CacheEntry) error
	Recent(ctx context.Context, workspace string, limit int) ([]CacheEntry, error)
	Delete(ctx context.Context, key string) error
	Stats(ctx context.Context) (CacheStats, error)
}

// Job is an alias to the canonical job metadata type.
type Job = jobtypes.Job

// JobState aliases the canonical job state enumeration.
type JobState = jobtypes.State

const (
	// JobStateQueued mirrors jobtypes.StateQueued.
	JobStateQueued = jobtypes.StateQueued
	// JobStateRunning mirrors jobtypes.StateRunning.
	JobStateRunning = jobtypes.StateRunning
	// JobStateOK mirrors jobtypes.StateOK.
	JobStateOK = jobtypes.StateOK
	// JobStateError mirrors jobtypes.StateError.
	JobStateError = jobtypes.StateError
	// JobStateCanceled mirrors jobtypes.StateCanceled.
	JobStateCanceled = jobtypes.StateCanceled
)

// JobStore exposes the public job management surface.
type JobStore interface {
	Store
	SubmitEcho(ctx context.Context, message string) (Job, error)
	List(ctx context.Context, limit int) ([]Job, error)
	Get(ctx context.Context, id string) (Job, error)
	Result(ctx context.Context, id string) ([]byte, error)
	Cancel(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (Job, bool, error)
	SetWorkspace(ctx context.Context, jobID, workspacePath string) error
	WaitForCompletion(ctx context.Context, jobID string, pollInterval time.Duration) (Job, error)
	TailProgress(ctx context.Context, jobID string, follow bool, w io.Writer) error
	ExecutePreparedSkill(ctx context.Context, jobID, manifestPath, artifactPath string) ([]byte, error)
}

// CASMetadata describes a stored CAS object.
type CASMetadata struct {
	Digest    string    `json:"digest"`
	Size      int64     `json:"size_bytes"`
	Kind      string    `json:"kind,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Metadata provides a compatibility alias for embedded object metadata.
type Metadata = CASMetadata

// CASObject augments metadata with pin state.
type CASObject struct {
	Metadata
	Pinned bool `json:"pinned"`
}

// CASGCOptions controls garbage collection behavior.
type CASGCOptions struct {
	DryRun     bool
	OlderThan  time.Duration
	KeepPinned bool
	MaxDelete  int
}

// CASGCResult summarizes the outcome of a GC run.
type CASGCResult struct {
	ObjectsDeleted int   `json:"objects_deleted"`
	ObjectsSkipped int   `json:"objects_skipped"`
	BytesFreed     int64 `json:"bytes_freed"`
	Errors         int   `json:"errors"`
}

// CASStore defines the CAS operations used by the CLI.
type CASStore interface {
	Store
	Put(ctx context.Context, r io.Reader, kind string, tags []string) (CASObject, error)
	Get(ctx context.Context, digest string) (io.ReadCloser, CASMetadata, error)
	Head(ctx context.Context, digest string) (CASObject, error)
	List(ctx context.Context) ([]CASObject, error)
	Remove(ctx context.Context, digest string) error
	Pin(ctx context.Context, digest string) error
	Unpin(ctx context.Context, digest string) error
	AddTags(ctx context.Context, digest string, tags []string) error
	GC(ctx context.Context, opts CASGCOptions) (CASGCResult, error)
}

// NamedEntry captures a stored named memory entry.
type NamedEntry struct {
	ID          string
	Name        string
	Type        string
	Workspace   string
	Summary     string
	Result      []byte
	Digests     []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastAccess  time.Time
	AccessCount int
}

// ScoredEntry couples a named entry with a relevance score.
type ScoredEntry struct {
	Entry NamedEntry
	Score float64
}

// MemoryStats summarizes named memory metadata.
type MemoryStats struct {
	Named int64
	Path  string
}

// MemoryStore persists named memories and auto-cache entries.
type MemoryStore interface {
	Store
	Save(ctx context.Context, entry NamedEntry) (NamedEntry, error)
	SaveFromResult(ctx context.Context, name, typ, workspace, summary string, result []byte) (NamedEntry, error)
	Get(ctx context.Context, name, workspace string) (NamedEntry, error)
	List(ctx context.Context, workspace string, limit int) ([]NamedEntry, error)
	Delete(ctx context.Context, name, workspace string) error
	Search(ctx context.Context, workspace, query string, limit int) ([]ScoredEntry, error)
	Update(ctx context.Context, name, workspace string, summary, typ *string) (NamedEntry, error)
	Relevant(ctx context.Context, workspace string, limit int) ([]ScoredEntry, error)
	Stats(ctx context.Context) (MemoryStats, error)
}
