package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/joshka0/foxctl/internal/context/contextplane/taskhistory"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/logging"
	v2jido "github.com/joshka0/foxctl/internal/v2/adapters/jido"
	tursoorchestration "github.com/joshka0/foxctl/internal/v2/adapters/turso/orchestration"
	tursoworkers "github.com/joshka0/foxctl/internal/v2/adapters/turso/workers"
	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	coreorchestration "github.com/joshka0/foxctl/internal/v2/core/orchestration"
	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
	v2services "github.com/joshka0/foxctl/internal/v2/services"
)

const (
	commandOrchestrationDispatchIssueCLI       = "orchestration/dispatch-issue"
	commandOrchestrationCardActionCLI          = "orchestration/card-action"
	commandOrchestrationBoardCardRuntimeGetCLI = "orchestration/board-card-runtime-get"

	orchestrationActionRetryNowCLI = "retry-now"
	orchestrationActionReleaseCLI  = "release"
	orchestrationActionMarkDoneCLI = "mark-done"

	defaultRuntimeTreeDepthCLI = 2
	maxRuntimeTreeDepthCLI     = 5

	cliRuntimeBackendJido      = "jido"
	cliRuntimeBackendGoruntime = "goruntime"
	envCLIRuntimeBackend       = "FOXCTL_V2_ORCHESTRATION_RUNTIME_BACKEND"
)

type orchestrationCardActionResponseCLI struct {
	RequestID  string                 `json:"request_id"`
	Action     string                 `json:"action"`
	Card       coreorchestration.Card `json:"card"`
	Idempotent bool                   `json:"idempotent,omitempty"`
	Timestamp  time.Time              `json:"ts"`
}

type orchestrationRuntimeTreeDataCLI struct {
	Enabled bool                             `json:"enabled"`
	AgentID string                           `json:"agent_id,omitempty"`
	Depth   int                              `json:"depth"`
	Root    *orchestrationRuntimeTreeNodeCLI `json:"root,omitempty"`
	Error   string                           `json:"error,omitempty"`
}

type orchestrationRuntimeTreeNodeCLI struct {
	Tag      string                             `json:"tag,omitempty"`
	AgentID  string                             `json:"agent_id,omitempty"`
	PID      string                             `json:"pid,omitempty"`
	Metadata map[string]any                     `json:"metadata,omitempty"`
	Status   string                             `json:"status,omitempty"`
	State    any                                `json:"state,omitempty"`
	Error    string                             `json:"error,omitempty"`
	Children []*orchestrationRuntimeTreeNodeCLI `json:"children,omitempty"`
}

type orchestrationBoardCardRuntimeDataCLI struct {
	Card    coreorchestration.Card           `json:"card"`
	Runtime *orchestrationRuntimeTreeDataCLI `json:"runtime,omitempty"`
}

func init() {
	rootCmd.AddCommand(newOrchestrationCommand())
}

func newOrchestrationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orchestration",
		Short: "Inspect and control the v2 orchestration board",
	}
	cmd.AddCommand(newOrchestrationDispatchIssueCommand())
	cmd.AddCommand(newOrchestrationCardActionCommand())
	cmd.AddCommand(newOrchestrationCardRuntimeCommand())
	return cmd
}

func newOrchestrationDispatchIssueCommand() *cobra.Command {
	var requestID string
	var workspaceID string
	var issueIdentifier string
	var title string
	var prompt string
	var parentAgentID string
	var role string
	var execMode string
	var maxIterations int
	var maxContextTokens int
	var maxAutoTurns int

	cmd := &cobra.Command{
		Use:   "dispatch-issue <issue-id>",
		Short: "Dispatch one orchestration issue through the configured runtime",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd.Context())
			if err != nil {
				return writeOrchestrationCLIError(cmd, commandOrchestrationDispatchIssueCLI, fmt.Errorf("load config: %w", err))
			}

			req := coreorchestration.DispatchRequest{
				RequestID:        strings.TrimSpace(requestID),
				WorkspaceID:      strings.TrimSpace(workspaceID),
				IssueID:          strings.TrimSpace(args[0]),
				IssueIdentifier:  strings.TrimSpace(issueIdentifier),
				Title:            strings.TrimSpace(title),
				Prompt:           strings.TrimSpace(prompt),
				ParentAgentID:    strings.TrimSpace(parentAgentID),
				Role:             strings.TrimSpace(role),
				ExecMode:         strings.TrimSpace(execMode),
				MaxIterations:    maxIterations,
				MaxContextTokens: maxContextTokens,
				MaxAutoTurns:     maxAutoTurns,
			}

			resp, err := runOrchestrationDispatchIssueCLI(cmd.Context(), cfg, req)
			if err != nil {
				return writeOrchestrationCLIError(cmd, commandOrchestrationDispatchIssueCLI, err)
			}
			return writeOK(cmd, commandOrchestrationDispatchIssueCLI, resp, "run", nil)
		},
	}

	cmd.Flags().StringVar(&requestID, "request-id", "", "Idempotency key for this dispatch")
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace identifier/path")
	cmd.Flags().StringVar(&issueIdentifier, "issue-identifier", "", "Human issue identifier")
	cmd.Flags().StringVar(&title, "title", "", "Human card title")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Explicit dispatch prompt (defaults from issue/title)")
	cmd.Flags().StringVar(&parentAgentID, "parent-agent-id", "", "Parent Jido agent id (defaults from env)")
	cmd.Flags().StringVar(&role, "role", "", "Dispatch role (defaults to service default)")
	cmd.Flags().StringVar(&execMode, "exec-mode", "", "Execution mode override")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Max iterations override")
	cmd.Flags().IntVar(&maxContextTokens, "max-context-tokens", 0, "Max context tokens override")
	cmd.Flags().IntVar(&maxAutoTurns, "max-auto-turns", 0, "Max autonomous turns override")
	return cmd
}

func newOrchestrationCardActionCommand() *cobra.Command {
	var requestID string
	var workspaceID string
	var action string

	cmd := &cobra.Command{
		Use:   "card-action <issue-id>",
		Short: "Apply a human control-plane action to an orchestration card",
		Long: `Apply a control-plane action to one orchestration card.

Supported actions:
  retry-now  Move a blocked/retry-queued card back to RetryQueued immediately
  release    Move a non-running card back toward Todo
  mark-done  Mark a non-running card as done`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(action) == "" {
				return writeOrchestrationCLIError(cmd, commandOrchestrationCardActionCLI, &v2errors.V2Error{
					Kind:    v2errors.ErrValidation,
					Message: "action must be one of retry-now, release, or mark-done",
					Details: map[string]any{"field": "action"},
				})
			}

			cfg, err := loadConfig(cmd.Context())
			if err != nil {
				return writeOrchestrationCLIError(cmd, commandOrchestrationCardActionCLI, fmt.Errorf("load config: %w", err))
			}

			resp, err := runOrchestrationCardActionCLI(cmd.Context(), cfg, args[0], workspaceID, action, requestID)
			if err != nil {
				return writeOrchestrationCLIError(cmd, commandOrchestrationCardActionCLI, err)
			}
			return writeOK(cmd, commandOrchestrationCardActionCLI, resp, "run", nil)
		},
	}

	cmd.Flags().StringVar(&action, "action", "", "Card action (retry-now|release|mark-done)")
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace identifier/path")
	cmd.Flags().StringVar(&requestID, "request-id", "", "Idempotency key for the action event")
	_ = cmd.MarkFlagRequired("action")
	return cmd
}

func newOrchestrationCardRuntimeCommand() *cobra.Command {
	var requestID string
	var workspaceID string
	var depth int

	cmd := &cobra.Command{
		Use:   "card-runtime <issue-id>",
		Short: "Inspect the Jido runtime subtree for one orchestration card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(cmd.Context())
			if err != nil {
				return writeOrchestrationCLIError(cmd, commandOrchestrationBoardCardRuntimeGetCLI, fmt.Errorf("load config: %w", err))
			}
			resp, err := runOrchestrationCardRuntimeCLI(cmd.Context(), cfg, logging.FromContext(cmd.Context()), args[0], workspaceID, requestID, depth)
			if err != nil {
				return writeOrchestrationCLIError(cmd, commandOrchestrationBoardCardRuntimeGetCLI, err)
			}
			return writeOK(cmd, commandOrchestrationBoardCardRuntimeGetCLI, resp, "run", nil)
		},
	}

	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace identifier/path")
	cmd.Flags().StringVar(&requestID, "request-id", "", "Request correlation id")
	cmd.Flags().IntVar(&depth, "depth", defaultRuntimeTreeDepthCLI, "Runtime subtree depth (max 5)")
	return cmd
}

func runOrchestrationCardActionCLI(
	ctx context.Context,
	cfg config.Config,
	issueID string,
	workspaceID string,
	action string,
	requestID string,
) (orchestrationCardActionResponseCLI, error) {
	issueID = strings.TrimSpace(issueID)
	workspaceID = strings.TrimSpace(workspaceID)
	action = normalizeOrchestrationCardActionCLI(action)
	requestID = chooseNonEmptyCLI(strings.TrimSpace(requestID), newOrchestrationCLIRequestID("orch-action", issueID))

	if issueID == "" {
		return orchestrationCardActionResponseCLI{}, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "issue_id is required",
			Details: map[string]any{"field": "issue_id"},
		}
	}
	if action == "" {
		return orchestrationCardActionResponseCLI{}, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "action must be one of retry-now, release, or mark-done",
			Details: map[string]any{"field": "action"},
		}
	}

	eventStore, err := openOverseerOrchestrationEventStore(ctx, cfg)
	if err != nil {
		return orchestrationCardActionResponseCLI{}, fmt.Errorf("open event store for card action: %w", err)
	}
	defer func() { _ = eventStore.Close() }()

	orchestrationStore, closeFn, err := openOverseerOrchestrationStore(ctx, cfg)
	if err != nil {
		return orchestrationCardActionResponseCLI{}, fmt.Errorf("open orchestration store for card action: %w", err)
	}
	defer func() {
		if closeFn != nil {
			_ = closeFn()
		}
	}()

	current, err := orchestrationStore.Card(ctx, coreorchestration.CardRequest{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
	})
	if err != nil {
		if errors.Is(err, tursoorchestration.ErrNotFound) {
			return orchestrationCardActionResponseCLI{}, &v2errors.V2Error{
				Kind:    v2errors.ErrNotFound,
				Message: "orchestration card not found",
				Details: map[string]any{"workspace_id": workspaceID, "issue_id": issueID},
			}
		}
		return orchestrationCardActionResponseCLI{}, fmt.Errorf("read orchestration card for action: %w", err)
	}

	now := time.Now().UTC()
	payload, err := orchestrationCardActionPayloadCLI(current.Card, action, now)
	if err != nil {
		return orchestrationCardActionResponseCLI{}, err
	}

	evt := coreevents.Event{
		ID:            fmt.Sprintf("evt-orch-action-%s-%s-%s", stableTokenCLI(requestID), stableTokenCLI(issueID), stableTokenCLI(action)),
		StreamID:      chooseNonEmptyCLI(strings.TrimSpace(current.Card.RunID), "orch:"+strings.TrimSpace(current.Card.IssueID)),
		StreamType:    coreevents.StreamTypeRun,
		EventType:     coreevents.EventOrchestrationUpdated,
		OccurredAt:    now,
		CorrelationID: requestID,
		CausationID:   requestID,
		ActorID:       "actor:cli:orchestration",
		RequestID:     requestID,
		Command:       commandOrchestrationCardActionCLI,
		Payload:       coreevents.MustMarshalPayload(payload),
	}

	idempotent := false
	if err := eventStore.Append(ctx, evt); err != nil {
		if !errors.Is(err, coreevents.ErrVersionConflict) {
			return orchestrationCardActionResponseCLI{}, fmt.Errorf("append orchestration card action: %w", err)
		}
		idempotent = true
	} else {
		if err := orchestrationStore.Apply(ctx, evt); err != nil {
			return orchestrationCardActionResponseCLI{}, fmt.Errorf("apply orchestration card action: %w", err)
		}
	}

	updated, err := orchestrationStore.Card(ctx, coreorchestration.CardRequest{
		WorkspaceID: chooseNonEmptyCLI(workspaceID, current.Card.WorkspaceID),
		IssueID:     issueID,
	})
	if err != nil {
		return orchestrationCardActionResponseCLI{}, fmt.Errorf("read orchestration card after action: %w", err)
	}

	return orchestrationCardActionResponseCLI{
		RequestID:  requestID,
		Action:     action,
		Card:       updated.Card,
		Idempotent: idempotent,
		Timestamp:  now,
	}, nil
}

func runOrchestrationCardRuntimeCLI(
	ctx context.Context,
	cfg config.Config,
	log zerolog.Logger,
	issueID string,
	workspaceID string,
	requestID string,
	depth int,
) (orchestrationBoardCardRuntimeDataCLI, error) {
	issueID = strings.TrimSpace(issueID)
	workspaceID = strings.TrimSpace(workspaceID)
	requestID = chooseNonEmptyCLI(strings.TrimSpace(requestID), newOrchestrationCLIRequestID("orch-runtime", issueID))

	if issueID == "" {
		return orchestrationBoardCardRuntimeDataCLI{}, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "issue_id is required",
			Details: map[string]any{"field": "issue_id"},
		}
	}
	if depth < 0 {
		return orchestrationBoardCardRuntimeDataCLI{}, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "depth must be a non-negative integer",
			Details: map[string]any{"field": "depth"},
		}
	}
	if depth > maxRuntimeTreeDepthCLI {
		depth = maxRuntimeTreeDepthCLI
	}
	_ = requestID

	orchestrationStore, closeFn, err := openOverseerOrchestrationStore(ctx, cfg)
	if err != nil {
		return orchestrationBoardCardRuntimeDataCLI{}, fmt.Errorf("open orchestration store for runtime: %w", err)
	}
	defer func() {
		if closeFn != nil {
			_ = closeFn()
		}
	}()

	resp, err := orchestrationStore.Card(ctx, coreorchestration.CardRequest{
		WorkspaceID: workspaceID,
		IssueID:     issueID,
	})
	if err != nil {
		if errors.Is(err, tursoorchestration.ErrNotFound) {
			return orchestrationBoardCardRuntimeDataCLI{}, &v2errors.V2Error{
				Kind:    v2errors.ErrNotFound,
				Message: "orchestration card not found",
				Details: map[string]any{"workspace_id": workspaceID, "issue_id": issueID},
			}
		}
		return orchestrationBoardCardRuntimeDataCLI{}, fmt.Errorf("read orchestration card for runtime: %w", err)
	}

	return orchestrationBoardCardRuntimeDataCLI{
		Card:    resp.Card,
		Runtime: loadOrchestrationCardRuntimeTreeCLI(ctx, cfg, log, resp.Card, depth),
	}, nil
}

func runOrchestrationDispatchIssueCLI(
	ctx context.Context,
	cfg config.Config,
	req coreorchestration.DispatchRequest,
) (coreorchestration.DispatchResponse, error) {
	if strings.EqualFold(resolveCLIRuntimeBackend(), cliRuntimeBackendGoruntime) {
		return coreorchestration.DispatchResponse{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "goruntime orchestration dispatch requires a persistent host (use overseer or web server), not the one-shot CLI command",
			Fatal:   true,
		}
	}
	req.RequestID = chooseNonEmptyCLI(strings.TrimSpace(req.RequestID), newOrchestrationCLIRequestID("orch-dispatch", req.IssueID))
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.IssueID = strings.TrimSpace(req.IssueID)
	req.IssueIdentifier = strings.TrimSpace(req.IssueIdentifier)
	req.Title = strings.TrimSpace(req.Title)
	req.ParentAgentID = chooseNonEmptyCLI(strings.TrimSpace(req.ParentAgentID), resolveOverseerDispatchParentAgentID())
	req.Prompt = chooseNonEmptyCLI(strings.TrimSpace(req.Prompt), defaultOrchestrationDispatchPromptCLI(req))

	if req.ParentAgentID == "" {
		return coreorchestration.DispatchResponse{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "jido orchestration dispatch parent_agent_id is not configured",
			Fatal:   true,
		}
	}

	eventStore, err := openOverseerOrchestrationEventStore(ctx, cfg)
	if err != nil {
		return coreorchestration.DispatchResponse{}, fmt.Errorf("open event store for dispatch: %w", err)
	}
	defer func() { _ = eventStore.Close() }()

	orchestrationStore, closeFn, err := openOverseerOrchestrationStore(ctx, cfg)
	if err != nil {
		return coreorchestration.DispatchResponse{}, fmt.Errorf("open orchestration store for dispatch: %w", err)
	}
	defer func() {
		if closeFn != nil {
			_ = closeFn()
		}
	}()

	runtime, err := v2jido.NewOrchestrationRuntime(v2jido.OrchestrationRuntimeConfig{
		Events:         eventStore,
		Projections:    orchestrationStore,
		Reader:         orchestrationStore,
		ParentAgentIDs: []string{req.ParentAgentID},
	})
	if err != nil {
		return coreorchestration.DispatchResponse{}, fmt.Errorf("configure jido orchestration runtime: %w", err)
	}

	spawnService := v2services.NewSpawnService(v2services.SpawnDependencies{
		RuntimeSpawner: runtime.ChildSpawner,
	})
	dispatchService := v2services.NewOrchestrationService(v2services.OrchestrationDependencies{
		Spawn:       spawnService,
		Reader:      orchestrationStore,
		LaneOptions: coreorchestration.DefaultLaneOptions(),
	})
	return dispatchService.DispatchIssue(ctx, req)
}

func resolveCLIRuntimeBackend() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envCLIRuntimeBackend))) {
	case cliRuntimeBackendGoruntime:
		return cliRuntimeBackendGoruntime
	case cliRuntimeBackendJido:
		return cliRuntimeBackendJido
	default:
		return cliRuntimeBackendGoruntime
	}
}

func loadOptionalRuntimeStateReaderCLI(ctx context.Context, cfg config.Config) (coreworker.StateReader, func() error, bool, error) {
	if strings.EqualFold(resolveCLIRuntimeBackend(), cliRuntimeBackendGoruntime) {
		store, closeFn, err := tursoworkers.Open(ctx, cfg.Storage.Root)
		if err != nil {
			return nil, nil, false, err
		}
		return store, closeFn, true, nil
	}

	client, available, err := loadOptionalJidoClientCLI()
	if err != nil || !available {
		return nil, nil, available, err
	}
	reader, err := v2jido.NewRuntimeAdapter(v2jido.RuntimeAdapterConfig{Client: client})
	if err != nil {
		return nil, nil, false, err
	}
	return reader, func() error { return nil }, true, nil
}

func loadOptionalJidoClientCLI() (v2jido.Client, bool, error) {
	client, err := v2jido.NewEnvJSONRPCClient()
	if err != nil {
		return nil, false, nil
	}
	return client, true, nil
}

func loadOrchestrationCardRuntimeTreeCLI(ctx context.Context, cfg config.Config, log zerolog.Logger, card coreorchestration.Card, depth int) *orchestrationRuntimeTreeDataCLI {
	runtime := &orchestrationRuntimeTreeDataCLI{
		Enabled: strings.TrimSpace(card.AgentID) != "",
		AgentID: strings.TrimSpace(card.AgentID),
		Depth:   depth,
	}
	if runtime.AgentID == "" {
		runtime.Error = "card has no agent_id"
		return runtime
	}

	reader, closeFn, available, err := loadOptionalRuntimeStateReaderCLI(ctx, cfg)
	if err != nil {
		runtime.Error = err.Error()
		return runtime
	}
	if !available {
		return runtime
	}
	defer func() {
		if closeFn != nil {
			_ = closeFn()
		}
	}()

	visited := map[string]struct{}{}
	runtime.Root = loadOrchestrationRuntimeTreeNodeCLI(ctx, cfg, log, reader, coreworker.Record{
		Tag:      runtime.AgentID,
		AgentID:  runtime.AgentID,
		Metadata: map[string]any{"workspace_id": card.WorkspaceID, "issue_id": card.IssueID},
	}, depth, visited)
	if runtime.Root != nil && strings.TrimSpace(runtime.Root.Error) != "" {
		runtime.Error = runtime.Root.Error
	}
	return runtime
}

func loadOrchestrationRuntimeTreeNodeCLI(
	ctx context.Context,
	cfg config.Config,
	log zerolog.Logger,
	reader coreworker.StateReader,
	seed coreworker.Record,
	depth int,
	visited map[string]struct{},
) *orchestrationRuntimeTreeNodeCLI {
	agentID := strings.TrimSpace(seed.AgentID)
	node := &orchestrationRuntimeTreeNodeCLI{
		Tag:      strings.TrimSpace(seed.Tag),
		AgentID:  agentID,
		PID:      strings.TrimSpace(seed.PID),
		Metadata: seed.Metadata,
	}
	if agentID == "" {
		node.Error = "runtime node has no agent_id"
		return node
	}
	if _, ok := visited[agentID]; ok {
		node.Error = "runtime subtree cycle detected"
		return node
	}
	visited[agentID] = struct{}{}
	defer delete(visited, agentID)

	record, err := reader.Worker(ctx, coreworker.LookupRequest{AgentID: agentID})
	if err != nil {
		node.Error = err.Error()
		return node
	}
	node.Tag = chooseNonEmptyCLI(strings.TrimSpace(record.Tag), node.Tag)
	node.PID = chooseNonEmptyCLI(strings.TrimSpace(record.PID), node.PID)
	if len(record.Metadata) > 0 && node.Metadata == nil {
		node.Metadata = record.Metadata
	} else if len(record.Metadata) > 0 {
		merged := make(map[string]any, len(node.Metadata)+len(record.Metadata))
		for k, v := range node.Metadata {
			merged[k] = v
		}
		for k, v := range record.Metadata {
			merged[k] = v
		}
		node.Metadata = merged
	}
	node.Status = string(record.Status)
	if len(record.RawState) > 0 && string(record.RawState) != "null" {
		var state any
		if err := json.Unmarshal(record.RawState, &state); err != nil {
			node.State = string(record.RawState)
			log.Debug().Err(err).Str("agent_id", agentID).Msg("failed to decode orchestration runtime node state; returning raw payload")
		} else {
			if stateMap, ok := state.(map[string]any); ok {
				state = taskhistory.RefreshJidoRuntimeState(ctx, cfg.Storage.Root, cfg.Paths.CAS, stateMap)
			}
			node.State = state
		}
	}
	if depth <= 0 {
		return node
	}

	children, err := reader.Children(ctx, coreworker.ChildrenRequest{ParentAgentID: agentID})
	if err != nil {
		node.Error = chooseNonEmptyCLI(node.Error, err.Error())
		return node
	}
	for _, child := range children {
		node.Children = append(node.Children, loadOrchestrationRuntimeTreeNodeCLI(ctx, cfg, log, reader, child, depth-1, visited))
	}
	return node
}

func orchestrationCardActionPayloadCLI(card coreorchestration.Card, action string, now time.Time) (map[string]any, error) {
	action = normalizeOrchestrationCardActionCLI(action)
	if action == "" {
		return nil, &v2errors.V2Error{Kind: v2errors.ErrValidation, Message: "unsupported orchestration card action"}
	}
	if card.IssueID == "" {
		return nil, &v2errors.V2Error{Kind: v2errors.ErrNotFound, Message: "orchestration card not found"}
	}
	if card.State == coreorchestration.StateRunning || card.State == coreorchestration.StateClaimed {
		return nil, &v2errors.V2Error{
			Kind:    v2errors.ErrPolicyViolation,
			Message: "card action is not allowed while the card is actively running",
			Details: map[string]any{"state": card.State, "action": action},
		}
	}

	payload := map[string]any{
		"workspace_id":     strings.TrimSpace(card.WorkspaceID),
		"issue_id":         strings.TrimSpace(card.IssueID),
		"issue_identifier": strings.TrimSpace(card.IssueIdentifier),
		"title":            strings.TrimSpace(card.Title),
		"run_id":           strings.TrimSpace(card.RunID),
		"agent_id":         strings.TrimSpace(card.AgentID),
		"actor_id":         strings.TrimSpace(card.ActorID),
		"attempt":          card.Attempt,
	}

	switch action {
	case orchestrationActionRetryNowCLI:
		if card.Lane != coreorchestration.LaneRetryQueue && card.Lane != coreorchestration.LaneBlocked {
			return nil, &v2errors.V2Error{
				Kind:    v2errors.ErrPolicyViolation,
				Message: "retry-now is only allowed for blocked or retry-queued cards",
				Details: map[string]any{"lane": card.Lane},
			}
		}
		payload["state"] = string(coreorchestration.StateRetryQueue)
		payload["eligibility"] = string(coreorchestration.EligibilityEligible)
		payload["policy_status"] = string(coreorchestration.PolicyStatusOK)
		payload["retry_due_at"] = now.Format(time.RFC3339Nano)
		payload["tracker_state"] = ""
		payload["last_outcome"] = ""
		payload["denial_reason"] = ""
		payload["suggestion"] = ""
	case orchestrationActionReleaseCLI:
		payload["state"] = string(coreorchestration.StateReleased)
		payload["eligibility"] = string(coreorchestration.EligibilityEligible)
		payload["policy_status"] = string(coreorchestration.PolicyStatusOK)
		payload["retry_due_at"] = ""
		payload["tracker_state"] = ""
		payload["last_outcome"] = ""
		payload["denial_reason"] = ""
		payload["suggestion"] = ""
	case orchestrationActionMarkDoneCLI:
		payload["state"] = string(coreorchestration.StateReleased)
		payload["eligibility"] = string(coreorchestration.EligibilityEligible)
		payload["policy_status"] = string(coreorchestration.PolicyStatusOK)
		payload["tracker_state"] = "Done"
		payload["retry_due_at"] = ""
		payload["last_outcome"] = ""
		payload["denial_reason"] = ""
		payload["suggestion"] = ""
	default:
		return nil, &v2errors.V2Error{Kind: v2errors.ErrValidation, Message: "unsupported orchestration card action"}
	}
	return payload, nil
}

func defaultOrchestrationDispatchPromptCLI(req coreorchestration.DispatchRequest) string {
	switch {
	case strings.TrimSpace(req.IssueIdentifier) != "" && strings.TrimSpace(req.Title) != "":
		return fmt.Sprintf("Work on issue %s: %s", strings.TrimSpace(req.IssueIdentifier), strings.TrimSpace(req.Title))
	case strings.TrimSpace(req.Title) != "":
		return "Work on issue: " + strings.TrimSpace(req.Title)
	case strings.TrimSpace(req.IssueIdentifier) != "":
		return "Work on issue " + strings.TrimSpace(req.IssueIdentifier)
	case strings.TrimSpace(req.IssueID) != "":
		return "Work on issue " + strings.TrimSpace(req.IssueID)
	default:
		return ""
	}
}

func normalizeOrchestrationCardActionCLI(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case orchestrationActionRetryNowCLI:
		return orchestrationActionRetryNowCLI
	case orchestrationActionReleaseCLI:
		return orchestrationActionReleaseCLI
	case orchestrationActionMarkDoneCLI:
		return orchestrationActionMarkDoneCLI
	default:
		return ""
	}
}

func chooseNonEmptyCLI(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func newOrchestrationCLIRequestID(prefix string, issueID string) string {
	return fmt.Sprintf("%s-%s-%d", stableTokenCLI(prefix), stableTokenCLI(issueID), time.Now().UTC().UnixNano())
}

func stableTokenCLI(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return "x"
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	prevDash := false
	for _, r := range trimmed {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "x"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

func writeOrchestrationCLIError(cmd *cobra.Command, command string, err error) error {
	code := "ERUNTIME"
	message := "request failed"
	data := map[string]any{}

	var verr *v2errors.V2Error
	if errors.As(err, &verr) {
		code = verr.EnvelopeCode()
		if strings.TrimSpace(verr.Message) != "" {
			message = strings.TrimSpace(verr.Message)
		} else if strings.TrimSpace(verr.Error()) != "" {
			message = strings.TrimSpace(verr.Error())
		}
		data["kind"] = string(verr.Kind)
		if len(verr.Details) > 0 {
			data["details"] = verr.Details
		}
	} else if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = strings.TrimSpace(err.Error())
	}

	env := envelope.Error(command, code, message, data)
	if writeErr := envelope.Write(cmd.OutOrStdout(), env); writeErr != nil {
		return fmt.Errorf("write error envelope: %w", writeErr)
	}
	return err
}
