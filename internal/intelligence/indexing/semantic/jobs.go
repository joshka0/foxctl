package semantic

import (
	"errors"
	"strings"
	"time"
)

// Job type identifiers for semantic indexing jobs.
const (
	// JobTypeInitFiles is the job type for initial file indexing.
	JobTypeInitFiles = "semantic_index.init_files"

	// JobTypeUpdateFiles is the job type for updating existing file embeddings.
	JobTypeUpdateFiles = "semantic_index.update_files"
)

// IndexReason describes why an indexing job was triggered.
type IndexReason string

const (
	// ReasonInitialIndex indicates first-time indexing.
	ReasonInitialIndex IndexReason = "initial_index"

	// ReasonPostReview indicates indexing triggered by post-review.
	ReasonPostReview IndexReason = "post_review"

	// ReasonManual indicates explicit CLI/API-triggered indexing.
	ReasonManual IndexReason = "manual"

	// ReasonGitPull indicates indexing triggered by git pull/merge.
	ReasonGitPull IndexReason = "git_pull"
)

// FileChangeKind describes what kind of change occurred to a file.
type FileChangeKind string

// FileChangeKind values.
const (
	ChangeKindAdded    FileChangeKind = "added"
	ChangeKindModified FileChangeKind = "modified"
	ChangeKindDeleted  FileChangeKind = "deleted"
)

// JobFileInput describes a single file to be indexed.
// This mirrors the conceptual shape from semantic_file_index.md §6.2.
type JobFileInput struct {
	// Path is the workspace-relative file path.
	Path string `json:"path"`

	// Digest is the CAS digest of the file content (optional).
	// If omitted, the job reads from the filesystem.
	Digest string `json:"digest,omitempty"`

	// SizeBytes is the file size in bytes (optional).
	SizeBytes int64 `json:"size_bytes,omitempty"`

	// Language is the detected or configured language (optional).
	Language string `json:"language,omitempty"`

	// ChangeKind describes the type of change (added, modified, deleted).
	ChangeKind FileChangeKind `json:"change_kind,omitempty"`
}

// JobArgs contains the common input arguments for semantic indexing jobs.
// Used by both init_files and update_files job types.
type JobArgs struct {
	// WorkspaceID identifies the workspace being indexed.
	WorkspaceID string `json:"workspace_id"`

	// Files is the list of files to index.
	Files []JobFileInput `json:"files"`

	// Reason describes why the job was triggered.
	Reason IndexReason `json:"reason,omitempty"`

	// TaskID is the task that triggered indexing (optional).
	TaskID string `json:"task_id,omitempty"`

	// ReviewID is the review record if triggered post-review (optional).
	ReviewID string `json:"review_id,omitempty"`
}

// Validate checks that the job arguments are valid.
func (a *JobArgs) Validate() error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return errors.New("workspace_id is required")
	}
	if len(a.Files) == 0 {
		return errors.New("files list cannot be empty")
	}
	for i, f := range a.Files {
		if strings.TrimSpace(f.Path) == "" {
			return errors.New("file path is required at index " + itoa(i))
		}
	}
	return nil
}

// JobSummary contains the summary counts from a completed indexing job.
// This matches the data.summary shape from semantic_file_index.md §6.4.
type JobSummary struct {
	// FilesIndexed is the number of files successfully indexed.
	FilesIndexed int `json:"files_indexed"`

	// ChunksIndexed is the number of chunks indexed (for chunked files).
	ChunksIndexed int `json:"chunks_indexed"`

	// FilesSkipped is the number of files skipped (e.g., filtered by globs).
	FilesSkipped int `json:"files_skipped"`

	// ChunkPlannerCounts counts chunks by planner kind.
	ChunkPlannerCounts map[string]int `json:"chunk_planner_counts,omitempty"`

	// ChunkSizeBytes summarizes semantic chunk sizes.
	ChunkSizeBytes *ChunkSizeSummary `json:"chunk_size_bytes,omitempty"`
}

// ChunkSizeSummary captures simple distribution metrics for semantic chunks.
type ChunkSizeSummary struct {
	Count        int   `json:"count"`
	MinBytes     int64 `json:"min_bytes"`
	MaxBytes     int64 `json:"max_bytes"`
	TotalBytes   int64 `json:"total_bytes"`
	AverageBytes int64 `json:"average_bytes"`
}

// JobFailure describes a per-file failure during indexing.
// This matches the data.failures[*] shape from semantic_file_index.md §6.4.
type JobFailure struct {
	// File contains path and optional digest of the failed file.
	File JobFailureFile `json:"file"`

	// ErrorCode is one of the standard error codes from §11.
	ErrorCode string `json:"error_code"`

	// ErrorMessage is a human-readable description of the error.
	ErrorMessage string `json:"error_message"`

	// ProviderRequestID is the request ID from the embedding provider (optional).
	ProviderRequestID string `json:"provider_request_id,omitempty"`

	// Timestamp is when the failure occurred (RFC3339 UTC).
	Timestamp time.Time `json:"timestamp"`
}

// JobFailureFile identifies the file that failed.
type JobFailureFile struct {
	// Path is the workspace-relative file path.
	Path string `json:"path"`

	// Digest is the CAS digest (optional; may be empty on read failures).
	Digest string `json:"digest,omitempty"`
}

// CASArtifact describes a CAS-stored artifact containing detailed results.
// This matches the data.cas_artifact shape from semantic_file_index.md §6.4.
type CASArtifact struct {
	// ArtifactID is the artifact identifier.
	ArtifactID string `json:"artifact_id"`

	// Path is the path within the job directory.
	Path string `json:"path"`

	// Digest is the CAS digest of the artifact.
	Digest string `json:"digest"`

	// EntriesCount is the number of entries in the artifact.
	EntriesCount int `json:"entries_count"`
}

// JobResult contains the complete result of an indexing job.
// This matches the data shape from semantic_file_index.md §6.4.
type JobResult struct {
	// Summary contains the numeric counts.
	Summary JobSummary `json:"summary"`

	// Failures contains per-file failure details.
	Failures []JobFailure `json:"failures,omitempty"`

	// CASArtifact points to detailed per-file/chunk results (optional).
	CASArtifact *CASArtifact `json:"cas_artifact,omitempty"`
}

// HasFailures returns true if there are any per-file failures.
func (r *JobResult) HasFailures() bool {
	return len(r.Failures) > 0
}

// IsPartialSuccess returns true if the job succeeded but with some failures.
func (r *JobResult) IsPartialSuccess() bool {
	return r.Summary.FilesIndexed > 0 && r.HasFailures()
}

// Standard error codes from semantic_file_index.md §11.
const (
	// ErrCodeSemanticIndexNotFound indicates the requested file has no index entry.
	ErrCodeSemanticIndexNotFound = "SEMANTIC_INDEX_NOT_FOUND"

	// ErrCodeProviderConfigInvalid indicates invalid embedding provider configuration.
	ErrCodeProviderConfigInvalid = "PROVIDER_CONFIG_INVALID"

	// ErrCodeChunkBoundaryMismatch indicates existing chunks don't match current config.
	ErrCodeChunkBoundaryMismatch = "CHUNK_BOUNDARY_MISMATCH"

	// ErrCodeVectorNotEnabled indicates vector search is not enabled.
	ErrCodeVectorNotEnabled = "VECTOR_NOT_ENABLED"

	// ErrCodeEmbeddingProviderFailure indicates the embedding provider returned an error.
	ErrCodeEmbeddingProviderFailure = "EMBEDDING_PROVIDER_FAILURE"

	// ErrCodeCASResolveError indicates a CAS read/write failure.
	ErrCodeCASResolveError = "CAS_RESOLVE_ERROR"
)

// IsRecoverableError returns true if the error code represents a recoverable error.
// Recoverable errors may be retried within a single job attempt.
func IsRecoverableError(code string) bool {
	switch code {
	case ErrCodeSemanticIndexNotFound,
		ErrCodeChunkBoundaryMismatch,
		ErrCodeVectorNotEnabled,
		ErrCodeEmbeddingProviderFailure:
		return true
	case ErrCodeProviderConfigInvalid,
		ErrCodeCASResolveError:
		return false
	default:
		return false
	}
}

// itoa is a simple int-to-string helper to avoid importing strconv.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
