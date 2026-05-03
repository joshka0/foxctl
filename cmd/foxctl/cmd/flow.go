package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/protocol"
	flowmodel "github.com/joshka0/foxctl/internal/runtime/flow"
	flowstore "github.com/joshka0/foxctl/internal/storage/flow"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

var flowCmd = &cobra.Command{
	Use:   "flow",
	Short: "Manage flows",
	Long:  "Create, inspect, and manipulate envelope-contract flow graphs",
}

// ---------------------------------------------------------------------------
// Flags (package-level vars for cobra flag binding)
// ---------------------------------------------------------------------------

var (
	flowNameFlag        string
	flowWorkspaceFlag   string
	flowDescriptionFlag string

	flowNodeLabelFlag    string
	flowNodeKindFlag     string
	flowNodeConfigFlag   string
	flowNodePositionFlag string

	flowEdgeFromFlag         string
	flowEdgeToFlag           string
	flowEdgeTriggerFlag      string
	flowEdgeTransformFlag    string
	flowEdgeTransformCfgFlag string
	flowEdgeConditionFlag    string
	flowEdgeRetryFlag        string
)

// ---------------------------------------------------------------------------
// Subcommands
// ---------------------------------------------------------------------------

var flowCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new flow",
	Long:  "Create a new flow with the given name in the specified workspace",
	RunE:  runFlowCreate,
}

var flowListCmd = &cobra.Command{
	Use:   "list",
	Short: "List flows",
	Long:  "List all flows in the specified workspace",
	RunE:  runFlowList,
}

var flowShowCmd = &cobra.Command{
	Use:   "show <id-or-name>",
	Short: "Show flow detail",
	Long:  "Display a flow with its nodes and edges, resolved by ID or name",
	Args:  cobra.ExactArgs(1),
	RunE:  runFlowShow,
}

var flowDeleteCmd = &cobra.Command{
	Use:   "delete <id-or-name>",
	Short: "Delete a flow",
	Long:  "Delete a flow and all its nodes and edges by ID or name",
	Args:  cobra.ExactArgs(1),
	RunE:  runFlowDelete,
}

var flowAddNodeCmd = &cobra.Command{
	Use:   "add-node <flow-id-or-name>",
	Short: "Add a node to a flow",
	Long:  "Add a new node (skill, pty, http, playwright, image, transform, agent) to a flow",
	Args:  cobra.ExactArgs(1),
	RunE:  runFlowAddNode,
}

var flowRemoveNodeCmd = &cobra.Command{
	Use:   "remove-node <flow-id-or-name> <node-id-or-label>",
	Short: "Remove a node from a flow",
	Long:  "Remove a node (and its connected edges) from a flow",
	Args:  cobra.ExactArgs(2),
	RunE:  runFlowRemoveNode,
}

var flowAddEdgeCmd = &cobra.Command{
	Use:   "add-edge <flow-id-or-name>",
	Short: "Add an edge to a flow",
	Long:  "Add a new edge connecting two nodes in a flow",
	Args:  cobra.ExactArgs(1),
	RunE:  runFlowAddEdge,
}

var flowRemoveEdgeCmd = &cobra.Command{
	Use:   "remove-edge <flow-id-or-name> <edge-id>",
	Short: "Remove an edge from a flow",
	Long:  "Remove an edge from a flow by its ID",
	Args:  cobra.ExactArgs(2),
	RunE:  runFlowRemoveEdge,
}

var flowStartCmd = &cobra.Command{
	Use:   "start <flow-id-or-name>",
	Short: "Start a flow",
	Long:  "Start executing a flow: validates the graph has source nodes, creates a FlowRun, and starts the engine",
	Args:  cobra.ExactArgs(1),
	RunE:  runFlowStart,
}

var flowStopCmd = &cobra.Command{
	Use:   "stop <flow-id-or-name>",
	Short: "Stop a running flow",
	Long:  "Gracefully stop a running flow, terminating all node execution",
	Args:  cobra.ExactArgs(1),
	RunE:  runFlowStop,
}

var flowPauseCmd = &cobra.Command{
	Use:   "pause <flow-id-or-name>",
	Short: "Pause a running flow",
	Long:  "Pause all evaluators in a running flow without stopping node executors",
	Args:  cobra.ExactArgs(1),
	RunE:  runFlowPause,
}

var flowStatusCmd = &cobra.Command{
	Use:   "status <flow-id-or-name>",
	Short: "Show flow runtime status",
	Long:  "Display the current runtime state of a flow including per-node and per-edge status",
	Args:  cobra.ExactArgs(1),
	RunE:  runFlowStatus,
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func init() {
	rootCmd.AddCommand(flowCmd)

	// Parent flags
	flowCmd.PersistentFlags().StringVar(&flowWorkspaceFlag, "workspace", ".", "workspace directory for flow storage")

	// create flags
	flowCreateCmd.Flags().StringVar(&flowNameFlag, "name", "", "flow name (required)")
	flowCreateCmd.Flags().StringVar(&flowDescriptionFlag, "description", "", "flow description")
	_ = flowCreateCmd.MarkFlagRequired("name")

	// add-node flags
	flowAddNodeCmd.Flags().StringVar(&flowNodeLabelFlag, "label", "", "node label (required)")
	flowAddNodeCmd.Flags().StringVar(&flowNodeKindFlag, "kind", "", "node kind (required)")
	flowAddNodeCmd.Flags().StringVar(&flowNodeConfigFlag, "config", "", "node config as JSON object (required)")
	flowAddNodeCmd.Flags().StringVar(&flowNodePositionFlag, "position", "", "node position as JSON {\"x\":N,\"y\":N}")
	_ = flowAddNodeCmd.MarkFlagRequired("label")
	_ = flowAddNodeCmd.MarkFlagRequired("kind")
	_ = flowAddNodeCmd.MarkFlagRequired("config")

	// add-edge flags
	flowAddEdgeCmd.Flags().StringVar(&flowEdgeFromFlag, "from", "", "source node ID or label (required)")
	flowAddEdgeCmd.Flags().StringVar(&flowEdgeToFlag, "to", "", "target node ID or label (required)")
	flowAddEdgeCmd.Flags().StringVar(&flowEdgeTriggerFlag, "trigger", "output_ready", "trigger kind")
	flowAddEdgeCmd.Flags().StringVar(&flowEdgeTransformFlag, "transform", "passthrough", "transform kind")
	flowAddEdgeCmd.Flags().StringVar(&flowEdgeTransformCfgFlag, "transform-config", "", "transform config as JSON string")
	flowAddEdgeCmd.Flags().StringVar(&flowEdgeConditionFlag, "condition", "", "edge condition expression")
	flowAddEdgeCmd.Flags().StringVar(&flowEdgeRetryFlag, "retry", "", "retry policy as JSON {\"max_attempts\":N,\"delay_ms\":N}")
	_ = flowAddEdgeCmd.MarkFlagRequired("from")
	_ = flowAddEdgeCmd.MarkFlagRequired("to")

	flowCmd.AddCommand(flowCreateCmd)
	flowCmd.AddCommand(flowListCmd)
	flowCmd.AddCommand(flowShowCmd)
	flowCmd.AddCommand(flowDeleteCmd)
	flowCmd.AddCommand(flowAddNodeCmd)
	flowCmd.AddCommand(flowRemoveNodeCmd)
	flowCmd.AddCommand(flowAddEdgeCmd)
	flowCmd.AddCommand(flowRemoveEdgeCmd)
	flowCmd.AddCommand(flowStartCmd)
	flowCmd.AddCommand(flowStopCmd)
	flowCmd.AddCommand(flowPauseCmd)
	flowCmd.AddCommand(flowStatusCmd)
}

// ---------------------------------------------------------------------------
// Store helper
// ---------------------------------------------------------------------------

// openFlowStore creates a flow store for the given workspace.
func openFlowStore(ctx context.Context, workspace string) (flowmodel.Store, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("flow: resolve workspace: %w", err)
	}
	return flowstore.Open(ctx, abs)
}

// resolveFlow resolves an ID-or-name argument to a Flow, using the given store
// and workspace path. It first tries ID lookup, then name lookup.
func resolveFlow(ctx context.Context, store flowmodel.Store, workspace, ref string) (flowmodel.Flow, error) {
	// Try by ID first.
	f, err := store.GetFlow(ctx, ref)
	if err == nil {
		return f, nil
	}
	if !errors.Is(err, flowmodel.ErrNotFound) {
		return flowmodel.Flow{}, err
	}
	// Try by name.
	f, err = store.GetFlowByName(ctx, workspace, ref)
	if err != nil {
		return flowmodel.Flow{}, err
	}
	return f, nil
}

// resolveNodeInFlow resolves a node ID-or-label within the context of a flow.
// It first tries ID lookup, then label lookup. Returns error if label is
// ambiguous (multiple matches).
func resolveNodeInFlow(ctx context.Context, store flowmodel.Store, flowID, ref string) (flowmodel.FlowNode, error) {
	// Try by ID first.
	n, err := store.GetNode(ctx, ref)
	if err == nil && n.FlowID == flowID {
		return n, nil
	}

	// Label lookup: scan all nodes in flow.
	nodes, err := store.ListNodesByFlow(ctx, flowID)
	if err != nil {
		return flowmodel.FlowNode{}, err
	}

	var matches []flowmodel.FlowNode
	for _, node := range nodes {
		if node.Label == ref {
			matches = append(matches, node)
		}
	}

	switch len(matches) {
	case 0:
		return flowmodel.FlowNode{}, flowmodel.ErrNotFound
	case 1:
		return matches[0], nil
	default:
		var ids []string
		for _, m := range matches {
			ids = append(ids, m.ID)
		}
		return flowmodel.FlowNode{}, fmt.Errorf("flow: ambiguous node label %q: multiple matches [%s]; use node ID instead", ref, strings.Join(ids, ", "))
	}
}

// ---------------------------------------------------------------------------
// Command implementations
// ---------------------------------------------------------------------------

func runFlowCreate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/create", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	f := flowmodel.Flow{
		ID:          ulid.Make().String(),
		Name:        flowNameFlag,
		Workspace:   flowResolveWorkspace(flowWorkspaceFlag),
		State:       flowmodel.FlowDraft,
		Description: flowDescriptionFlag,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Validate the flow before persisting.
	if err := f.Validate(); err != nil {
		return writeFlowError(cmd, "flow/create", err)
	}

	created, err := store.CreateFlow(ctx, f)
	if err != nil {
		return writeFlowError(cmd, "flow/create", err)
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/create", created)
}

func runFlowList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/list", err)
	}
	defer store.Close()

	flows, err := store.ListFlows(ctx, workspace)
	if err != nil {
		return writeFlowError(cmd, "flow/list", err)
	}
	if flows == nil {
		flows = []flowmodel.Flow{}
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/list", map[string]any{
		"flows": flows,
	})
}

func runFlowShow(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	ref := args[0]
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/show", err)
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, ref)
	if err != nil {
		return writeFlowError(cmd, "flow/show", err)
	}

	nodes, err := store.ListNodesByFlow(ctx, f.ID)
	if err != nil {
		return writeFlowError(cmd, "flow/show", err)
	}
	if nodes == nil {
		nodes = []flowmodel.FlowNode{}
	}

	edges, err := store.ListEdgesByFlow(ctx, f.ID)
	if err != nil {
		return writeFlowError(cmd, "flow/show", err)
	}
	if edges == nil {
		edges = []flowmodel.FlowEdge{}
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/show", map[string]any{
		"id":          f.ID,
		"name":        f.Name,
		"workspace":   f.Workspace,
		"state":       f.State,
		"description": f.Description,
		"created_at":  f.CreatedAt,
		"updated_at":  f.UpdatedAt,
		"nodes":       nodes,
		"edges":       edges,
	})
}

func runFlowDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	ref := args[0]
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/delete", err)
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, ref)
	if err != nil {
		return writeFlowError(cmd, "flow/delete", err)
	}

	// Reject deletion of running flows.
	if f.State == flowmodel.FlowRunning {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/delete",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("cannot delete flow %q in running state; stop it first", f.Name),
			map[string]any{"flow_id": f.ID, "state": string(f.State)})
	}

	if err := store.DeleteFlow(ctx, f.ID); err != nil {
		return writeFlowError(cmd, "flow/delete", err)
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/delete", map[string]any{
		"id":      f.ID,
		"name":    f.Name,
		"deleted": true,
	})
}

func runFlowAddNode(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	flowRef := args[0]
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/add-node", err)
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, flowRef)
	if err != nil {
		return writeFlowError(cmd, "flow/add-node", err)
	}

	// Validate kind.
	kind := flowmodel.NodeKind(flowNodeKindFlag)
	if !kind.IsValid() {
		validKinds := make([]string, len(flowmodel.ValidNodeKinds))
		for i, k := range flowmodel.ValidNodeKinds {
			validKinds[i] = string(k)
		}
		return protocol.WriteError(cmd.OutOrStdout(), "flow/add-node",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("invalid node kind %q; valid kinds: %s", flowNodeKindFlag, strings.Join(validKinds, ", ")),
			nil)
	}

	// Validate config is valid JSON object.
	if !json.Valid([]byte(flowNodeConfigFlag)) {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/add-node",
			protocol.ErrorCodeEParse,
			fmt.Sprintf("invalid JSON in --config: %s", flowNodeConfigFlag),
			nil)
	}

	var config json.RawMessage = json.RawMessage(flowNodeConfigFlag)

	// Parse optional position.
	var position *flowmodel.Position
	if flowNodePositionFlag != "" {
		if !json.Valid([]byte(flowNodePositionFlag)) {
			return protocol.WriteError(cmd.OutOrStdout(), "flow/add-node",
				protocol.ErrorCodeEParse,
				fmt.Sprintf("invalid JSON in --position: %s", flowNodePositionFlag),
				nil)
		}
		var pos flowmodel.Position
		if err := json.Unmarshal([]byte(flowNodePositionFlag), &pos); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "flow/add-node",
				protocol.ErrorCodeEParse,
				fmt.Sprintf("invalid position object: %v", err),
				nil)
		}
		position = &pos
	}

	node := flowmodel.FlowNode{
		ID:       ulid.Make().String(),
		FlowID:   f.ID,
		Kind:     kind,
		Label:    flowNodeLabelFlag,
		Config:   config,
		Position: position,
	}

	created, err := store.AddNode(ctx, node)
	if err != nil {
		return writeFlowError(cmd, "flow/add-node", err)
	}

	// Update the flow's updated_at timestamp.
	if _, err := store.UpdateFlow(ctx, f); err != nil {
		// Non-fatal: the node was created, just the timestamp update failed.
		_ = err
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/add-node", created)
}

func runFlowRemoveNode(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	flowRef := args[0]
	nodeRef := args[1]
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/remove-node", err)
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, flowRef)
	if err != nil {
		return writeFlowError(cmd, "flow/remove-node", err)
	}

	node, err := resolveNodeInFlow(ctx, store, f.ID, nodeRef)
	if err != nil {
		return writeFlowError(cmd, "flow/remove-node", err)
	}

	if err := store.RemoveNode(ctx, node.ID); err != nil {
		return writeFlowError(cmd, "flow/remove-node", err)
	}

	// Update the flow's updated_at timestamp.
	if _, err := store.UpdateFlow(ctx, f); err != nil {
		_ = err
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/remove-node", map[string]any{
		"node_id": node.ID,
		"label":   node.Label,
		"removed": true,
	})
}

func runFlowAddEdge(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	flowRef := args[0]
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/add-edge", err)
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, flowRef)
	if err != nil {
		return writeFlowError(cmd, "flow/add-edge", err)
	}

	// Resolve from and to nodes.
	fromNode, err := resolveNodeInFlow(ctx, store, f.ID, flowEdgeFromFlag)
	if err != nil {
		if errors.Is(err, flowmodel.ErrNotFound) {
			return protocol.WriteError(cmd.OutOrStdout(), "flow/add-edge",
				protocol.ErrorCodeENotFound,
				fmt.Sprintf("source node %q not found in flow %q", flowEdgeFromFlag, f.Name),
				nil)
		}
		return writeFlowError(cmd, "flow/add-edge", err)
	}

	toNode, err := resolveNodeInFlow(ctx, store, f.ID, flowEdgeToFlag)
	if err != nil {
		if errors.Is(err, flowmodel.ErrNotFound) {
			return protocol.WriteError(cmd.OutOrStdout(), "flow/add-edge",
				protocol.ErrorCodeENotFound,
				fmt.Sprintf("target node %q not found in flow %q", flowEdgeToFlag, f.Name),
				nil)
		}
		return writeFlowError(cmd, "flow/add-edge", err)
	}

	// Reject self-loops.
	if fromNode.ID == toNode.ID {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/add-edge",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("self-loop not allowed: from and to resolve to the same node %q (%s)", fromNode.Label, fromNode.ID),
			nil)
	}

	// Validate trigger kind.
	trigger := flowmodel.TriggerKind(flowEdgeTriggerFlag)
	if !trigger.IsValid() {
		validTriggers := make([]string, len(flowmodel.ValidTriggerKinds))
		for i, t := range flowmodel.ValidTriggerKinds {
			validTriggers[i] = string(t)
		}
		return protocol.WriteError(cmd.OutOrStdout(), "flow/add-edge",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("invalid trigger kind %q; valid kinds: %s", flowEdgeTriggerFlag, strings.Join(validTriggers, ", ")),
			nil)
	}

	// Validate transform kind.
	transform := flowmodel.TransformKind(flowEdgeTransformFlag)
	if !transform.IsValid() {
		validTransforms := make([]string, len(flowmodel.ValidTransformKinds))
		for i, t := range flowmodel.ValidTransformKinds {
			validTransforms[i] = string(t)
		}
		return protocol.WriteError(cmd.OutOrStdout(), "flow/add-edge",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("invalid transform kind %q; valid kinds: %s", flowEdgeTransformFlag, strings.Join(validTransforms, ", ")),
			nil)
	}

	// Parse optional retry policy.
	var retryPolicy *flowmodel.RetryPolicy
	if flowEdgeRetryFlag != "" {
		if !json.Valid([]byte(flowEdgeRetryFlag)) {
			return protocol.WriteError(cmd.OutOrStdout(), "flow/add-edge",
				protocol.ErrorCodeEParse,
				fmt.Sprintf("invalid JSON in --retry: %s", flowEdgeRetryFlag),
				nil)
		}
		var rp flowmodel.RetryPolicy
		if err := json.Unmarshal([]byte(flowEdgeRetryFlag), &rp); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "flow/add-edge",
				protocol.ErrorCodeEParse,
				fmt.Sprintf("invalid retry policy: %v", err),
				nil)
		}
		retryPolicy = &rp
	}

	edge := flowmodel.FlowEdge{
		ID:              ulid.Make().String(),
		FlowID:          f.ID,
		FromNodeID:      fromNode.ID,
		ToNodeID:        toNode.ID,
		Transform:       transform,
		TransformConfig: flowEdgeTransformCfgFlag,
		Trigger:         trigger,
		Condition:       flowEdgeConditionFlag,
		RetryPolicy:     retryPolicy,
	}

	created, err := store.AddEdge(ctx, edge)
	if err != nil {
		return writeFlowError(cmd, "flow/add-edge", err)
	}

	// Update the flow's updated_at timestamp.
	if _, err := store.UpdateFlow(ctx, f); err != nil {
		_ = err
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/add-edge", created)
}

func runFlowRemoveEdge(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	flowRef := args[0]
	edgeID := args[1]
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/remove-edge", err)
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, flowRef)
	if err != nil {
		return writeFlowError(cmd, "flow/remove-edge", err)
	}

	// Verify the edge belongs to this flow.
	edge, err := store.GetEdge(ctx, edgeID)
	if err != nil {
		return writeFlowError(cmd, "flow/remove-edge", err)
	}
	if edge.FlowID != f.ID {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/remove-edge",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("edge %s does not belong to flow %q", edgeID, f.Name),
			nil)
	}

	if err := store.RemoveEdge(ctx, edgeID); err != nil {
		return writeFlowError(cmd, "flow/remove-edge", err)
	}

	// Update the flow's updated_at timestamp.
	if _, err := store.UpdateFlow(ctx, f); err != nil {
		_ = err
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/remove-edge", map[string]any{
		"edge_id": edgeID,
		"removed": true,
	})
}

// ---------------------------------------------------------------------------
// Engine registry (flow execution lifecycle)
// ---------------------------------------------------------------------------

// flowEngineRegistry holds active engines keyed by flow ID.
// When a flow is started, an engine is created and kept in this registry
// until the flow is stopped. The store is kept alive alongside the engine.
var flowEngineRegistry struct {
	mu      sync.Mutex
	engines map[string]*flowmodel.Engine
	stores  map[string]flowmodel.Store
	cancels map[string]context.CancelFunc

	// testExecutors overrides the default executor map for testing.
	// When nil, the default SkillExecutor + TransformExecutor are used.
	testExecutors map[flowmodel.NodeKind]flowmodel.NodeExecutor
}

func init() {
	flowEngineRegistry.engines = make(map[string]*flowmodel.Engine)
	flowEngineRegistry.stores = make(map[string]flowmodel.Store)
	flowEngineRegistry.cancels = make(map[string]context.CancelFunc)
}

// getEngine returns the engine for the given flow ID, or nil if not active.
func getEngine(flowID string) *flowmodel.Engine {
	flowEngineRegistry.mu.Lock()
	defer flowEngineRegistry.mu.Unlock()
	return flowEngineRegistry.engines[flowID]
}

// removeEngine removes the engine and store from the registry.
func removeEngine(flowID string) {
	flowEngineRegistry.mu.Lock()
	defer flowEngineRegistry.mu.Unlock()
	if cancel, ok := flowEngineRegistry.cancels[flowID]; ok {
		cancel()
		delete(flowEngineRegistry.cancels, flowID)
	}
	delete(flowEngineRegistry.engines, flowID)
	if s, ok := flowEngineRegistry.stores[flowID]; ok {
		s.Close()
		delete(flowEngineRegistry.stores, flowID)
	}
}

// ---------------------------------------------------------------------------
// Flow execution command implementations
// ---------------------------------------------------------------------------

func runFlowStart(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	flowRef := args[0]
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	// Try daemon routing first.
	handled, err := routeFlowViaDaemon(cmd, "start", flowRef)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	// Fallback: in-process execution.
	fmt.Fprintf(cmd.ErrOrStderr(), "flow: using in-process execution (daemon unavailable)\n")

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/start", err)
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, flowRef)
	if err != nil {
		return writeFlowError(cmd, "flow/start", err)
	}

	// Check flow state: only draft or stopped flows can be started.
	if f.State == flowmodel.FlowRunning {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/start",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("flow %q is already running", f.Name),
			map[string]any{"flow_id": f.ID, "state": string(f.State)})
	}
	if f.State == flowmodel.FlowPaused {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/start",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("flow %q is paused; stop and restart to resume", f.Name),
			map[string]any{"flow_id": f.ID, "state": string(f.State)})
	}

	// Validate the flow has nodes.
	nodes, err := store.ListNodesByFlow(ctx, f.ID)
	if err != nil {
		return writeFlowError(cmd, "flow/start", err)
	}
	if len(nodes) == 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/start",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("flow %q has no nodes; add nodes before starting", f.Name),
			map[string]any{"flow_id": f.ID})
	}

	// Validate source nodes exist.
	edges, err := store.ListEdgesByFlow(ctx, f.ID)
	if err != nil {
		return writeFlowError(cmd, "flow/start", err)
	}
	hasIncoming := make(map[string]bool)
	for _, e := range edges {
		hasIncoming[e.ToNodeID] = true
	}
	var sourceCount int
	for _, n := range nodes {
		if !hasIncoming[n.ID] {
			sourceCount++
		}
	}
	if sourceCount == 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/start",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("flow %q has no source nodes (all nodes have incoming edges)", f.Name),
			map[string]any{"flow_id": f.ID})
	}

	// Check if engine already exists for this flow (shouldn't happen given state checks,
	// but defensive).
	if existingEng := getEngine(f.ID); existingEng != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/start",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("flow %q already has an active engine", f.Name),
			map[string]any{"flow_id": f.ID})
	}

	// Create a long-lived store for the engine (separate from the one we close at end).
	engineStore, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/start", err)
	}

	// Build executors registry.
	flowEngineRegistry.mu.Lock()
	executors := flowEngineRegistry.testExecutors
	flowEngineRegistry.mu.Unlock()
	if executors == nil {
		executors = map[flowmodel.NodeKind]flowmodel.NodeExecutor{
			flowmodel.NodeSkill:     &flowmodel.SkillExecutor{},
			flowmodel.NodeTransform: &flowmodel.TransformExecutor{},
		}
	}

	eng := flowmodel.NewEngine(engineStore, executors, 64)

	// Register engine before starting.
	flowEngineRegistry.mu.Lock()
	runCtx, runCancel := context.WithCancel(context.Background())
	flowEngineRegistry.engines[f.ID] = eng
	flowEngineRegistry.stores[f.ID] = engineStore
	flowEngineRegistry.cancels[f.ID] = runCancel
	flowEngineRegistry.mu.Unlock()

	// Start the engine using the run context (not the CLI command context).
	if err := eng.Start(runCtx, f.ID); err != nil {
		// Clean up on start failure.
		removeEngine(f.ID)
		return writeFlowError(cmd, "flow/start", err)
	}

	// Read updated state from store.
	updatedFlow, _ := store.GetFlow(ctx, f.ID)
	status := eng.Status(f.ID)
	runID := ""
	if status != nil {
		runID = status.RunID
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/start", map[string]any{
		"id":        f.ID,
		"name":      f.Name,
		"state":     updatedFlow.State,
		"run_id":    runID,
		"workspace": workspace,
	})
}

func runFlowStop(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	flowRef := args[0]
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	// Try daemon routing first.
	handled, err := routeFlowViaDaemon(cmd, "stop", flowRef)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	// Fallback: in-process execution.
	fmt.Fprintf(cmd.ErrOrStderr(), "flow: using in-process execution (daemon unavailable)\n")

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/stop", err)
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, flowRef)
	if err != nil {
		return writeFlowError(cmd, "flow/stop", err)
	}

	if f.State != flowmodel.FlowRunning && f.State != flowmodel.FlowPaused && f.State != flowmodel.FlowErrored {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/stop",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("flow %q is not running (state: %s)", f.Name, f.State),
			map[string]any{"flow_id": f.ID, "state": string(f.State)})
	}

	eng := getEngine(f.ID)
	if eng != nil {
		// Engine is active in this process — stop it.
		if err := eng.Stop(f.ID); err != nil {
			return writeFlowError(cmd, "flow/stop", err)
		}
		removeEngine(f.ID)
	} else {
		// No active engine (separate CLI process or engine exited).
		// Update the database state directly.
		f.State = flowmodel.FlowStopped
		if _, err := store.UpdateFlow(ctx, f); err != nil {
			return writeFlowError(cmd, "flow/stop", err)
		}
	}

	updatedFlow, _ := store.GetFlow(ctx, f.ID)
	return protocol.WriteOK(cmd.OutOrStdout(), "flow/stop", map[string]any{
		"id":      f.ID,
		"name":    f.Name,
		"state":   updatedFlow.State,
		"stopped": true,
	})
}

func runFlowPause(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	flowRef := args[0]
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	// Try daemon routing first.
	handled, err := routeFlowViaDaemon(cmd, "pause", flowRef)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	// Fallback: in-process execution.
	fmt.Fprintf(cmd.ErrOrStderr(), "flow: using in-process execution (daemon unavailable)\n")

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/pause", err)
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, flowRef)
	if err != nil {
		return writeFlowError(cmd, "flow/pause", err)
	}

	if f.State != flowmodel.FlowRunning {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/pause",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("flow %q is not running (state: %s)", f.Name, f.State),
			map[string]any{"flow_id": f.ID, "state": string(f.State)})
	}

	eng := getEngine(f.ID)
	if eng == nil {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/pause",
			protocol.ErrorCodeEARG,
			fmt.Sprintf("flow %q has no active engine", f.Name),
			map[string]any{"flow_id": f.ID})
	}

	if err := eng.Pause(f.ID); err != nil {
		return writeFlowError(cmd, "flow/pause", err)
	}

	updatedFlow, _ := store.GetFlow(ctx, f.ID)
	return protocol.WriteOK(cmd.OutOrStdout(), "flow/pause", map[string]any{
		"id":     f.ID,
		"name":   f.Name,
		"state":  updatedFlow.State,
		"paused": true,
	})
}

func runFlowStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	flowRef := args[0]
	workspace := flowResolveWorkspace(flowWorkspaceFlag)

	// Try daemon routing first.
	handled, err := routeFlowViaDaemon(cmd, "status", flowRef)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	// Fallback: in-process execution.
	fmt.Fprintf(cmd.ErrOrStderr(), "flow: using in-process execution (daemon unavailable)\n")

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/status", err)
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, flowRef)
	if err != nil {
		return writeFlowError(cmd, "flow/status", err)
	}

	// Try to get engine status (only available for running flows).
	eng := getEngine(f.ID)
	if eng != nil {
		status := eng.Status(f.ID)
		if status != nil {
			return protocol.WriteOK(cmd.OutOrStdout(), "flow/status", map[string]any{
				"id":         f.ID,
				"name":       f.Name,
				"flow_state": string(status.FlowState),
				"run_id":     status.RunID,
				"nodes":      status.Nodes,
				"edges":      status.Edges,
				"workspace":  workspace,
			})
		}
	}

	// No active engine — return persisted state with idle node/edge states.
	nodes, err := store.ListNodesByFlow(ctx, f.ID)
	if err != nil {
		return writeFlowError(cmd, "flow/status", err)
	}
	if nodes == nil {
		nodes = []flowmodel.FlowNode{}
	}

	edges, err := store.ListEdgesByFlow(ctx, f.ID)
	if err != nil {
		return writeFlowError(cmd, "flow/status", err)
	}
	if edges == nil {
		edges = []flowmodel.FlowEdge{}
	}

	// Build node states from persisted data (all idle since not running).
	nodeStates := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		nodeStates = append(nodeStates, map[string]any{
			"id":    n.ID,
			"label": n.Label,
			"kind":  string(n.Kind),
			"state": "idle",
		})
	}

	edgeStates := make([]map[string]any, 0, len(edges))
	for _, e := range edges {
		edgeStates = append(edgeStates, map[string]any{
			"id":             e.ID,
			"from":           e.FromNodeID,
			"to":             e.ToNodeID,
			"delivery_count": 0,
		})
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/status", map[string]any{
		"id":         f.ID,
		"name":       f.Name,
		"flow_state": string(f.State),
		"nodes":      nodeStates,
		"edges":      edgeStates,
		"workspace":  workspace,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// flowResolveWorkspace returns the absolute path for a workspace flag value.
func flowResolveWorkspace(workspace string) string {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return workspace
	}
	return abs
}

// writeFlowError writes an appropriate error envelope for the given error.
// It maps flow.ErrNotFound to ENOTFOUND and other errors to ERUNTIME.
func writeFlowError(cmd *cobra.Command, command string, err error) error {
	code := protocol.ErrorCodeERuntime
	msg := err.Error()

	if errors.Is(err, flowmodel.ErrNotFound) {
		code = protocol.ErrorCodeENotFound
	} else if errors.Is(err, flowmodel.ErrNameTooLong) {
		code = protocol.ErrorCodeEARG
	} else if strings.Contains(msg, "UNIQUE constraint failed") {
		code = protocol.ErrorCodeEARG
		msg = "flow already exists with this name in the workspace"
	}

	return protocol.WriteError(cmd.OutOrStdout(), command, code, msg, nil)
}
