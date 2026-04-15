package observability

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EventQueryOptions controls filtering for observability event queries.
type EventQueryOptions struct {
	ObsDir          string
	Limit           int
	Since           time.Time
	Component       string
	OperationPrefix string
	WorkspaceID     string
	WorkspaceIDs    []string
	ErrorsOnly      bool
	TextQuery       string
	SessionID       string
	TraceIDs        []string
}

// EventRecord is the JSON-friendly shape used by the GUI and CLI error/log views.
type EventRecord struct {
	Timestamp    string         `json:"ts"`
	Operation    string         `json:"operation"`
	Command      string         `json:"command,omitempty"`
	Status       string         `json:"status"`
	Component    string         `json:"component,omitempty"`
	TraceID      string         `json:"trace_id,omitempty"`
	SpanID       string         `json:"span_id,omitempty"`
	ParentID     string         `json:"parent_id,omitempty"`
	Service      string         `json:"service,omitempty"`
	Version      string         `json:"version,omitempty"`
	Subtype      string         `json:"subtype,omitempty"`
	SessionID    string         `json:"session_id,omitempty"`
	AgentID      string         `json:"agent_id,omitempty"`
	WorkspaceID  string         `json:"workspace_id,omitempty"`
	JobID        string         `json:"job_id,omitempty"`
	DurationMS   int64          `json:"duration_ms,omitempty"`
	ErrorType    string         `json:"error_type,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	Retriable    *bool          `json:"retriable,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
}

type timedEventRecord struct {
	EventRecord
	parsedAt time.Time
}

// ResolveObsDir returns the configured observability directory or the default path.
func ResolveObsDir() string {
	if dir := strings.TrimSpace(os.Getenv("FOXCTL_OBS_DIR")); dir != "" {
		return dir
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".foxctl", "observability")
}

// QueryEventRecords returns observability events in the same shape consumed by the GUI logs view.
func QueryEventRecords(ctx context.Context, opts EventQueryOptions) ([]EventRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	obsDir := strings.TrimSpace(opts.ObsDir)
	if obsDir == "" {
		obsDir = ResolveObsDir()
	}
	if obsDir == "" {
		return []EventRecord{}, nil
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	workspaceFilters := opts.WorkspaceIDs
	if len(workspaceFilters) == 0 && strings.TrimSpace(opts.WorkspaceID) != "" {
		workspaceFilters = []string{strings.TrimSpace(opts.WorkspaceID)}
	}

	return readEventRecords(ctx, filepath.Join(obsDir, "events"), limit, opts.Since, opts.Component, opts.OperationPrefix, workspaceFilters, opts.ErrorsOnly, opts.TextQuery, opts.SessionID, opts.TraceIDs)
}

func readEventRecords(ctx context.Context, eventsDir string, limit int, sinceTime time.Time, componentFilter, operationFilter string, workspaceFilters []string, errorsOnly bool, textQuery, sessionID string, traceIDs []string) ([]EventRecord, error) {
	files, err := filepath.Glob(filepath.Join(eventsDir, "*.ndjson*"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return []EventRecord{}, nil
	}

	type fileMeta struct {
		modTime time.Time
		ok      bool
	}
	metas := make(map[string]fileMeta, len(files))
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil || info == nil {
			metas[path] = fileMeta{}
			continue
		}
		metas[path] = fileMeta{modTime: info.ModTime(), ok: true}
	}

	sort.SliceStable(files, func(i, j int) bool {
		metaI := metas[files[i]]
		metaJ := metas[files[j]]
		if !metaI.ok || !metaJ.ok {
			return files[i] < files[j]
		}
		if metaI.modTime.Equal(metaJ.modTime) {
			return files[i] < files[j]
		}
		return metaI.modTime.After(metaJ.modTime)
	})

	var entries []timedEventRecord
	internalLimit := limit * 20
	if internalLimit < 500 {
		internalLimit = 500
	}

	for _, file := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if len(entries) >= internalLimit {
			break
		}

		if !strings.HasSuffix(file, ".gz") {
			fileEntries, partial, err := readEventFileTail(ctx, file, internalLimit-len(entries), sinceTime, componentFilter, operationFilter, workspaceFilters, errorsOnly, textQuery, sessionID, traceIDs)
			if err == nil && !partial {
				entries = append(entries, fileEntries...)
				continue
			}
		}

		fileEntries, err := readEventFile(ctx, file, internalLimit-len(entries), sinceTime, componentFilter, operationFilter, workspaceFilters, errorsOnly, textQuery, sessionID, traceIDs)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			continue
		}
		entries = append(entries, fileEntries...)
	}

	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].parsedAt.Equal(entries[j].parsedAt) {
			return entries[i].parsedAt.After(entries[j].parsedAt)
		}
		if entries[i].TraceID != entries[j].TraceID {
			return entries[i].TraceID > entries[j].TraceID
		}
		return entries[i].SpanID > entries[j].SpanID
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	if len(entries) == 0 {
		return []EventRecord{}, nil
	}
	out := make([]EventRecord, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.EventRecord)
	}
	return out, nil
}

func readEventFileTail(ctx context.Context, path string, limit int, sinceTime time.Time, componentFilter, operationFilter string, workspaceFilters []string, errorsOnly bool, textQuery, sessionID string, traceIDs []string) ([]timedEventRecord, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, false, err
	}

	chunkSize := int64(512 * 1024)
	fileSize := stat.Size()
	if fileSize < chunkSize {
		chunkSize = fileSize
	}
	start := fileSize - chunkSize

	buf := make([]byte, chunkSize)
	n, err := file.ReadAt(buf, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	buf = buf[:n]

	if start > 0 {
		if newlineIdx := bytes.IndexByte(buf, '\n'); newlineIdx >= 0 {
			buf = buf[newlineIdx+1:]
		} else {
			return []timedEventRecord{}, true, nil
		}
	}

	lines := strings.Split(string(buf), "\n")
	var entries []timedEventRecord
	for i := len(lines) - 1; i >= 0 && len(entries) < limit; i-- {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		default:
		}

		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		var event WideEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if !matchesEventFilters(event, sinceTime, componentFilter, operationFilter, workspaceFilters, errorsOnly, textQuery, sessionID, traceIDs) {
			continue
		}
		entries = append(entries, eventToRecord(event))
	}
	return entries, start > 0 && len(entries) < limit, nil
}

func readEventFile(ctx context.Context, path string, limit int, sinceTime time.Time, componentFilter, operationFilter string, workspaceFilters []string, errorsOnly bool, textQuery, sessionID string, traceIDs []string) ([]timedEventRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var reader *bufio.Scanner
	if strings.HasSuffix(path, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, err
		}
		defer gzReader.Close()
		reader = bufio.NewScanner(gzReader)
	} else {
		reader = bufio.NewScanner(file)
	}
	reader.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var entries []timedEventRecord
	for reader.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := reader.Bytes()
		if len(line) == 0 {
			continue
		}

		var event WideEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if !matchesEventFilters(event, sinceTime, componentFilter, operationFilter, workspaceFilters, errorsOnly, textQuery, sessionID, traceIDs) {
			continue
		}
		entries = append(entries, eventToRecord(event))
		if len(entries) > limit {
			entries = entries[1:]
		}
	}
	return entries, reader.Err()
}

func matchesEventFilters(event WideEvent, sinceTime time.Time, componentFilter, operationFilter string, workspaceFilters []string, errorsOnly bool, textQuery, sessionID string, traceIDs []string) bool {
	if !sinceTime.IsZero() && event.Ts.Before(sinceTime) {
		return false
	}
	if componentFilter != "" && event.Component != componentFilter {
		return false
	}
	if operationFilter != "" && !strings.HasPrefix(event.Operation, operationFilter) {
		return false
	}
	if len(workspaceFilters) > 0 && !matchesWorkspaceFilter(event.WorkspaceID, workspaceFilters) {
		return false
	}
	if errorsOnly && event.Status != StatusError {
		return false
	}
	if trimmedSessionID := strings.TrimSpace(sessionID); trimmedSessionID != "" && strings.TrimSpace(event.SessionID) != trimmedSessionID {
		return false
	}
	if len(traceIDs) > 0 && !matchesTraceFilter(event.TraceID, traceIDs) {
		return false
	}
	if trimmedQuery := strings.TrimSpace(textQuery); trimmedQuery != "" && !matchesTextQuery(event, trimmedQuery) {
		return false
	}
	return true
}

func matchesWorkspaceFilter(value string, filters []string) bool {
	value = strings.TrimSpace(value)
	for _, filter := range filters {
		if value == strings.TrimSpace(filter) {
			return true
		}
	}
	return false
}

func matchesTraceFilter(value string, filters []string) bool {
	value = strings.TrimSpace(value)
	for _, filter := range filters {
		if value == strings.TrimSpace(filter) {
			return true
		}
	}
	return false
}

func matchesTextQuery(event WideEvent, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(event.Operation), query) {
		return true
	}
	if strings.Contains(strings.ToLower(event.Command), query) {
		return true
	}
	if strings.Contains(strings.ToLower(event.ErrorMessage), query) {
		return true
	}
	if len(event.Data) == 0 {
		return false
	}
	raw, err := json.Marshal(event.Data)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(raw)), query)
}

func eventToRecord(event WideEvent) timedEventRecord {
	return timedEventRecord{
		EventRecord: EventRecord{
			Timestamp:    event.Ts.Format(time.RFC3339Nano),
			Operation:    event.Operation,
			Command:      event.Command,
			Status:       string(event.Status),
			Component:    event.Component,
			TraceID:      event.TraceID,
			SpanID:       event.SpanID,
			ParentID:     event.ParentID,
			Service:      event.Service,
			Version:      event.Version,
			Subtype:      event.Subtype,
			SessionID:    event.SessionID,
			AgentID:      event.AgentID,
			WorkspaceID:  event.WorkspaceID,
			JobID:        event.JobID,
			DurationMS:   event.DurationMS,
			ErrorType:    event.ErrorType,
			ErrorCode:    event.ErrorCode,
			ErrorMessage: event.ErrorMessage,
			Retriable:    event.Retriable,
			Data:         event.Data,
		},
		parsedAt: event.Ts,
	}
}
