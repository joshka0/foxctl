// Package agentmanager provides agent lifecycle management for the Agent Profile.
package agentmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/oklog/ulid/v2"
)

// Manager handles agent lifecycle operations.
type Manager struct {
	agentStore   agents.Store
	mailboxStore mailbox.Store
}

// New creates a new agent manager.
func New(agentStore agents.Store, mailboxStore mailbox.Store) *Manager {
	return &Manager{
		agentStore:   agentStore,
		mailboxStore: mailboxStore,
	}
}

// SpawnRequest contains parameters for spawning a new agent.
type SpawnRequest struct {
	ParentNS    string
	Role        string
	Prompt      string
	SkillsAllow []string
	Policy      agent.Policy
	ShareBB     string // all|scoped|none
}

// SpawnResponse contains the result of spawning an agent.
type SpawnResponse struct {
	AgentID string
	NS      string
	Role    string
}

// Spawn creates a new agent instance.
func (m *Manager) Spawn(ctx context.Context, req SpawnRequest) (SpawnResponse, error) {
	var parentID string

	// Validate policy narrowing if parent exists
	if req.ParentNS != "" {
		parent, err := m.agentStore.GetByNamespace(ctx, req.ParentNS)
		if err != nil {
			return SpawnResponse{}, fmt.Errorf("get parent agent: %w", err)
		}

		if err := validatePolicyNarrowing(parent.Policy, req.Policy); err != nil {
			return SpawnResponse{}, fmt.Errorf("policy narrowing validation failed: %w", err)
		}

		// Validate skills allowlist
		if err := validateSkillsAllowlist(parent.SkillsAllow, req.SkillsAllow); err != nil {
			return SpawnResponse{}, fmt.Errorf("skills allowlist validation failed: %w", err)
		}

		parentID = parent.ID
	}

	// Generate agent ID
	agentID := ulid.Make().String()

	// Build namespace
	ns := buildNamespace(req.ParentNS, agentID)

	// Create agent record
	now := time.Now().UTC()
	a := agent.Agent{
		ID:          agentID,
		ParentID:    parentID,
		Namespace:   ns,
		Role:        req.Role,
		Prompt:      req.Prompt,
		SkillsAllow: req.SkillsAllow,
		Policy:      req.Policy,
		ShareBB:     req.ShareBB,
		State:       agent.StateStarting,
		CreatedAt:   now,
		HeartbeatAt: now,
	}

	if err := m.agentStore.Create(ctx, a); err != nil {
		return SpawnResponse{}, fmt.Errorf("create agent: %w", err)
	}

	return SpawnResponse{
		AgentID: agentID,
		NS:      ns,
		Role:    req.Role,
	}, nil
}

// KillRequest contains parameters for terminating an agent.
type KillRequest struct {
	AgentID   string
	Graceful  bool
	TimeoutS  int
}

// KillResponse contains the result of killing an agent.
type KillResponse struct {
	AgentID     string
	FinalStatus agent.State
	ExitCode    int
}

// Kill terminates an agent.
// TODO: Graceful and TimeoutS fields are currently unused and will be wired
// into the runtime when termination logic is implemented.
func (m *Manager) Kill(ctx context.Context, req KillRequest) (KillResponse, error) {
	if _, err := m.agentStore.Get(ctx, req.AgentID); err != nil {
		return KillResponse{}, fmt.Errorf("get agent: %w", err)
	}

	// Update state to stopped
	if err := m.agentStore.UpdateState(ctx, req.AgentID, agent.StateStopped); err != nil {
		return KillResponse{}, fmt.Errorf("update state: %w", err)
	}

	return KillResponse{
		AgentID:     req.AgentID,
		FinalStatus: agent.StateStopped,
		ExitCode:    0, // TODO: surface real exit code once the runtime provides it
	}, nil
}

// Heartbeat updates the agent's heartbeat timestamp.
func (m *Manager) Heartbeat(ctx context.Context, agentID string) error {
	return m.agentStore.UpdateHeartbeat(ctx, agentID)
}

// GetAgent retrieves agent information.
func (m *Manager) GetAgent(ctx context.Context, agentID string) (agent.Agent, error) {
	return m.agentStore.Get(ctx, agentID)
}

// ListAgents lists all agents.
func (m *Manager) ListAgents(ctx context.Context, limit int) ([]agent.Agent, error) {
	return m.agentStore.List(ctx, limit)
}

// buildNamespace constructs a hierarchical namespace.
func buildNamespace(parentNS, agentID string) string {
	if parentNS == "" {
		return agentID
	}
	return parentNS + "/child-" + agentID
}

// extractParentID extracts the parent agent ID from a namespace.
func extractParentID(parentNS string) string {
	if parentNS == "" {
		return ""
	}
	// This is a simplified implementation
	// In a real system, you'd parse the namespace properly
	return parentNS
}

// validatePolicyNarrowing ensures child policy is narrower than parent.
func validatePolicyNarrowing(parent, child agent.Policy) error {
	// CPU: child must not exceed parent
	if child.CPU > parent.CPU {
		return fmt.Errorf("child CPU (%d) exceeds parent CPU (%d)", child.CPU, parent.CPU)
	}

	// Memory: child must not exceed parent
	if child.MemoryMB > parent.MemoryMB {
		return fmt.Errorf("child memory (%d MB) exceeds parent memory (%d MB)", child.MemoryMB, parent.MemoryMB)
	}

	// Network: child must be more restrictive
	if parent.Network == "none" && child.Network == "egress" {
		return fmt.Errorf("child network (egress) is less restrictive than parent (none)")
	}

	// Egress: child must be subset of parent
	if child.Network == "egress" && parent.Network == "egress" {
		if !isSubset(child.EgressAllow, parent.EgressAllow) {
			return fmt.Errorf("child egressAllow is not a subset of parent egressAllow")
		}
	}

	// Secrets: child must be subset of parent
	if !isSubset(child.Secrets, parent.Secrets) {
		return fmt.Errorf("child secrets is not a subset of parent secrets")
	}

	// Env: child must be subset of parent
	if !isSubset(child.EnvAllow, parent.EnvAllow) {
		return fmt.Errorf("child envAllow is not a subset of parent envAllow")
	}

	return nil
}

// validateSkillsAllowlist ensures child skills are subset of parent.
func validateSkillsAllowlist(parent, child []string) error {
	if !isSubset(child, parent) {
		return fmt.Errorf("child skills_allow is not a subset of parent skills_allow")
	}
	return nil
}

// isSubset checks if child is a subset of parent.
func isSubset(child, parent []string) bool {
	parentMap := make(map[string]bool)
	for _, p := range parent {
		parentMap[p] = true
	}

	for _, c := range child {
		if !parentMap[c] {
			return false
		}
	}
	return true
}
