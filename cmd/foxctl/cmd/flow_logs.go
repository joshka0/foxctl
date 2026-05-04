package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/protocol"
	flowmodel "github.com/joshka0/foxctl/internal/runtime/flow"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Flags
// ---------------------------------------------------------------------------

var (
	flowLogsRunFlag    string
	flowLogsNodeFlag   string
	flowLogsFollowFlag bool
)

// ---------------------------------------------------------------------------
// Command
// ---------------------------------------------------------------------------

var flowLogsCmd = &cobra.Command{
	Use:   "logs [run-id]",
	Short: "Show flow run logs",
	Long: `Display log entries for a flow run. Each entry captures a node's output envelope.

Supports:
  - Positional run-id or --run flag to specify the run
  - --node flag to filter by node ID or label
  - --follow flag for NDJSON streaming of live output
  - --workspace flag (required) to locate the flow database

Output follows the foxctl envelope contract. With --follow, each line is a
valid JSON envelope with status:"progress" for intermediate entries and a
terminal envelope with meta.final:true.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFlowLogs,
}

func init() {
	flowLogsCmd.Flags().StringVar(&flowLogsRunFlag, "run", "", "run ID (alternative to positional argument)")
	flowLogsCmd.Flags().StringVar(&flowLogsNodeFlag, "node", "", "filter by node ID or label")
	flowLogsCmd.Flags().BoolVar(&flowLogsFollowFlag, "follow", false, "stream logs in NDJSON format (replay history, then follow live)")
	flowCmd.AddCommand(flowLogsCmd)
}

// ---------------------------------------------------------------------------
// Implementation
// ---------------------------------------------------------------------------

func runFlowLogs(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	ws := flowResolveWorkspace(flowWorkspaceFlag)

	// Resolve run ID from positional arg or --run flag.
	runID := resolveRunID(args)
	if runID == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/logs",
			protocol.ErrorCodeEARG,
			"run-id is required: provide as positional argument or --run flag",
			nil)
	}

	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return writeFlowError(cmd, "flow/logs", err)
	}
	defer store.Close()

	// Resolve --node label to ID if needed.
	nodeID := ""
	if flowLogsNodeFlag != "" {
		nodeID, err = resolveNodeForLogs(ctx, store, runID, flowLogsNodeFlag)
		if err != nil {
			// If node resolution fails, treat as filter that matches nothing
			// (return empty ok per VAL-M2-053).
			nodeID = flowLogsNodeFlag
		}
	}

	if flowLogsFollowFlag {
		return streamFlowLogs(cmd, ctx, store, runID, nodeID, ws)
	}

	return queryFlowLogs(cmd, ctx, store, runID, nodeID, ws)
}

// resolveRunID determines the run ID from positional args or --run flag.
func resolveRunID(args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	return flowLogsRunFlag
}

// resolveNodeForLogs attempts to resolve a node label to an ID for filtering.
// If the value is a valid node ID (exists in store), returns it as-is.
// If it matches a label in any run's flow, returns the ID.
// Returns empty string if no match found (caller should use as-is for empty filter).
func resolveNodeForLogs(ctx context.Context, store flowmodel.Store, runID, ref string) (string, error) {
	// Try as a node ID first.
	if _, err := store.GetNode(ctx, ref); err == nil {
		return ref, nil
	}

	// Try to find the run to get the flow ID, then search labels.
	run, err := store.GetRun(ctx, runID)
	if err != nil {
		return ref, nil // Can't resolve, use as-is
	}

	nodes, err := store.ListNodesByFlow(ctx, run.FlowID)
	if err != nil {
		return ref, nil
	}

	for _, n := range nodes {
		if n.Label == ref {
			return n.ID, nil
		}
	}

	return ref, nil
}

// queryFlowLogs returns a single envelope with data.logs array of entries.
func queryFlowLogs(cmd *cobra.Command, ctx context.Context, store flowmodel.Store, runID, nodeID, ws string) error {
	// Verify the run exists.
	_, err := store.GetRun(ctx, runID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "flow/logs",
			protocol.ErrorCodeENotFound,
			fmt.Sprintf("run %q not found", runID),
			map[string]any{"run_id": runID})
	}

	// Build options.
	var opts []flowmodel.RunLogOption
	if nodeID != "" {
		opts = append(opts, flowmodel.WithNodeID(nodeID))
	}

	logs, err := store.ListRunLogs(ctx, runID, opts...)
	if err != nil {
		return writeFlowError(cmd, "flow/logs", err)
	}
	if logs == nil {
		logs = []flowmodel.RunLog{}
	}

	// Convert logs to response format.
	entries := make([]map[string]any, 0, len(logs))
	for _, l := range logs {
		entries = append(entries, map[string]any{
			"seq":        l.Seq,
			"node_id":    l.NodeID,
			"envelope":   l.Envelope,
			"created_at": l.CreatedAt,
		})
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "flow/logs", map[string]any{
		"logs":   entries,
		"run_id": runID,
	})
}

// streamFlowLogs streams NDJSON envelopes: historical replay first, then live.
func streamFlowLogs(cmd *cobra.Command, ctx context.Context, store flowmodel.Store, runID, nodeID, ws string) error {
	stdout := cmd.OutOrStdout()

	// Verify the run exists.
	run, err := store.GetRun(ctx, runID)
	if err != nil {
		return protocol.WriteError(stdout, "flow/logs",
			protocol.ErrorCodeENotFound,
			fmt.Sprintf("run %q not found", runID),
			map[string]any{"run_id": runID})
	}

	// Set up context with signal handling.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	go func() {
		<-sigCh
		cancel()
	}()

	// Build options.
	var opts []flowmodel.RunLogOption
	if nodeID != "" {
		opts = append(opts, flowmodel.WithNodeID(nodeID))
	}

	// Start stream from store.
	logCh, err := store.StreamRunLogs(ctx, runID, opts...)
	if err != nil {
		return writeFlowError(cmd, "flow/logs", err)
	}

	seq := 0
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)

	// Stream entries.
	for l := range logCh {
		seq++
		isTerminal := false

		// Build data for this entry.
		data := map[string]any{
			"seq":        l.Seq,
			"node_id":    l.NodeID,
			"envelope":   l.Envelope,
			"created_at": l.CreatedAt,
			"run_id":     runID,
		}

		env := envelope.Envelope{
			Version: envelope.Version,
			Status:  envelope.StatusProgress,
			Command: "flow/logs",
			Data:    data,
			Meta: envelope.Meta{
				TS:  time.Now().UTC().Format(time.RFC3339),
				Seq: intPtr(seq),
			},
		}

		if err := enc.Encode(env); err != nil {
			return fmt.Errorf("flow/logs: write envelope: %w", err)
		}
		_ = isTerminal
	}

	// Check if the run failed — emit error terminal envelope.
	finalStatus := envelope.StatusOK
	finalData := map[string]any{
		"logs":   []any{},
		"run_id": runID,
	}

	if run.State == flowmodel.RunFailed {
		finalStatus = envelope.StatusError
		finalData = map[string]any{
			"run_id": runID,
		}
	}

	// Emit terminal envelope.
	seq++
	terminalEnv := envelope.Envelope{
		Version: envelope.Version,
		Status:  finalStatus,
		Command: "flow/logs",
		Data:    finalData,
		Meta: envelope.Meta{
			TS:    time.Now().UTC().Format(time.RFC3339),
			Seq:   intPtr(seq),
			Final: boolPtr(true),
		},
	}

	if finalStatus == envelope.StatusError {
		terminalEnv.Error = envelope.ErrorFields{
			Code:    string(protocol.ErrorCodeERuntime),
			Message: fmt.Sprintf("run %q failed", runID),
		}
	}

	if err := enc.Encode(terminalEnv); err != nil {
		return fmt.Errorf("flow/logs: write terminal envelope: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }
