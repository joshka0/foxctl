package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentdaemon "github.com/jkatigb/agentctl/internal/agent/daemon"
	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/agent/prompts"
	"github.com/jkatigb/agentctl/internal/agent/toolnames"
	"github.com/jkatigb/agentctl/internal/daemon"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/execution/agentmanager"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/protocol"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
	libsqlevents "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/events"
	libsqlprojections "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/projections"
	v2ask "github.com/jkatigb/agentctl/internal/v2/core/ask"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/events"
	v2services "github.com/jkatigb/agentctl/internal/v2/services"
	"github.com/jkatigb/agentctl/internal/zellijbridge"
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

var agentOutputCmd = &cobra.Command{
	Use:   "output [agent-id]",
	Short: "Get agent output",
	Long:  "Retrieve the output/summary from an agent's most recent session",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentOutput,
}

var agentRenameCmd = &cobra.Command{
	Use:   "rename <agent-ref>",
	Short: "Rename an agent",
	Long:  "Update an agent's human name or slug handle",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentRename,
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

var agentAskStatusCmd = &cobra.Command{
	Use:   "ask-status <ask-id>",
	Short: "Get ask status and callback details",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentAskStatus,
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
	spawnParentNS          string
	spawnName              string
	spawnSlug              string
	spawnRole              string
	spawnPrompt            string
	spawnPromptFile        string
	spawnSkillsAllow       string
	spawnPolicyFile        string
	spawnShareBB           string
	spawnLLMProvider       string
	spawnLLMModel          string
	spawnLLMAPIKey         string
	spawnLLMBaseURL        string
	spawnLLMAuthMode       string
	spawnLLMAuthHeader     string
	spawnLLMAuthPrefix     string
	spawnWorkspace         string
	spawnExecMode          string
	spawnMaxIterations     int
	spawnMaxContextTokens  int
	spawnMaxAutoTurns      int
	spawnThinkInterval     int
	spawnMemoryScope       string
	spawnMemoryRetention   string
	spawnTimeout           string // Session timeout (e.g. "10m", "30m")
	spawnDryRun            bool
	spawnChat              bool // Convenience flag for chat/roleplay companions
	spawnMuxBackend        string
	spawnMuxSession        string
	spawnMuxPaneID         string
	spawnParticipantID     string
	spawnParentParticipant string
	spawnParentAgentID     string
	spawnRoomID            string
	spawnRoomAccess        string
	spawnInPane            bool
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

// Flags for agent rename
var (
	renameName   string
	renameSlug   string
	renameDryRun bool
)

// Flags for agent list
var (
	listLimit int
	listState string
)

// Flags for agent ask
var (
	askDryRun         bool
	askDispatcherMode string
)

// Flags for agent spawn dispatcher/runtime routing.
var (
	spawnDispatcher string
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
	agentCmd.AddCommand(agentOutputCmd)
	agentCmd.AddCommand(agentRenameCmd)
	agentCmd.AddCommand(agentWatchCmd)
	agentCmd.AddCommand(agentRunCmd)
	agentCmd.AddCommand(agentAskCmd)
	agentCmd.AddCommand(agentAskStatusCmd)
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
	agentSpawnCmd.Flags().StringVar(&spawnLLMProvider, "llm-provider", "", "LLM provider (lmstudio|gemini|openai|anthropic|groq|openrouter)")
	agentSpawnCmd.Flags().StringVar(&spawnLLMModel, "llm-model", "", "LLM model ID (e.g., claude-haiku-4-5)")
	agentSpawnCmd.Flags().StringVar(&spawnLLMAPIKey, "llm-api-key", "", "LLM API key (or env var like $GROQ_API_KEY)")
	agentSpawnCmd.Flags().StringVar(&spawnLLMBaseURL, "llm-base-url", "", "LLM base URL override for OpenAI-compatible/self-hosted backends")
	agentSpawnCmd.Flags().StringVar(&spawnLLMAuthMode, "llm-auth-mode", "", "LLM auth mode: auto, none, bearer, header")
	agentSpawnCmd.Flags().StringVar(&spawnLLMAuthHeader, "llm-auth-header", "", "LLM auth header name when --llm-auth-mode=header")
	agentSpawnCmd.Flags().StringVar(&spawnLLMAuthPrefix, "llm-auth-prefix", "", "LLM auth prefix for bearer/header auth (e.g. 'Bearer ' or 'Token ')")
	agentSpawnCmd.Flags().StringVar(&spawnWorkspace, "workspace", "", "Workspace root for filesystem-bound tools (default: current directory)")
	agentSpawnCmd.Flags().StringVar(&spawnExecMode, "exec-mode", "reactive", "Execution mode (reactive|autonomous|proactive|tick)")
	agentSpawnCmd.Flags().IntVar(&spawnMaxIterations, "max-iterations", 10, "Max tool calls per turn")
	agentSpawnCmd.Flags().IntVar(&spawnMaxContextTokens, "max-context-tokens", 0, "Max context tokens before stopping (0=no limit)")
	agentSpawnCmd.Flags().IntVar(&spawnMaxAutoTurns, "max-auto-turns", 1, "Max autonomous turns per session (only for autonomous/proactive/tick modes)")
	agentSpawnCmd.Flags().IntVar(&spawnThinkInterval, "think-interval", 60, "Seconds between proactive/tick cycles")
	agentSpawnCmd.Flags().StringVar(&spawnMemoryScope, "memory-scope", "", "Memory lineage scope (agent|session)")
	agentSpawnCmd.Flags().StringVar(&spawnMemoryRetention, "memory-retention", "", "Memory retention preset (companion|durable|task|ephemeral)")
	agentSpawnCmd.Flags().StringVar(&spawnTimeout, "timeout", "", "Session timeout (e.g. 10m, 30m). Default: 30m")
	agentSpawnCmd.Flags().BoolVar(&spawnDryRun, "dry-run", false, "Preview what would be spawned without creating the agent")
	agentSpawnCmd.Flags().BoolVar(&spawnChat, "chat", false, "Convenience flag for chat/roleplay companions (sets role=companion, exec-mode=reactive, max-iterations=3)")
	agentSpawnCmd.Flags().StringVar(&spawnDispatcher, "dispatcher", "", "Execution layer for spawned agents: mailbox|jido (default from AGENTCTL_V2_ASK_DISPATCHER)")
	agentSpawnCmd.Flags().StringVar(&spawnMuxBackend, "mux-backend", "", "Mux backend for terminal binding metadata (for example: tmux or zellij)")
	agentSpawnCmd.Flags().StringVar(&spawnMuxSession, "mux-session", "", "Mux session name for terminal binding metadata")
	agentSpawnCmd.Flags().StringVar(&spawnMuxPaneID, "mux-pane-id", "", "Mux pane id for terminal binding metadata")
	agentSpawnCmd.Flags().StringVar(&spawnParticipantID, "participant-id", "", "Participant id for room or direct parent-child routing")
	agentSpawnCmd.Flags().StringVar(&spawnParentParticipant, "parent-participant", "", "Parent participant id for parent-private child agents")
	agentSpawnCmd.Flags().StringVar(&spawnParentAgentID, "parent-agent-id", "", "Parent agent id for parent-private child agents")
	agentSpawnCmd.Flags().StringVar(&spawnRoomID, "room-id", "", "Room id for directly room-visible agents")
	agentSpawnCmd.Flags().StringVar(&spawnRoomAccess, "room-access", "default", "Room access policy for this agent: default|direct|none")
	agentSpawnCmd.Flags().BoolVar(&spawnInPane, "spawn-in-pane", false, "Allocate a dedicated tmux pane for the agent and repurpose it into agent watch output")

	// Run flags
	agentRunCmd.Flags().StringVar(&runCompanionMode, "companion-mode", "", "Memory mode for conversation memory: standard (40K tokens) or roleplay (50K tokens)")

	// Kill flags
	agentKillCmd.Flags().BoolVar(&killGraceful, "graceful", true, "Graceful shutdown")
	agentKillCmd.Flags().IntVar(&killTimeoutS, "timeout", 30, "Timeout in seconds")
	agentKillCmd.Flags().BoolVar(&killDryRun, "dry-run", false, "Preview what would be killed without terminating the agent")

	// Rename flags
	agentRenameCmd.Flags().StringVar(&renameName, "name", "", "Human name for the agent (e.g., 'Luna', 'Atlas')")
	agentRenameCmd.Flags().StringVar(&renameSlug, "slug", "", "Human-readable handle for referencing (e.g., 'companion')")
	agentRenameCmd.Flags().BoolVar(&renameDryRun, "dry-run", false, "Preview what would be renamed without updating the agent")

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
	agentAskCmd.Flags().StringVar(&askDispatcherMode, "dispatcher", "", "Ask dispatcher backend: mailbox|jido (default from AGENTCTL_V2_ASK_DISPATCHER)")
	_ = agentAskCmd.MarkFlagRequired("question") //nolint:errcheck

	// Cmd flags
	agentCmdCmd.Flags().String("action", "", "Command action: run_skill|run_turn|do_work")
	agentCmdCmd.Flags().String("skill", "", "Skill to run (for run_skill action)")
	agentCmdCmd.Flags().String("args", "{}", "JSON args for the command")
	agentCmdCmd.Flags().BoolVar(&cmdDryRun, "dry-run", false, "Preview what would be sent without sending the command")
	_ = agentCmdCmd.MarkFlagRequired("action") //nolint:errcheck
}

func runAgentSpawn(cmd *cobra.Command, args []string) error {
	return runAgentSpawnWithRoute(cmd)
}

func currentSpawnWorkspaceRoot() string {
	target := strings.TrimSpace(spawnWorkspace)
	if target == "" {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		target = wd
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return target
	}
	return abs
}

func currentSpawnWorkspaceID() string {
	root := currentSpawnWorkspaceRoot()
	if root == "" {
		return ""
	}
	return ws.ID(root)
}

func currentSpawnTerminalBinding() agent.TerminalBinding {
	return agent.NormalizeTerminalBinding(agent.TerminalBinding{
		Backend:             spawnMuxBackend,
		Session:             spawnMuxSession,
		PaneID:              spawnMuxPaneID,
		ParticipantID:       spawnParticipantID,
		ParentParticipantID: spawnParentParticipant,
		ParentAgentID:       spawnParentAgentID,
		RoomID:              spawnRoomID,
		RoomAccess:          spawnRoomAccess,
	})
}

func deriveSpawnPaneLabel() string {
	for _, value := range []string{spawnSlug, spawnName, spawnRole} {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		value = strings.ReplaceAll(value, " ", "-")
		value = strings.ReplaceAll(value, "/", "-")
		value = strings.ReplaceAll(value, ":", "-")
		value = strings.Trim(value, "-")
		if value != "" {
			return value
		}
	}
	return "agent"
}

func deriveZellijParticipantID(binding agent.TerminalBinding) string {
	if value := strings.TrimSpace(binding.ParticipantID); value != "" {
		return value
	}
	label := deriveSpawnPaneLabel()
	if strings.TrimSpace(binding.Session) == "" {
		return label
	}
	return label
}

func maybePrepareSpawnPane(ctx context.Context, binding agent.TerminalBinding) (agent.TerminalBinding, map[string]any, error) {
	if !spawnInPane {
		return binding, nil, nil
	}
	binding = agent.NormalizeTerminalBinding(binding)
	if strings.TrimSpace(binding.Backend) == "" {
		if strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME")) != "" {
			binding.Backend = "zellij"
		} else {
			binding.Backend = "tmux"
		}
	}
	if strings.TrimSpace(binding.Session) == "" && strings.TrimSpace(binding.RoomID) != "" {
		binding.Session = roomSourceSessionName(strings.TrimSpace(binding.RoomID), binding.Backend)
	}
	switch binding.Backend {
	case "tmux":
	case "zellij":
		if strings.TrimSpace(binding.Session) == "" {
			if value := strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME")); value != "" {
				binding.Session = value
			}
		}
		if strings.TrimSpace(binding.Session) == "" {
			return binding, nil, fmt.Errorf("zellij spawn-in-pane requires --mux-session or ZELLIJ_SESSION_NAME")
		}
		if strings.TrimSpace(binding.ParticipantID) == "" {
			binding.ParticipantID = deriveZellijParticipantID(binding)
		}
		if strings.TrimSpace(binding.PaneID) == "" {
			binding.PaneID = binding.ParticipantID
		}
		binding = agent.NormalizeTerminalBinding(binding)
		return binding, nil, nil
	default:
		return binding, nil, fmt.Errorf("spawn-in-pane currently supports only tmux and zellij")
	}
	client := tmuxbridge.New()
	opts := tmuxbridge.CreatePaneOptions{
		Session:       strings.TrimSpace(binding.Session),
		CWD:           currentSpawnWorkspaceRoot(),
		Label:         deriveSpawnPaneLabel(),
		ParticipantID: strings.TrimSpace(binding.ParticipantID),
		RoomID:        strings.TrimSpace(binding.RoomID),
		RoomAccess:    strings.TrimSpace(binding.RoomAccess),
	}
	result, err := client.CreatePane(ctx, opts)
	if err != nil {
		return binding, nil, err
	}
	binding.Backend = "tmux"
	binding.Session = result.Session
	binding.PaneID = result.Pane.ID
	if strings.TrimSpace(binding.ParticipantID) == "" {
		binding.ParticipantID = "tmux:" + result.Session + ":" + result.Pane.ID
	}
	binding = agent.NormalizeTerminalBinding(binding)
	return binding, map[string]any{
		"pane":           result.Pane,
		"attach_command": result.AttachCommand,
		"socket_mode":    result.SocketMode,
	}, nil
}

// roomSourceSessionName derives a mux session name scoped to a room so agents
// created for the same room land in the same session.
func roomSourceSessionName(roomID, backend string) string {
	slug := strings.TrimSpace(strings.ToLower(roomID))
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "/", "-")
	slug = strings.ReplaceAll(slug, ":", "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "room"
	}
	switch strings.TrimSpace(backend) {
	case "zellij":
		return slug + "-room"
	default:
		return "room-" + slug
	}
}

func maybeRespawnSpawnPane(ctx context.Context, binding agent.TerminalBinding, agentID string) (map[string]any, error) {
	if !spawnInPane {
		return nil, nil
	}
	binding = agent.NormalizeTerminalBinding(binding)
	if strings.TrimSpace(binding.PaneID) == "" {
		return nil, nil
	}
	command := spawnPaneCommand(agentID, binding)
	switch binding.Backend {
	case "tmux":
		client := tmuxbridge.New()
		result, err := client.RespawnPane(ctx, tmuxbridge.RespawnPaneOptions{
			Target:            binding.PaneID,
			CWD:               currentSpawnWorkspaceRoot(),
			Command:           command,
			ParticipantID:     binding.ParticipantID,
			ParentParticipant: binding.ParentParticipantID,
			ParentAgentID:     binding.ParentAgentID,
			RoomID:            binding.RoomID,
			RoomAccess:        binding.RoomAccess,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"pane":    result.Pane,
			"command": command,
		}, nil
	case "zellij":
		client := zellijbridge.New()
		result, err := client.CreatePane(ctx, zellijbridge.CreatePaneOptions{
			Session:           binding.Session,
			CWD:               currentSpawnWorkspaceRoot(),
			Name:              binding.PaneID,
			Command:           command,
			ParticipantID:     binding.ParticipantID,
			ParentParticipant: binding.ParentParticipantID,
			ParentAgentID:     binding.ParentAgentID,
			RoomID:            binding.RoomID,
			RoomAccess:        binding.RoomAccess,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"session":        result.Session,
			"pane_name":      result.PaneName,
			"participant_id": result.ParticipantID,
			"command":        command,
		}, nil
	default:
		return nil, nil
	}
}

// spawnPaneCommand returns the command to run in the agent source pane.
// When room context is available, the agent runs live in the pane so users
// can watch it work. Otherwise, falls back to watch mode.
func spawnPaneCommand(agentID string, binding agent.TerminalBinding) string {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "agentctl agent watch"
	}
	if strings.TrimSpace(binding.RoomID) != "" {
		return "agentctl agent run " + agentID
	}
	return "agentctl agent watch " + agentID
}

// maybeAutoJoinRoom registers the newly spawned agent as a room member with
// pane binding metadata so the room relay can route messages to the source pane.
// This is only called when --room-id and --spawn-in-pane are both set.
func maybeAutoJoinRoom(ctx context.Context, binding agent.TerminalBinding) (map[string]any, error) {
	if !spawnInPane {
		return nil, nil
	}
	binding = agent.NormalizeTerminalBinding(binding)
	roomID := strings.TrimSpace(binding.RoomID)
	if roomID == "" {
		return nil, nil
	}
	if strings.TrimSpace(binding.ParticipantID) == "" {
		return nil, nil
	}
	absWorkspace, err := resolveRoomWorkspace(".")
	if err != nil {
		return nil, nil
	}
	store, err := openRoomBoardStore(ctx)
	if err != nil {
		return nil, nil
	}
	defer store.Close()

	if _, err := store.EnsureRoom(ctx, absWorkspace, roomID, roomID); err != nil {
		return nil, nil
	}
	member := agent.RoomMember{
		ActorID: binding.ParticipantID,
		Role:    strings.TrimSpace(spawnRole),
		Backend: binding.Backend,
		Session: binding.Session,
		PaneID:  binding.PaneID,
	}
	if member.Role == "" {
		member.Role = "worker"
	}
	summary, err := store.GetRoom(ctx, absWorkspace, roomID, "")
	if err != nil {
		return nil, nil
	}
	updatedMembers := mergeRoomMembers(summary.Members, member)
	if _, err := store.ReplaceRoomMembers(ctx, absWorkspace, roomID, updatedMembers); err != nil {
		return nil, nil
	}
	return map[string]any{
		"room_id":        roomID,
		"participant_id": binding.ParticipantID,
		"role":           member.Role,
		"backend":        binding.Backend,
		"session":        binding.Session,
		"pane_id":        binding.PaneID,
	}, nil
}

func resolveSpawnPromptVariantTarget(cfg config.Config, executionLayer agent.ExecutionLayer) (string, string) {
	provider := strings.TrimSpace(spawnLLMProvider)
	if provider == "" {
		provider = strings.TrimSpace(cfg.LLM.Provider)
	}
	if executionLayer == agent.ExecutionLayerClassic && provider == "" {
		provider = "lmstudio"
	}

	model := strings.TrimSpace(spawnLLMModel)
	if model == "" {
		model = strings.TrimSpace(cfg.LLM.Model)
	}
	if model == "" && provider != "" {
		model = llmproviders.DefaultModelForProvider(provider)
	}
	return provider, model
}

func runAgentSpawnWithRoute(cmd *cobra.Command) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	executionLayer := resolvedSpawnExecutionLayer(spawnDispatcher)
	terminalBinding := currentSpawnTerminalBinding()
	var spawnPaneMetadata map[string]any

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
		workspaceID := currentSpawnWorkspaceID()
		effectiveProvider, effectiveModel := resolveSpawnPromptVariantTarget(cfg, executionLayer)
		targetProfile := optimization.DerivePromptTargetProfile(string(executionLayer), effectiveProvider, effectiveModel)
		if variantStore, err := optimization.OpenPromptVariantStore(ctx, cfg.Storage.Root); err == nil {
			if variant, resolveErr := variantStore.ResolveLatestCompatible(ctx, workspaceID, spawnRole, targetProfile); resolveErr == nil && strings.TrimSpace(variant.Prompt) != "" {
				prompt = variant.Prompt
			}
			variantStore.Close() //nolint:errcheck
		}
		if prompt == "" {
			if defaultPrompt, ok := prompts.DefaultPrompt(spawnRole); ok {
				prompt = defaultPrompt
			}
		}
	}
	if strings.TrimSpace(spawnRoomID) != "" {
		prompt = prompts.ComposeRoomAwarePrompt(prompt, prompts.RoomOnboardingOptions{
			RoomID:      strings.TrimSpace(spawnRoomID),
			WorkspaceID: currentSpawnWorkspaceID(),
			Role:        strings.TrimSpace(spawnRole),
		})
	}

	// Parse skills allow list (used in daemon and legacy spawn paths).
	var skillsAllow []string
	if spawnSkillsAllow != "" {
		trimmed := strings.TrimSpace(spawnSkillsAllow)
		if strings.HasPrefix(trimmed, "[") {
			if err := json.Unmarshal([]byte(trimmed), &skillsAllow); err != nil {
				return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeEARG), fmt.Sprintf("invalid JSON in skills-allow: %v", err), "Use a JSON array like [\"code/smart_search\"] or a comma-separated list.")
			}
		} else {
			skillsAllow = parseCommaSeparated(trimmed)
		}
	}

	// Normalize skills allowlist
	if len(skillsAllow) > 0 {
		skillsAllow = toolnames.NormalizeAllowlist(toolnames.ToolModeRuntime, skillsAllow)
	}

	// Resolve LLM API key (support $ENV_VAR syntax)
	llmAPIKey := spawnLLMAPIKey
	if strings.HasPrefix(llmAPIKey, "$") {
		llmAPIKey = os.Getenv(strings.TrimPrefix(llmAPIKey, "$"))
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

	// Jido-managed agents are persisted locally but started in the Jido runtime instead
	// of the classic daemon/runtime path.
	if !spawnDryRun {
		var err error
		terminalBinding, spawnPaneMetadata, err = maybePrepareSpawnPane(ctx, terminalBinding)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to prepare tmux pane: %v", err))
		}
	}
	if executionLayer == agent.ExecutionLayerJido {
		req := agentmanager.SpawnRequest{
			ParentNS:        spawnParentNS,
			Name:            spawnName,
			Slug:            spawnSlug,
			Role:            spawnRole,
			Prompt:          prompt,
			SkillsAllow:     skillsAllow,
			Policy:          policy,
			ShareBB:         spawnShareBB,
			MemoryScope:     agent.MemoryScope(strings.TrimSpace(spawnMemoryScope)),
			MemoryRetention: agent.MemoryRetention(strings.TrimSpace(spawnMemoryRetention)),
			LLMProvider:     spawnLLMProvider,
			LLMModel:        spawnLLMModel,
			LLMAPIKey:       llmAPIKey,
			LLMBaseURL:      spawnLLMBaseURL,
			LLMAuthMode:     spawnLLMAuthMode,
			LLMAuthHeader:   spawnLLMAuthHeader,
			LLMAuthPrefix:   spawnLLMAuthPrefix,
			ExecMode:        agent.ExecutionMode(spawnExecMode),
			MaxIterations:   spawnMaxIterations,
			MaxAutoTurns:    spawnMaxAutoTurns,
			ThinkInterval:   spawnThinkInterval,
			TerminalBinding: terminalBinding,
		}

		if spawnDryRun {
			data := map[string]any{
				"dry_run":             true,
				"would_spawn":         true,
				"dispatcher":          "jido",
				"execution_layer":     string(executionLayer),
				"name":                req.Name,
				"slug":                req.Slug,
				"role":                req.Role,
				"skills_allow":        req.SkillsAllow,
				"share_bb":            req.ShareBB,
				"memory_scope":        string(req.MemoryScope),
				"memory_retention":    string(req.MemoryRetention),
				"llm_provider":        req.LLMProvider,
				"llm_model":           req.LLMModel,
				"exec_mode":           string(req.ExecMode),
				"max_iterations":      req.MaxIterations,
				"max_auto_turns":      req.MaxAutoTurns,
				"think_interval":      req.ThinkInterval,
				"terminal_binding":    req.TerminalBinding,
				"would_spawn_in_pane": spawnInPane,
				"has_prompt":          len(req.Prompt) > 0,
				"has_policy":          req.Policy.CPU > 0 || req.Policy.MemoryMB > 0 || req.Policy.Timeout != "",
			}
			return writeOK(cmd, "agent/spawn", data, "run", nil)
		}

		workspaceRoot, err := filepath.Abs(".")
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to resolve workspace root: %v", err))
		}
		resp, err := spawnJidoManagedAgent(ctx, cfg.Storage.Root, workspaceRoot, req)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to spawn jido agent: %v", err))
		}
		watchPane, err := maybeRespawnSpawnPane(ctx, terminalBinding, resp.AgentID)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to attach tmux pane watcher: %v", err))
		}
		roomJoin, _ := maybeAutoJoinRoom(ctx, terminalBinding)

		data := map[string]any{
			"agent_id":         resp.AgentID,
			"ns":               resp.NS,
			"role":             resp.Role,
			"dispatcher":       "jido",
			"execution_layer":  string(executionLayer),
			"via_daemon":       false,
			"terminal_binding": terminalBinding,
			"spawn_pane":       spawnPaneMetadata,
			"watch_pane":       watchPane,
			"room_join":        roomJoin,
		}
		return writeOK(cmd, "agent/spawn", data, "run", nil)
	}

	// Try daemon client first (new runtime with tools like session.recall, memory.query)
	daemonClient := daemon.NewClient()
	if daemonClient.IsRunning() {
		// Guard against unsupported flags in daemon mode
		var unsupportedFlags []string
		if spawnPolicyFile != "" {
			unsupportedFlags = append(unsupportedFlags, "--policy")
		}
		if spawnShareBB != "scoped" {
			unsupportedFlags = append(unsupportedFlags, "--share-bb")
		}
		if spawnParentNS != "" {
			unsupportedFlags = append(unsupportedFlags, "--ns")
		}
		if len(unsupportedFlags) > 0 {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeEARG),
				fmt.Sprintf("flags not supported in daemon mode: %s (use direct spawn instead)", strings.Join(unsupportedFlags, ", ")))
		}

		params := daemon.AgentSpawnParams{
			Role:             spawnRole,
			WorkspaceID:      currentSpawnWorkspaceID(),
			WorkspaceRoot:    currentSpawnWorkspaceRoot(),
			Prompt:           prompt,
			Name:             spawnName,
			Slug:             spawnSlug,
			SkillsAllow:      skillsAllow,
			MemoryScope:      spawnMemoryScope,
			MemoryRetention:  spawnMemoryRetention,
			MaxIterations:    spawnMaxIterations,
			MaxContextTokens: spawnMaxContextTokens,
			ExecMode:         spawnExecMode,
			MaxAutoTurns:     spawnMaxAutoTurns,
			ThinkInterval:    spawnThinkInterval,
			Timeout:          spawnTimeout,
			LLMProvider:      spawnLLMProvider,
			LLMModel:         spawnLLMModel,
			LLMAPIKey:        llmAPIKey,
			LLMBaseURL:       spawnLLMBaseURL,
			LLMAuthMode:      spawnLLMAuthMode,
			LLMAuthHeader:    spawnLLMAuthHeader,
			LLMAuthPrefix:    spawnLLMAuthPrefix,
			TerminalBinding:  terminalBinding,
		}

		// Dry-run mode: show what would be spawned via daemon
		if spawnDryRun {
			data := map[string]any{
				"dry_run":             true,
				"would_spawn":         true,
				"via_daemon":          true,
				"workspace_id":        params.WorkspaceID,
				"workspace_root":      params.WorkspaceRoot,
				"dispatcher":          "mailbox",
				"execution_layer":     string(executionLayer),
				"role":                params.Role,
				"name":                params.Name,
				"slug":                params.Slug,
				"llm_provider":        params.LLMProvider,
				"llm_model":           params.LLMModel,
				"exec_mode":           params.ExecMode,
				"memory_scope":        params.MemoryScope,
				"memory_retention":    params.MemoryRetention,
				"max_iterations":      params.MaxIterations,
				"max_auto_turns":      params.MaxAutoTurns,
				"think_interval":      params.ThinkInterval,
				"terminal_binding":    params.TerminalBinding,
				"would_spawn_in_pane": spawnInPane,
				"has_prompt":          len(params.Prompt) > 0,
			}
			return writeOK(cmd, "agent/spawn", data, "run", nil)
		}

		result, err := daemonClient.AgentSpawn(params)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("daemon spawn failed: %v", err))
		}
		watchPane, err := maybeRespawnSpawnPane(ctx, terminalBinding, result.AgentID)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to attach tmux pane watcher: %v", err))
		}
		roomJoin, _ := maybeAutoJoinRoom(ctx, terminalBinding)

		// Write success envelope
		data := map[string]any{
			"session_id":       result.SessionID,
			"actor_id":         result.ActorID,
			"agent_id":         result.AgentID,
			"name":             result.Name,
			"status":           result.Status,
			"role":             result.Role,
			"ns":               result.NS,
			"dispatcher":       "mailbox",
			"execution_layer":  string(executionLayer),
			"via_daemon":       true,
			"terminal_binding": terminalBinding,
			"spawn_pane":       spawnPaneMetadata,
			"watch_pane":       watchPane,
			"room_join":        roomJoin,
		}
		return writeOK(cmd, "agent/spawn", data, "run", nil)
	}

	// Fall back to old agentmanager (legacy, does not have new tools)

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
		ParentNS:        spawnParentNS,
		Name:            spawnName,
		Slug:            spawnSlug,
		Role:            spawnRole,
		Prompt:          prompt,
		SkillsAllow:     skillsAllow,
		Policy:          policy,
		ShareBB:         spawnShareBB,
		MemoryScope:     agent.MemoryScope(strings.TrimSpace(spawnMemoryScope)),
		MemoryRetention: agent.MemoryRetention(strings.TrimSpace(spawnMemoryRetention)),
		LLMProvider:     spawnLLMProvider,
		LLMModel:        spawnLLMModel,
		LLMAPIKey:       llmAPIKey,
		LLMBaseURL:      spawnLLMBaseURL,
		LLMAuthMode:     spawnLLMAuthMode,
		LLMAuthHeader:   spawnLLMAuthHeader,
		LLMAuthPrefix:   spawnLLMAuthPrefix,
		ExecMode:        agent.ExecutionMode(spawnExecMode),
		MaxIterations:   spawnMaxIterations,
		MaxAutoTurns:    spawnMaxAutoTurns,
		ThinkInterval:   spawnThinkInterval,
		TerminalBinding: terminalBinding,
	}

	// Dry-run mode: show what would be spawned
	if spawnDryRun {
		data := map[string]any{
			"dry_run":             true,
			"would_spawn":         true,
			"via_daemon":          false,
			"dispatcher":          "mailbox",
			"execution_layer":     string(executionLayer),
			"parent_ns":           req.ParentNS,
			"name":                req.Name,
			"slug":                req.Slug,
			"role":                req.Role,
			"skills_allow":        req.SkillsAllow,
			"share_bb":            req.ShareBB,
			"memory_scope":        string(req.MemoryScope),
			"memory_retention":    string(req.MemoryRetention),
			"llm_provider":        req.LLMProvider,
			"llm_model":           req.LLMModel,
			"exec_mode":           string(req.ExecMode),
			"max_iterations":      req.MaxIterations,
			"max_auto_turns":      req.MaxAutoTurns,
			"think_interval":      req.ThinkInterval,
			"terminal_binding":    req.TerminalBinding,
			"would_spawn_in_pane": spawnInPane,
			"has_prompt":          len(req.Prompt) > 0,
			"has_policy":          req.Policy.CPU > 0 || req.Policy.MemoryMB > 0 || req.Policy.Timeout != "",
		}
		return writeOK(cmd, "agent/spawn", data, "run", nil)
	}

	resp, err := mgr.Spawn(ctx, req)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to spawn agent: %v", err))
	}
	watchPane, err := maybeRespawnSpawnPane(ctx, terminalBinding, resp.AgentID)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/spawn", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to attach tmux pane watcher: %v", err))
	}
	roomJoin, _ := maybeAutoJoinRoom(ctx, terminalBinding)

	// Write success envelope
	data := map[string]any{
		"agent_id":         resp.AgentID,
		"ns":               resp.NS,
		"role":             resp.Role,
		"dispatcher":       "mailbox",
		"execution_layer":  string(executionLayer),
		"via_daemon":       false,
		"terminal_binding": terminalBinding,
		"spawn_pane":       spawnPaneMetadata,
		"watch_pane":       watchPane,
		"room_join":        roomJoin,
	}

	return writeOK(cmd, "agent/spawn", data, "run", nil)
}

func runAgentList(cmd *cobra.Command, args []string) error {
	return runAgentListWithRoute(cmd)
}

func runAgentListWithRoute(cmd *cobra.Command) error {
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
	return runAgentKillWithRoute(cmd, args)
}

func runAgentKillWithRoute(cmd *cobra.Command, args []string) error {
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

	agentRecord, err := agentStore.Get(ctx, agentID)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/kill", string(protocol.ErrorCodeENotFound), fmt.Sprintf("agent not found: %v", err))
	}
	if agent.NormalizeExecutionLayer(agentRecord.ExecutionLayer) == agent.ExecutionLayerJido {
		if killDryRun {
			data := map[string]any{
				"dry_run":         true,
				"would_kill":      true,
				"agent_id":        agentID,
				"namespace":       agentRecord.Namespace,
				"role":            agentRecord.Role,
				"state":           agentRecord.State,
				"execution_layer": string(agent.NormalizeExecutionLayer(agentRecord.ExecutionLayer)),
				"graceful":        killGraceful,
				"timeout_s":       killTimeoutS,
			}
			return writeOK(cmd, "agent/kill", data, "run", nil)
		}
		if err := jidoStopAgentForRecord(ctx, agentRecord); err != nil {
			return writeErrorEnvelope(cmd, "agent/kill", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to stop jido agent: %v", err))
		}
		if err := agentStore.UpdateState(ctx, agentID, agent.StateStopped); err != nil {
			return writeErrorEnvelope(cmd, "agent/kill", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to update agent state: %v", err))
		}
		return writeOK(cmd, "agent/kill", map[string]any{
			"agent_id":        agentID,
			"final_status":    agent.StateStopped,
			"exit_code":       0,
			"execution_layer": string(agent.NormalizeExecutionLayer(agentRecord.ExecutionLayer)),
		}, "run", nil)
	}

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

func runAgentOutput(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	agentRef := args[0]

	// Resolve agent to get namespace
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/output", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err))
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	a, err := agentStore.Resolve(ctx, agentRef)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/output", string(protocol.ErrorCodeENotFound), fmt.Sprintf("agent not found: %v", err))
	}

	// Open session store and find latest session for this agent by namespace
	sessionStore, err := sessions.OpenFromConfig(ctx, cfg)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/output", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open session store: %v", err))
	}
	defer func() { errs.Ignore(sessionStore.Close(), "close session store") }()

	sess, err := sessionStore.FindByAgentNamespace(ctx, a.Namespace)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/output", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to find session: %v", err))
	}
	if sess == nil {
		return writeErrorEnvelope(cmd, "agent/output", string(protocol.ErrorCodeENotFound), "no session found for agent")
	}

	type AgentOutput struct {
		AgentID      string `json:"agent_id"`
		AgentName    string `json:"agent_name"`
		SessionID    string `json:"session_id"`
		Status       string `json:"status"`
		Summary      string `json:"summary"`
		ErrorMessage string `json:"error_message,omitempty"`
	}

	result := AgentOutput{
		AgentID:      a.ID,
		AgentName:    a.Name,
		SessionID:    sess.ID,
		Status:       sess.Status,
		Summary:      sess.Summary,
		ErrorMessage: sess.ErrorMessage,
	}

	return writeOK(cmd, "agent/output", result, "run", nil)
}

func runAgentRename(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	ref := args[0]

	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("slug") {
		return writeErrorEnvelope(cmd, "agent/rename", string(protocol.ErrorCodeEARG), "at least one of --name or --slug is required", "Use --name and/or --slug to set the new identity.")
	}

	// Open agent store
	agentStore, err := agents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/rename", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open agent store: %v", err), "Verify the storage root and permissions.")
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	agentRecord, err := agentStore.Resolve(ctx, ref)
	if err != nil {
		if errors.Is(err, agents.ErrNotFound) {
			return writeErrorEnvelope(cmd, "agent/rename", string(protocol.ErrorCodeENotFound), fmt.Sprintf("agent not found: %v", err), "Check the agent ID, name, or slug.")
		}
		return writeErrorEnvelope(cmd, "agent/rename", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to resolve agent: %v", err), "Verify the agent reference (ID, name, or slug).")
	}

	nextName := agentRecord.Name
	nextSlug := agentRecord.Slug
	if cmd.Flags().Changed("name") {
		nextName = renameName
	}
	if cmd.Flags().Changed("slug") {
		nextSlug = renameSlug
	}

	// Dry-run mode: show what would be renamed
	if renameDryRun {
		data := map[string]any{
			"dry_run":      true,
			"would_rename": true,
			"agent_id":     agentRecord.ID,
			"namespace":    agentRecord.Namespace,
			"from": map[string]any{
				"name": agentRecord.Name,
				"slug": agentRecord.Slug,
			},
			"to": map[string]any{
				"name": nextName,
				"slug": nextSlug,
			},
		}
		return writeOK(cmd, "agent/rename", data, "run", nil)
	}

	if err := agentStore.UpdateIdentity(ctx, agentRecord.ID, nextName, nextSlug); err != nil {
		return writeErrorEnvelope(cmd, "agent/rename", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to rename agent: %v", err), "Ensure the slug is unique and the store is writable.")
	}

	data := map[string]any{
		"agent_id":  agentRecord.ID,
		"namespace": agentRecord.Namespace,
		"name":      nextName,
		"slug":      nextSlug,
	}

	return writeOK(cmd, "agent/rename", data, "run", nil)
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
	return runAgentRunWithRoute(cmd, args)
}

func runAgentRunWithRoute(cmd *cobra.Command, args []string) error {
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
	if agent.NormalizeExecutionLayer(agentRecord.ExecutionLayer) == agent.ExecutionLayerJido {
		return writeErrorEnvelope(
			cmd,
			"agent/run",
			string(protocol.ErrorCodeEARG),
			"agent is Jido-managed and cannot be run via the classic foreground runtime",
			"Use `agent ask --dispatcher jido`, `runtime.signal`, or Jido runtime controls for this agent.",
		)
	}

	// Resolve LLM provider - use agent's provider first, then config, then auto-detect
	provider := agentRecord.LLMProvider
	if provider == "" {
		provider = cfg.LLM.Provider
	}
	if provider == "" {
		provider = "lmstudio"
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

	workspaceRoot, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}

	model := cfg.LLM.ResolveModel(provider)
	if model == "" {
		model = llmproviders.DefaultModelForProvider(provider)
	}

	opts := agentdaemon.Options{
		AgentID:               agentRecord.ID, // Use resolved ID
		StorageRoot:           cfg.Storage.Root,
		WorkspaceRoot:         workspaceRoot,
		PollInterval:          500 * time.Millisecond,
		HeartbeatInterval:     10 * time.Second,
		MaxPollMessages:       10,
		LLMProvider:           provider,
		LLMModel:              model,
		LLMAPIKey:             cfg.LLM.ResolveAPIKey(provider),
		LLMBaseURL:            firstNonEmpty(agentRecord.LLMBaseURL, cfg.LLM.ResolveBaseURL(provider)),
		LLMAuthMode:           firstNonEmpty(agentRecord.LLMAuthMode, cfg.LLM.ResolveAuthMode(provider)),
		LLMAuthHeader:         firstNonEmpty(agentRecord.LLMAuthHeader, cfg.LLM.ResolveAuthHeader(provider)),
		LLMAuthPrefix:         firstNonEmpty(agentRecord.LLMAuthPrefix, cfg.LLM.ResolveAuthPrefix(provider)),
		EnableCompanionMemory: enableCompanionMemory,
		CompanionMode:         companionMode,
	}

	return agentdaemon.Run(ctx, opts)
}

func runAgentAsk(cmd *cobra.Command, args []string) error {
	return runAgentAskWithRoute(cmd, args)
}

func runAgentAskStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	askID := strings.TrimSpace(args[0])
	if askID == "" {
		return writeErrorEnvelope(cmd, "agent/ask_status", string(protocol.ErrorCodeEARG), "ask_id is required")
	}
	runID := "ask:" + askID

	projectionStore, closeProjections, err := libsqlprojections.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/ask_status", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open v2 projection store: %v", err))
	}
	defer func() {
		if closeProjections != nil {
			_ = closeProjections()
		}
	}()

	runState, err := projectionStore.GetRunState(ctx, runID)
	if err != nil {
		if errors.Is(err, libsqlprojections.ErrNotFound) {
			return writeErrorEnvelope(cmd, "agent/ask_status", string(protocol.ErrorCodeENotFound), fmt.Sprintf("ask status not found for ask_id=%s", askID))
		}
		return writeErrorEnvelope(cmd, "agent/ask_status", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to load ask run state: %v", err))
	}

	callback := jidoTerminalCallback{}
	eventStore, eventErr := libsqlevents.Open(ctx, cfg.Storage.Root)
	if eventErr == nil {
		defer func() { _ = eventStore.Close() }()
		if resolved, resolveErr := resolveJidoTerminalCallback(ctx, eventStore, askID); resolveErr == nil {
			callback = resolved
		}
	}

	data := buildAskRunResponseData(askID, runState, callback)
	return writeOK(cmd, "agent/ask_status", data, "ask_status", nil)
}

func runAgentAskWithRoute(cmd *cobra.Command, args []string) error {
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
	dispatcherMode := resolvedAskDispatcherMode(askDispatcherMode)

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
	var mailboxStore mailbox.Store
	if dispatcherMode == askDispatchModeMailbox {
		mailboxStore, err = mailbox.Open(ctx, cfg.Storage.Root)
		if err != nil {
			return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeERuntime), fmt.Sprintf("failed to open mailbox store: %v", err))
		}
		defer func() { errs.Ignore(mailboxStore.Close(), "close mailbox store") }()
	}

	askID := ulid.Make().String()
	callerNS := "cli:" + ulid.Make().String()

	// Dry-run mode: show what would be sent
	if askDryRun {
		data := map[string]any{
			"dry_run":    true,
			"would_send": true,
			"agent_id":   agentID,
			"namespace":  agentRecord.Namespace,
			"ask_id":     askID,
			"caller_ns":  callerNS,
			"kind":       kind,
			"question":   question,
			"timeout_ms": timeout.Milliseconds(),
			"dispatcher": dispatcherMode,
		}
		return writeOK(cmd, "agent/ask", data, "ask", nil)
	}

	nowFn := func() time.Time { return time.Now().UTC() }
	newID := func() string { return ulid.Make().String() }

	var (
		dispatcher  v2services.AskDispatcher
		eventStore  events.Appender
		projections v2services.AskProjectionApplier
		cleanupFn   func()
	)

	switch dispatcherMode {
	case askDispatchModeMailbox:
		dispatcher = newMailboxAskDispatcher(mailboxStore, nowFn, newID)
	case askDispatchModeJido:
		workspaceRoot := resolveContextWorkspace("")
		var runtimeErr error
		dispatcher, eventStore, projections, cleanupFn, runtimeErr = newJidoAskRuntime(ctx, cfg.Storage.Root, workspaceRoot, nowFn, newID)
		if runtimeErr != nil {
			return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeERuntime), runtimeErr.Error())
		}
		if cleanupFn != nil {
			defer cleanupFn()
		}
	default:
		return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeEARG), fmt.Sprintf("unsupported dispatcher mode %q", dispatcherMode))
	}

	svc := v2services.NewAskService(v2services.AskDependencies{
		Dispatcher:  dispatcher,
		Events:      eventStore,
		Projections: projections,
		DefaultTTL:  timeout,
		Now:         nowFn,
		NewID:       newID,
	})

	resp, err := svc.Ask(ctx, v2ask.Request{
		AskID:          askID,
		AgentID:        agentRecord.ID,
		Namespace:      agentRecord.Namespace,
		Kind:           kind,
		Question:       question,
		ConversationID: conversationID,
		CallerNS:       callerNS,
		Timeout:        timeout,
	})
	if err != nil {
		return writeErrorEnvelope(cmd, "agent/ask", string(askServiceErrorCode(err)), err.Error())
	}

	// Output ask confirmation
	if err := writeOK(cmd, "agent/ask", map[string]any{
		"ask_id":     resp.AskID,
		"message_id": resp.MessageID,
		"sent_to":    resp.Namespace,
	}, "ask", nil); err != nil {
		return err
	}

	if wait {
		switch dispatcherMode {
		case askDispatchModeMailbox:
			if mailboxStore == nil {
				return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeERuntime), "mailbox store is not configured for wait mode")
			}
			return waitForReply(ctx, mailboxStore, cmd.OutOrStdout(), callerNS, resp.AskID, timeout)
		case askDispatchModeJido:
			runStateReader, ok := projections.(jidoRunStateReader)
			if !ok || runStateReader == nil {
				return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeERuntime), "jido run-state reader is not configured for wait mode")
			}
			runState, waitErr := waitForJidoRunState(ctx, runStateReader, resp.AskID, timeout)
			if waitErr != nil {
				return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeERuntime), waitErr.Error())
			}
			callback := jidoTerminalCallback{}
			if runEventReader, ok := eventStore.(jidoRunEventReader); ok && runEventReader != nil {
				if resolved, resolveErr := resolveJidoTerminalCallback(ctx, runEventReader, resp.AskID); resolveErr == nil {
					callback = resolved
				}
			}
			if strings.EqualFold(strings.TrimSpace(runState.Status), "completed") {
				data := buildAskRunResponseData(resp.AskID, runState, callback)
				return writeOK(cmd, "agent/ask_wait", data, "ask_wait", nil)
			}
			errMessage := fmt.Sprintf("ask run ended with status %q", strings.TrimSpace(runState.Status))
			if callback.Error != "" {
				errMessage = fmt.Sprintf("%s: %s", errMessage, callback.Error)
			}
			hint := askWaitFailureHint(callback)
			if hint == "" {
				return writeErrorEnvelope(
					cmd,
					"agent/ask_wait",
					string(protocol.ErrorCodeERuntime),
					errMessage,
				)
			}
			return writeErrorEnvelope(
				cmd,
				"agent/ask_wait",
				string(protocol.ErrorCodeERuntime),
				errMessage,
				hint,
			)
		default:
			return writeErrorEnvelope(cmd, "agent/ask", string(protocol.ErrorCodeEARG), fmt.Sprintf("unsupported dispatcher mode %q", dispatcherMode))
		}
	}
	return nil
}

func askServiceErrorCode(err error) protocol.ErrorCode {
	var verr *v2errors.V2Error
	if errors.As(err, &verr) {
		return protocol.ErrorCode(verr.EnvelopeCode())
	}
	return protocol.ErrorCodeERuntime
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
				observability.Emit(ctx, observability.NewEvent("agent.mailbox_poll_error").
					WithComponent(observability.ComponentCLI).
					WithData("attempt", consecutiveErrors).
					WithData("max_attempts", maxConsecutiveErrors).
					WithData("namespace", callerNS).
					Error(err, 0))
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

type jidoRunStateReader interface {
	GetRunState(ctx context.Context, runID string) (libsqlprojections.RunState, error)
}

type jidoRunEventReader interface {
	ListStream(ctx context.Context, filter events.StreamFilter) ([]events.Event, error)
}

type jidoTerminalCallback struct {
	EventID  string
	Status   string
	Summary  string
	Error    string
	Metadata map[string]any
}

func waitForJidoRunState(
	ctx context.Context,
	reader jidoRunStateReader,
	askID string,
	timeout time.Duration,
) (libsqlprojections.RunState, error) {
	return waitForJidoRunStateWithPoll(ctx, reader, askID, timeout, 250*time.Millisecond)
}

func waitForJidoRunStateWithPoll(
	ctx context.Context,
	reader jidoRunStateReader,
	askID string,
	timeout time.Duration,
	pollInterval time.Duration,
) (libsqlprojections.RunState, error) {
	if reader == nil {
		return libsqlprojections.RunState{}, fmt.Errorf("jido run-state reader is required")
	}
	askID = strings.TrimSpace(askID)
	if askID == "" {
		return libsqlprojections.RunState{}, fmt.Errorf("ask_id is required")
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if pollInterval <= 0 {
		pollInterval = 250 * time.Millisecond
	}

	runID := "ask:" + askID
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return libsqlprojections.RunState{}, ctx.Err()
		case <-time.After(pollInterval):
			state, err := reader.GetRunState(ctx, runID)
			if err != nil {
				if errors.Is(err, libsqlprojections.ErrNotFound) {
					continue
				}
				return libsqlprojections.RunState{}, fmt.Errorf("query run state for %s: %w", runID, err)
			}
			switch strings.ToLower(strings.TrimSpace(state.Status)) {
			case "completed", "failed", "canceled", "cancelled", "timed_out", "timeout":
				return state, nil
			}
		}
	}

	return libsqlprojections.RunState{}, fmt.Errorf("timeout waiting for jido ask run to complete: ask_id=%s", askID)
}

func resolveJidoTerminalCallback(
	ctx context.Context,
	reader jidoRunEventReader,
	askID string,
) (jidoTerminalCallback, error) {
	if reader == nil {
		return jidoTerminalCallback{}, fmt.Errorf("jido run-event reader is required")
	}
	askID = strings.TrimSpace(askID)
	if askID == "" {
		return jidoTerminalCallback{}, fmt.Errorf("ask_id is required")
	}

	streamID := "ask:" + askID
	list, err := reader.ListStream(ctx, events.StreamFilter{
		StreamID:   streamID,
		StreamType: events.StreamTypeRun,
		Limit:      256,
	})
	if err != nil {
		return jidoTerminalCallback{}, fmt.Errorf("list run events for %s: %w", streamID, err)
	}

	for i := len(list) - 1; i >= 0; i-- {
		evt := list[i]
		switch evt.EventType {
		case events.EventRunCompleted, events.EventRunFailed:
			parsed := parseJidoTerminalCallbackPayload(evt.Payload)
			parsed.EventID = strings.TrimSpace(evt.ID)
			if parsed.Status == "" {
				if evt.EventType == events.EventRunFailed {
					parsed.Status = "failed"
				} else {
					parsed.Status = "completed"
				}
			}
			return parsed, nil
		}
	}

	return jidoTerminalCallback{}, events.ErrNotFound
}

func parseJidoTerminalCallbackPayload(payload json.RawMessage) jidoTerminalCallback {
	if len(payload) == 0 || !json.Valid(payload) {
		return jidoTerminalCallback{}
	}
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return jidoTerminalCallback{}
	}
	out := jidoTerminalCallback{
		Status:  strings.TrimSpace(anyToString(raw["status"])),
		Summary: strings.TrimSpace(anyToString(raw["summary"])),
		Error:   strings.TrimSpace(anyToString(raw["error"])),
	}
	if md, ok := raw["metadata"].(map[string]any); ok && len(md) > 0 {
		out.Metadata = md
	}
	return out
}

func anyToString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		return fmt.Sprintf("%v", value)
	}
}

func askWaitFailureHint(callback jidoTerminalCallback) string {
	parts := make([]string, 0, 4)
	if callback.EventID != "" {
		parts = append(parts, "callback_event_id="+callback.EventID)
	}
	if callback.Status != "" {
		parts = append(parts, "callback_status="+callback.Status)
	}
	if callback.Summary != "" {
		parts = append(parts, "callback_summary="+callback.Summary)
	}
	if len(callback.Metadata) > 0 {
		if raw, err := json.Marshal(callback.Metadata); err == nil {
			parts = append(parts, "callback_metadata="+string(raw))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "; "))
}

func buildAskRunResponseData(
	askID string,
	runState libsqlprojections.RunState,
	callback jidoTerminalCallback,
) map[string]any {
	data := map[string]any{
		"ask_id":        strings.TrimSpace(askID),
		"run_id":        strings.TrimSpace(runState.RunID),
		"status":        strings.TrimSpace(runState.Status),
		"last_event_id": strings.TrimSpace(runState.LastEventID),
		"request_id":    strings.TrimSpace(runState.RequestID),
		"actor_id":      strings.TrimSpace(runState.ActorID),
	}
	if callback.EventID != "" {
		data["callback_event_id"] = callback.EventID
	}
	if callback.Status != "" {
		data["callback_status"] = callback.Status
	}
	if callback.Summary != "" {
		data["callback_summary"] = callback.Summary
	}
	if callback.Error != "" {
		data["callback_error"] = callback.Error
	}
	if len(callback.Metadata) > 0 {
		data["callback_metadata"] = callback.Metadata
	}
	return data
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
			"session_id":   result.SessionID,
			"actor_id":     result.ActorID,
			"status":       result.Status,
			"from_session": sessionID,
			"via_daemon":   true,
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
