package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	agentdaemon "github.com/jkatigb/agentctl/internal/agent/daemon"
	"github.com/jkatigb/agentctl/internal/agent/prompts"
	"github.com/jkatigb/agentctl/internal/daemon"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/execution/agentmanager"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents (Agent Profile v1)",
	Long:  "Manage agents for multi-agent orchestration",
}

var agentSpawnCmd = &cobra.Command{
	Use:   "spawn",
	Short: "Create a new agent",
	Long:  "Spawn a new agent with specified role, skills, and policy",
	RunE:  runAgentSpawn,
}

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agents",
	Long:  "List all agents with optional limit",
	RunE:  runAgentList,
}

var agentKillCmd = &cobra.Command{
	Use:   "kill [agent-id]",
	Short: "Terminate an agent",
	Long:  "Gracefully or forcefully terminate an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentKill,
}

var agentInfoCmd = &cobra.Command{
	Use:   "info [agent-id]",
	Short: "Get agent information",
	Long:  "Retrieve detailed information about an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentInfo,
}

var agentWatchCmd = &cobra.Command{
	Use:   "watch [agent-id]",
	Short: "Watch agent events",
	Long:  "Stream agent events in real-time (NDJSON)",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentWatch,
}

var agentRunCmd = &cobra.Command{
	Use:   "run <agent-id>",
	Short: "Run an agent daemon in the foreground",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentRun,
}

var agentAskCmd = &cobra.Command{
	Use:   "ask <agent-id>",
	Short: "Send an ask message to an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentAsk,
}

var agentCmdCmd = &cobra.Command{
	Use:   "cmd <agent-id>",
	Short: "Send a command to an agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentCmd,
}

var agentResumeCmd = &cobra.Command{
	Use:   "resume <session-id>",
	Short: "Continue a previous agent session",
	Long:  "Resume a previous session with an additional prompt. Loads the conversation history and continues.",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentResume,
}

var agentHierarchyCmd = &cobra.Command{
	Use:   "hierarchy [session-id]",
	Short: "Show agent hierarchy tree",
	Long:  "Display the agent hierarchy tree starting from a session or all roots.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAgentHierarchy,
}

// Flags for agent resume
var (
	resumePrompt string
)

// Flags for agent spawn
var (
	spawnParentNS      string
	spawnName          string
	spawnSlug          string
	spawnRole          string
	spawnPrompt        string
	spawnPromptFile    string
	spawnSkillsAllow   string
	spawnPolicyFile    string
	spawnShareBB       string
	spawnLLMProvider   string
	spawnLLMModel      string
	spawnLLMAPIKey     string
	spawnExecMode         string
	spawnMaxIterations    int
	spawnMaxContextTokens int
	spawnMaxAutoTurns     int
	spawnThinkInterval    int
	spawnDryRun           bool
	spawnChat          bool // Convenience flag for chat/roleplay companions
)

// Flags for agent run
var (
	runCompanionMode string // "standard" or "roleplay"
)

// Flags for agent kill
var (
	killGraceful bool
	killTimeoutS int
	killDryRun   bool
)

// Flags for agent list
var (
	listLimit int
	listState string
)

// Flags for agent ask
var (
	askDryRun bool
)

// Flags for agent cmd
var (
	cmdDryRun bool
)

func init() {
	// Add agent commands to root
	rootCmd.AddCommand(agentCmd)
	agentCmd.AddCommand(agentSpawnCmd)
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentKillCmd)
	agentCmd.AddCommand(agentInfoCmd)
	agentCmd.AddCommand(agentWatchCmd)
	agentCmd.AddCommand(agentRunCmd)
	agentCmd.AddCommand(agentAskCmd)
	agentCmd.AddCommand(agentCmdCmd)
	agentCmd.AddCommand(agentResumeCmd)
	agentCmd.AddCommand(agentHierarchyCmd)

	// Resume flags
	agentResumeCmd.Flags().StringVar(&resumePrompt, "prompt", "", "Additional prompt for the continuation (required)")
	_ = agentResumeCmd.MarkFlagRequired("prompt")

	// Spawn flags
	agentSpawnCmd.Flags().StringVar(&spawnParentNS, "ns", "", "Parent namespace (optional)")
	agentSpawnCmd.Flags().StringVar(&spawnName, "name", "", "Human name for the agent (e.g., 'Luna', 'Atlas')")
	agentSpawnCmd.Flags().StringVar(&spawnSlug, "slug", "", "Human-readable handle for referencing (e.g., 'researcher', 'companion')")
	agentSpawnCmd.Flags().StringVar(&spawnRole, "role", "", "Agent role")
	agentSpawnCmd.Flags().StringVar(&spawnPrompt, "prompt", "", "Inline prompt text (mutually exclusive with --prompt-file)")
	agentSpawnCmd.Flags().StringVar(&spawnPromptFile, "prompt-file", "", "Path to prompt file (mutually exclusive with --prompt)")
	agentSpawnCmd.Flags().StringVar(&spawnSkillsAllow, "skills-allow", "", "Comma-separated list of allowed skills")
	agentSpawnCmd.Flags().StringVar(&spawnPolicyFile, "policy", "", "Path to policy JSON file")
	agentSpawnCmd.Flags().StringVar(&spawnShareBB, "share-bb", "scoped", "Blackboard sharing mode (all|scoped|none)")
	agentSpawnCmd.Flags().StringVar(&spawnLLMProvider, "llm-provider", "", "LLM provider (gemini|openai|anthropic|groq|openrouter)")
	agentSpawnCmd.Flags().StringVar(&spawnLLMModel, "llm-model", "", "LLM model ID (e.g., claude-haiku-4-5)")
	agentSpawnCmd.Flags().StringVar(&spawnLLMAPIKey, "llm-api-key", "", "LLM API key (or env var like $GROQ_API_KEY)")
	agentSpawnCmd.Flags().StringVar(&spawnExecMode, "exec-mode", "reactive", "Execution mode (reactive|autonomous|proactive)")
	agentSpawnCmd.Flags().IntVar(&spawnMaxIterations, "max-iterations", 10, "Max tool calls per turn")
	agentSpawnCmd.Flags().IntVar(&spawnMaxContextTokens, "max-context-tokens", 0, "Max context tokens before stopping (0=no limit)")
	agentSpawnCmd.Flags().IntVar(&spawnMaxAutoTurns, "max-auto-turns", 1, "Max autonomous turns per session (only for autonomous/proactive modes)")
	agentSpawnCmd.Flags().IntVar(&spawnThinkInterval, "think-interval", 60, "Seconds between proactive think cycles (only for proactive mode)")
	agentSpawnCmd.Flags().BoolVar(&spawnDryRun, "dry-run", false, "Preview what would be spawned without creating the agent")
	agentSpawnCmd.Flags().BoolVar(&spawnChat, "chat", false, "Convenience flag for chat/roleplay companions (sets role=companion, exec-mode=reactive, max-iterations=3)")

	// Run flags
	agentRunCmd.Flags().StringVar(&runCompanionMode, "companion-mode", "", "Memory mode for conversation memory: standard (40K tokens) or roleplay (50K tokens)")

	// Kill flags
	agentKillCmd.Flags().BoolVar(&killGraceful, "graceful", true, "Graceful shutdown")
	agentKillCmd.Flags().IntVar(&killTimeoutS, "timeout", 30, "Timeout in seconds")
	agentKillCmd.Flags().BoolVar(&killDryRun, "dry-run", false, "Preview what would be killed without terminating the agent")

	// List flags
	agentListCmd.Flags().IntVar(&listLimit, "limit", 20, "Maximum number of agents to list")
	agentListCmd.Flags().StringVar(&listState, "state", "", "Filter by state (starting|running|stopped|error|all). Defaults to excluding stopped.")

	// Ask flags
	agentAskCmd.Flags().String("question", "", "The question to ask (required)")
	agentAskCmd.Flags().String("kind", "context", "Ask kind: context|secret|approval|toolhint|other")
	agentAskCmd.Flags().String("conversation-id", "", "Conversation ID for memory continuity (default: unique per call)")
	agentAskCmd.Flags().Bool("wait", false, "Wait for reply before returning")
	agentAskCmd.Flags().Duration("timeout", 5*time.Minute, "Timeout for --wait")
	agentAskCmd.Flags().BoolVar(&askDryRun, "dry-run", false, "Preview what would be sent without sending the message")
	_ = agentAskCmd.MarkFlagRequired("question") //nolint:errcheck

	// Cmd flags
	agentCmdCmd.Flags().String("action", "", "Command action: run_skill|run_turn|do_work")
	agentCmdCmd.Flags().String("skill", "", "Skill to run (for run_skill action)")
	agentCmdCmd.Flags().String("args", "{}", "JSON args for the command")
	agentCmdCmd.Flags().BoolVar(&cmdDryRun, "dry-run", false, "Preview what would be sent without sending the command")
	_ = agentCmdCmd.MarkFlagRequired("action") //nolint:errcheck
}

func runAgentSpawn(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	// Apply chat/roleplay companion defaults if --chat flag is set
	// These can be overridden by explicit flags
	if spawnChat {
		if spawnRole == "" {
			spawnRole = "companion"
		}
		if spawnExecMode == "reactive" && !cmd.Flags().Changed("exec-mode") {
			spawnExecMode = "reactive" // Confirm reactive for chat
		}
		if spawnMaxIterations == 10 && !cmd.Flags().Changed("max-iterations") {
			spawnMaxIterations = 3 // Minimal tool use for chat
		}
		if spawnMaxAutoTurns == 1 && !cmd.Flags().Changed("max-auto-turns") {
			spawnMaxAutoTurns = 1 // No autonomous continuation for natural chat
		}
	}

	// Load prompt (--prompt and --prompt-file are mutually exclusive)
	var prompt string
	if spawnPrompt != "" && spawnPromptFile != "" {
		return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeEARG), "--prompt and --prompt-file are mutually exclusive")
	}
	if spawnPrompt != "" {
		prompt = spawnPrompt
	} else if spawnPromptFile != "" {
		data, err := os.ReadFile(spawnPromptFile)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeEARG), fmt.Sprintf("failed to read prompt file: %v", err))
		}
		prompt = string(data)
	}
	if prompt == "" && spawnRole != "" {
		if defaultPrompt, ok := prompts.DefaultPrompt(spawnRole); ok {
			prompt = defaultPrompt
		}
	}

	// Resolve LLM API key (support $ENV_VAR syntax)
	llmAPIKey := spawnLLMAPIKey
	if strings.HasPrefix(llmAPIKey, "$") {
		llmAPIKey = os.Getenv(strings.TrimPrefix(llmAPIKey, "$"))
	}

	// Try daemon client first (new runtime with tools like session.recall, memory.query)
	daemonClient := daemon.NewClient()
	if daemonClient.IsRunning() {
		// Guard against unsupported flags in daemon mode
		var unsupportedFlags []string
		if spawnSkillsAllow != "" {
			unsupportedFlags = append(unsupportedFlags, "--skills-allow")
		}
		if spawnPolicyFile != "" {
			unsupportedFlags = append(unsupportedFlags, "--policy")
		}
		if spawnShareBB != "scoped" {
			unsupportedFlags = append(unsupportedFlags, "--share-bb")
		}
		if spawnParentNS != "" {
			unsupportedFlags = append(unsupportedFlags, "--ns")
		}
		if spawnThinkInterval != 60 {
			unsupportedFlags = append(unsupportedFlags, "--think-interval")
		}
		if len(unsupportedFlags) > 0 {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeEARG),
				fmt.Sprintf("flags not supported in daemon mode: %s (use direct spawn instead)", strings.Join(unsupportedFlags, ", ")))
		}

		params := daemon.AgentSpawnParams{
			Role:             spawnRole,
			Prompt:           prompt,
			Name:             spawnName,
			Slug:             spawnSlug,
			MaxIterations:    spawnMaxIterations,
			MaxContextTokens: spawnMaxContextTokens,
			ExecMode:         spawnExecMode,
			MaxAutoTurns:     spawnMaxAutoTurns,
			LLMProvider:      spawnLLMProvider,
			LLMModel:         spawnLLMModel,
			LLMAPIKey:        llmAPIKey,
		}

		// Dry-run mode: show what would be spawned via daemon
		if spawnDryRun {
			data := map[string]any{
				"dry_run":        true,
				"would_spawn":    true,
				"via_daemon":     true,
				"role":           params.Role,
				"name":           params.Name,
				"slug":           params.Slug,
				"llm_provider":   params.LLMProvider,
				"llm_model":      params.LLMModel,
				"exec_mode":      params.ExecMode,
				"max_iterations": params.MaxIterations,
				"max_auto_turns": params.MaxAutoTurns,
				"has_prompt":     len(params.Prompt) > 0,
			}
			return writeOK(cmd, "agent/spawn", data, "run", nil)
		}

		result, err := daemonClient.AgentSpawn(params)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("daemon spawn failed: %v", err))
		}

		// Write success envelope
		data := map[string]any{
			"session_id": result.SessionID,
			"actor_id":   result.ActorID,
			"status":     result.Status,
			"role":       result.Role,
			"ns":         result.NS,
			"via_daemon": true,
		}
		return writeOK(cmd, "agent/spawn", data, "run", nil)
	}

	// Fall back to old agentmanager (legacy, does not have new tools)
	// Parse skills allow
	var skillsAllow []string
	if spawnSkillsAllow != "" {
		trimmed := strings.TrimSpace(spawnSkillsAllow)
		if strings.HasPrefix(trimmed, "[") {
			// Looks like JSON, require valid JSON
			if err := json.Unmarshal([]byte(trimmed), &skillsAllow); err != nil {
				return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeEARG), fmt.Sprintf("invalid JSON in skills-allow: %v", err))
			}
		} else {
			// Treat as comma-separated
			skillsAllow = parseCommaSeparated(trimmed)
		}
	}

	// Load policy
	var policy agent.Policy
	if spawnPolicyFile != "" {
		data, err := os.ReadFile(spawnPolicyFile)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeEARG), fmt.Sprintf("failed to read policy file: %v", err))
		}
		if err := json.Unmarshal(data, &policy); err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeEARG), fmt.Sprintf("failed to parse policy JSON: %v", err))
		}
	}

	// Open stores
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	mailboxStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer func() { errs.Ignore(mailboxStore.Close(), "close mailbox store") }()

	// Create manager
	mgr := agentmanager.New(agentStore, mailboxStore)

	// Spawn agent
	req := agentmanager.SpawnRequest{
		ParentNS:      spawnParentNS,
		Name:          spawnName,
		Slug:          spawnSlug,
		Role:          spawnRole,
		Prompt:        prompt,
		SkillsAllow:   skillsAllow,
		Policy:        policy,
		ShareBB:       spawnShareBB,
		LLMProvider:   spawnLLMProvider,
		LLMModel:      spawnLLMModel,
		LLMAPIKey:     llmAPIKey,
		ExecMode:      agent.ExecutionMode(spawnExecMode),
		MaxIterations: spawnMaxIterations,
		MaxAutoTurns:  spawnMaxAutoTurns,
		ThinkInterval: spawnThinkInterval,
	}

	// Dry-run mode: show what would be spawned
	if spawnDryRun {
		data := map[string]any{
			"dry_run":        true,
			"would_spawn":    true,
			"via_daemon":     false,
			"parent_ns":      req.ParentNS,
			"name":           req.Name,
			"slug":           req.Slug,
			"role":           req.Role,
			"skills_allow":   req.SkillsAllow,
			"share_bb":       req.ShareBB,
			"llm_provider":   req.LLMProvider,
			"llm_model":      req.LLMModel,
			"exec_mode":      string(req.ExecMode),
			"max_iterations": req.MaxIterations,
			"max_auto_turns": req.MaxAutoTurns,
			"think_interval": req.ThinkInterval,
			"has_prompt":     len(req.Prompt) > 0,
			"has_policy":     req.Policy.CPU > 0 || req.Policy.MemoryMB > 0 || req.Policy.Timeout != "",
		}
		return writeOK(cmd, "agent/spawn", data, "run", nil)
	}

	resp, err := mgr.Spawn(ctx, req)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to spawn agent: %v", err))
	}

	// Write success envelope
	data := map[string]any{
		"agent_id":   resp.AgentID,
		"ns":         resp.NS,
		"role":       resp.Role,
		"via_daemon": false,
	}

	return writeOK(cmd, "agent/spawn", data, "run", nil)
}

func runAgentList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	// Open agent store
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/list", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	// List agents
	list, err := agentStore.List(ctx, listLimit)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/list", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to list agents: %v", err))
	}

	filtered, err := filterAgentsByState(list, listState)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/list", string(protocol.ErrorCodeEARG), err.Error())
	}

	// Write success envelope
	return writeOK(cmd, "agent/list", map[string]any{
		"agents": filtered,
		"count":  len(filtered),
	}, "run", nil)
}

func runAgentKill(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentID := args[0]

	// Open stores
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/kill", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	mailboxStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/kill", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer func() { errs.Ignore(mailboxStore.Close(), "close mailbox store") }()

	// Create manager
	mgr := agentmanager.New(agentStore, mailboxStore)

	// Kill agent
	req := agentmanager.KillRequest{
		AgentID:  agentID,
		Graceful: killGraceful,
		TimeoutS: killTimeoutS,
	}

	// Dry-run mode: show what would be killed
	if killDryRun {
		// Get agent info to show what would be killed
		agentRecord, err := agentStore.Get(ctx, agentID)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/kill", string(protocol.ErrorCodeENotFound), fmt.Sprintf("agent not found: %v", err))
		}
		data := map[string]any{
			"dry_run":    true,
			"would_kill": true,
			"agent_id":   agentID,
			"namespace":  agentRecord.Namespace,
			"role":       agentRecord.Role,
			"state":      agentRecord.State,
			"graceful":   killGraceful,
			"timeout_s":  killTimeoutS,
		}
		return writeOK(cmd, "agent/kill", data, "run", nil)
	}

	resp, err := mgr.Kill(ctx, req)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/kill", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to kill agent: %v", err))
	}

	// Write success envelope
	data := map[string]any{
		"agent_id":     resp.AgentID,
		"final_status": resp.FinalStatus,
		"exit_code":    resp.ExitCode,
	}

	return writeOK(cmd, "agent/kill", data, "run", nil)
}

func runAgentInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentRef := args[0] // Can be ID, slug, or name

	// Open agent store
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/info", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	// Get agent (supports slug, name, or ID)
	a, err := agentStore.Resolve(ctx, agentRef)
	if err != nil {
		code := string(protocol.ErrorCodeERuntime)
		msg := fmt.Sprintf("failed to get agent: %v", err)
		if errors.Is(err, agents.ErrNotFound) {
			code = string(protocol.ErrorCodeENotFound)
			msg = fmt.Sprintf("agent not found: %v", err)
		}
		return writeErrorEnvelope(cmd, "agent/info", code, msg)
	}

	// Write success envelope
	return writeOK(cmd, "agent/info", a, "run", nil)
}

func runAgentWatch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentID := args[0]

	// Open stores
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/watch", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	mailboxStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/watch", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer func() { errs.Ignore(mailboxStore.Close(), "close mailbox store") }()

	// Verify agent exists
	a, err := agentStore.Get(ctx, agentID)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/watch", string(protocol.ErrorCodeENotFound), fmt.Sprintf("agent not found: %v", err))
	}

	// Create NDJSON writer
	writer := envelope.NewWriter(cmd.OutOrStdout())

	// Track sequence number
	seq := 0

	// Create ticker for periodic checks
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	lastHeartbeat := a.HeartbeatAt
	lastState := a.State

	for {
		select {
		case <-ctx.Done():
			// Write final envelope
			finalBool := true
			env := envelope.OK("agent/watch", map[string]any{
				"status": "stopped",
			}, envelope.WithMetaMutator(func(m *envelope.Meta) {
				m.Source = "run"
				m.Profiles = profilesCoreAgent
				m.AgentID = agentID
				m.Seq = &seq
				m.Final = &finalBool
			}))
			if err := writer.Write(env); err != nil {
				return fmt.Errorf("write final envelope: %w", err)
			}
			return nil

		case <-ticker.C:
			// Check for agent updates
			current, err := agentStore.Get(ctx, agentID)
			if err != nil {
				if errors.Is(err, agents.ErrNotFound) {
					// Agent deleted, stop watching
					finalBool := true
					env := envelope.OK("agent/watch", map[string]any{
						"status": "terminated",
					}, envelope.WithMetaMutator(func(m *envelope.Meta) {
						m.Source = "run"
						m.Profiles = profilesCoreAgent
						m.Seq = &seq
						m.Final = &finalBool
					}))
					if err := writer.Write(env); err != nil {
						return fmt.Errorf("write final envelope: %w", err)
					}
					return nil
				}
				return writeErrorEnvelope(cmd, "agent/watch", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to get agent: %v", err))
			}

			// Check for state change
			if current.State != lastState {
				seq++
				finalBool := false
				data := map[string]any{
					"event":     "agent_state_changed",
					"agent_id":  agentID,
					"old_state": lastState,
					"new_state": current.State,
				}

				env := envelope.Envelope{
					Version: envelope.Version,
					Status:  "progress",
					Command: "agent/watch",
					Data:    data,
					Meta: envelope.Meta{
						TS:       time.Now().UTC().Format(time.RFC3339),
						Source:   "run",
						Profiles: profilesCoreAgent,
						AgentID:  agentID,
						Seq:      &seq,
						Final:    &finalBool,
					},
				}

				if err := writer.Write(env); err != nil {
					return fmt.Errorf("write progress envelope: %w", err)
				}

				lastState = current.State
			}

			// Check for heartbeat update
			if current.HeartbeatAt.After(lastHeartbeat) {
				seq++
				finalBool := false
				data := map[string]any{
					"event":        "agent_heartbeat",
					"agent_id":     agentID,
					"heartbeat_at": current.HeartbeatAt.Format(time.RFC3339),
				}

				env := envelope.Envelope{
					Version: envelope.Version,
					Status:  "progress",
					Command: "agent/watch",
					Data:    data,
					Meta: envelope.Meta{
						TS:       time.Now().UTC().Format(time.RFC3339),
						Source:   "run",
						Profiles: profilesCoreAgent,
						AgentID:  agentID,
						Seq:      &seq,
						Final:    &finalBool,
					},
				}

				if err := writer.Write(env); err != nil {
					return fmt.Errorf("write progress envelope: %w", err)
				}

				lastHeartbeat = current.HeartbeatAt
			}

			// Check for new mailbox messages
			messages, err := mailboxStore.List(ctx, current.Namespace, 10)
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/watch", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to list messages: %v", err))
			}

			if len(messages) > 0 {
				seq++
				finalBool := false

				// Include sample details
				messageSamples := make([]map[string]any, 0, min(len(messages), 3))
				for i, msg := range messages {
					if i >= 3 {
						break
					}
					messageSamples = append(messageSamples, map[string]any{
						"id":          msg.ID,
						"type":        msg.Type,
						"from":        msg.FromNS,
						"correlation": msg.Headers["correlation"],
					})
				}

				data := map[string]any{
					"event":           "mailbox_messages",
					"agent_id":        agentID,
					"message_count":   len(messages),
					"message_samples": messageSamples,
				}

				env := envelope.Envelope{
					Version: envelope.Version,
					Status:  "progress",
					Command: "agent/watch",
					Data:    data,
					Meta: envelope.Meta{
						TS:        time.Now().UTC().Format(time.RFC3339),
						Source:    "run",
						Profiles:  profilesCoreAgent,
						AgentID:   agentID,
						MailboxID: "mailbox:" + a.Namespace,
						Seq:       &seq,
						Final:     &finalBool,
					},
				}

				if err := writer.Write(env); err != nil {
					return fmt.Errorf("write progress envelope: %w", err)
				}
			}
		}
	}
}

func writeErrorEnvelope(cmd *cobra.Command, command, code, message string, hints ...string) error {
	var data map[string]any
	if len(hints) > 0 && hints[0] != "" {
		data = map[string]any{"hint": hints[0]}
	}
	env := envelope.Error(command, code, message, data)
	if err := envelope.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s", message)
}

func parseCommaSeparated(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func filterAgentsByState(list []agent.Agent, stateFilter string) ([]agent.Agent, error) {
	trimmed := strings.ToLower(strings.TrimSpace(stateFilter))
	if trimmed == "" {
		filtered := make([]agent.Agent, 0, len(list))
		for _, a := range list {
			if a.State != agent.StateStopped {
				filtered = append(filtered, a)
			}
		}
		return filtered, nil
	}
	if trimmed == "all" {
		return list, nil
	}

	state := agent.State(trimmed)
	switch state {
	case agent.StateStarting, agent.StateRunning, agent.StateStopped, agent.StateError:
		filtered := make([]agent.Agent, 0, len(list))
		for _, a := range list {
			if a.State == state {
				filtered = append(filtered, a)
			}
		}
		return filtered, nil
	default:
		return nil, fmt.Errorf("invalid state filter %q", stateFilter)
	}
}

func runAgentRun(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentRef := args[0] // Can be ID, slug, or name

	// Load agent first to check its LLM provider (supports slug, name, or ID)
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open agent store: %w", err)
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	agentRecord, err := agentStore.Resolve(ctx, agentRef)
	if err != nil {
		return fmt.Errorf("resolve agent %q: %w", agentRef, err)
	}

	// Resolve LLM provider - use agent's provider first, then config, then auto-detect
	provider := agentRecord.LLMProvider
	if provider == "" {
		provider = cfg.LLM.Provider
	}
	if provider == "" {
		// Auto-detect provider based on available API keys
		// Priority: cerebras > groq > openrouter > gemini > anthropic > openai
		switch {
		case cfg.LLM.CerebrasAPIKey != "":
			provider = "cerebras"
		case cfg.LLM.GroqAPIKey != "":
			provider = "groq"
		case cfg.LLM.OpenRouterAPIKey != "":
			provider = "openrouter"
		case cfg.LLM.GeminiAPIKey != "":
			provider = "gemini"
		case cfg.LLM.AnthropicAPIKey != "":
			provider = "anthropic"
		case cfg.LLM.OpenAIAPIKey != "":
			provider = "openai"
		default:
			provider = "gemini" // fallback default
		}
	}

	// Determine companion mode: use flag if set, else auto-detect based on role
	companionMode := runCompanionMode
	// Enable memory for all roles by default - memory provides context continuity across requests
	enableCompanionMemory := true
	if runCompanionMode == "" {
		// Default to roleplay mode for companion agents, standard for others
		if agentRecord.Role == "companion" {
			companionMode = "roleplay"
		} else {
			companionMode = "standard"
		}
	}

	opts := agentdaemon.Options{
		AgentID:               agentRecord.ID, // Use resolved ID
		StorageRoot:           cfg.Storage.Root,
		PollInterval:          500 * time.Millisecond,
		HeartbeatInterval:     10 * time.Second,
		MaxPollMessages:       10,
		LLMProvider:           provider,
		LLMModel:              cfg.LLM.ResolveModel(provider),
		LLMAPIKey:             cfg.LLM.ResolveAPIKey(provider),
		EnableCompanionMemory: enableCompanionMemory,
		CompanionMode:         companionMode,
	}

	return agentdaemon.Run(ctx, opts)
}

func runAgentAsk(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentID := args[0]
	question, err := cmd.Flags().GetString("question")
	if err != nil {
		return fmt.Errorf("get question flag: %w", err)
	}
	kind, err := cmd.Flags().GetString("kind")
	if err != nil {
		return fmt.Errorf("get kind flag: %w", err)
	}
	wait, err := cmd.Flags().GetBool("wait")
	if err != nil {
		return fmt.Errorf("get wait flag: %w", err)
	}
	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return fmt.Errorf("get timeout flag: %w", err)
	}
	conversationID, err := cmd.Flags().GetString("conversation-id")
	if err != nil {
		return fmt.Errorf("get conversation-id flag: %w", err)
	}

	// Open mailbox store
	mailboxStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer func() { errs.Ignore(mailboxStore.Close(), "close mailbox store") }()

	// Get agent to find its namespace (supports slug, name, or ID)
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()
	agentRecord, err := agentStore.Resolve(ctx, agentID)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeENotFound), fmt.Sprintf("agent not found: %v", err))
	}

	// Build ask message
	askID := ulid.Make().String()
	askData := agent.AskData{
		AskID:          askID,
		Kind:           kind,
		Question:       question,
		ConversationID: conversationID,
	}
	payload, err := json.Marshal(envelope.OK("agent.ask", askData))
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to marshal ask payload: %v", err))
	}

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    "cli:" + ulid.Make().String(), // unique caller namespace
		ToNS:      agentRecord.Namespace,
		Type:      agent.MessageTypeAsk,
		TTLMS:     int64(timeout.Milliseconds()),
		Headers:   map[string]string{"correlation": askID},
		Payload:   payload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}

	// Dry-run mode: show what would be sent
	if askDryRun {
		data := map[string]any{
			"dry_run":    true,
			"would_send": true,
			"agent_id":   agentID,
			"namespace":  agentRecord.Namespace,
			"ask_id":     askID,
			"kind":       kind,
			"question":   question,
			"timeout_ms": timeout.Milliseconds(),
		}
		return writeOK(cmd, "agent/ask", data, "ask", nil)
	}

	if err := mailboxStore.Send(ctx, msg); err != nil {
		return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeEIO), err.Error())
	}

	// Output ask confirmation
	if err := writeOK(cmd, "agent/ask", map[string]any{
		"ask_id":     askID,
		"message_id": msg.ID,
		"sent_to":    agentRecord.Namespace,
	}, "ask", nil); err != nil {
		return err
	}

	if wait {
		return waitForReply(ctx, mailboxStore, cmd.OutOrStdout(), msg.FromNS, askID, timeout)
	}
	return nil
}

func runAgentCmd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentID := args[0]
	action, err := cmd.Flags().GetString("action")
	if err != nil {
		return fmt.Errorf("get action flag: %w", err)
	}
	skill, err := cmd.Flags().GetString("skill")
	if err != nil {
		return fmt.Errorf("get skill flag: %w", err)
	}
	argsJSON, err := cmd.Flags().GetString("args")
	if err != nil {
		return fmt.Errorf("get args flag: %w", err)
	}

	var cmdArgs map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &cmdArgs); err != nil {
		return writeErrorEnvelope(cmd, "agent/cmd", string(protocol.ErrorCodeEARG), fmt.Sprintf("invalid JSON in args: %v", err))
	}

	// Open mailbox store
	mailboxStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/cmd", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer func() { errs.Ignore(mailboxStore.Close(), "close mailbox store") }()

	// Get agent to find its namespace
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/cmd", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	agentRecord, err := agentStore.Get(ctx, agentID)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/cmd", string(protocol.ErrorCodeENotFound), fmt.Sprintf("agent not found: %v", err))
	}

	cmdID := ulid.Make().String()
	cmdData := agent.CmdData{
		CmdID:  cmdID,
		Action: action,
		Skill:  skill,
		Args:   cmdArgs,
	}

	payload, err := json.Marshal(envelope.OK("agent.cmd", cmdData))
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/cmd", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to marshal cmd payload: %v", err))
	}

	msg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    "cli:" + ulid.Make().String(),
		ToNS:      agentRecord.Namespace,
		Type:      agent.MessageTypeCmd,
		TTLMS:     0,
		Headers:   map[string]string{},
		Payload:   payload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}

	// Dry-run mode: show what would be sent
	if cmdDryRun {
		data := map[string]any{
			"dry_run":    true,
			"would_send": true,
			"agent_id":   agentID,
			"namespace":  agentRecord.Namespace,
			"cmd_id":     cmdID,
			"action":     action,
			"skill":      skill,
			"args":       cmdArgs,
		}
		return writeOK(cmd, "agent/cmd", data, "cmd", nil)
	}

	if err := mailboxStore.Send(ctx, msg); err != nil {
		return writeErrorEnvelope(cmd, "agent/cmd", string(protocol.ErrorCodeEIO), err.Error())
	}

	return writeOK(cmd, "agent/cmd", map[string]any{
		"cmd_id":     cmdID,
		"message_id": msg.ID,
		"sent_to":    agentRecord.Namespace,
	}, "cmd", nil)
}

func waitForReply(ctx context.Context, store mailbox.Store, out io.Writer, callerNS, askID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pollInterval := 500 * time.Millisecond
	const maxConsecutiveErrors = 5

	consecutiveErrors := 0
	currentBackoff := pollInterval

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(currentBackoff):
			// Poll for replies to our namespace
			messages, err := store.List(ctx, callerNS, 50)
			if err != nil {
				consecutiveErrors++
				fmt.Fprintf(os.Stderr, "warning: failed to list mailbox messages (attempt %d/%d): %v\n",
					consecutiveErrors, maxConsecutiveErrors, err)
				if consecutiveErrors >= maxConsecutiveErrors {
					return fmt.Errorf("too many consecutive mailbox errors (%d): %w", consecutiveErrors, err)
				}
				// Exponential backoff: double the interval up to 8 seconds
				currentBackoff = min(currentBackoff*2, 8*time.Second)
				continue
			}
			// Reset on success
			consecutiveErrors = 0
			currentBackoff = pollInterval
			for _, msg := range messages {
				if msg.Type != agent.MessageTypeReply {
					continue
				}
				if msg.Headers["correlation"] == askID {
					// Found our reply!
					var replyEnv envelope.Envelope
					if err := json.Unmarshal(msg.Payload, &replyEnv); err != nil {
						return fmt.Errorf("failed to unmarshal reply payload: %w", err)
					}

					// Ack the reply
					_ = store.Ack(ctx, msg.ID) //nolint:errcheck

					// Output the reply envelope
					if err := envelope.Write(out, replyEnv); err != nil {
						return fmt.Errorf("write reply envelope: %w", err)
					}
					return nil
				}
			}
		}
	}
	return fmt.Errorf("timeout waiting for reply to ask_id=%s", askID)
}

func runAgentResume(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	if resumePrompt == "" {
		return writeErrorEnvelope(cmd, "agent/resume", string(protocol.ErrorCodeEARG), "--prompt is required")
	}

	// Try daemon client first
	daemonClient := daemon.NewClient()
	if daemonClient.IsRunning() {
		params := daemon.AgentResumeParams{
			SessionID: sessionID,
			Prompt:    resumePrompt,
		}

		result, err := daemonClient.AgentResume(params)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/resume", string(protocol.ErrorCodeEIO), err.Error())
		}

		return writeOK(cmd, "agent/resume", map[string]any{
			"session_id":     result.SessionID,
			"actor_id":       result.ActorID,
			"status":         result.Status,
			"from_session":   sessionID,
			"via_daemon":     true,
		}, "resume", nil)
	}

	return writeErrorEnvelope(cmd, "agent/resume", string(protocol.ErrorCodeESkillDown), "daemon not running - agent resume requires the daemon")
}

func runAgentHierarchy(cmd *cobra.Command, args []string) error {
	var sessionID string
	if len(args) > 0 {
		sessionID = args[0]
	}

	daemonClient := daemon.NewClient()
	if !daemonClient.IsRunning() {
		return writeErrorEnvelope(cmd, "agent/hierarchy", string(protocol.ErrorCodeESkillDown), "daemon not running")
	}

	result, err := daemonClient.AgentHierarchy(sessionID)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/hierarchy", string(protocol.ErrorCodeEIO), err.Error())
	}

	if len(result.Nodes) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No agent hierarchy found")
		return nil
	}

	// Print tree
	for _, node := range result.Nodes {
		printHierarchyNode(cmd, node, 0)
	}

	return nil
}

func printHierarchyNode(cmd *cobra.Command, node daemon.HierarchyNode, depth int) {
	prefix := ""
	if depth > 0 {
		prefix = strings.Repeat("  ", depth-1) + "└─ "
	}

	status := node.Status
	switch status {
	case "running":
		status = "●" // Running indicator
	case "ok":
		status = "✓"
	case "error":
		status = "✗"
	default:
		status = "○"
	}

	// Safely truncate SessionID to prevent panic on short/empty strings
	safeSession := node.SessionID
	if len(safeSession) >= 8 {
		safeSession = safeSession[:8]
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s%s %s [%s] depth=%d session=%s\n",
		prefix, status, node.ActorID, node.Role, node.Depth, safeSession)

	for _, child := range node.Children {
		printHierarchyNode(cmd, child, depth+1)
	}
}
