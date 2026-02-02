package indexing

import (
	"context"
	"fmt"
	"time"
)

// PostReviewEvent represents the data emitted when a review passes and
// changes are accepted. This is the canonical trigger for all post-review indexers.
// See: docs/spec/post_review_harness.md §4.1
type PostReviewEvent struct {
	// ID is a unique identifier for this event (ULID or sha256: digest).
	ID string `json:"id"`

	// WorkspaceID identifies the workspace where the review occurred.
	WorkspaceID string `json:"workspace_id"`

	// TaskID is the task that was reviewed.
	TaskID string `json:"task_id"`

	// ReviewID is the review artifact ID (matches ReviewArtifact.ID).
	ReviewID string `json:"review_id"`

	// ReviewKind describes the review type: "auto", "human", or "mixed".
	ReviewKind string `json:"review_kind"`

	// ReviewStatus MUST be "ok" for emitted events.
	ReviewStatus string `json:"review_status"`

	// DiffAppliedAt is the UTC timestamp when the diff was applied.
	DiffAppliedAt time.Time `json:"diff_applied_at"`

	// Files lists the files affected by the reviewed changes.
	Files []FileChange `json:"files"`

	// Source describes what produced this event (e.g., "review_gate_v1").
	Source string `json:"source,omitempty"`

	// Metadata holds optional context (commit ID, branch, etc.).
	Metadata map[string]any `json:"metadata,omitempty"`

	// CreatedAt is when this event was created.
	CreatedAt time.Time `json:"created_at"`

	// Sequence is a monotonic counter per (workspace, task) for ordering.
	Sequence int `json:"sequence,omitempty"`

	// Reason describes why this event was triggered (e.g., "post_review", "manual", "git_commit").
	Reason string `json:"reason,omitempty"`
}

// FileChange represents a single file affected by a review.
type FileChange struct {
	// Path is the workspace-relative path to the file.
	Path string `json:"path"`

	// Digest is the CAS digest of the file content (optional).
	// If omitted, indexers should read from the filesystem.
	Digest string `json:"digest,omitempty"`

	// SizeBytes is the file size in bytes (optional, for filtering).
	SizeBytes int64 `json:"size_bytes,omitempty"`

	// Language is the detected language (optional, for filtering).
	Language string `json:"language,omitempty"`

	// ChangeKind describes the type of change.
	ChangeKind ChangeKind `json:"change_kind"`
}

// ChangeKind describes the type of file change.
type ChangeKind string

const (
	// ChangeKindAdded indicates a new file was added.
	ChangeKindAdded ChangeKind = "added"
	// ChangeKindModified indicates an existing file was modified.
	ChangeKindModified ChangeKind = "modified"
	// ChangeKindDeleted indicates a file was deleted.
	ChangeKindDeleted ChangeKind = "deleted"
	// ChangeKindRenamed indicates a file was renamed (may include modifications).
	ChangeKindRenamed ChangeKind = "renamed"
)

// IndexerResult contains the outcome of an indexer run.
type IndexerResult struct {
	// IndexerID identifies which indexer produced this result.
	IndexerID string `json:"indexer_id"`

	// FilesIndexed is the count of files successfully indexed.
	FilesIndexed int `json:"files_indexed"`

	// FilesSkipped is the count of files skipped (e.g., too large, excluded).
	FilesSkipped int `json:"files_skipped"`

	// FilesFailed is the count of files that failed to index.
	FilesFailed int `json:"files_failed"`

	// Failures contains details about individual file failures.
	Failures []IndexerFailure `json:"failures,omitempty"`

	// Error is set if the indexer failed entirely (not per-file).
	Error string `json:"error,omitempty"`
}

// IndexerFailure describes a per-file indexing failure.
type IndexerFailure struct {
	// Path is the file that failed.
	Path string `json:"path"`

	// ErrorCode is a standardized error code (see spec §11).
	ErrorCode string `json:"error_code"`

	// ErrorMessage is a human-readable description.
	ErrorMessage string `json:"error_message"`
}

// Indexer is the interface that post-review indexers must implement.
// Indexers are called by the PostReviewHandler when a review passes.
type Indexer interface {
	// ID returns a unique identifier for this indexer (e.g., "semantic_embed", "code_symbol_dag").
	ID() string

	// Index processes a post-review event and updates the indexer's storage.
	// It should be idempotent and handle partial failures gracefully.
	Index(ctx context.Context, event PostReviewEvent) (*IndexerResult, error)
}

// IndexerConfig defines the configuration for a single indexer.
// See: docs/spec/semantic_file_index.md §8.2
type IndexerConfig struct {
	// ID is a unique identifier for this indexer instance.
	ID string `yaml:"id" json:"id"`

	// Kind identifies the indexer type (e.g., "semantic_file_index", "code_symbol_dag").
	Kind string `yaml:"kind" json:"kind"`

	// Enabled controls whether this indexer is active.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// IncludeGlobs are glob patterns for files to include.
	IncludeGlobs []string `yaml:"include_globs" json:"include_globs"`

	// ExcludeGlobs are glob patterns for files to exclude.
	ExcludeGlobs []string `yaml:"exclude_globs" json:"exclude_globs"`

	// MaxFileKB is the maximum file size in KB to index (0 = no limit).
	MaxFileKB int `yaml:"max_file_kb" json:"max_file_kb"`

	// Extra contains indexer-specific configuration.
	Extra map[string]any `yaml:"extra" json:"extra,omitempty"`
}

// FanoutMode controls how post-review events are dispatched to indexers.
type FanoutMode string

const (
	// FanoutModeInline runs indexers synchronously in the handler goroutine.
	// Suitable for dev/test; not recommended for production.
	FanoutModeInline FanoutMode = "inline"
	// FanoutModeJobs enqueues one job per indexer via the WFQ scheduler.
	// This is the default for production.
	FanoutModeJobs FanoutMode = "jobs"
)

// PostReviewConfig holds the configuration for the post-review indexing pipeline.
// See: docs/spec/post_review_harness.md §7
type PostReviewConfig struct {
	// Enabled controls whether post-review indexing is active.
	// Default: false; must be true to emit events/jobs.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Mode controls how indexers are invoked: "inline" or "jobs".
	// Default: "jobs" for production.
	Mode FanoutMode `yaml:"mode" json:"mode"`

	// Indexers lists the configured indexers.
	Indexers []IndexerConfig `yaml:"indexers" json:"indexers"`

	// ConcurrencyPerIndexer limits concurrent jobs per indexer in jobs mode.
	// Default: 3. A value of 0 means unlimited concurrency.
	ConcurrencyPerIndexer int `yaml:"concurrency_per_indexer" json:"concurrency_per_indexer"`

	// Async controls whether indexing runs asynchronously (default: true).
	// When false, task completion blocks until indexing completes.
	// Deprecated: use Mode instead; kept for backward compatibility.
	Async bool `yaml:"async" json:"async"`
}

// DefaultPostReviewConfig returns a sensible default configuration.
func DefaultPostReviewConfig() PostReviewConfig {
	return PostReviewConfig{
		Enabled:               false, // Off by default until explicitly enabled
		Mode:                  FanoutModeJobs,
		Indexers:              []IndexerConfig{},
		ConcurrencyPerIndexer: 3,
		Async:                 true,
	}
}

// Validate checks that the configuration is valid and returns an error if not.
// This should be called during startup to catch misconfigurations early.
func (c PostReviewConfig) Validate() error {
	// Validate mode
	switch c.Mode {
	case FanoutModeInline, FanoutModeJobs, "": // empty defaults to jobs
		// ok
	default:
		return fmt.Errorf("indexing.post_review.mode: invalid value %q, must be %q or %q",
			c.Mode, FanoutModeInline, FanoutModeJobs)
	}

	// Validate concurrency (0 = unlimited, negative is invalid).
	if c.ConcurrencyPerIndexer < 0 {
		return fmt.Errorf("EARG: indexing.post_review.concurrency_per_indexer: must be >= 0 (0 = unlimited), got %d",
			c.ConcurrencyPerIndexer)
	}

	// Validate indexer configs
	seen := make(map[string]bool)
	for i, idx := range c.Indexers {
		if idx.ID == "" {
			return fmt.Errorf("indexing.post_review.indexers[%d]: id is required", i)
		}
		if seen[idx.ID] {
			return fmt.Errorf("indexing.post_review.indexers: duplicate id %q", idx.ID)
		}
		seen[idx.ID] = true
	}

	return nil
}

// EffectiveMode returns the mode to use, defaulting to jobs if empty.
func (c PostReviewConfig) EffectiveMode() FanoutMode {
	if c.Mode == "" {
		return FanoutModeJobs
	}
	return c.Mode
}

// PostReviewConfigFromSettings converts platform config settings to indexing config.
// This allows the indexing package to remain independent of the config package.
func PostReviewConfigFromSettings(
	enabled bool,
	async bool,
	indexers []struct {
		ID           string
		Kind         string
		Enabled      bool
		IncludeGlobs []string
		ExcludeGlobs []string
		MaxFileKB    int
	},
) PostReviewConfig {
	cfg := PostReviewConfig{
		Enabled:  enabled,
		Async:    async,
		Indexers: make([]IndexerConfig, len(indexers)),
	}
	for i, idx := range indexers {
		cfg.Indexers[i] = IndexerConfig{
			ID:           idx.ID,
			Kind:         idx.Kind,
			Enabled:      idx.Enabled,
			IncludeGlobs: idx.IncludeGlobs,
			ExcludeGlobs: idx.ExcludeGlobs,
			MaxFileKB:    idx.MaxFileKB,
		}
	}
	return cfg
}
