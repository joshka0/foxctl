// Package types defines core job data structures and utilities for the jobs system.
package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// State represents the lifecycle of a job.
type State string

const (
	// StateQueued represents a job waiting to be executed.
	StateQueued State = "queued"
	// StateRunning represents a job currently executing.
	StateRunning State = "running"
	// StateOK indicates the job finished successfully.
	StateOK State = "ok"
	// StateError indicates the job failed with an error.
	StateError State = "error"
	// StateCanceled indicates the job was canceled by the user.
	StateCanceled State = "canceled"
)

// Job captures the persisted metadata for a job.
type Job struct {
	ID         string    `json:"id"`
	Command    string    `json:"command"`
	ArgsJSON   string    `json:"args_json"`
	ArgsHash   string    `json:"args_hash"`
	State      State     `json:"state"`
	ResultPath string    `json:"result_path,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

var (
	// ErrNotFound indicates the requested job id does not exist.
	ErrNotFound = errors.New("jobs: not found")
	// ErrInvalidState is returned when a transition is not allowed.
	ErrInvalidState = errors.New("jobs: invalid state transition")
)

// HashArgs deterministically hashes a command and its JSON arguments payload.
func HashArgs(command string, argsJSON []byte) string {
	h := sha256.New()
	if _, err := h.Write([]byte(command)); err != nil {
		panic(fmt.Sprintf("hash command write: %v", err))
	}
	if _, err := h.Write(argsJSON); err != nil {
		panic(fmt.Sprintf("hash args write: %v", err))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// MarshalSkillArgs constructs the canonical argument payload for a skill job.
func MarshalSkillArgs(name string, input []byte) []byte {
	args := map[string]any{
		"skill": name,
	}
	if len(input) > 0 {
		sum := sha256.Sum256(input)
		args["input_size_bytes"] = len(input)
		args["input_sha256"] = hex.EncodeToString(sum[:])
	}
	buf, err := json.Marshal(args)
	if err != nil {
		return []byte("{}")
	}
	return buf
}

// ComputeSkillArgsHash deterministically computes the hash for skill inputs.
func ComputeSkillArgsHash(name string, input []byte) string {
	argsBuf := MarshalSkillArgs(name, input)
	return HashArgs(name, argsBuf)
}
