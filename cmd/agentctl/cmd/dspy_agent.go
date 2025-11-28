package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/runtime"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

// Global dspy-go agent runtime (singleton for the process)
var globalDspyRuntime *runtime.Runtime

var dspyAgentCmd = &cobra.Command{
	Use:   "dspy-agent",
	Short: "Manage dspy-go powered agents",
	Long:  "Manage dspy-go ReActAgents with agentctl tool integration",
}

var dspySpawnCmd = &cobra.Command{
	Use:   "spawn",
	Short: "Spawn a new dspy-go agent",
	Long:  "Spawn a new dspy-go ReActAgent with specified role and configuration",
	RunE:  runDspySpawn,
}

var dspyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active dspy-go agents",
	Long:  "List all active dspy-go agent sessions",
	RunE:  runDspyList,
}

var dspyKillCmd = &cobra.Command{
	Use:   "kill [session-id]",
	Short: "Kill a dspy-go agent session",
	Long:  "Terminate a dspy-go agent session by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runDspyKill,
}

var dspyStatusCmd = &cobra.Command{
	Use:   "status [session-id]",
	Short: "Get status of a dspy-go agent session",
	Long:  "Get detailed status of a dspy-go agent session including tool calls",
	Args:  cobra.ExactArgs(1),
	RunE:  runDspyStatus,
}

// Flags for dspy spawn
var (
	dspyRole        string
	dspyTaskID      string
	dspyEpicID      string
	dspyWorkspaceID string
	dspyMaxIter     int
	dspyTimeoutMins int
	dspyLLMProvider string
	dspyLLMModel    string
)

func init() {
	// Add dspy-agent commands to root
	rootCmd.AddCommand(dspyAgentCmd)
	dspyAgentCmd.AddCommand(dspySpawnCmd)
	dspyAgentCmd.AddCommand(dspyListCmd)
	dspyAgentCmd.AddCommand(dspyKillCmd)
	dspyAgentCmd.AddCommand(dspyStatusCmd)

	// Spawn flags
	dspySpawnCmd.Flags().StringVar(&dspyRole, "role", "coder", "Agent role (coder, planner, reviewer, fixer)")
	dspySpawnCmd.Flags().StringVar(&dspyTaskID, "task", "", "Task ID to work on")
	dspySpawnCmd.Flags().StringVar(&dspyEpicID, "epic", "", "Epic ID for context")
	dspySpawnCmd.Flags().StringVar(&dspyWorkspaceID, "workspace", "", "Workspace ID (defaults to current directory)")
	dspySpawnCmd.Flags().IntVar(&dspyMaxIter, "max-iterations", 10, "Maximum ReAct iterations")
	dspySpawnCmd.Flags().IntVar(&dspyTimeoutMins, "timeout", 30, "Session timeout in minutes")
	dspySpawnCmd.Flags().StringVar(&dspyLLMProvider, "llm-provider", "", "LLM provider (gemini, openai, anthropic)")
	dspySpawnCmd.Flags().StringVar(&dspyLLMModel, "llm-model", "", "LLM model name")
}

// getOrCreateRuntime returns the global dspy-go runtime, creating it if necessary.
func getOrCreateRuntime(ctx context.Context) (*runtime.Runtime, error) {
	if globalDspyRuntime != nil {
		return globalDspyRuntime, nil
	}

	_ = config.MustFromContext(ctx) // Validate context has config

	// Get workspace root
	workspaceRoot, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}

	runtimeCfg := runtime.RuntimeConfig{
		DefaultMaxIterations: 10,
		DefaultTimeout:       30 * time.Minute,
		LLMProvider:          os.Getenv("AGENTCTL_LLM_PROVIDER"),
		LLMModel:             os.Getenv("AGENTCTL_LLM_MODEL"),
		LLMAPIKey:            os.Getenv("AGENTCTL_LLM_API_KEY"),
		WorkspaceRoot:        workspaceRoot,
	}

	globalDspyRuntime = runtime.NewRuntime(runtimeCfg)
	return globalDspyRuntime, nil
}

func runDspySpawn(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	rt, err := getOrCreateRuntime(ctx)
	if err != nil {
		return writeDspyErrorEnvelope(cmd, "dspy-agent/spawn", "ERUNTIME", fmt.Sprintf("failed to create runtime: %v", err))
	}

	// Determine workspace
	workspace := dspyWorkspaceID
	if workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			return writeDspyErrorEnvelope(cmd, "dspy-agent/spawn", "EARG", fmt.Sprintf("failed to get working directory: %v", err))
		}
		workspace = wd
	}

	// Parse role
	role := parseAgentRole(dspyRole)

	// Build agent config
	agentCfg := types.AgentConfig{
		Role:          role,
		TaskID:        dspyTaskID,
		EpicID:        dspyEpicID,
		WorkspaceID:   workspace,
		ActorID:       fmt.Sprintf("dspy:%s", dspyRole),
		MaxIterations: dspyMaxIter,
		Timeout:       time.Duration(dspyTimeoutMins) * time.Minute,
		LLMProvider:   dspyLLMProvider,
		LLMModel:      dspyLLMModel,
		LLMAPIKey:     os.Getenv("AGENTCTL_LLM_API_KEY"), // Get from env
	}

	// Spawn the agent
	session, err := rt.Spawn(ctx, agentCfg)
	if err != nil {
		return writeDspyErrorEnvelope(cmd, "dspy-agent/spawn", "ERUNTIME", fmt.Sprintf("failed to spawn agent: %v", err))
	}

	// Write success envelope
	data := map[string]interface{}{
		"session_id": session.ID,
		"role":       string(role),
		"workspace":  workspace,
		"task_id":    dspyTaskID,
		"status":     string(session.Status),
		"started_at": session.StartedAt.Format(time.RFC3339),
	}

	env := envelope.OK("dspy-agent/spawn", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runDspyList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	rt, err := getOrCreateRuntime(ctx)
	if err != nil {
		return writeDspyErrorEnvelope(cmd, "dspy-agent/list", "ERUNTIME", fmt.Sprintf("failed to create runtime: %v", err))
	}

	sessions := rt.List()

	// Build session list for envelope
	sessionData := make([]map[string]interface{}, 0, len(sessions))
	for _, s := range sessions {
		sess := s.GetSession()
		data := map[string]interface{}{
			"session_id": sess.ID,
			"role":       string(sess.Config.Role),
			"status":     string(sess.Status),
			"started_at": sess.StartedAt.Format(time.RFC3339),
			"iterations": sess.Iterations,
		}
		if sess.EndedAt != nil {
			data["ended_at"] = sess.EndedAt.Format(time.RFC3339)
		}
		sessionData = append(sessionData, data)
	}

	// Also print a human-readable table to stderr
	w := tabwriter.NewWriter(os.Stderr, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION ID\tROLE\tSTATUS\tITERATIONS\tSTARTED")
	for _, s := range sessions {
		sess := s.GetSession()
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
			sess.ID[:8]+"...",
			sess.Config.Role,
			sess.Status,
			sess.Iterations,
			sess.StartedAt.Format("15:04:05"),
		)
	}
	w.Flush()

	// Write success envelope
	env := envelope.OK("dspy-agent/list", map[string]interface{}{
		"sessions": sessionData,
		"count":    len(sessions),
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runDspyKill(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	sessionID := args[0]

	rt, err := getOrCreateRuntime(ctx)
	if err != nil {
		return writeDspyErrorEnvelope(cmd, "dspy-agent/kill", "ERUNTIME", fmt.Sprintf("failed to create runtime: %v", err))
	}

	// Try to find session by prefix if full ID not provided
	sessions := rt.List()
	var matchedID string
	for _, s := range sessions {
		if s.ID == sessionID || strings.HasPrefix(s.ID, sessionID) {
			matchedID = s.ID
			break
		}
	}

	if matchedID == "" {
		return writeDspyErrorEnvelope(cmd, "dspy-agent/kill", "ENOTFOUND", fmt.Sprintf("session not found: %s", sessionID))
	}

	if err := rt.Kill(matchedID); err != nil {
		return writeDspyErrorEnvelope(cmd, "dspy-agent/kill", "ERUNTIME", fmt.Sprintf("failed to kill session: %v", err))
	}

	// Write success envelope
	env := envelope.OK("dspy-agent/kill", map[string]interface{}{
		"session_id": matchedID,
		"status":     "canceled",
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runDspyStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	sessionID := args[0]

	rt, err := getOrCreateRuntime(ctx)
	if err != nil {
		return writeDspyErrorEnvelope(cmd, "dspy-agent/status", "ERUNTIME", fmt.Sprintf("failed to create runtime: %v", err))
	}

	// Try to find session by prefix if full ID not provided
	sessions := rt.List()
	var session *runtime.Session
	for _, s := range sessions {
		if s.ID == sessionID || strings.HasPrefix(s.ID, sessionID) {
			session = s
			break
		}
	}

	if session == nil {
		return writeDspyErrorEnvelope(cmd, "dspy-agent/status", "ENOTFOUND", fmt.Sprintf("session not found: %s", sessionID))
	}

	sess := session.GetSession()
	toolCalls := session.GetToolCalls()

	// Build tool call data
	toolCallData := make([]map[string]interface{}, 0, len(toolCalls))
	for _, tc := range toolCalls {
		data := map[string]interface{}{
			"tool":      tc.ToolName,
			"timestamp": tc.Timestamp.Format(time.RFC3339),
			"duration":  tc.Duration.String(),
			"success":   tc.Error == "",
		}
		if tc.Error != "" {
			data["error"] = tc.Error
		}
		toolCallData = append(toolCallData, data)
	}

	// Build session data
	data := map[string]interface{}{
		"session_id": sess.ID,
		"role":       string(sess.Config.Role),
		"status":     string(sess.Status),
		"started_at": sess.StartedAt.Format(time.RFC3339),
		"iterations": sess.Iterations,
		"tool_calls": toolCallData,
	}

	if sess.EndedAt != nil {
		data["ended_at"] = sess.EndedAt.Format(time.RFC3339)
		data["duration"] = sess.EndedAt.Sub(sess.StartedAt).String()
	}

	if sess.Summary != "" {
		data["summary"] = sess.Summary
	}

	if sess.Error != "" {
		data["error"] = sess.Error
	}

	// Write success envelope
	env := envelope.OK("dspy-agent/status", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func writeDspyErrorEnvelope(_ *cobra.Command, command, code, message string) error {
	env := envelope.Error(command, code, message, nil)
	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s", message)
}

func parseAgentRole(role string) types.AgentRole {
	switch strings.ToLower(role) {
	case "coder":
		return types.RoleCoder
	case "planner":
		return types.RolePlanner
	case "reviewer":
		return types.RoleReviewer
	case "fixer":
		return types.RoleFixer
	default:
		return types.RoleCoder
	}
}
