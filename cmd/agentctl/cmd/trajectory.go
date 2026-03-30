package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/secrets"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/spf13/cobra"
)

const trajectoryExportCommand = "trajectory.export"

// defaultEventLimit is the default maximum number of events to fetch per trajectory.
// If the result count equals this limit, the output is likely truncated.
const defaultEventLimit = 1000

type trajectoryEpisode struct {
	EpisodeID   string         `json:"episode_id"`
	WorkspaceID string         `json:"workspace_id"`
	TaskID      string         `json:"task_id,omitempty"`
	EpicID      string         `json:"epic_id,omitempty"`
	AgentRole   string         `json:"agent_role,omitempty"`
	Input       map[string]any `json:"input,omitempty"`
	Output      map[string]any `json:"output,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
}

type trajectoryExporter struct {
	store            trajectory.Store
	includeRawTraces bool
}

func (e trajectoryExporter) forEachEpisode(ctx context.Context, filter trajectory.ListFilter, fn func(trajectoryEpisode) error) (int, error) {
	if e.store == nil {
		return 0, fmt.Errorf("trajectory export: store required")
	}
	if filter.WorkspaceID == "" {
		return 0, fmt.Errorf("trajectory export: workspace_id required")
	}

	trajectories, err := e.store.ListTrajectories(ctx, filter)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, t := range trajectories {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		ep, err := e.buildEpisode(ctx, t)
		if err != nil {
			return count, err
		}
		if err := fn(ep); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (e trajectoryExporter) buildEpisode(ctx context.Context, t trajectory.Trajectory) (trajectoryEpisode, error) {
	var ur trajectory.UserRequestCapture
	var err error
	if t.RootRequestID != "" {
		ur, err = e.store.GetUserRequest(ctx, t.WorkspaceID, t.RootRequestID)
		if err != nil {
			return trajectoryEpisode{}, err
		}
	}

	events, err := e.store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: t.ID, Limit: defaultEventLimit})
	if err != nil {
		return trajectoryEpisode{}, err
	}

	// Detect truncation: if we hit the limit, the result is likely incomplete.
	truncated := len(events) == defaultEventLimit

	firstTS, lastTS := eventBounds(events)
	duration := int64(0)
	if !firstTS.IsZero() && !lastTS.IsZero() && lastTS.After(firstTS) {
		duration = lastTS.Sub(firstTS).Milliseconds()
	}

	input := map[string]any{}
	if ur.Text != "" {
		input["user_request"] = secrets.Redact(ur.Text)
	}

	output := map[string]any{}
	output["status"] = mapTrajectoryStatus(t.Status)
	metrics := map[string]any{
		"tool_calls":   countKind(events, trajectory.EventKindToolCall),
		"tool_results": countKind(events, trajectory.EventKindToolResult),
		"duration_ms":  duration,
		"event_count":  len(events),
	}
	if truncated {
		metrics["event_count_truncated"] = true
	}
	output["metrics"] = metrics

	meta := map[string]any{
		"trace_id": t.TraceID,
		"job_id":   t.JobID,
	}
	if e.includeRawTraces {
		artifacts := collectArtifacts(t, events)
		if len(artifacts) > 0 {
			meta["artifacts"] = artifacts
		}
	}

	taskID := ""
	if len(t.TaskIDs) > 0 {
		taskID = t.TaskIDs[0]
	}

	ep := trajectoryEpisode{
		EpisodeID:   t.ID,
		WorkspaceID: t.WorkspaceID,
		TaskID:      taskID,
		EpicID:      t.EpicID,
		AgentRole:   t.AgentRole,
		Input:       secrets.RedactMap(input),
		Output:      secrets.RedactMap(output),
		Meta:        secrets.RedactMap(meta),
	}
	return ep, nil
}

func mapTrajectoryStatus(s trajectory.Status) string {
	switch s {
	case trajectory.StatusOK:
		return "ok"
	case trajectory.StatusError:
		return "failed"
	case trajectory.StatusAborted:
		return "aborted"
	case trajectory.StatusPartial:
		return "aborted"
	default:
		return "failed"
	}
}

func eventBounds(events []trajectory.Event) (time.Time, time.Time) {
	if len(events) == 0 {
		return time.Time{}, time.Time{}
	}
	first := events[0].TS
	last := events[0].TS
	for _, e := range events[1:] {
		if e.TS.Before(first) {
			first = e.TS
		}
		if e.TS.After(last) {
			last = e.TS
		}
	}
	return first, last
}

func countKind(events []trajectory.Event, kind trajectory.EventKind) int {
	count := 0
	for _, e := range events {
		if e.Kind == kind {
			count++
		}
	}
	return count
}

func collectArtifacts(t trajectory.Trajectory, events []trajectory.Event) []string {
	set := map[string]struct{}{}
	if t.ArtifactDigest != "" {
		set[t.ArtifactDigest] = struct{}{}
	}
	for _, e := range events {
		if e.DataArtifact != "" {
			set[e.DataArtifact] = struct{}{}
		}
		if e.Meta != nil && e.Meta.CASDigest != "" {
			set[e.Meta.CASDigest] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func newTrajectoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trajectory",
		Short: "Trajectory capture and export",
	}
	cmd.AddCommand(newTrajectoryExportCommand())
	return cmd
}

func init() {
	rootCmd.AddCommand(newTrajectoryCommand())
}

func newTrajectoryExportCommand() *cobra.Command {
	var (
		workspace        string
		taskID           string
		epicID           string
		agentRole        string
		traceID          string
		status           string
		since            string
		until            string
		limit            int
		format           string
		includeRawTraces bool
		toCAS            bool
		pin              bool
		dryRun           bool
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export stored trajectories as dspy-ready episodes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			start := time.Now()
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return err
			}

			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return writeTrajectoryExportError(out, fmt.Sprintf("resolve workspace: %v", err))
			}

			sinceTS, err := parseExportTime(since)
			if err != nil {
				return writeTrajectoryExportError(out, err.Error())
			}
			untilTS, err := parseExportTime(until)
			if err != nil {
				return writeTrajectoryExportError(out, err.Error())
			}

			filter := trajectory.ListFilter{
				WorkspaceID: absWorkspace,
				TaskID:      strings.TrimSpace(taskID),
				EpicID:      strings.TrimSpace(epicID),
				AgentRole:   strings.TrimSpace(agentRole),
				TraceID:     strings.TrimSpace(traceID),
				Since:       sinceTS,
				Until:       untilTS,
				Limit:       limit,
			}
			if strings.TrimSpace(status) != "" {
				st, err := parseTrajectoryStatus(status)
				if err != nil {
					return writeTrajectoryExportError(out, err.Error())
				}
				filter.Status = st
			}

			if format == "" {
				format = "ndjson"
			}
			if format != "ndjson" {
				return writeTrajectoryExportError(out, "format must be 'ndjson'")
			}

			trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return writeTrajectoryExportError(out, fmt.Sprintf("open trajectory store: %v", err))
			}
			defer func() {
				// Store cleanup in defer; error is not actionable.
				_ = trajStore.Close() //nolint:errcheck
			}()

			exporter := trajectoryExporter{store: trajStore, includeRawTraces: includeRawTraces}

			if toCAS {
				return exportTrajectoryEpisodesToCAS(ctx, out, cfg, exporter, filter, absWorkspace, start, pin, dryRun)
			}

			if dryRun {
				result, err := runTrajectoryExportDryRun(ctx, exporter, filter)
				if err != nil {
					return writeTrajectoryExportError(out, err.Error())
				}
				result["duration_ms"] = time.Since(start).Milliseconds()
				return protocol.WriteOK(out, trajectoryExportCommand, result, protocol.WithSource("run"), protocol.WithWorkspace(absWorkspace))
			}

			return streamEpisodesInline(ctx, out, exporter, filter, absWorkspace, start)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root (used as workspace_id)")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Filter by task id")
	cmd.Flags().StringVar(&epicID, "epic-id", "", "Filter by epic id")
	cmd.Flags().StringVar(&agentRole, "agent-role", "", "Filter by agent role")
	cmd.Flags().StringVar(&traceID, "trace-id", "", "Filter by trace id")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (ok, error, aborted, partial)")
	cmd.Flags().StringVar(&since, "since", "", "Filter trajectories created at/after RFC3339 timestamp")
	cmd.Flags().StringVar(&until, "until", "", "Filter trajectories created at/before RFC3339 timestamp")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum number of trajectories to export")
	cmd.Flags().StringVar(&format, "format", "ndjson", "Export format")
	cmd.Flags().BoolVar(&includeRawTraces, "include-raw-traces", false, "Include referenced CAS digests in episode meta")
	cmd.Flags().BoolVar(&toCAS, "to-cas", false, "Write NDJSON episodes to CAS and return a digest")
	cmd.Flags().BoolVar(&pin, "pin", false, "Pin the exported CAS artifact")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview export without writing to CAS")
	return cmd
}

func writeTrajectoryExportError(w io.Writer, msg string) error {
	// Error envelope writing; error is not actionable since we're already in error path.
	_ = protocol.WriteError(w, trajectoryExportCommand, protocol.ErrorCodeEARG, msg, map[string]any{"hint": "check flags and workspace"}, protocol.WithSource("run")) //nolint:errcheck
	return fmt.Errorf("trajectory export: %s", msg)
}

func parseExportTime(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := timeutil.ParseRFC3339Nano(v)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q", v)
	}
	return t.UTC(), nil
}

func parseTrajectoryStatus(v string) (trajectory.Status, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "ok":
		return trajectory.StatusOK, nil
	case "error":
		return trajectory.StatusError, nil
	case "aborted":
		return trajectory.StatusAborted, nil
	case "partial":
		return trajectory.StatusPartial, nil
	default:
		return "", fmt.Errorf("invalid status %q", v)
	}
}

func runTrajectoryExportDryRun(ctx context.Context, exporter trajectoryExporter, filter trajectory.ListFilter) (map[string]any, error) {
	data := map[string]any{"dry_run": true}
	count, err := exporter.forEachEpisode(ctx, filter, func(_ trajectoryEpisode) error { return nil })
	if err != nil {
		return nil, err
	}
	data["count"] = count
	return data, nil
}

func exportTrajectoryEpisodesToCAS(ctx context.Context, out io.Writer, cfg config.Config, exporter trajectoryExporter, filter trajectory.ListFilter, workspace string, start time.Time, pin, dryRun bool) error {
	jobID := fmt.Sprintf("trajectory-export-%d", start.UTC().UnixNano())
	if dryRun {
		result, err := runTrajectoryExportDryRun(ctx, exporter, filter)
		if err != nil {
			return writeTrajectoryExportError(out, err.Error())
		}
		result["duration_ms"] = time.Since(start).Milliseconds()
		return protocol.WriteOK(out, trajectoryExportCommand, result, protocol.WithSource("run"), protocol.WithWorkspace(workspace), protocol.WithJobID(jobID))
	}

	casStore, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return writeTrajectoryExportError(out, fmt.Sprintf("open cas store: %v", err))
	}
	count, digest, err := writeTrajectoryEpisodesToCAS(ctx, exporter, filter, casStore)
	if err != nil {
		return writeTrajectoryExportError(out, err.Error())
	}
	if pin {
		if err := casStore.Pin(ctx, digest); err != nil {
			return writeTrajectoryExportError(out, fmt.Sprintf("pin cas artifact: %v", err))
		}
	}
	data := map[string]any{
		"summary": map[string]any{
			"count":  count,
			"format": "ndjson",
		},
		"artifact":    digest,
		"duration_ms": time.Since(start).Milliseconds(),
	}
	return protocol.WriteOK(out, trajectoryExportCommand, data, protocol.WithSource("run"), protocol.WithWorkspace(workspace), protocol.WithJobID(jobID), protocol.WithCASDigest(digest))
}

func writeTrajectoryEpisodesToCAS(ctx context.Context, exporter trajectoryExporter, filter trajectory.ListFilter, store interface {
	Put(ctx context.Context, r io.Reader, kind string, tags []string) (cas.Object, error)
},
) (int, string, error) {
	pr, pw := io.Pipe()
	type result struct {
		count int
		err   error
	}
	resCh := make(chan result, 1)

	go func() {
		enc := json.NewEncoder(pw)
		enc.SetEscapeHTML(false)
		count, err := exporter.forEachEpisode(ctx, filter, func(ep trajectoryEpisode) error {
			return enc.Encode(ep)
		})
		if err != nil {
			_ = pw.CloseWithError(err)
			resCh <- result{count: count, err: err}
			return
		}
		_ = pw.Close()
		resCh <- result{count: count, err: nil}
	}()

	obj, err := store.Put(ctx, pr, "application/x-ndjson", []string{"trajectory.export"})
	_ = pr.Close()
	res := <-resCh
	if err != nil {
		return res.count, "", err
	}
	if res.err != nil {
		return res.count, "", res.err
	}
	return res.count, obj.Digest, nil
}

func streamEpisodesInline(ctx context.Context, out io.Writer, exporter trajectoryExporter, filter trajectory.ListFilter, workspace string, start time.Time) error {
	seq := 0
	count, err := exporter.forEachEpisode(ctx, filter, func(ep trajectoryEpisode) error {
		seq++
		seqVal := seq
		finalVal := false
		data := map[string]any{"episode": ep}
		return protocol.WriteOK(out, trajectoryExportCommand, data,
			protocol.WithSource("run"),
			protocol.WithWorkspace(workspace),
			protocol.WithMetaMutator(func(m *envelope.Meta) {
				m.Seq = &seqVal
				m.Final = &finalVal
			}),
		)
	})
	if err != nil {
		return err
	}

	seq++
	seqVal := seq
	finalVal := true
	summary := map[string]any{
		"count":       count,
		"duration_ms": time.Since(start).Milliseconds(),
	}
	data := map[string]any{"summary": summary}
	return protocol.WriteOK(out, trajectoryExportCommand, data,
		protocol.WithSource("run"),
		protocol.WithWorkspace(workspace),
		protocol.WithMetaMutator(func(m *envelope.Meta) {
			m.Seq = &seqVal
			m.Final = &finalVal
		}),
	)
}
