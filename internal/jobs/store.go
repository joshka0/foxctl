package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/oklog/ulid/v2"
)

// SubmitEcho creates a job that echoes the provided message.
func (s *Store) SubmitEcho(ctx context.Context, message string) (Job, error) {
	args := map[string]string{"message": message}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return Job{}, fmt.Errorf("jobs: marshal args: %w", err)
	}
	argsHash := hashArgs("echo", argsJSON)
	jobID := ulid.Make().String()
	now := time.Now().UTC()

	jobDir := filepath.Join(s.root, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return Job{}, fmt.Errorf("jobs: job dir: %w", err)
	}

	job := Job{
		ID:        jobID,
		Command:   "echo",
		ArgsJSON:  string(argsJSON),
		ArgsHash:  argsHash,
		State:     StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.insertJob(ctx, job); err != nil {
		return Job{}, err
	}

	if err := s.updateState(ctx, jobID, StateRunning, "", ""); err != nil {
		return Job{}, err
	}

	env := envelope.OK("jobs.echo", map[string]string{"message": message})
	resultPath := filepath.Join(jobDir, "result.json")
	if err := writeResult(resultPath, env); err != nil {
		_ = s.updateState(ctx, jobID, StateError, err.Error(), "")
		return Job{}, err
	}

	if err := s.updateState(ctx, jobID, StateOK, "", resultPath); err != nil {
		return Job{}, err
	}

	return s.Get(ctx, jobID)
}

// RunSkill executes a skill binary, recording its output as a job.
func (s *Store) RunSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) (Job, []byte, error) {
	executor := NewExecutor(s)
	return executor.RunSkill(ctx, manifest, artifactPath, input)
}

// PrepareSkillJob enqueues a job without executing the skill.
func (s *Store) PrepareSkillJob(ctx context.Context, name string, input []byte) (Job, error) {
	executor := NewExecutor(s)
	return executor.PrepareSkillJob(ctx, name, input)
}

// ExecutePreparedSkill runs a previously prepared job.
func (s *Store) ExecutePreparedSkill(ctx context.Context, jobID string, manifestPath string, artifactPath string) ([]byte, error) {
	executor := NewExecutor(s)
	return executor.ExecutePreparedSkill(ctx, jobID, manifestPath, artifactPath)
}

func writeResult(path string, env envelope.Envelope) error {
	buf, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("jobs: marshal result: %w", err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return fmt.Errorf("jobs: write result: %w", err)
	}
	return nil
}
