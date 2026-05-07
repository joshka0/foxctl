package semantic

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedqueue"
	"github.com/joshka0/foxctl/internal/platform/fsutil"
	"github.com/joshka0/foxctl/internal/storage/queue"
)

const SemanticEmbeddingQueueTable = "semantic_embedding_queue_jobs"

type QueueStore struct {
	queue *queue.Store
}

type FileQueueRequest struct {
	Workspace    string
	JobType      string
	Args         JobArgs
	Provider     string
	Model        string
	ChunkBytes   int
	ChunkOverlap int
	ChunkDelay   time.Duration
}

type FileQueueResult struct {
	Queued  int      `json:"queued"`
	Skipped int      `json:"skipped"`
	JobIDs  []string `json:"job_ids,omitempty"`
}

type FileQueuePayload struct {
	Task         embedqueue.Task `json:"task"`
	JobType      string          `json:"job_type"`
	Workspace    string          `json:"workspace"`
	WorkspaceID  string          `json:"workspace_id"`
	File         JobFileInput    `json:"file"`
	Reason       IndexReason     `json:"reason,omitempty"`
	TaskID       string          `json:"task_id,omitempty"`
	ReviewID     string          `json:"review_id,omitempty"`
	Provider     string          `json:"provider,omitempty"`
	Model        string          `json:"model,omitempty"`
	ChunkBytes   int             `json:"chunk_bytes,omitempty"`
	ChunkOverlap int             `json:"chunk_overlap,omitempty"`
	ChunkDelayMS int64           `json:"chunk_delay_ms,omitempty"`
}

type QueuedFileJob struct {
	ID       string           `json:"id"`
	State    queue.JobState   `json:"state"`
	Payload  FileQueuePayload `json:"payload"`
	Attempts int              `json:"attempts"`
	Error    string           `json:"error,omitempty"`
}

func OpenQueueStore(ctx context.Context, root string) (*QueueStore, error) {
	store, err := queue.OpenStore(ctx, root, embedqueue.StoreName, embedqueue.DefaultDBFile, queue.Options{Table: SemanticEmbeddingQueueTable})
	if err != nil {
		return nil, err
	}
	return &QueueStore{queue: store}, nil
}

func (s *QueueStore) Close() error {
	if s == nil || s.queue == nil {
		return nil
	}
	return s.queue.Close()
}

// EnqueueFiles queues file embedding work behind workspace-scoped dedupe keys.
//
// Index:
//
//	Purpose: Queues paced semantic file embedding jobs for repo indexing reuse.
//	Related: ClaimNext, FileQueueDedupeKey, NewFileQueuePayload
//	Keywords: embedding queue, semantic files, batching, paced embeddings
//
// [[domain:semantic-embedding-queue]]
// [[invariant:workspace-scoped-file-embedding-dedupe]]
func (s *QueueStore) EnqueueFiles(ctx context.Context, req FileQueueRequest) (*FileQueueResult, error) {
	if s == nil || s.queue == nil {
		return nil, fmt.Errorf("semantic queue store is nil")
	}
	if strings.TrimSpace(req.Workspace) == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	if err := req.Args.Validate(); err != nil {
		return nil, err
	}
	if req.JobType != JobTypeInitFiles && req.JobType != JobTypeUpdateFiles {
		return nil, fmt.Errorf("unsupported semantic queue job type %q", req.JobType)
	}

	queueReqs := make([]queue.EnqueueRequest, 0, len(req.Args.Files))
	for _, file := range req.Args.Files {
		payload, err := NewFileQueuePayload(req, file)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal semantic queue payload: %w", err)
		}
		queueReqs = append(queueReqs, queue.EnqueueRequest{
			GroupID:   req.Args.WorkspaceID,
			Payload:   raw,
			DedupeKey: FileQueueDedupeKey(payload),
			Priority:  queue.PriorityNormal,
		})
	}

	result, err := s.queue.EnqueueBatch(ctx, queueReqs)
	if err != nil {
		return nil, err
	}
	return &FileQueueResult{
		Queued:  result.Queued,
		Skipped: result.Skipped,
		JobIDs:  result.JobIDs,
	}, nil
}

func NewFileQueuePayload(req FileQueueRequest, file JobFileInput) (FileQueuePayload, error) {
	file.Path = filepath.ToSlash(strings.TrimSpace(file.Path))
	if file.Path == "" {
		return FileQueuePayload{}, fmt.Errorf("file path is required")
	}
	if file.ChangeKind == "" {
		if req.JobType == JobTypeInitFiles {
			file.ChangeKind = ChangeKindAdded
		} else {
			file.ChangeKind = ChangeKindModified
		}
	}
	if file.ChangeKind != ChangeKindDeleted {
		prepared, err := prepareFileQueueInput(req.Workspace, file)
		if err != nil {
			return FileQueuePayload{}, err
		}
		file = prepared
	}

	targetID := FileEmbeddingName(req.Args.WorkspaceID, file.Path)
	payload := FileQueuePayload{
		Task: embedqueue.Task{
			Kind:          embedqueue.TaskKindSemanticFile,
			Scope:         string(ScopeFileSummaries),
			WorkspaceID:   req.Args.WorkspaceID,
			TargetID:      targetID,
			ContentDigest: file.Digest,
			Provider:      strings.TrimSpace(req.Provider),
			Model:         strings.TrimSpace(req.Model),
		},
		JobType:      req.JobType,
		Workspace:    req.Workspace,
		WorkspaceID:  req.Args.WorkspaceID,
		File:         file,
		Reason:       req.Args.Reason,
		TaskID:       req.Args.TaskID,
		ReviewID:     req.Args.ReviewID,
		Provider:     strings.TrimSpace(req.Provider),
		Model:        strings.TrimSpace(req.Model),
		ChunkBytes:   req.ChunkBytes,
		ChunkOverlap: req.ChunkOverlap,
		ChunkDelayMS: req.ChunkDelay.Milliseconds(),
	}
	return payload, nil
}

func FileQueueDedupeKey(payload FileQueuePayload) string {
	digest := strings.TrimSpace(payload.File.Digest)
	if payload.File.ChangeKind == ChangeKindDeleted {
		digest = "deleted"
	}
	return embedqueue.StableDedupeKey(
		string(embedqueue.TaskKindSemanticFile),
		payload.WorkspaceID,
		payload.JobType,
		payload.File.Path,
		string(payload.File.ChangeKind),
		digest,
		payload.Provider,
		payload.Model,
		strconv.Itoa(payload.ChunkBytes),
		strconv.Itoa(payload.ChunkOverlap),
	)
}

func (s *QueueStore) ClaimNext(ctx context.Context, workspaceID string) (*QueuedFileJob, error) {
	job, err := s.queue.ClaimNext(ctx, queue.ClaimOptions{GroupID: workspaceID})
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, nil
	}
	var payload FileQueuePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		_ = s.queue.Fail(ctx, job.ID, fmt.Sprintf("decode semantic queue payload: %v", err)) //nolint:errcheck
		return nil, err
	}
	return &QueuedFileJob{
		ID:       job.ID,
		State:    job.State,
		Payload:  payload,
		Attempts: job.Attempts,
		Error:    job.Error,
	}, nil
}

func (s *QueueStore) Complete(ctx context.Context, jobID string) error {
	return s.queue.Complete(ctx, jobID)
}

func (s *QueueStore) Fail(ctx context.Context, jobID string, errMsg string) error {
	return s.queue.Fail(ctx, jobID, errMsg)
}

func (s *QueueStore) RequeueStaleRunning(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.queue.RequeueStaleRunning(ctx, olderThan)
}

func (s *QueueStore) Stats(ctx context.Context, workspaceID string) (*queue.Stats, error) {
	return s.queue.Stats(ctx, workspaceID)
}

func (p FileQueuePayload) ChunkDelay() time.Duration {
	if p.ChunkDelayMS <= 0 {
		return 0
	}
	return time.Duration(p.ChunkDelayMS) * time.Millisecond
}

func (p FileQueuePayload) JobArgs() JobArgs {
	return JobArgs{
		WorkspaceID: p.WorkspaceID,
		Files:       []JobFileInput{p.File},
		Reason:      p.Reason,
		TaskID:      p.TaskID,
		ReviewID:    p.ReviewID,
	}
}

func prepareFileQueueInput(workspace string, file JobFileInput) (JobFileInput, error) {
	path := filepath.Clean(file.Path)
	if filepath.IsAbs(path) || strings.HasPrefix(path, "..") {
		return JobFileInput{}, fmt.Errorf("invalid file path %q", file.Path)
	}
	fullPath := filepath.Join(workspace, path)
	info, err := os.Stat(fullPath)
	if err != nil {
		return JobFileInput{}, fmt.Errorf("stat %s: %w", file.Path, err)
	}
	if !info.Mode().IsRegular() {
		return JobFileInput{}, fmt.Errorf("%s is not a regular file", file.Path)
	}
	if info.Size() > maxReadFileSize {
		return JobFileInput{}, fmt.Errorf("%s exceeds max read size %d", file.Path, maxReadFileSize)
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return JobFileInput{}, fmt.Errorf("read %s: %w", file.Path, err)
	}
	file.SizeBytes = info.Size()
	file.Digest = computeDigest(content)
	if strings.TrimSpace(file.Language) == "" {
		file.Language = fsutil.DetectLanguage(file.Path)
	}
	return file, nil
}
