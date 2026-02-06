package handlers

import (
	"context"
	"fmt"

	"github.com/jkatigb/agentctl/internal/actor"
)

// ReviewerHandler handles messages for reviewer role actors.
//
// Reviewers specialize in read-only analysis:
// - Code review and quality assessment
// - Security analysis
// - Best practices verification
// - Documentation review
//
// Reviewers should NOT have write access to files.
type ReviewerHandler struct {
	baseHandler
}

// NewReviewerHandler creates a new reviewer handler.
func NewReviewerHandler() *ReviewerHandler {
	return &ReviewerHandler{
		baseHandler: baseHandler{role: "reviewer"},
	}
}

// HandleAsk processes ask messages for reviewer role.
func (h *ReviewerHandler) HandleAsk(ctx context.Context, msg *actor.Message, agent AgentExecutor, mem *MemoryContext) (*actor.Message, error) {
	askData, err := parseAskData(msg.Body)
	if err != nil {
		return nil, fmt.Errorf("parse ask: %w", err)
	}

	// Build prompt based on ask kind
	var prompt string
	switch askData.Kind {
	case "review":
		prompt = fmt.Sprintf(`Review the following code/changes: %s

Provide:
1. Summary of what the code does
2. Potential issues or bugs
3. Suggestions for improvement
4. Security considerations
5. Overall assessment (approve/request changes/needs discussion)`, askData.Question)

	case "security":
		prompt = fmt.Sprintf(`Perform a security review of: %s

Check for:
1. OWASP Top 10 vulnerabilities
2. Input validation issues
3. Authentication/authorization problems
4. Data exposure risks
5. Injection vulnerabilities`, askData.Question)

	case "quality":
		prompt = fmt.Sprintf(`Assess code quality for: %s

Evaluate:
1. Code readability
2. Maintainability
3. Test coverage gaps
4. Documentation completeness
5. Adherence to best practices`, askData.Question)

	case "architecture":
		prompt = fmt.Sprintf(`Review architecture of: %s

Analyze:
1. Component structure
2. Dependency management
3. Separation of concerns
4. Scalability considerations
5. Technical debt`, askData.Question)

	default:
		prompt = fmt.Sprintf("As a code reviewer, analyze: %s", askData.Question)
	}

	// Run agent turn
	result, err := runAgentTurn(ctx, agent, prompt, mem)
	if err != nil {
		return nil, fmt.Errorf("run agent turn: %w", err)
	}

	// Build reply
	answer := map[string]any{
		"review": result,
		"role":   h.role,
	}

	return buildReplyMessage(askData.AskID, answer)
}

// HandleCmd processes command messages for reviewer role.
func (h *ReviewerHandler) HandleCmd(ctx context.Context, msg *actor.Message, agent AgentExecutor, mem *MemoryContext) (*actor.Message, error) {
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

		// Wrap in review context
		reviewPrompt := fmt.Sprintf("As a code reviewer (read-only): %s", prompt)
		result, err := runAgentTurn(ctx, agent, reviewPrompt, mem)
		if err != nil {
			return nil, fmt.Errorf("run turn: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"result": result,
			"action": cmdData.Action,
		})

	case "review_code":
		// Review specific code
		code, _ := cmdData.Args["code"].(string)
		filepath, _ := cmdData.Args["filepath"].(string)
		focus, _ := cmdData.Args["focus"].(string)

		prompt := fmt.Sprintf(`Review this code:
File: %s

%s

Focus areas: %s

Provide detailed feedback with line-specific comments where applicable.`, filepath, code, focus)

		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("review code: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"review":   result,
			"filepath": filepath,
			"action":   cmdData.Action,
		})

	case "review_pr":
		// Review a pull request
		prDesc, _ := cmdData.Args["description"].(string)
		changes, _ := cmdData.Args["changes"].(string)

		prompt := fmt.Sprintf(`Review this pull request:

Description: %s

Changes:
%s

Provide:
1. Summary of changes
2. Potential issues
3. Suggestions
4. Recommendation (approve/request changes)`, prDesc, changes)

		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("review pr: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"review": result,
			"action": cmdData.Action,
		})

	case "check_patterns":
		// Check for anti-patterns
		code, _ := cmdData.Args["code"].(string)
		language, _ := cmdData.Args["language"].(string)

		prompt := fmt.Sprintf(`Check this %s code for anti-patterns:

%s

Identify:
1. Common anti-patterns
2. Code smells
3. Potential bugs
4. Performance issues
5. Suggested fixes`, language, code)

		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("check patterns: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"findings": result,
			"action":   cmdData.Action,
		})

	case "do_work":
		task, _ := cmdData.Args["task"].(string)
		if task == "" {
			return nil, fmt.Errorf("do_work requires task arg")
		}

		// Reviewers interpret do_work as review work (read-only)
		prompt := fmt.Sprintf("As a reviewer (read-only analysis): %s", task)
		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("do work: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"result": result,
			"action": cmdData.Action,
		})

	case "run_skill":
		// Reviewers can only run read-only skills
		skill := cmdData.Skill
		if skill == "" {
			return nil, fmt.Errorf("run_skill requires skill name")
		}

		// Filter to read-only skills
		readOnlySkills := map[string]bool{
			"code/symbols":         true,
			"code/complexity":      true,
			"code/snippet_extract": true,
			"search/grep":          true,
			"search/ripgrep":       true,
			"fs/read":              true,
			"fs/list":              true,
			"fs/tree":              true,
		}

		if !readOnlySkills[skill] {
			return nil, fmt.Errorf("reviewer can only run read-only skills, '%s' is not allowed", skill)
		}

		prompt := fmt.Sprintf("Execute read-only skill '%s' with args: %v", skill, cmdData.Args)
		result, err := runAgentTurn(ctx, agent, prompt, mem)
		if err != nil {
			return nil, fmt.Errorf("run skill: %w", err)
		}

		return buildReplyMessage(cmdData.CmdID, map[string]any{
			"result": result,
			"skill":  skill,
		})

	default:
		return nil, fmt.Errorf("unknown action: %s", cmdData.Action)
	}
}

// HandleEvent processes event messages for reviewer role.
func (h *ReviewerHandler) HandleEvent(ctx context.Context, msg *actor.Message, _ AgentExecutor, mem *MemoryContext) error {
	eventData, err := parseEventData(msg.Body)
	if err != nil {
		return fmt.Errorf("parse event: %w", err)
	}

	switch eventData.Kind {
	case "code_pushed":
		// Could trigger automatic review
		if mem != nil {
			mem.AppendTurn(ctx, "system", "Code push event received: New code has been pushed - may need review")
		}

	case "pr_opened":
		// Could trigger PR review
		if mem != nil {
			mem.AppendTurn(ctx, "system", "Pull request opened event received: A new pull request needs review")
		}

	case "heartbeat":
		return nil
	}

	return nil
}
