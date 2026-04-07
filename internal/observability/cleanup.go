package observability

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DeleteEventOptions configures destructive cleanup of persisted observability events.
type DeleteEventOptions struct {
	ObsDir          string
	Since           time.Time
	Component       string
	OperationPrefix string
	WorkspaceID     string
	WorkspaceIDs    []string
	ErrorsOnly      bool
	TextQuery       string
	SessionID       string
	TraceIDs        []string
	DryRun          bool
}

// DeleteEventResult summarizes a persisted event cleanup operation.
type DeleteEventResult struct {
	EventsDeleted  int64    `json:"events_deleted"`
	EventsKept     int64    `json:"events_kept"`
	FilesProcessed int      `json:"files_processed"`
	Errors         []string `json:"errors,omitempty"`
}

// DeleteEventRecords removes persisted observability events that match the provided filters.
func DeleteEventRecords(ctx context.Context, opts DeleteEventOptions) (*DeleteEventResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	obsDir := strings.TrimSpace(opts.ObsDir)
	if obsDir == "" {
		obsDir = ResolveObsDir()
	}
	if obsDir == "" {
		return &DeleteEventResult{}, nil
	}

	eventsDir := filepath.Join(obsDir, "events")
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &DeleteEventResult{}, nil
		}
		return nil, fmt.Errorf("read events directory: %w", err)
	}

	workspaceFilters := opts.WorkspaceIDs
	if len(workspaceFilters) == 0 && strings.TrimSpace(opts.WorkspaceID) != "" {
		workspaceFilters = []string{strings.TrimSpace(opts.WorkspaceID)}
	}

	result := &DeleteEventResult{}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".ndjson" {
			continue
		}

		filePath := filepath.Join(eventsDir, entry.Name())
		deleted, kept, err := deleteMatchingEventsFromFile(ctx, filePath, opts.Since, opts.Component, opts.OperationPrefix, workspaceFilters, opts.ErrorsOnly, opts.TextQuery, opts.SessionID, opts.TraceIDs, opts.DryRun)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entry.Name(), err))
			continue
		}
		result.EventsDeleted += deleted
		result.EventsKept += kept
		result.FilesProcessed++
	}

	return result, nil
}

func deleteMatchingEventsFromFile(ctx context.Context, filePath string, sinceTime time.Time, componentFilter, operationFilter string, workspaceFilters []string, errorsOnly bool, textQuery, sessionID string, traceIDs []string, dryRun bool) (int64, int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return 0, 0, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var keptLines [][]byte
	var deletedCount int64
	var keptCount int64

	for scanner.Scan() {
		if ctx.Err() != nil {
			return deletedCount, keptCount, ctx.Err()
		}

		line := append([]byte(nil), scanner.Bytes()...)
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}

		var event WideEvent
		if err := json.Unmarshal(line, &event); err != nil {
			keptLines = append(keptLines, line)
			keptCount++
			continue
		}

		if matchesEventFilters(event, sinceTime, componentFilter, operationFilter, workspaceFilters, errorsOnly, textQuery, sessionID, traceIDs) {
			deletedCount++
			continue
		}

		keptLines = append(keptLines, line)
		keptCount++
	}
	if err := scanner.Err(); err != nil {
		return deletedCount, keptCount, fmt.Errorf("scan file: %w", err)
	}

	if dryRun || deletedCount == 0 {
		return deletedCount, keptCount, nil
	}

	tmpPath := filePath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return deletedCount, keptCount, fmt.Errorf("create temp file: %w", err)
	}

	for _, line := range keptLines {
		if _, err := tmpFile.Write(line); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return deletedCount, keptCount, fmt.Errorf("write event: %w", err)
		}
		if _, err := tmpFile.WriteString("\n"); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return deletedCount, keptCount, fmt.Errorf("write newline: %w", err)
		}
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return deletedCount, keptCount, fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return deletedCount, keptCount, fmt.Errorf("rename temp file: %w", err)
	}

	return deletedCount, keptCount, nil
}
