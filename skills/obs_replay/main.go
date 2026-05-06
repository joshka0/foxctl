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

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

const command = "obs/replay"

// input defines the skill input parameters for trace event reconstruction with filtering options.
type input struct {
	TraceID     string `json:"trace_id" validate:"required"`
	SpanID      string `json:"span_id,omitempty"`
	IncludeData bool   `json:"include_data,omitempty"` // Include full artifact data
}

// reconstructedEvent contains the event plus any fetched artifacts with full data reconstruction.
type reconstructedEvent struct {
	Event     observability.Event `json:"event"`
	Artifacts map[string]any      `json:"artifacts,omitempty"`
}

// trajectoryEvent contains trajectory data with full payload and artifact fetching capabilities.
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

// main is the skill entry point for obs/replay with trace event reconstruction capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates trace event reconstruction from multiple sources with artifact fetching.
//
// Index:
//   Purpose: Reconstruct events from trace IDs by combining observability events and trajectory data with optional artifact fetching
//   Keywords: obs/replay, trace_reconstruction, event_timeline, artifact_fetching, observability
//   Related: findEvents, findTrajectoryEvents, fetchEventArtifacts, fetchCASContent
//   Flow: locate observability directory → find events → query trajectory events → fetch artifacts → emit results
//   Resources: NDJSON log files, trajectory store, CAS store
//   Events: trace reconstruction events
//   OutputFields: trace_id, event_count, traj_event_count, events, trajectory_events, summary
// [[domain:trace-reconstruction]]
// [[protocol:observability-event-envelope]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Find the events file
	obsDir := rc.Config.Paths.Observability
	if obsDir == "" {
		obsDir = os.Getenv("FOXCTL_OBS_DIR")
	}
	if obsDir == "" {
		obsDir = filepath.Join(os.Getenv("HOME"), ".foxctl", "observability")
	}
	eventsFile := filepath.Join(obsDir, "events", observability.EventFileName+".ndjson")

	// Find matching events
	var events []observability.Event
	if _, err := os.Stat(eventsFile); err == nil {
		events, _ = findEvents(eventsFile, in.TraceID, in.SpanID)
	}

	// Find matching trajectory events
	var trajEvents []trajectoryEvent
	workspaceID := rc.Workspace
	if workspaceID == "" && len(events) > 0 {
		workspaceID = observability.EventDataString(&events[0], observability.DataKeyWorkspaceID)
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

	// Reconstruct events with artifacts
	var reconstructed []reconstructedEvent
	for _, evt := range events {
		result := reconstructedEvent{Event: evt}

		if in.IncludeData {
			artifacts, err := fetchEventArtifacts(ctx, rc, evt)
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
	if len(events) > 0 {
		primary := events[0]
		summary = fmt.Sprintf("Found %d event(s), %d trajectory event(s) for trace %s: %s %s (%s, %dms)",
			len(events), len(trajEvents),
			truncateID(in.TraceID),
			primary.Operation, primary.Name,
			primary.Status, primary.Duration.Milliseconds())
	} else if len(trajEvents) > 0 {
		summary = fmt.Sprintf("Found %d trajectory event(s) for trace %s", len(trajEvents), truncateID(in.TraceID))
	} else {
		return fmt.Errorf("no events found for trace_id=%s", in.TraceID)
	}

	data := map[string]any{
		"trace_id":          in.TraceID,
		"event_count":       len(reconstructed),
		"traj_event_count":  len(trajEvents),
		"events":            reconstructed,
		"trajectory_events": trajEvents,
		"summary":           summary,
	}

	return skillout.Emit(rc, command, data)
}

// truncateID truncates long IDs for display while preserving readability with ellipsis.
func truncateID(id string) string {
	if len(id) > 16 {
		return id[:16] + "..."
	}
	return id
}

// findEvents searches NDJSON log file for events matching trace ID and optional span ID.
func findEvents(path, traceID, spanID string) ([]observability.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { errs.Ignore(f.Close(), "close events file") }()

	var events []observability.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var evt observability.Event
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

// findTrajectoryEvents queries trajectory store for events matching the specified trace ID.
// It opens the trajectory store, queries for events by trace ID, and returns the results.
func findTrajectoryEvents(ctx context.Context, rc *skillmain.RunContext, workspaceID, traceID string) ([]trajectory.Event, error) {
	store, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return nil, fmt.Errorf("open trajectory store: %w", err)
	}
	defer func() { errs.Ignore(store.Close(), "close trajectory store") }()

	return store.GetEventsByTraceID(ctx, workspaceID, traceID)
}

// fetchEventArtifacts retrieves CAS artifacts referenced in event data with error handling.
// It fetches artifacts from the CAS store, handling errors and returning the artifacts as a map.
func fetchEventArtifacts(ctx context.Context, rc *skillmain.RunContext, evt observability.Event) (map[string]any, error) {
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

// fetchCASContent retrieves and parses CAS content with support for JSON, NDJSON, and text formats.
// It fetches content from the CAS store, determines the content type, and parses it accordingly.
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
