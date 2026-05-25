package agentmanager

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/agents"
	"github.com/joshka0/foxctl/internal/storage/mailbox"
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
	ParentNS        string
	WorkspaceRoot   string
	WorkspaceSource string
	Name            string // Human name (e.g., "Luna", "Atlas")
	Slug            string // Human-readable handle for referencing (e.g., "researcher", "companion")
	Role            string
	Prompt          string
	SkillsAllow     []string
	Policy          agent.Policy
	ShareBB         string // all|scoped|none
	MemoryScope     agent.MemoryScope
	MemoryRetention agent.MemoryRetention
	LLMProvider     string // Per-agent LLM provider override
	LLMModel        string // Per-agent LLM model override
	LLMAPIKey       string // Per-agent LLM API key override
	LLMBaseURL      string // Per-agent LLM base URL override
	LLMAuthMode     string // Per-agent LLM auth mode override
	LLMAuthHeader   string // Per-agent LLM auth header override
	LLMAuthPrefix   string // Per-agent LLM auth prefix override
	SandboxProvider string
	SandboxID       string
	RepoURL         string
	RepoRef         string
	TerminalBinding agent.TerminalBinding

	// Execution mode configuration
	ExecMode      agent.ExecutionMode // reactive|autonomous|proactive|tick (default: reactive)
	MaxIterations int                 // Max tool calls per turn (default: 10)
	MaxAutoTurns  int                 // Max autonomous turns per session (default: 1)
	ThinkInterval int                 // Seconds between proactive/tick cycles (default: 60)
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

	// Apply defaults for execution mode settings
	execMode := req.ExecMode
	if execMode == "" {
		execMode = agent.ModeReactive
	}
	maxIterations := req.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 10 // Default: 10 tool calls per turn
	}
	maxAutoTurns := req.MaxAutoTurns
	if maxAutoTurns <= 0 {
		maxAutoTurns = 1 // Default: 1 turn (no autonomous continuation)
	}
	thinkInterval := req.ThinkInterval
	if thinkInterval <= 0 {
		thinkInterval = 60 // Default: 60 seconds between proactive cycles
	}

	a := agent.Agent{
		ID:              agentID,
		ParentID:        parentID,
		Namespace:       ns,
		WorkspaceRoot:   strings.TrimSpace(req.WorkspaceRoot),
		WorkspaceSource: strings.TrimSpace(req.WorkspaceSource),
		Name:            req.Name,
		Slug:            req.Slug,
		Role:            req.Role,
		Prompt:          req.Prompt,
		SkillsAllow:     req.SkillsAllow,
		Policy:          req.Policy,
		ShareBB:         req.ShareBB,
		MemoryScope:     agent.NormalizeMemoryScope(req.MemoryScope),
		MemoryRetention: func() agent.MemoryRetention {
			if req.MemoryRetention == "" {
				return agent.DefaultMemoryRetentionForScope(agent.NormalizeMemoryScope(req.MemoryScope))
			}
			return agent.NormalizeMemoryRetention(req.MemoryRetention)
		}(),
		State:           agent.StateStarting,
		CreatedAt:       now,
		HeartbeatAt:     now,
		LLMProvider:     req.LLMProvider,
		LLMModel:        req.LLMModel,
		LLMAPIKey:       req.LLMAPIKey,
		LLMBaseURL:      req.LLMBaseURL,
		LLMAuthMode:     req.LLMAuthMode,
		LLMAuthHeader:   req.LLMAuthHeader,
		LLMAuthPrefix:   req.LLMAuthPrefix,
		SandboxProvider: strings.TrimSpace(req.SandboxProvider),
		SandboxID:       strings.TrimSpace(req.SandboxID),
		RepoURL:         strings.TrimSpace(req.RepoURL),
		RepoRef:         strings.TrimSpace(req.RepoRef),
		TerminalBinding: agent.NormalizeTerminalBinding(req.TerminalBinding),
		ExecMode:        execMode,
		ExecutionLayer:  agent.ExecutionLayerClassic,
		MaxIterations:   maxIterations,
		MaxAutoTurns:    maxAutoTurns,
		ThinkInterval:   thinkInterval,
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
	AgentID  string
	Graceful bool
	TimeoutS int
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

// validatePolicyNarrowing ensures child policy is narrower than parent.
func validatePolicyNarrowing(parent, child agent.Policy) error {
	if err := validatePolicyLimitNarrowing("CPU", parent.CPU, child.CPU); err != nil {
		return err
	}
	if err := validatePolicyLimitNarrowing("memory", parent.MemoryMB, child.MemoryMB); err != nil {
		return err
	}

	parentTimeout, parentHasTimeout, err := parsePolicyTimeout(parent.Timeout)
	if err != nil {
		return fmt.Errorf("invalid parent timeout: %w", err)
	}
	childTimeout, childHasTimeout, err := parsePolicyTimeout(child.Timeout)
	if err != nil {
		return fmt.Errorf("invalid child timeout: %w", err)
	}
	if parentHasTimeout && (!childHasTimeout || childTimeout > parentTimeout) {
		return fmt.Errorf("child timeout exceeds parent timeout")
	}

	if err := validatePolicyLimitNarrowing("MaxOutputKB", parent.MaxOutputKB, child.MaxOutputKB); err != nil {
		return err
	}

	// Filesystem: child mounts must be no broader than parent mounts.
	if err := validateFilesystemNarrowing(parent.Filesystem, child.Filesystem); err != nil {
		return err
	}

	parentNetwork, err := networkPolicyRank(parent.Network)
	if err != nil {
		return fmt.Errorf("invalid parent network policy: %w", err)
	}
	childNetwork, err := networkPolicyRank(child.Network)
	if err != nil {
		return fmt.Errorf("invalid child network policy: %w", err)
	}
	if childNetwork > parentNetwork {
		return fmt.Errorf("child network (%s) is less restrictive than parent (%s)", normalizeNetworkPolicy(child.Network), normalizeNetworkPolicy(parent.Network))
	}

	// Egress: child must be subset of parent
	if childNetwork == networkPolicyEgress && parentNetwork == networkPolicyEgress {
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

func validatePolicyLimitNarrowing(name string, parent, child int) error {
	if parent < 0 {
		return fmt.Errorf("invalid parent %s (%d)", name, parent)
	}
	if child < 0 {
		return fmt.Errorf("invalid child %s (%d)", name, child)
	}
	if parent <= 0 {
		return nil
	}
	if child <= 0 {
		return fmt.Errorf("child %s must be set when parent %s is set", name, name)
	}
	if child > parent {
		return fmt.Errorf("child %s (%d) exceeds parent %s (%d)", name, child, name, parent)
	}
	return nil
}

type filesystemMountKey struct {
	from string
	to   string
}

func validateFilesystemNarrowing(parent, child []agent.FilesystemPolicy) error {
	parentMounts := make(map[filesystemMountKey]int)
	for i, fs := range parent {
		key, rank, err := filesystemPolicyRank(fs)
		if err != nil {
			return fmt.Errorf("invalid parent filesystem policy[%d]: %w", i, err)
		}
		if currentRank, ok := parentMounts[key]; !ok || rank > currentRank {
			parentMounts[key] = rank
		}
	}

	for i, fs := range child {
		key, childRank, err := filesystemPolicyRank(fs)
		if err != nil {
			return fmt.Errorf("invalid child filesystem policy[%d]: %w", i, err)
		}
		parentRank, ok := parentMounts[key]
		if !ok {
			return fmt.Errorf("child filesystem mount %s from %s not allowed by parent", key.to, key.from)
		}
		if childRank > parentRank {
			return fmt.Errorf("child filesystem mount %s from %s is less restrictive than parent", key.to, key.from)
		}
	}

	return nil
}

func filesystemPolicyRank(fs agent.FilesystemPolicy) (filesystemMountKey, int, error) {
	key := filesystemMountKey{
		from: strings.TrimSpace(fs.From),
		to:   strings.TrimSpace(fs.To),
	}
	if key.from == "" {
		return key, 0, fmt.Errorf("from is required")
	}
	if key.to == "" {
		return key, 0, fmt.Errorf("to is required")
	}

	switch strings.TrimSpace(fs.Type) {
	case "ro":
		return key, 0, nil
	case "workdir":
		return key, 1, nil
	default:
		return key, 0, fmt.Errorf("unsupported type %q", fs.Type)
	}
}

// validateSkillsAllowlist ensures child skills are subset of parent.
func validateSkillsAllowlist(parent, child []string) error {
	if !isSubset(child, parent) {
		return fmt.Errorf("child skills_allow is not a subset of parent skills_allow")
	}
	return nil
}

const (
	networkPolicyNone = iota
	networkPolicyEgress
)

func networkPolicyRank(value string) (int, error) {
	switch normalizeNetworkPolicy(value) {
	case "none":
		return networkPolicyNone, nil
	case "egress":
		return networkPolicyEgress, nil
	default:
		return 0, fmt.Errorf("unsupported network policy %q", value)
	}
}

func normalizeNetworkPolicy(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "none"
	}
	return value
}

func parsePolicyTimeout(value string) (time.Duration, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, false, err
	}
	return d, true, nil
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
