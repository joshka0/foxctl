package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jkatigb/agentctl/internal/actor"
	agenttypes "github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var actorSysCmd = &cobra.Command{
	Use:   "actorsys",
	Short: "Manage reactive actor system",
	Long: `Manage the reactive actor system with supervisor, actors, and event bus.

The actor system provides:
- Supervisor: Manages actor lifecycles and message routing
- Watcher: Reactive notifications from SQLite triggers
- EventBus: Cross-actor event distribution
- AgentActor: dspy-go ReActAgent as reactive actors`,
}

var actorSysSupervisorCmd = &cobra.Command{
	Use:   "supervisor",
	Short: "Manage the actor supervisor",
}

var actorSysSupervisorStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the supervisor (foreground)",
	Long:  "Start the actor system supervisor in the foreground. Use Ctrl+C to stop.",
	RunE:  runActorSysSupervisorStart,
}

var actorSysSupervisorStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show supervisor status",
	Long:  "Show the current status of the actor supervisor",
	RunE:  runActorSysSupervisorStatus,
}

var actorSysSpawnCmd = &cobra.Command{
	Use:   "spawn",
	Short: "Spawn a new actor",
	Long:  "Spawn a new AgentActor with specified role and namespace",
	RunE:  runActorSysSpawn,
}

var actorSysListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered actors",
	Long:  "List all actors registered in the actor system",
	RunE:  runActorSysList,
}

var actorSysStatusCmd = &cobra.Command{
	Use:   "status <namespace>",
	Short: "Show actor status",
	Long:  "Show detailed status of a specific actor",
	Args:  cobra.ExactArgs(1),
	RunE:  runActorSysStatus,
}

var actorSysSendCmd = &cobra.Command{
	Use:   "send <namespace>",
	Short: "Send message to actor",
	Long:  "Send a message to an actor's mailbox",
	Args:  cobra.ExactArgs(1),
	RunE:  runActorSysSend,
}

var actorSysLogsCmd = &cobra.Command{
	Use:   "logs <namespace>",
	Short: "Stream actor events",
	Long:  "Stream events for a specific actor from the event bus",
	Args:  cobra.ExactArgs(1),
	RunE:  runActorSysLogs,
}

var actorSysUnregisterCmd = &cobra.Command{
	Use:   "unregister <namespace>",
	Short: "Unregister an actor",
	Long:  "Remove an actor from the registry",
	Args:  cobra.ExactArgs(1),
	RunE:  runActorSysUnregister,
}

// Flags for supervisor start
var (
	supervisorPollInterval      int
	supervisorEnableTrajectory  bool
	supervisorRespawnRegistered bool
)

// Flags for spawn
var (
	spawnActorNamespace   string
	spawnActorRole        string
	spawnActorLLMProvider string
	spawnActorLLMModel    string
	spawnActorWorkspace   string
)

// Flags for send
var (
	sendActorType    string
	sendActorPayload string
)

// Flags for logs
var (
	logsActorFollow bool
	logsActorLimit  int
)

// Flags for list
var (
	listActorStatus string
)

// Common flags
var (
	actorSysDryRun bool
)

func init() {
	rootCmd.AddCommand(actorSysCmd)

	// Supervisor commands
	actorSysCmd.AddCommand(actorSysSupervisorCmd)
	actorSysSupervisorCmd.AddCommand(actorSysSupervisorStartCmd)
	actorSysSupervisorCmd.AddCommand(actorSysSupervisorStatusCmd)

	// Actor commands
	actorSysCmd.AddCommand(actorSysSpawnCmd)
	actorSysCmd.AddCommand(actorSysListCmd)
	actorSysCmd.AddCommand(actorSysStatusCmd)
	actorSysCmd.AddCommand(actorSysSendCmd)
	actorSysCmd.AddCommand(actorSysLogsCmd)
	actorSysCmd.AddCommand(actorSysUnregisterCmd)

	// Supervisor start flags
	actorSysSupervisorStartCmd.Flags().IntVar(&supervisorPollInterval, "poll-interval", 100, "Poll interval in milliseconds")
	actorSysSupervisorStartCmd.Flags().BoolVar(&supervisorEnableTrajectory, "enable-trajectory", true, "Enable trajectory persistence")
	actorSysSupervisorStartCmd.Flags().BoolVar(&supervisorRespawnRegistered, "respawn-registered", true, "Respawn registered actors on startup")

	// Spawn flags
	actorSysSpawnCmd.Flags().StringVar(&spawnActorNamespace, "namespace", "", "Actor namespace (required)")
	actorSysSpawnCmd.Flags().StringVar(&spawnActorRole, "role", "coder", "Actor role (coder|planner|reviewer|fixer|verifier)")
	actorSysSpawnCmd.Flags().StringVar(&spawnActorLLMProvider, "llm-provider", "gemini", "LLM provider")
	actorSysSpawnCmd.Flags().StringVar(&spawnActorLLMModel, "llm-model", "", "LLM model (defaults based on provider)")
	actorSysSpawnCmd.Flags().StringVar(&spawnActorWorkspace, "workspace", "", "Workspace root path")
	actorSysSpawnCmd.Flags().BoolVar(&actorSysDryRun, "dry-run", false, "Preview without registering actor")
	_ = actorSysSpawnCmd.MarkFlagRequired("namespace")

	// Send flags
	actorSysSendCmd.Flags().StringVar(&sendActorType, "type", "agent.cmd", "Message type (agent.ask|agent.cmd|agent.event)")
	actorSysSendCmd.Flags().StringVar(&sendActorPayload, "payload", "{}", "JSON payload")
	actorSysSendCmd.Flags().BoolVar(&actorSysDryRun, "dry-run", false, "Preview without sending message")

	// Logs flags
	actorSysLogsCmd.Flags().BoolVar(&logsActorFollow, "follow", false, "Follow log output")
	actorSysLogsCmd.Flags().IntVar(&logsActorLimit, "limit", 50, "Maximum events to show")

	// List flags
	actorSysListCmd.Flags().StringVar(&listActorStatus, "status", "", "Filter by status (registered|running|stopped|error)")

	// Unregister flags
	actorSysUnregisterCmd.Flags().BoolVar(&actorSysDryRun, "dry-run", false, "Preview without unregistering actor")
}

func runActorSysSupervisorStart(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	// Setup signal handling
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Open mailbox store
	mbStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/supervisor/start", "failed to open mailbox store: "+err.Error())
	}
	defer func() { errs.Ignore(mbStore.Close(), "close mailbox store") }()

	// Configure system options
	var sysOpts []actor.SystemOption

	// Add watcher options
	pollInterval := time.Duration(supervisorPollInterval) * time.Millisecond
	sysOpts = append(sysOpts, actor.WithWatcherOptions(actor.WithPollInterval(pollInterval)))

	// Add trajectory store if enabled
	if supervisorEnableTrajectory {
		trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
		if err != nil {
			return writeActorSysError(cmd, "actorsys/supervisor/start", "failed to open trajectory store: "+err.Error())
		}
		defer func() { errs.Ignore(trajStore.Close(), "close trajectory store") }()
		sysOpts = append(sysOpts, actor.WithTrajectoryStore(trajStore))
	}

	// Create actor system
	system, err := actor.NewSystem(mbStore, sysOpts...)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/supervisor/start", "failed to create actor system: "+err.Error())
	}

	// Create hooks dispatcher for actors
	var hookDispatcher hooks.Dispatcher
	hooksCfg, err := hooks.LoadConfigWithDefaults(cfg.Storage.Root)
	if err != nil {
		// Hooks are optional - emit warning but continue
		observability.Emit(ctx, observability.NewEvent("actorsys.hooks_config_warning").
			WithComponent(observability.ComponentCLI).
			WithData("reason", "continuing without hooks").
			Error(err, 0))
	} else if hooksCfg != nil {
		hookDispatcher = hooks.NewDispatcherWithRegistry(hooksCfg, cfg.Paths.Skills)
	}

	// Respawn registered actors if enabled
	if supervisorRespawnRegistered {
		if err := respawnRegisteredActors(ctx, cfg, system, hookDispatcher); err != nil {
			// Emit warning but don't fail startup
			observability.Emit(ctx, observability.NewEvent("actorsys.respawn_warning").
				WithComponent(observability.ComponentCLI).
				Error(err, 0))
		}
	}

	// Start the system
	if err := system.Start(ctx); err != nil {
		return writeActorSysError(cmd, "actorsys/supervisor/start", "failed to start actor system: "+err.Error())
	}

	// Write startup envelope
	env := envelope.OK("actorsys/supervisor/start", map[string]any{
		"status":       "running",
		"poll_ms":      supervisorPollInterval,
		"trajectory":   supervisorEnableTrajectory,
		"message":      "Supervisor started. Press Ctrl+C to stop.",
		"actor_count":  0, // Will be updated
		"storage_root": cfg.Storage.Root,
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "actorsys"
		m.Profiles = []string{"core/v1", "actor/v1"}
	}))
	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	// Wait for shutdown
	<-ctx.Done()

	// Stop the system
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopCancel()

	if err := system.Stop(stopCtx); err != nil {
		return writeActorSysError(cmd, "actorsys/supervisor/start", "failed to stop actor system: "+err.Error())
	}

	// Write shutdown envelope
	shutdownEnv := envelope.OK("actorsys/supervisor/start", map[string]any{
		"status":  "stopped",
		"message": "Supervisor stopped gracefully",
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "actorsys"
		m.Profiles = []string{"core/v1", "actor/v1"}
	}))
	if err := envelope.Write(os.Stdout, shutdownEnv); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runActorSysSupervisorStatus(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	// Open registry store to check registered actors
	db, err := openActorRegistryDB(cfg.Storage.Root)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/supervisor/status", "failed to open registry: "+err.Error())
	}
	defer func() { errs.Ignore(db.Close(), "close registry db") }()

	regStore, err := actor.NewRegistryStore(ctx, db)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/supervisor/status", "failed to create registry store: "+err.Error())
	}

	actors, err := regStore.ListActors(ctx)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/supervisor/status", "failed to list actors: "+err.Error())
	}

	// Count by status
	statusCounts := make(map[string]int)
	for _, a := range actors {
		statusCounts[string(a.Status)]++
	}

	env := envelope.OK("actorsys/supervisor/status", map[string]any{
		"registered_actors": len(actors),
		"status_counts":     statusCounts,
		"storage_root":      cfg.Storage.Root,
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "actorsys"
		m.Profiles = []string{"core/v1", "actor/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runActorSysSpawn(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	// Validate role
	switch spawnActorRole {
	case "coder", "planner", "reviewer", "fixer", "verifier":
		// Valid
	default:
		return writeActorSysError(cmd, "actorsys/spawn", fmt.Sprintf("invalid role: %s (must be coder|planner|reviewer|fixer|verifier)", spawnActorRole))
	}

	// Open registry store
	db, err := openActorRegistryDB(cfg.Storage.Root)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/spawn", "failed to open registry: "+err.Error())
	}
	defer func() { errs.Ignore(db.Close(), "close registry db") }()

	regStore, err := actor.NewRegistryStore(ctx, db)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/spawn", "failed to create registry store: "+err.Error())
	}

	// Build actor config
	actorCfg := actor.Config{
		ID:        spawnActorNamespace,
		Namespace: spawnActorNamespace,
		Role:      spawnActorRole,
	}

	// Serialize config
	configJSON, err := actor.MarshalConfig(actorCfg)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/spawn", "failed to marshal config: "+err.Error())
	}

	// Register actor
	record := actor.ActorRecord{
		Namespace:  spawnActorNamespace,
		Role:       spawnActorRole,
		ConfigJSON: configJSON,
		Status:     actor.ActorStatusRegistered,
	}

	// Dry-run: show what would be done
	if actorSysDryRun {
		env := envelope.OK("actorsys/spawn", map[string]any{
			"dry_run":      true,
			"namespace":    spawnActorNamespace,
			"role":         spawnActorRole,
			"llm_provider": spawnActorLLMProvider,
			"llm_model":    spawnActorLLMModel,
			"status":       "would_register",
			"message":      "Dry run: actor would be registered",
		}, envelope.WithMetaMutator(func(m *envelope.Meta) {
			m.Source = "actorsys"
			m.Profiles = []string{"core/v1", "actor/v1"}
		}))
		if err := envelope.Write(os.Stdout, env); err != nil {
			return fmt.Errorf("write envelope: %w", err)
		}
		return nil
	}

	if err := regStore.RegisterActor(ctx, record); err != nil {
		return writeActorSysError(cmd, "actorsys/spawn", "failed to register actor: "+err.Error())
	}

	env := envelope.OK("actorsys/spawn", map[string]any{
		"namespace":    spawnActorNamespace,
		"role":         spawnActorRole,
		"llm_provider": spawnActorLLMProvider,
		"llm_model":    spawnActorLLMModel,
		"status":       "registered",
		"message":      "Actor registered. Start supervisor to activate.",
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "actorsys"
		m.Profiles = []string{"core/v1", "actor/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runActorSysList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	// Open registry store
	db, err := openActorRegistryDB(cfg.Storage.Root)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/list", "failed to open registry: "+err.Error())
	}
	defer func() { errs.Ignore(db.Close(), "close registry db") }()

	regStore, err := actor.NewRegistryStore(ctx, db)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/list", "failed to create registry store: "+err.Error())
	}

	var actors []actor.ActorRecord
	if listActorStatus != "" {
		actors, err = regStore.ListActorsByStatus(ctx, actor.ActorStatus(listActorStatus))
	} else {
		actors, err = regStore.ListActors(ctx)
	}
	if err != nil {
		return writeActorSysError(cmd, "actorsys/list", "failed to list actors: "+err.Error())
	}

	// Build response
	actorList := make([]map[string]any, 0, len(actors))
	for _, a := range actors {
		actorList = append(actorList, map[string]any{
			"namespace":  a.Namespace,
			"role":       a.Role,
			"status":     a.Status,
			"created_at": a.CreatedAt.Format(time.RFC3339),
			"updated_at": a.UpdatedAt.Format(time.RFC3339),
		})
	}

	env := envelope.OK("actorsys/list", map[string]any{
		"actors": actorList,
		"count":  len(actorList),
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "actorsys"
		m.Profiles = []string{"core/v1", "actor/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runActorSysStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	namespace := args[0]

	// Open registry store
	db, err := openActorRegistryDB(cfg.Storage.Root)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/status", "failed to open registry: "+err.Error())
	}
	defer func() { errs.Ignore(db.Close(), "close registry db") }()

	regStore, err := actor.NewRegistryStore(ctx, db)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/status", "failed to create registry store: "+err.Error())
	}

	record, err := regStore.GetActor(ctx, namespace)
	if err != nil {
		if err == actor.ErrActorNotFound {
			return writeActorSysError(cmd, "actorsys/status", fmt.Sprintf("actor not found: %s", namespace))
		}
		return writeActorSysError(cmd, "actorsys/status", "failed to get actor: "+err.Error())
	}

	env := envelope.OK("actorsys/status", map[string]any{
		"namespace":   record.Namespace,
		"role":        record.Role,
		"status":      record.Status,
		"config":      record.Config,
		"config_json": record.ConfigJSON,
		"created_at":  record.CreatedAt.Format(time.RFC3339),
		"updated_at":  record.UpdatedAt.Format(time.RFC3339),
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "actorsys"
		m.Profiles = []string{"core/v1", "actor/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runActorSysSend(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	namespace := args[0]

	// Validate payload JSON
	var payload map[string]any
	if err := json.Unmarshal([]byte(sendActorPayload), &payload); err != nil {
		return writeActorSysError(cmd, "actorsys/send", "invalid JSON payload: "+err.Error())
	}

	// Validate message type
	msgType := agent.MessageType(sendActorType)
	switch msgType {
	case agent.MessageTypeAsk, agent.MessageTypeReply, agent.MessageTypeCmd, agent.MessageTypeEvent:
		// Valid
	default:
		return writeActorSysError(cmd, "actorsys/send", fmt.Sprintf("invalid message type: %s", sendActorType))
	}

	// Open mailbox store
	mbStore, err := mailbox.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/send", "failed to open mailbox store: "+err.Error())
	}
	defer func() { errs.Ignore(mbStore.Close(), "close mailbox store") }()

	// Create agent.Message for mailbox
	now := time.Now().Unix()
	msg := agent.Message{
		ID:        generateMessageID(),
		FromNS:    "cli:actorsys",
		ToNS:      namespace,
		Type:      msgType,
		TTLMS:     300000, // 5 minutes default
		Headers:   map[string]string{},
		Payload:   json.RawMessage(sendActorPayload),
		VisibleAt: now,
		Timestamp: now,
	}

	// Dry-run: show what would be sent
	if actorSysDryRun {
		env := envelope.OK("actorsys/send", map[string]any{
			"dry_run":    true,
			"message_id": msg.ID,
			"namespace":  namespace,
			"type":       sendActorType,
			"payload":    payload,
			"sent":       false,
			"message":    "Dry run: message would be sent",
		}, envelope.WithMetaMutator(func(m *envelope.Meta) {
			m.Source = "actorsys"
			m.Profiles = []string{"core/v1", "actor/v1"}
		}))
		if err := envelope.Write(os.Stdout, env); err != nil {
			return fmt.Errorf("write envelope: %w", err)
		}
		return nil
	}

	// Send via mailbox - the watcher will pick it up and route to the actor
	if err := mbStore.Send(ctx, msg); err != nil {
		return writeActorSysError(cmd, "actorsys/send", "failed to send message: "+err.Error())
	}

	env := envelope.OK("actorsys/send", map[string]any{
		"message_id": msg.ID,
		"namespace":  namespace,
		"type":       sendActorType,
		"sent":       true,
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "actorsys"
		m.Profiles = []string{"core/v1", "actor/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runActorSysLogs(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	namespace := args[0]

	// Open trajectory store to get events
	trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/logs", "failed to open trajectory store: "+err.Error())
	}
	defer func() { errs.Ignore(trajStore.Close(), "close trajectory store") }()

	// List recent events and filter by actor
	// EventFilter doesn't support Actor field, so we fetch more and filter client-side
	filter := trajectory.EventFilter{
		Limit: logsActorLimit * 10, // Fetch more to allow filtering
	}

	events, err := trajStore.ListEvents(ctx, filter)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/logs", "failed to list events: "+err.Error())
	}

	// Build response, filtering by actor namespace
	eventList := make([]map[string]any, 0, logsActorLimit)
	for _, e := range events {
		if e.Actor != namespace {
			continue
		}
		if len(eventList) >= logsActorLimit {
			break
		}
		eventList = append(eventList, map[string]any{
			"id":    e.ID,
			"kind":  e.Kind,
			"actor": e.Actor,
			"ts":    e.TS.Format(time.RFC3339),
			"data":  e.DataInline,
		})
	}

	env := envelope.OK("actorsys/logs", map[string]any{
		"namespace": namespace,
		"events":    eventList,
		"count":     len(eventList),
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "actorsys"
		m.Profiles = []string{"core/v1", "actor/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

func runActorSysUnregister(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)
	namespace := args[0]

	// Open registry store
	db, err := openActorRegistryDB(cfg.Storage.Root)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/unregister", "failed to open registry: "+err.Error())
	}
	defer func() { errs.Ignore(db.Close(), "close registry db") }()

	regStore, err := actor.NewRegistryStore(ctx, db)
	if err != nil {
		return writeActorSysError(cmd, "actorsys/unregister", "failed to create registry store: "+err.Error())
	}

	// Check if actor exists first (for both dry-run and actual unregister)
	record, err := regStore.GetActor(ctx, namespace)
	if err != nil {
		if err == actor.ErrActorNotFound {
			return writeActorSysError(cmd, "actorsys/unregister", fmt.Sprintf("actor not found: %s", namespace))
		}
		return writeActorSysError(cmd, "actorsys/unregister", "failed to lookup actor: "+err.Error())
	}

	// Dry-run: show what would be done
	if actorSysDryRun {
		env := envelope.OK("actorsys/unregister", map[string]any{
			"dry_run":      true,
			"namespace":    namespace,
			"role":         record.Role,
			"status":       string(record.Status),
			"unregistered": false,
			"message":      "Dry run: actor would be unregistered",
		}, envelope.WithMetaMutator(func(m *envelope.Meta) {
			m.Source = "actorsys"
			m.Profiles = []string{"core/v1", "actor/v1"}
		}))
		if err := envelope.Write(os.Stdout, env); err != nil {
			return fmt.Errorf("write envelope: %w", err)
		}
		return nil
	}

	if err := regStore.UnregisterActor(ctx, namespace); err != nil {
		return writeActorSysError(cmd, "actorsys/unregister", "failed to unregister actor: "+err.Error())
	}

	env := envelope.OK("actorsys/unregister", map[string]any{
		"namespace":    namespace,
		"unregistered": true,
	}, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "actorsys"
		m.Profiles = []string{"core/v1", "actor/v1"}
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

// Helper functions

func openActorRegistryDB(storageRoot string) (*sql.DB, error) {
	// Use the agents.db for actor registry (shared with agent store)
	dbPath := filepath.Join(storageRoot, "agents.db")
	return sql.Open("sqlite3", dbPath)
}

func respawnRegisteredActors(ctx context.Context, cfg config.Config, system *actor.System, hookDispatcher hooks.Dispatcher) error {
	db, err := openActorRegistryDB(cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open registry db: %w", err)
	}
	defer func() { errs.Ignore(db.Close(), "close registry db") }()

	regStore, err := actor.NewRegistryStore(ctx, db)
	if err != nil {
		return fmt.Errorf("create registry store: %w", err)
	}

	// Get actors that were running
	actors, err := regStore.ListActorsByStatus(ctx, actor.ActorStatusRunning)
	if err != nil {
		return fmt.Errorf("list running actors: %w", err)
	}

	// Also include registered actors
	registered, err := regStore.ListActorsByStatus(ctx, actor.ActorStatusRegistered)
	if err != nil {
		return fmt.Errorf("list registered actors: %w", err)
	}
	actors = append(actors, registered...)

	for _, rec := range actors {
		// Create AgentActor from record
		// Note: ActorID flows through AgentConfig.ActorID → hook.Input.ActorID
		// Session ID is generated per-actor in onStart; environment session bridging
		// happens at hook level, not actor level (hooks can read CLAUDE_SESSION_ID etc.)
		agentCfg := actor.AgentActorConfig{
			ActorConfig: rec.Config,
			AgentConfig: agenttypes.AgentConfig{
				Role:    agenttypes.AgentRole(rec.Role),
				ActorID: rec.Namespace,
			},
			LLMProvider:   "gemini", // Default, should be stored in config
			WorkspaceRoot: cfg.Storage.Root,
			Hooks:         hookDispatcher, // Wire hooks dispatcher
		}

		// Use no-op logger for actor internals - we emit via observability instead
		noopLogger := zerolog.New(io.Discard) //nolint:forbidigo // no-op logger for actor internals

		agentActor, err := actor.NewAgentActor(agentCfg, actor.WithAgentLogger(noopLogger))
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("actorsys.actor_create_error").
				WithComponent(observability.ComponentCLI).
				WithData("actor", rec.Namespace).
				Error(err, 0))
			continue
		}

		if err := system.Register(ctx, agentActor); err != nil {
			observability.Emit(ctx, observability.NewEvent("actorsys.actor_register_error").
				WithComponent(observability.ComponentCLI).
				WithData("actor", rec.Namespace).
				Error(err, 0))
			continue
		}

		// Update status to running
		if err := regStore.UpdateStatus(ctx, rec.Namespace, actor.ActorStatusRunning); err != nil {
			observability.Emit(ctx, observability.NewEvent("actorsys.status_update_error").
				WithComponent(observability.ComponentCLI).
				WithData("actor", rec.Namespace).
				Error(err, 0))
		}
	}

	return nil
}

func generateMessageID() string {
	return "msg-" + ulid.Make().String()
}

func writeActorSysError(_ *cobra.Command, command, message string) error {
	env := envelope.Error(command, string(protocol.ErrorCodeERuntime), message, nil)
	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s", message)
}
