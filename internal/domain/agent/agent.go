// Package agent provides domain models for the Agent Profile extension.
package agent

import (
	"encoding/json"
	"time"
)

// State represents the lifecycle state of an agent.
type State string

const (
	// StateStarting indicates agent is being initialized.
	StateStarting State = "starting"
	// StateRunning indicates agent is active and processing.
	StateRunning State = "running"
	// StateStopped indicates agent has been gracefully stopped.
	StateStopped State = "stopped"
	// StateError indicates agent encountered a fatal error.
	StateError State = "error"
)

// Agent represents an autonomous actor in the multi-agent system.
type Agent struct {
	ID          string    `json:"id"`
	ParentID    string    `json:"parent_id,omitempty"`
	Namespace   string    `json:"ns"`
	Role        string    `json:"role,omitempty"`
	Prompt      string    `json:"prompt,omitempty"`
	SkillsAllow []string  `json:"skills_allow"`
	Policy      Policy    `json:"policy"`
	ShareBB     string    `json:"share_bb"` // all|scoped|none
	State       State     `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
	HeartbeatAt time.Time `json:"heartbeat_at,omitempty"`
}

// Policy defines execution constraints and capabilities for an agent.
type Policy struct {
	CPU             int                `json:"cpu,omitempty"`
	MemoryMB        int                `json:"memMB,omitempty"`
	Timeout         string             `json:"timeout,omitempty"` // Duration string like "20m"
	Network         string             `json:"network,omitempty"` // none|egress
	EgressAllow     []string           `json:"egressAllow,omitempty"`
	MaxOutputKB     int                `json:"max_output_kb,omitempty"`
	EnvAllow        []string           `json:"envAllow,omitempty"`
	Secrets         []string           `json:"secrets,omitempty"`
	Filesystem      []FilesystemPolicy `json:"filesystem,omitempty"`
}

// FilesystemPolicy defines filesystem access rules.
type FilesystemPolicy struct {
	Type string `json:"type"` // workdir|ro
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// Quotas defines resource limits for a namespace.
type Quotas struct {
	MaxConcurrentJobs  int `json:"max_concurrent_jobs,omitempty"`
	CPULimit           int `json:"cpu_limit,omitempty"`
	MemoryMBLimit      int `json:"memMB_limit,omitempty"`
	LLMCallsPerMin     int `json:"llm_calls_per_min,omitempty"`
	EgressBytesPerMin  int `json:"egress_bytes_per_min,omitempty"`
}

// MarshalPolicyJSON serializes a policy to JSON bytes.
func MarshalPolicyJSON(p Policy) ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalPolicyJSON deserializes a policy from JSON bytes.
func UnmarshalPolicyJSON(data []byte) (Policy, error) {
	var p Policy
	err := json.Unmarshal(data, &p)
	return p, err
}
