package types

import (
	"crypto/sha256"
	"encoding/binary"
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

const (
	// DefaultMaxJobAge caps how long a job can remain running before being recovered.
	DefaultMaxJobAge = time.Hour
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
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
}

var (
	// ErrNotFound indicates the requested job id does not exist.
	ErrNotFound = errors.New("jobs: not found")
	// ErrInvalidState is returned when a transition is not allowed.
	ErrInvalidState = errors.New("jobs: invalid state transition")
)

// ValidateState rejects states outside the persisted job lifecycle.
func ValidateState(state State) error {
	switch state {
	case StateQueued, StateRunning, StateOK, StateError, StateCanceled:
		return nil
	default:
		return fmt.Errorf("%w: unknown state %q", ErrInvalidState, state)
	}
}

// HashArgs deterministically hashes a command and its JSON arguments payload.
func HashArgs(command string, argsJSON []byte) string {
	h := sha256.New()
	var length [8]byte
	writeField := func(label string, value []byte) {
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		if _, err := h.Write(length[:]); err != nil {
			panic(fmt.Sprintf("hash %s length write: %v", label, err))
		}
		if _, err := h.Write(value); err != nil {
			panic(fmt.Sprintf("hash %s write: %v", label, err))
		}
	}

	writeField("command", []byte(command))
	writeField("args", argsJSON)
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
