// Package main implements the obs/replay skill for reconstructing events from trace IDs.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

const command = "obs/replay"

type input struct {
	TraceID     string `json:"trace_id" validate:"required"`
	SpanID      string `json:"span_id,omitempty"`
	IncludeData bool   `json:"include_data,omitempty"` // Include full artifact data
}

// wideEvent represents a wide event from the NDJSON log.
type wideEvent struct {
	Timestamp   string         `json:"ts"`
	TraceID     string         `json:"trace_id"`
	SpanID      string         `json:"span_id"`
	Service     string         `json:"service"`
	Version     string         `json:"version"`
	Component   string         `json:"component"`
	Operation   string         `json:"operation"`
	Command     string         `json:"command"`
	WorkspaceID string         `json:"workspace_id"`
	JobID       string         `json:"job_id"`
	Status      string         `json:"status"`
	DurationMS  int64          `json:"duration_ms"`
	ErrorCode   string         `json:"error_code,omitempty"`
	ErrorMsg    string         `json:"error_message,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
}

// reconstructedEvent contains the event plus any fetched artifacts.
type reconstructedEvent struct {
	Event     wideEvent      `json:"event"`
	Artifacts map[string]any `json:"artifacts,omitempty"`
}

// trajectoryEvent contains trajectory data with full payload.
type trajectoryEvent struct {
	ID           string         `json:"id"`
	TrajectoryID string         `json:"trajectory_id"`
	Timestamp    string         `json:"ts"`
	Kind         string         `json:"kind"`
	Actor        string         `json:"actor,omitempty"`
	Command      string         `json:"command,omitempty"`
	Status       string         `json:"status,omitempty"`
	DataInline   map[string]any `json:"data_inline,omitempty"`
	DataArtifact string         `json:"data_artifact,omitempty"`
	FullData     any            `json:"full_data,omitempty"` // Fetched from CAS
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Find the events file
	obsDir := rc.Config.Paths.Observability
	if obsDir == "" {
		obsDir = os.Getenv("AGENTCTL_OBS_DIR")
	}
	if obsDir == "" {
		obsDir = filepath.Join(os.Getenv("HOME"), ".agentctl", "observability")
	}
	eventsFile := filepath.Join(obsDir, "events", "wide_events.ndjson")

	// Find matching wide events
	var wideEvents []wideEvent
	if _, err := os.Stat(eventsFile); err == nil {
		wideEvents, _ = findWideEvents(eventsFile, in.TraceID, in.SpanID)
	}

	// Find matching trajectory events
	var trajEvents []trajectoryEvent
	workspaceID := rc.Workspace
	if workspaceID == "" && len(wideEvents) > 0 {
		workspaceID = wideEvents[0].WorkspaceID
	}

	if workspaceID != "" {
		events, err := findTrajectoryEvents(ctx, rc, workspaceID, in.TraceID)
		if err != nil {
			rc.Logger.Warn().Err(err).Msg("trajectory query failed")
		}
		for _, evt := range events {
			te := trajectoryEvent{
				ID:           evt.ID,
				TrajectoryID: evt.TrajectoryID,
				Timestamp:    evt.TS.Format("2006-01-02T15:04:05.000Z"),
				Kind:         string(evt.Kind),
				Actor:        evt.Actor,
				Command:      evt.Command,
				Status:       evt.Status,
				DataInline:   evt.DataInline,
				DataArtifact: evt.DataArtifact,
			}

			// Fetch full data from CAS if requested
			if in.IncludeData && evt.DataArtifact != "" {
				content, err := fetchCASContent(ctx, rc, evt.DataArtifact)
				if err != nil {
					rc.Logger.Warn().Err(err).Str("artifact", evt.DataArtifact).Msg("failed to fetch artifact")
				} else {
					te.FullData = content
				}
			}

			trajEvents = append(trajEvents, te)
		}
	}

	// Reconstruct wide events with artifacts
	var reconstructed []reconstructedEvent
	for _, evt := range wideEvents {
		result := reconstructedEvent{Event: evt}

		if in.IncludeData {
			artifacts, err := fetchWideEventArtifacts(ctx, rc, evt)
			if err != nil {
				rc.Logger.Warn().Err(err).Str("span_id", evt.SpanID).Msg("failed to fetch artifacts")
			}
			if len(artifacts) > 0 {
				result.Artifacts = artifacts
			}
		}

		reconstructed = append(reconstructed, result)
	}

	// Build summary
	var summary string
	if len(wideEvents) > 0 {
		primary := wideEvents[0]
		summary = fmt.Sprintf("Found %d wide event(s), %d trajectory event(s) for trace %s: %s %s (%s, %dms)",
			len(wideEvents), len(trajEvents),
			truncateID(in.TraceID),
			primary.Operation, primary.Command,
			primary.Status, primary.DurationMS)
	} else if len(trajEvents) > 0 {
		summary = fmt.Sprintf("Found %d trajectory event(s) for trace %s", len(trajEvents), truncateID(in.TraceID))
	} else {
		return fmt.Errorf("no events found for trace_id=%s", in.TraceID)
	}

	data := map[string]any{
		"trace_id":          in.TraceID,
		"wide_event_count":  len(reconstructed),
		"traj_event_count":  len(trajEvents),
		"wide_events":       reconstructed,
		"trajectory_events": trajEvents,
		"summary":           summary,
	}

	return skillout.Emit(rc, command, data)
}

func truncateID(id string) string {
	if len(id) > 16 {
		return id[:16] + "..."
	}
	return id
}

func findWideEvents(path, traceID, spanID string) ([]wideEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { errs.Ignore(f.Close(), "close events file") }()

	var events []wideEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var evt wideEvent
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}

		if evt.TraceID != traceID {
			continue
		}

		if spanID != "" && evt.SpanID != spanID {
			continue
		}

		events = append(events, evt)
	}

	return events, scanner.Err()
}

func findTrajectoryEvents(ctx context.Context, rc *skillmain.RunContext, workspaceID, traceID string) ([]trajectory.Event, error) {
	store, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return nil, fmt.Errorf("open trajectory store: %w", err)
	}
	defer func() { errs.Ignore(store.Close(), "close trajectory store") }()

	return store.GetEventsByTraceID(ctx, workspaceID, traceID)
}

func fetchWideEventArtifacts(ctx context.Context, rc *skillmain.RunContext, evt wideEvent) (map[string]any, error) {
	if evt.Data == nil || rc.CASStore == nil {
		return nil, nil
	}

	artifacts := make(map[string]any)

	artifactKeys := []string{
		"input_artifact",
		"result_artifact",
		"stderr_artifact",
	}

	for _, key := range artifactKeys {
		digest, ok := evt.Data[key].(string)
		if !ok || digest == "" {
			continue
		}

		content, err := fetchCASContent(ctx, rc, digest)
		if err != nil {
			artifacts[key] = map[string]any{
				"digest": digest,
				"error":  err.Error(),
			}
			continue
		}

		artifacts[key] = map[string]any{
			"digest":  digest,
			"content": content,
		}
	}

	return artifacts, nil
}

func fetchCASContent(ctx context.Context, rc *skillmain.RunContext, digest string) (any, error) {
	reader, meta, err := rc.CASStore.Get(ctx, digest)
	if err != nil {
		return nil, fmt.Errorf("cas get: %w", err)
	}
	defer func() { errs.Ignore(reader.Close(), "close cas reader") }()

	const maxSize = 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(reader, maxSize))
	if err != nil {
		return nil, fmt.Errorf("read content: %w", err)
	}

	// Handle JSON
	if meta.Kind == "application/json" || strings.HasSuffix(meta.Kind, "+json") {
		var parsed any
		if err := json.Unmarshal(data, &parsed); err == nil {
			return parsed, nil
		}
	}

	// Handle NDJSON (newline-delimited JSON)
	if meta.Kind == "application/x-ndjson" || strings.Contains(meta.Kind, "ndjson") {
		var lines []any
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			var obj any
			if err := json.Unmarshal([]byte(line), &obj); err == nil {
				lines = append(lines, obj)
			}
		}
		if len(lines) > 0 {
			return lines, nil
		}
	}

	// Handle text content
	if strings.HasPrefix(meta.Kind, "text/") {
		return string(data), nil
	}

	// Return metadata for binary content
	return map[string]any{
		"type": "binary",
		"kind": meta.Kind,
		"size": len(data),
	}, nil
}
