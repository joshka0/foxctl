package handlers

import (
	"context"
	"fmt"

	"github.com/jkatigb/agentctl/internal/runtime/actor"
)

// PlannerHandler handles messages for planner role actors.
//
// Planners specialize in:
// - Task decomposition and planning
// - Architecture decisions
// - Prioritization and dependency analysis
// - Coordination between other agents
type PlannerHandler struct {
	baseHandler
}

// NewPlannerHandler creates a new planner handler.
func NewPlannerHandler() *PlannerHandler {
	return &PlannerHandler{
		baseHandler: baseHandler{role: "planner"},
	}
}

// HandleAsk processes ask messages for planner role.
func (h *PlannerHandler) HandleAsk(ctx context.Context, msg *actor.Message, agent AgentExecutor, mem *MemoryContext) (*actor.Message, error) {
	askData, err := parseAskData(msg.Body)
	if err != nil {
		return nil, fmt.Errorf("parse ask: %w", err)
	}

	// Build prompt based on ask kind
	var prompt string
	switch askData.Kind {
	case "plan":
		prompt = fmt.Sprintf(`Create a detailed implementation plan for: %s

Break down into:
1. Clear, actionable tasks
2. Dependencies between tasks
3. Estimated complexity (low/medium/high)
4. Suggested order of execution`, askData.Question)

	case "architecture":
		prompt = fmt.Sprintf(`Analyze and recommend architecture for: %s

Consider:
1. Component design
2. Data flow
3. Integration points
4. Potential issues`, askData.Question)

	case "prioritize":
		prompt = fmt.Sprintf(`Prioritize the following work: %s

Provide:
1. Ordered list by priority
2. Reasoning for each priority
3. Quick wins vs long-term items`, askData.Question)

	case "decompose":
		prompt = fmt.Sprintf(`Decompose this task into subtasks: %s

For each subtask:
1. Clear description
2. Acceptance criteria
3. Dependencies
4. Estimated effort`, askData.Question)

	default:
		prompt = fmt.Sprintf("As a planning agent, help with: %s", askData.Question)
	}

	// Run agent turn
	result, err := runAgentTurn(ctx, agent, prompt, mem)
	if err != nil {
		return nil, fmt.Errorf("run agent turn: %w", err)
	}

	// Build reply
	answer := map[string]any{
		"plan": result,
		"role": h.role,
	}

	return buildReplyMessage(askData.AskID, answer)
}

// HandleCmd processes command messages for planner role.
func (h *PlannerHandler) HandleCmd(ctx context.Context, msg *actor.Message, agent AgentExecutor, mem *MemoryContext) (*actor.Message, error) {
	cmdData, err := parseCmdData(msg.Body)
	if err != nil {
		return nil, fmt.Errorf("parse cmd: %w", err)
	}

	switch cmdData.Action {
	case "run_turn":
		prompt, _ := cmdData.Args["prompt"].(string)
		if prompt == "" {
			return nil, fmt.Errorf("run_turn requires prompt arg")
		}

		// Wrap in planning context
		planningPrompt := fmt.Sprintf("As a planning agent: %s", prompt)
		result, err := runAgentTurn(ctx, agent, planningPrompt, mem)
		if err != nil {
			return nil, fmt.Errorf("run turn: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"result": result,
			"action": cmdData.Action,
		})

	case "create_plan":
		// Create a structured plan
		goal, _ := cmdData.Args["goal"].(string)
		constraints, _ := cmdData.Args["constraints"].(string)

		prompt := fmt.Sprintf(`Create a comprehensive plan for achieving: %s

Constraints: %s

The plan should include:
1. Overview
2. Phased approach
3. Key milestones
4. Risk mitigation
5. Success criteria`, goal, constraints)

		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("create plan: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"plan":   result,
			"action": cmdData.Action,
		})

	case "analyze_dependencies":
		// Analyze task dependencies
		tasks, _ := cmdData.Args["tasks"].(string)

		prompt := fmt.Sprintf(`Analyze dependencies between these tasks: %s

Provide:
1. Dependency graph (which tasks depend on which)
2. Critical path
3. Parallelizable tasks
4. Blocking dependencies`, tasks)

		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("analyze dependencies: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"analysis": result,
			"action":   cmdData.Action,
		})

	case "estimate_complexity":
		// Estimate task complexity
		task, _ := cmdData.Args["task"].(string)

		prompt := fmt.Sprintf(`Estimate the complexity of: %s

Provide:
1. Overall complexity (low/medium/high/very high)
2. Time estimate range
3. Key complexity factors
4. Simplification opportunities`, task)

		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("estimate complexity: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"estimate": result,
			"action":   cmdData.Action,
		})

	case "do_work":
		task, _ := cmdData.Args["task"].(string)
		if task == "" {
			return nil, fmt.Errorf("do_work requires task arg")
		}

		// Planners interpret do_work as planning work
		prompt := fmt.Sprintf("Plan how to accomplish: %s", task)
		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("do work: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"result": result,
			"action": cmdData.Action,
		})

	default:
		return nil, fmt.Errorf("unknown action: %s", cmdData.Action)
	}
}

// HandleEvent processes event messages for planner role.
func (h *PlannerHandler) HandleEvent(ctx context.Context, msg *actor.Message, _ AgentExecutor, mem *MemoryContext) error {
	eventData, err := parseEventData(msg.Body)
	if err != nil {
		return fmt.Errorf("parse event: %w", err)
	}

	switch eventData.Kind {
	case "task_completed":
		// Track progress
		if mem != nil {
			mem.AppendTurn(ctx, "system", fmt.Sprintf("Task completion event received: A task was completed (job count: %d)", eventData.JobCount))
		}

	case "task_blocked":
		// Could trigger re-planning
		if mem != nil {
			mem.AppendTurn(ctx, "system", "Task blocked event received: A task has been blocked - may need plan adjustment")
		}

	case "heartbeat":
		return nil
	}

	return nil
}
