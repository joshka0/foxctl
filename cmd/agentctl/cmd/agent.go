package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/daemon"
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

// Flags for agent spawn
var (
	spawnParentNS    string
	spawnRole        string
	spawnPromptFile  string
	spawnSkillsAllow string
	spawnPolicyFile  string
	spawnShareBB     string
	spawnLLMProvider string
	spawnLLMModel    string
	spawnLLMAPIKey   string
	spawnDryRun      bool
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

	// Spawn flags
	agentSpawnCmd.Flags().StringVar(&spawnParentNS, "ns", "", "Parent namespace (optional)")
	agentSpawnCmd.Flags().StringVar(&spawnRole, "role", "", "Agent role")
	agentSpawnCmd.Flags().StringVar(&spawnPromptFile, "prompt-file", "", "Path to prompt file")
	agentSpawnCmd.Flags().StringVar(&spawnSkillsAllow, "skills-allow", "", "Comma-separated list of allowed skills")
	agentSpawnCmd.Flags().StringVar(&spawnPolicyFile, "policy", "", "Path to policy JSON file")
	agentSpawnCmd.Flags().StringVar(&spawnShareBB, "share-bb", "scoped", "Blackboard sharing mode (all|scoped|none)")
	agentSpawnCmd.Flags().StringVar(&spawnLLMProvider, "llm-provider", "", "LLM provider (gemini|openai|anthropic|groq|openrouter)")
	agentSpawnCmd.Flags().StringVar(&spawnLLMModel, "llm-model", "", "LLM model ID (e.g., claude-haiku-4-5)")
	agentSpawnCmd.Flags().StringVar(&spawnLLMAPIKey, "llm-api-key", "", "LLM API key (or env var like $GROQ_API_KEY)")
	agentSpawnCmd.Flags().BoolVar(&spawnDryRun, "dry-run", false, "Preview what would be spawned without creating the agent")

	// Kill flags
	agentKillCmd.Flags().BoolVar(&killGraceful, "graceful", true, "Graceful shutdown")
	agentKillCmd.Flags().IntVar(&killTimeoutS, "timeout", 30, "Timeout in seconds")
	agentKillCmd.Flags().BoolVar(&killDryRun, "dry-run", false, "Preview what would be killed without terminating the agent")

	// List flags
	agentListCmd.Flags().IntVar(&listLimit, "limit", 20, "Maximum number of agents to list")

	// Ask flags
	agentAskCmd.Flags().String("question", "", "The question to ask (required)")
	agentAskCmd.Flags().String("kind", "context", "Ask kind: context|secret|approval|toolhint|other")
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

	// Load prompt
	var prompt string
	if spawnPromptFile != "" {
		data, err := os.ReadFile(spawnPromptFile)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(string(protocol.ErrorCodeEARG)), fmt.Sprintf("failed to read prompt file: %v", err))
		}
		prompt = string(data)
	}

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

	// Resolve LLM API key (support $ENV_VAR syntax)
	llmAPIKey := spawnLLMAPIKey
	if strings.HasPrefix(llmAPIKey, "$") {
		llmAPIKey = os.Getenv(strings.TrimPrefix(llmAPIKey, "$"))
	}

	// Spawn agent
	req := agentmanager.SpawnRequest{
		ParentNS:    spawnParentNS,
		Role:        spawnRole,
		Prompt:      prompt,
		SkillsAllow: skillsAllow,
		Policy:      policy,
		ShareBB:     spawnShareBB,
		LLMProvider: spawnLLMProvider,
		LLMModel:    spawnLLMModel,
		LLMAPIKey:   llmAPIKey,
	}

	// Dry-run mode: show what would be spawned
	if spawnDryRun {
		data := map[string]any{
			"dry_run":      true,
			"would_spawn":  true,
			"parent_ns":    req.ParentNS,
			"role":         req.Role,
			"skills_allow": req.SkillsAllow,
			"share_bb":     req.ShareBB,
			"llm_provider": req.LLMProvider,
			"llm_model":    req.LLMModel,
			"has_prompt":   len(req.Prompt) > 0,
			"has_policy":   req.Policy.CPU > 0 || req.Policy.MemoryMB > 0 || req.Policy.Timeout != "",
		}
		env := envelope.OK("agent/spawn", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
			m.Source = "run"
		}))
		if err := envelope.Write(os.Stdout, env); err != nil {
			return fmt.Errorf("write envelope: %w", err)
		}
		return nil
	}

	resp, err := mgr.Spawn(ctx, req)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to spawn agent: %v", err))
	}

	// Write success envelope
	data := map[string]any{
		"agent_id": resp.AgentID,
		"ns":       resp.NS,
		"role":     resp.Role,
	}

	env := envelope.OK("agent/spawn", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
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

	// Write success envelope
	env := envelope.OK("agent/list", map[string]any{
		"agents": list,
		"count":  len(list),
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
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
		env := envelope.OK("agent/kill", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
			m.Source = "run"
		}))
		if err := envelope.Write(os.Stdout, env); err != nil {
			return fmt.Errorf("write envelope: %w", err)
		}
		return nil
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

	env := envelope.OK("agent/kill", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runAgentInfo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentID := args[0]

	// Open agent store
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/info", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	// Get agent
	a, err := agentStore.Get(ctx, agentID)
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
	env := envelope.OK("agent/info", a, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
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
	writer := envelope.NewWriter(os.Stdout)

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
				m.Profiles = []string{"core/v1", "agent/v1"}
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
						m.Profiles = []string{"core/v1", "agent/v1"}
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
						Profiles: []string{"core/v1", "agent/v1"},
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
						Profiles: []string{"core/v1", "agent/v1"},
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
						Profiles:  []string{"core/v1", "agent/v1"},
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

func writeErrorEnvelope(_ *cobra.Command, command, code, message string, hints ...string) error {
	var data map[string]any
	if len(hints) > 0 && hints[0] != "" {
		data = map[string]any{"hint": hints[0]}
	}
	env := envelope.Error(command, code, message, data)
	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s", message)
}

func parseCommaSeparated(s string) []string {
	var result []string
	for _, part := range splitAndTrim(s, ",") {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func splitAndTrim(s, sep string) []string {
	var result []string
	for _, part := range splitString(s, sep) {
		trimmed := trimString(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitString(s, sep string) []string {
	if s == "" {
		return nil
	}
	// Simple split implementation
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if i < len(s)-len(sep)+1 && s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimString(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func runAgentRun(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentID := args[0]

	opts := daemon.Options{
		AgentID:           agentID,
		StorageRoot:       cfg.Storage.Root,
		PollInterval:      500 * time.Millisecond,
		HeartbeatInterval: 10 * time.Second,
		MaxPollMessages:   10,
	}

	return daemon.Run(ctx, opts)
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

	// Open mailbox store
	mailboxStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer func() { errs.Ignore(mailboxStore.Close(), "close mailbox store") }()

	// Get agent to find its namespace
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()
	agentRecord, err := agentStore.Get(ctx, agentID)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeENotFound), fmt.Sprintf("agent not found: %v", err))
	}

	// Build ask message
	askID := ulid.Make().String()
	askData := agent.AskData{
		AskID:    askID,
		Kind:     kind,
		Question: question,
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
		env := envelope.OK("agent/ask", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
			m.Source = "ask"
		}))
		if err := envelope.Write(os.Stdout, env); err != nil {
			return fmt.Errorf("write envelope: %w", err)
		}
		return nil
	}

	if err := mailboxStore.Send(ctx, msg); err != nil {
		return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeEIO), err.Error())
	}

	// Output ask confirmation
	env := envelope.OK("agent/ask", map[string]any{
		"ask_id":     askID,
		"message_id": msg.ID,
		"sent_to":    agentRecord.Namespace,
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "ask"
	}))
	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	if wait {
		return waitForReply(ctx, mailboxStore, msg.FromNS, askID, timeout)
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
		env := envelope.OK("agent/cmd", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
			m.Source = "cmd"
		}))
		if err := envelope.Write(os.Stdout, env); err != nil {
			return fmt.Errorf("write envelope: %w", err)
		}
		return nil
	}

	if err := mailboxStore.Send(ctx, msg); err != nil {
		return writeErrorEnvelope(cmd, "agent/cmd", string(protocol.ErrorCodeEIO), err.Error())
	}

	env := envelope.OK("agent/cmd", map[string]any{
		"cmd_id":     cmdID,
		"message_id": msg.ID,
		"sent_to":    agentRecord.Namespace,
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "cmd"
	}))
	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func waitForReply(ctx context.Context, store mailbox.Store, callerNS, askID string, timeout time.Duration) error {
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
					if err := envelope.Write(os.Stdout, replyEnv); err != nil {
						return fmt.Errorf("write reply envelope: %w", err)
					}
					return nil
				}
			}
		}
	}
	return fmt.Errorf("timeout waiting for reply to ask_id=%s", askID)
}
