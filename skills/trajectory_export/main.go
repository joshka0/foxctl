// Package main implements the trajectory.export skill for exporting agent execution trajectories with filtering and CAS storage.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/platform/secrets"
	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

// input defines the skill input parameters for trajectory export with comprehensive filtering and output options.
type input struct {
	WorkspaceID      string `json:"workspace_id"`
	TaskID           string `json:"task_id,omitempty"`
	EpicID           string `json:"epic_id,omitempty"`
	AgentRole        string `json:"agent_role,omitempty"`
	TraceID          string `json:"trace_id,omitempty"`
	Status           string `json:"status,omitempty"`
	Since            string `json:"since,omitempty"`
	Until            string `json:"until,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	IncludeRawTraces bool   `json:"include_raw_traces,omitempty"`
	Pin              bool   `json:"pin,omitempty"`
	DryRun           bool   `json:"dry_run,omitempty"`
	CLICommand       string `json:"cli_command,omitempty"`
}

// trajectoryEpisode represents a single trajectory episode with input/output metadata and execution metrics.
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

// exporter provides trajectory data export functionality with episode building and raw trace inclusion.
type exporter struct {
	store            trajectory.Store
	includeRawTraces bool
}

// forEachEpisode iterates over trajectories matching the filter and applies the provided function to each episode.
func (e exporter) forEachEpisode(ctx context.Context, filter trajectory.ListFilter, fn func(trajectoryEpisode) error) (int, error) {
	if e.store == nil {
		return 0, fmt.Errorf("trajectory export: store required")
	}
	if strings.TrimSpace(filter.WorkspaceID) == "" {
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

// buildEpisode constructs a trajectory episode from trajectory data with event processing and metric calculation.
func (e exporter) buildEpisode(ctx context.Context, t trajectory.Trajectory) (trajectoryEpisode, error) {
	var ur trajectory.UserRequestCapture
	var err error
	if t.RootRequestID != "" {
		ur, err = e.store.GetUserRequest(ctx, t.WorkspaceID, t.RootRequestID)
		if err != nil {
			return trajectoryEpisode{}, err
		}
	}

	events, err := e.store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: t.ID, Limit: 1000})
	if err != nil {
		return trajectoryEpisode{}, err
	}

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
	output["metrics"] = map[string]any{
		"tool_calls":   countKind(events, trajectory.EventKindToolCall),
		"tool_results": countKind(events, trajectory.EventKindToolResult),
		"duration_ms":  duration,
		"event_count":  len(events),
	}

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

// mapTrajectoryStatus converts internal trajectory status to human-readable string format.
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

// eventBounds determines the first and last timestamps from a slice of trajectory events.
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

// countKind counts the number of events of a specific kind in the trajectory events slice.
func countKind(events []trajectory.Event, kind trajectory.EventKind) int {
	count := 0
	for _, e := range events {
		if e.Kind == kind {
			count++
		}
	}
	return count
}

// collectArtifacts gathers all artifact digests from trajectory and events into a deduplicated slice.
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
	sortStrings(out)
	return out
}

// sortStrings sorts a string slice in-place using a simple bubble sort algorithm.
func sortStrings(v []string) {
	if len(v) < 2 {
		return
	}
	for i := 0; i < len(v)-1; i++ {
		for j := i + 1; j < len(v); j++ {
			if v[j] < v[i] {
				v[i], v[j] = v[j], v[i]
			}
		}
	}
}

const command = "trajectory.export"

// main is the skill entry point for trajectory.export with comprehensive data export capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates trajectory export with filtering, validation, and CAS storage with optional pinning.
//
// Index:
//   Purpose: Export agent execution trajectories with filtering, metrics, and CAS storage for analysis and backup
//   Keywords: trajectory/export, data_export, cas_storage, trajectory_analysis, agent_execution
//   Related: forEachEpisode, buildEpisode, writeEpisodesToCAS, estimateEpisodesBytes
//   Flow: validate input → parse parameters → open trajectory store → apply filters → export episodes → store in CAS → emit results
//   Resources: trajectory store, CAS store
//   Events: none
//   OutputFields: summary.count, summary.format, artifact
//
// [[domain:trajectory_export]]
// [[invariant:secret_redaction]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Normalize input
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.TaskID = strings.TrimSpace(in.TaskID)
	in.EpicID = strings.TrimSpace(in.EpicID)
	in.AgentRole = strings.TrimSpace(in.AgentRole)
	in.TraceID = strings.TrimSpace(in.TraceID)
	in.Status = strings.TrimSpace(in.Status)
	in.Since = strings.TrimSpace(in.Since)
	in.Until = strings.TrimSpace(in.Until)
	if in.Limit <= 0 {
		in.Limit = 100
	}

	if in.WorkspaceID == "" {
		return fmt.Errorf("workspace_id required")
	}

	sinceTS, err := parseExportTime(in.Since)
	if err != nil {
		return err
	}
	untilTS, err := parseExportTime(in.Until)
	if err != nil {
		return err
	}
	st, err := parseTrajectoryStatus(in.Status)
	if err != nil {
		return err
	}

	store, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return err
	}
	defer store.Close()

	filter := trajectory.ListFilter{
		WorkspaceID: in.WorkspaceID,
		TaskID:      in.TaskID,
		EpicID:      in.EpicID,
		AgentRole:   in.AgentRole,
		TraceID:     in.TraceID,
		Since:       sinceTS,
		Until:       untilTS,
		Limit:       in.Limit,
		Status:      st,
	}

	exp := exporter{store: store, includeRawTraces: in.IncludeRawTraces}

	if in.DryRun {
		count, estimatedBytes, err := estimateEpisodesBytes(ctx, exp, filter)
		if err != nil {
			return err
		}
		data := map[string]any{
			"dry_run": true,
			"summary": map[string]any{
				"count":           count,
				"estimated_bytes": estimatedBytes,
			},
		}
		return skillout.Emit(rc, command, secrets.RedactMap(data))
	}

	count, digest, err := writeEpisodesToCAS(ctx, exp, filter, rc.CASStore)
	if err != nil {
		return err
	}
	if in.Pin {
		if err := rc.CASStore.Pin(ctx, digest); err != nil {
			return err
		}
	}

	data := map[string]any{
		"summary": map[string]any{
			"count":  count,
			"format": "ndjson",
		},
		"artifact": digest,
	}

	return skillout.Emit(rc, command, secrets.RedactMap(data))
}

// parseExportTime parses a timestamp string into UTC time with RFC3339Nano format validation.
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

// parseTrajectoryStatus converts a string status to internal trajectory.Status enum with validation.
func parseTrajectoryStatus(v string) (trajectory.Status, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return "", nil
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

// countingWriter is a writer that counts bytes written for size estimation without actual storage.
type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

// estimateEpisodesBytes estimates the total byte size of episodes that would be exported without writing to storage.
func estimateEpisodesBytes(ctx context.Context, exp exporter, filter trajectory.ListFilter) (int, int64, error) {
	cw := &countingWriter{}
	enc := json.NewEncoder(cw)
	enc.SetEscapeHTML(false)
	count, err := exp.forEachEpisode(ctx, filter, func(ep trajectoryEpisode) error {
		return enc.Encode(ep)
	})
	if err != nil {
		return count, 0, err
	}
	return count, cw.n, nil
}

// writeEpisodesToCAS writes trajectory episodes to CAS storage as NDJSON with concurrent streaming.
func writeEpisodesToCAS(ctx context.Context, exp exporter, filter trajectory.ListFilter, store interface {
	Put(ctx context.Context, r io.Reader, kind string, tags []string) (storage.CASObject, error)
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
		count, err := exp.forEachEpisode(ctx, filter, func(ep trajectoryEpisode) error {
			return enc.Encode(ep)
		})
		if err != nil {
			_ = pw.CloseWithError(err)
			resCh <- result{count: count, err: err}
			return
		}
		pw.Close()
		resCh <- result{count: count, err: nil}
	}()

	obj, err := store.Put(ctx, pr, "application/x-ndjson", []string{"trajectory.export"})
	pr.Close()
	res := <-resCh
	if err != nil {
		return res.count, "", err
	}
	if res.err != nil {
		return res.count, "", res.err
	}
	return res.count, obj.Digest, nil
}
