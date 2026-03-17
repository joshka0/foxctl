package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	agenttools "github.com/jkatigb/agentctl/internal/agent/tools"
	"github.com/jkatigb/agentctl/internal/agent/types"
)

// OverseerActorID is the canonical actor ID for the overseer.
const OverseerActorID = "actor:system:overseer"

// Overseer coordinates agent spawning and manages the agent hierarchy.
// It implements SpawnHandler and controls session creation for subagents.
type Overseer struct {
	runtime *Runtime
	config  OverseerConfig

	mu       sync.RWMutex
	children map[string][]string // parentSessionID -> childSessionIDs
}

// OverseerConfig configures the overseer behavior.
type OverseerConfig struct {
	// MaxDepth is the global maximum hierarchy depth.
	// 0 = overseer only, 1 = overseer + agents, 2 = overseer + agents + subagents, etc.
	MaxDepth int

	// MaxConcurrentAgents is the maximum number of concurrent agent sessions.
	MaxConcurrentAgents int

	// AllowedRoles restricts which roles can be spawned.
	// Empty means all roles are allowed.
	AllowedRoles []types.AgentRole

	// PolicyValidator is called before spawning to enforce custom policies.
	// If nil, all spawn requests that pass depth checks are allowed.
	PolicyValidator func(req types.SpawnRequest) error
}

// NewOverseer creates a new overseer attached to a runtime.
func NewOverseer(rt *Runtime, cfg OverseerConfig) *Overseer {
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 3 // Default: overseer -> agent -> subagent
	}
	if cfg.MaxConcurrentAgents <= 0 {
		cfg.MaxConcurrentAgents = 10
	}

	o := &Overseer{
		runtime:  rt,
		config:   cfg,
		children: make(map[string][]string),
	}

	// Configure runtime to use this overseer as spawn handler
	rt.config.SpawnHandler = o
	rt.config.DefaultMaxDepth = cfg.MaxDepth
	// Pass limit to runtime for atomic enforcement in Spawn()
	rt.config.MaxConcurrentAgents = cfg.MaxConcurrentAgents

	return o
}

// HandleSpawnRequest implements SpawnHandler.
// It validates the spawn request and creates new agent sessions for approved subagents.
func (o *Overseer) HandleSpawnRequest(ctx context.Context, req types.SpawnRequest) (*types.SpawnResponse, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	resp := &types.SpawnResponse{
		SpawnedAgents: []types.SpawnedAgent{},
		DeniedAgents:  []types.DeniedAgent{},
	}

	// Advisory early check for concurrent agent limit.
	// The actual enforcement happens atomically in Runtime.Spawn() to avoid TOCTOU races.
	// This check provides a friendlier error response before attempting spawns.
	activeSessions := len(o.runtime.List())
	if activeSessions >= o.config.MaxConcurrentAgents {
		resp.Accepted = false
		resp.Reason = "resource_exhausted: max concurrent agents reached"
		resp.Suggestion = fmt.Sprintf("Wait for existing agents to complete. Current: %d, Max: %d",
			activeSessions, o.config.MaxConcurrentAgents)
		return resp, nil
	}

	// Validate each requested subagent
	for _, sub := range req.RequestedSubagents {
		// Check depth limits
		if err := agenttools.ValidateSpawnDepth(req.CallerDepth, req.CallerMaxDepth, req.CallerLocalMaxDepth); err != nil {
			resp.DeniedAgents = append(resp.DeniedAgents, types.DeniedAgent{
				Role:   sub.Role,
				Task:   sub.Task,
				Reason: err.Error(),
			})
			continue
		}

		// Check allowed roles
		if len(o.config.AllowedRoles) > 0 {
			allowed := false
			for _, r := range o.config.AllowedRoles {
				if r == sub.Role {
					allowed = true
					break
				}
			}
			if !allowed {
				resp.DeniedAgents = append(resp.DeniedAgents, types.DeniedAgent{
					Role:   sub.Role,
					Task:   sub.Task,
					Reason: types.DenialPolicyViolation,
				})
				continue
			}
		}

		// Run custom policy validator
		if o.config.PolicyValidator != nil {
			if err := o.config.PolicyValidator(req); err != nil {
				resp.DeniedAgents = append(resp.DeniedAgents, types.DeniedAgent{
					Role:   sub.Role,
					Task:   sub.Task,
					Reason: fmt.Sprintf("%s: %v", types.DenialPolicyViolation, err),
				})
				continue
			}
		}

		// Compute child depth limits
		childDepth, childMaxDepth, childLocalMaxDepth := agenttools.ComputeChildDepthLimits(
			req.CallerDepth,
			req.CallerMaxDepth,
			req.CallerLocalMaxDepth,
			sub.LocalMaxDepth,
		)

		// Generate actor ID for child
		actorID := sub.SuggestedActorID
		if actorID == "" {
			actorID = fmt.Sprintf("actor:agent:%s:%s", sub.Role, req.EpicID)
		}

		// Create child config
		childCfg := types.AgentConfig{
			Role:          sub.Role,
			ActorID:       actorID,
			WorkspaceID:   "", // Will be inherited from runtime
			EpicID:        req.EpicID,
			TaskID:        "",              // Can be set by caller or plan
			Prompt:        sub.Task,        // Pass the task as the agent's prompt
			RootActorID:   OverseerActorID, // Always the tree root
			ParentActorID: req.CallerActorID,
			Depth:         childDepth,
			MaxDepth:      childMaxDepth,
			LocalMaxDepth: childLocalMaxDepth,
			LLMProvider:   sub.LLMProvider, // Pass LLM config from spawn request
			LLMModel:      sub.LLMModel,
			LLMBaseURL:    sub.LLMBaseURL,
			LLMAuthMode:   sub.LLMAuthMode,
			LLMAuthHeader: sub.LLMAuthHeader,
			LLMAuthPrefix: sub.LLMAuthPrefix,
		}

		// Spawn the child session
		session, err := o.runtime.Spawn(ctx, childCfg)
		if err != nil {
			resp.DeniedAgents = append(resp.DeniedAgents, types.DeniedAgent{
				Role:   sub.Role,
				Task:   sub.Task,
				Reason: fmt.Sprintf("spawn_failed: %v", err),
			})
			continue
		}

		// Track parent-child relationship using thread-safe methods
		parentSessionID := o.runtime.FindSessionByActorID(req.CallerActorID)
		if parentSessionID != "" {
			o.children[parentSessionID] = append(o.children[parentSessionID], session.ID)
			// Update parent session's children list
			if parentSession, ok := o.runtime.Get(parentSessionID); ok {
				parentSession.mu.Lock()
				parentSession.Children = append(parentSession.Children, session.ID)
				parentSession.mu.Unlock()
			}
		}

		resp.SpawnedAgents = append(resp.SpawnedAgents, types.SpawnedAgent{
			SessionID: session.ID,
			ActorID:   actorID,
			Depth:     childDepth,
		})
	}

	// Set overall response status
	resp.Accepted = len(resp.SpawnedAgents) > 0
	if len(resp.DeniedAgents) > 0 && len(resp.SpawnedAgents) == 0 {
		resp.Reason = "all requested agents were denied"
	} else if len(resp.DeniedAgents) > 0 {
		resp.Reason = "some agents were denied"
	}

	return resp, nil
}

// SpawnOverseerAgent spawns the root overseer agent session.
func (o *Overseer) SpawnOverseerAgent(ctx context.Context, epicID string, prompt string) (*Session, error) {
	cfg := types.AgentConfig{
		Role:          types.RoleOverseer, // Overseer uses its dedicated role
		ActorID:       OverseerActorID,
		EpicID:        epicID,
		RootActorID:   OverseerActorID,
		ParentActorID: "", // No parent for overseer
		Depth:         0,
		MaxDepth:      o.config.MaxDepth,
		LocalMaxDepth: o.config.MaxDepth,
		Prompt:        prompt,
	}

	return o.runtime.Spawn(ctx, cfg)
}

// GetChildren returns the child session IDs for a parent session.
func (o *Overseer) GetChildren(parentSessionID string) []string {
	o.mu.RLock()
	defer o.mu.RUnlock()

	children := o.children[parentSessionID]
	result := make([]string, len(children))
	copy(result, children)
	return result
}

// GetHierarchy returns the full hierarchy starting from a session.
func (o *Overseer) GetHierarchy(sessionID string) *HierarchyNode {
	o.mu.RLock()
	defer o.mu.RUnlock()

	session, ok := o.runtime.Get(sessionID)
	if !ok {
		return nil
	}

	return o.buildHierarchyNode(session)
}

// HierarchyNode represents a node in the agent hierarchy tree.
type HierarchyNode struct {
	SessionID string
	ActorID   string
	Role      types.AgentRole
	Depth     int
	Status    types.AgentStatus
	Children  []*HierarchyNode
}

func (o *Overseer) buildHierarchyNode(session *Session) *HierarchyNode {
	// Copy session fields into locals under lock to avoid data races
	session.mu.RLock()
	sessionID := session.ID
	actorID := session.Config.ActorID
	role := session.Config.Role
	depth := session.Config.Depth
	status := session.Status
	session.mu.RUnlock()

	// Construct node using copied values (lock released)
	node := &HierarchyNode{
		SessionID: sessionID,
		ActorID:   actorID,
		Role:      role,
		Depth:     depth,
		Status:    status,
		Children:  []*HierarchyNode{},
	}

	// Recursively build children (no lock held)
	childIDs := o.children[sessionID]
	for _, childID := range childIDs {
		if childSession, ok := o.runtime.Get(childID); ok {
			childNode := o.buildHierarchyNode(childSession)
			node.Children = append(node.Children, childNode)
		}
	}

	return node
}

// WaitForChildren waits for all children of a session to complete.
func (o *Overseer) WaitForChildren(ctx context.Context, parentSessionID string) error {
	childIDs := o.GetChildren(parentSessionID)

	for _, childID := range childIDs {
		// Poll until session completes or context is cancelled
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			session, ok := o.runtime.Get(childID)
			if !ok {
				// Session no longer exists, move to next child
				break
			}

			session.mu.RLock()
			status := session.Status
			session.mu.RUnlock()

			if status != types.StatusRunning {
				// Session completed, move to next child
				break
			}

			// Sleep to avoid busy loop
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
				// Continue polling
			}
		}
	}

	return nil
}
