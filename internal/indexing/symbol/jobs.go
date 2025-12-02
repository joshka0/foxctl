// Package symbol implements the code symbol index as a post-review indexer.
// This file defines job contracts for code_symbol_index.init_files and
// code_symbol_index.update_files jobs per code_symbol_index_and_swe_grep.md spec.
package symbol

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing"
)

// Job type identifiers for symbol indexing jobs.
const (
	// JobTypeInitFiles is the job type for initial symbol indexing.
	JobTypeInitFiles = "code_symbol_index.init_files"

	// JobTypeUpdateFiles is the job type for updating existing symbol entries.
	JobTypeUpdateFiles = "code_symbol_index.update_files"
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
)

// JobFileInput describes a single file to be indexed.
// This mirrors the semantic index job shape for consistency.
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
	ChangeKind indexing.ChangeKind `json:"change_kind,omitempty"`
}

// JobArgs contains the common input arguments for symbol indexing jobs.
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
	if a.WorkspaceID == "" {
		return errors.New("workspace_id is required")
	}
	if len(a.Files) == 0 {
		return errors.New("files list cannot be empty")
	}
	for i, f := range a.Files {
		if f.Path == "" {
			return errors.New("file path is required at index " + strconv.Itoa(i))
		}
	}
	return nil
}

// JobSummary contains the summary counts from a completed indexing job.
type JobSummary struct {
	// FilesIndexed is the number of files successfully indexed.
	FilesIndexed int `json:"files_indexed"`

	// SymbolsIndexed is the number of symbols indexed across all files.
	SymbolsIndexed int `json:"symbols_indexed"`

	// FilesSkipped is the number of files skipped (e.g., unchanged, unsupported language).
	FilesSkipped int `json:"files_skipped"`
}

// JobFailure describes a per-file failure during indexing.
type JobFailure struct {
	// File contains path and optional digest of the failed file.
	File JobFailureFile `json:"file"`

	// ErrorCode is a canonical AGENTS.md error code (e.g., ENOTFOUND, EIO, ERUNTIME).
	ErrorCode string `json:"error_code"`

	// ErrorMessage is a human-readable description of the error.
	ErrorMessage string `json:"error_message"`

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

// JobResult contains the complete result of an indexing job.
type JobResult struct {
	// Summary contains the numeric counts.
	Summary JobSummary `json:"summary"`

	// Failures contains per-file failure details.
	Failures []JobFailure `json:"failures,omitempty"`
}

// HasFailures returns true if there are any per-file failures.
func (r *JobResult) HasFailures() bool {
	return len(r.Failures) > 0
}

// IsPartialSuccess returns true if the job succeeded but with some failures.
func (r *JobResult) IsPartialSuccess() bool {
	return r.Summary.FilesIndexed > 0 && r.HasFailures()
}

// Canonical AGENTS.md error codes for symbol indexing operations.
// These map internal error conditions to standard envelope error codes.
const (
	// ErrCodeSymbolIndexNotFound indicates the requested file has no index entry.
	// Maps to ENOTFOUND per AGENTS.md.
	ErrCodeSymbolIndexNotFound = "ENOTFOUND"

	// ErrCodeExtractorNotFound indicates no extractor for the file's language.
	// Maps to ENOTFOUND per AGENTS.md.
	ErrCodeExtractorNotFound = "ENOTFOUND"

	// ErrCodeExtractFailed indicates symbol extraction failed.
	// Maps to ERUNTIME per AGENTS.md.
	ErrCodeExtractFailed = "ERUNTIME"

	// ErrCodeFileReadError indicates a file read/access failure.
	// Maps to EIO per AGENTS.md.
	ErrCodeFileReadError = "EIO"

	// ErrCodeFileTooLarge indicates the file exceeds size limits.
	// Maps to EOUTPUT_TOO_LARGE per AGENTS.md.
	ErrCodeFileTooLarge = "EOUTPUT_TOO_LARGE"
)

// IsRecoverableError returns true if the error code represents a recoverable error.
// Recoverable errors may be retried within a single job attempt.
func IsRecoverableError(code string) bool {
	switch code {
	case "ENOTFOUND":
		return true
	case "ERUNTIME", "EIO", "EOUTPUT_TOO_LARGE":
		return false
	default:
		return false
	}
}

// RunInitFilesJob executes a code_symbol_index.init_files job in-process.
// It indexes all provided files, treating them as new additions.
func (idx *Indexer) RunInitFilesJob(ctx context.Context, args JobArgs) (*JobResult, error) {
	if err := args.Validate(); err != nil {
		return nil, err
	}
	return idx.runIndexJob(ctx, args, true)
}

// RunUpdateFilesJob executes a code_symbol_index.update_files job in-process.
// It processes file changes (add, modify, delete) based on ChangeKind.
func (idx *Indexer) RunUpdateFilesJob(ctx context.Context, args JobArgs) (*JobResult, error) {
	if err := args.Validate(); err != nil {
		return nil, err
	}
	return idx.runIndexJob(ctx, args, false)
}

// runIndexJob is the shared implementation for init_files and update_files.
// isInit=true treats all files as new (ignores ChangeKind).
func (idx *Indexer) runIndexJob(ctx context.Context, args JobArgs, isInit bool) (*JobResult, error) {
	// Convert JobArgs to PostReviewEvent for the existing Index method
	event := indexing.PostReviewEvent{
		WorkspaceID: args.WorkspaceID,
		TaskID:      args.TaskID,
		ReviewID:    args.ReviewID,
		Reason:      string(args.Reason),
		Files:       make([]indexing.FileChange, 0, len(args.Files)),
	}

	for _, file := range args.Files {
		changeKind := file.ChangeKind
		if isInit {
			changeKind = indexing.ChangeKindAdded
		}
		event.Files = append(event.Files, indexing.FileChange{
			Path:       file.Path,
			Digest:     file.Digest,
			SizeBytes:  file.SizeBytes,
			Language:   file.Language,
			ChangeKind: changeKind,
		})
	}

	// Run the existing Index method
	indexResult, err := idx.Index(ctx, event)
	if err != nil {
		return nil, err
	}

	// Convert IndexerResult to JobResult
	result := &JobResult{
		Summary: JobSummary{
			FilesIndexed: indexResult.FilesIndexed,
			FilesSkipped: indexResult.FilesSkipped,
			// SymbolsIndexed would require tracking in the indexer; for now leave as 0
		},
		Failures: []JobFailure{}, // Initialize to empty slice for stable JSON serialization
	}

	// Convert failures
	for _, f := range indexResult.Failures {
		result.Failures = append(result.Failures, JobFailure{
			File: JobFailureFile{
				Path: f.Path,
			},
			ErrorCode:    f.ErrorCode,
			ErrorMessage: f.ErrorMessage,
			Timestamp:    time.Now().UTC(),
		})
	}

	return result, nil
}
