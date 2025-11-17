package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/execution/agentmanager"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
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
	Long:  "List all agents or agents under a specific namespace",
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

// Flags for agent spawn
var (
	spawnParentNS    string
	spawnRole        string
	spawnPromptFile  string
	spawnSkillsAllow string
	spawnPolicyFile  string
	spawnShareBB     string
)

// Flags for agent kill
var (
	killGraceful bool
	killTimeoutS int
)

// Flags for agent list
var (
	listLimit int
)

func init() {
	// Add agent commands to root
	rootCmd.AddCommand(agentCmd)
	agentCmd.AddCommand(agentSpawnCmd)
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentKillCmd)
	agentCmd.AddCommand(agentInfoCmd)
	agentCmd.AddCommand(agentWatchCmd)

	// Spawn flags
	agentSpawnCmd.Flags().StringVar(&spawnParentNS, "ns", "", "Parent namespace (optional)")
	agentSpawnCmd.Flags().StringVar(&spawnRole, "role", "", "Agent role")
	agentSpawnCmd.Flags().StringVar(&spawnPromptFile, "prompt-file", "", "Path to prompt file")
	agentSpawnCmd.Flags().StringVar(&spawnSkillsAllow, "skills-allow", "", "Comma-separated list of allowed skills")
	agentSpawnCmd.Flags().StringVar(&spawnPolicyFile, "policy", "", "Path to policy JSON file")
	agentSpawnCmd.Flags().StringVar(&spawnShareBB, "share-bb", "scoped", "Blackboard sharing mode (all|scoped|none)")

	// Kill flags
	agentKillCmd.Flags().BoolVar(&killGraceful, "graceful", true, "Graceful shutdown")
	agentKillCmd.Flags().IntVar(&killTimeoutS, "timeout", 30, "Timeout in seconds")

	// List flags
	agentListCmd.Flags().IntVar(&listLimit, "limit", 20, "Maximum number of agents to list")
}

func runAgentSpawn(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	// Load prompt
	var prompt string
	if spawnPromptFile != "" {
		data, err := os.ReadFile(spawnPromptFile)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", protocol.ErrorCodeEARG, fmt.Sprintf("failed to read prompt file: %v", err))
		}
		prompt = string(data)
	}

	// Parse skills allow
	var skillsAllow []string
	if spawnSkillsAllow != "" {
		if err := json.Unmarshal([]byte(spawnSkillsAllow), &skillsAllow); err != nil {
			// Try comma-separated
			skillsAllow = parseCommaSeparated(spawnSkillsAllow)
		}
	}

	// Load policy
	var policy agent.Policy
	if spawnPolicyFile != "" {
		data, err := os.ReadFile(spawnPolicyFile)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", protocol.ErrorCodeEARG, fmt.Sprintf("failed to read policy file: %v", err))
		}
		if err := json.Unmarshal(data, &policy); err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", protocol.ErrorCodeEARG, fmt.Sprintf("failed to parse policy JSON: %v", err))
		}
	}

	// Open stores
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/spawn", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer agentStore.Close()

	mailboxStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/spawn", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer mailboxStore.Close()

	// Create manager
	mgr := agentmanager.New(agentStore, mailboxStore)

	// Spawn agent
	req := agentmanager.SpawnRequest{
		ParentNS:    spawnParentNS,
		Role:        spawnRole,
		Prompt:      prompt,
		SkillsAllow: skillsAllow,
		Policy:      policy,
		ShareBB:     spawnShareBB,
	}

	resp, err := mgr.Spawn(ctx, req)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/spawn", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to spawn agent: %v", err))
	}

	// Write success envelope
	data := map[string]interface{}{
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

func runAgentList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	// Open agent store
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/list", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer agentStore.Close()

	// List agents
	list, err := agentStore.List(ctx, listLimit)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/list", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to list agents: %v", err))
	}

	// Write success envelope
	env := envelope.OK("agent/list", map[string]interface{}{
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
		return writeErrorEnvelope(cmd, "agent/kill", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer agentStore.Close()

	mailboxStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/kill", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer mailboxStore.Close()

	// Create manager
	mgr := agentmanager.New(agentStore, mailboxStore)

	// Kill agent
	req := agentmanager.KillRequest{
		AgentID:  agentID,
		Graceful: killGraceful,
		TimeoutS: killTimeoutS,
	}

	resp, err := mgr.Kill(ctx, req)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/kill", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to kill agent: %v", err))
	}

	// Write success envelope
	data := map[string]interface{}{
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
		return writeErrorEnvelope(cmd, "agent/info", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer agentStore.Close()

	// Get agent
	a, err := agentStore.Get(ctx, agentID)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/info", protocol.ErrorCodeENOTFOUND, fmt.Sprintf("agent not found: %v", err))
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
		return writeErrorEnvelope(cmd, "agent/watch", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer agentStore.Close()

	mailboxStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/watch", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to open mailbox store: %v", err))
	}
	defer mailboxStore.Close()

	// Verify agent exists
	a, err := agentStore.Get(ctx, agentID)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/watch", protocol.ErrorCodeENOTFOUND, fmt.Sprintf("agent not found: %v", err))
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
			env := envelope.OK("agent/watch", map[string]interface{}{
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
				return writeErrorEnvelope(cmd, "agent/watch", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to get agent: %v", err))
			}

			// Check for state change
			if current.State != lastState {
				seq++
				finalBool := false
				data := map[string]interface{}{
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
				data := map[string]interface{}{
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
			messages, err := mailboxStore.List(ctx, a.Namespace, 10)
			if err != nil {
				return writeErrorEnvelope(cmd, "agent/watch", protocol.ErrorCodeERUNTIME, fmt.Sprintf("failed to list messages: %v", err))
			}

			if len(messages) > 0 {
				seq++
				finalBool := false
				data := map[string]interface{}{
					"event":         "mailbox_messages",
					"agent_id":      agentID,
					"message_count": len(messages),
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
						MailboxID: "mailbox:" + a.Namespace,
						Seq:      &seq,
						Final:    &finalBool,
					},
				}

				if err := writer.Write(env); err != nil {
					return fmt.Errorf("write progress envelope: %w", err)
				}
			}
		}
	}
}

func writeErrorEnvelope(cmd *cobra.Command, command, code, message string) error {
	env := envelope.Error(command, code, message, nil)
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
