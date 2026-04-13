package observability

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// PruneOptions configures log pruning behavior.
type PruneOptions struct {
	// OlderThan deletes events older than this duration (default: 30 days)
	OlderThan time.Duration

	// MaxSizeBytes is the maximum total size of event files to keep (0 = no limit)
	MaxSizeBytes int64

	// DryRun simulates pruning without actually deleting
	DryRun bool
}

// PruneResult contains the results of a prune operation.
type PruneResult struct {
	// EventsPruned is the number of events removed
	EventsPruned int64 `json:"events_pruned"`

	// EventsKept is the number of events retained
	EventsKept int64 `json:"events_kept"`

	// BytesFreed is the approximate bytes freed
	BytesFreed int64 `json:"bytes_freed"`

	// FilesProcessed is the number of event files processed
	FilesProcessed int `json:"files_processed"`

	// Errors contains any non-fatal errors encountered
	Errors []string `json:"errors,omitempty"`
}

// DefaultPruneOptions returns sensible defaults for pruning.
func DefaultPruneOptions() PruneOptions {
	return PruneOptions{
		OlderThan:    30 * 24 * time.Hour, // 30 days
		MaxSizeBytes: 0,                   // No size limit by default
		DryRun:       false,
	}
}

// Prune removes old events from the observability directory.
// Events are pruned based on their timestamp field, not file modification time.
//
// Index:
// - Purpose: Remove events older than a cutoff across NDJSON files
// - Flow: validate obs dir → scan event files → prune each file → aggregate results
// - SideEffects: reads event files; rewrites NDJSON files; deletes temp files
// - FailureModes: directory read errors, prune failures, context cancellation
// - Related: pruneEventFile, PruneBySize
// - Keywords: prune, ndjson, events_dir, older_than, dry_run
func Prune(ctx context.Context, obsDir string, opts PruneOptions) (*PruneResult, error) {
	if obsDir == "" {
		return nil, fmt.Errorf("observability directory not specified")
	}

	eventsDir := filepath.Join(obsDir, "events")
	if _, err := os.Stat(eventsDir); os.IsNotExist(err) {
		return &PruneResult{}, nil // Nothing to prune
	}

	result := &PruneResult{}
	cutoff := time.Now().Add(-opts.OlderThan)

	// Find all NDJSON files
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		return nil, fmt.Errorf("read events directory: %w", err)
	}

	for _, entry := range entries {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".ndjson" {
			continue
		}

		filePath := filepath.Join(eventsDir, entry.Name())
		pruned, kept, bytesFreed, err := pruneEventFile(ctx, filePath, cutoff, opts.DryRun)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entry.Name(), err))
			continue
		}

		result.EventsPruned += pruned
		result.EventsKept += kept
		result.BytesFreed += bytesFreed
		result.FilesProcessed++
	}

	return result, nil
}

// pruneEventFile removes old events from a single NDJSON file.
// Returns (eventsPruned, eventsKept, bytesFreed, error).
//
// Index:
// - Purpose: Filter NDJSON events by timestamp and rewrite a single file
// - Flow: scan file → parse timestamps → retain/prune → rewrite temp → rename
// - SideEffects: reads file; writes temp file; renames files; deletes temp file
// - FailureModes: scan errors, JSON parse errors, write/rename errors, context cancellation
// - Related: Prune
// - Keywords: prune_file, ndjson, cutoff, rename, temp_file
func pruneEventFile(ctx context.Context, filePath string, cutoff time.Time, dryRun bool) (int64, int64, int64, error) {
	// Read the file
	f, err := os.Open(filePath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("open file: %w", err)
	}

	var keptEvents []json.RawMessage
	var prunedCount, keptCount int64
	var prunedBytes int64

	scanner := bufio.NewScanner(f)
	// Increase buffer size for large events
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024) // Max 1MB per line

	for scanner.Scan() {
		if ctx.Err() != nil {
			_ = f.Close()
			return prunedCount, keptCount, prunedBytes, ctx.Err()
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse just the timestamp
		var event struct {
			Ts time.Time `json:"ts"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			// Keep unparseable events
			keptEvents = append(keptEvents, append([]byte(nil), line...))
			keptCount++
			continue
		}

		if event.Ts.Before(cutoff) {
			prunedCount++
			prunedBytes += int64(len(line)) + 1 // +1 for newline
		} else {
			keptEvents = append(keptEvents, append([]byte(nil), line...))
			keptCount++
		}
	}

	if err := scanner.Err(); err != nil {
		_ = f.Close()
		return prunedCount, keptCount, prunedBytes, fmt.Errorf("scan file: %w", err)
	}
	_ = f.Close()

	if dryRun {
		return prunedCount, keptCount, prunedBytes, nil
	}

	// If nothing was pruned, we're done
	if prunedCount == 0 {
		return 0, keptCount, 0, nil
	}

	// Write kept events to a temp file, then rename
	tmpPath := filePath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return prunedCount, keptCount, prunedBytes, fmt.Errorf("create temp file: %w", err)
	}

	for _, event := range keptEvents {
		if _, err := tmpFile.Write(event); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return prunedCount, keptCount, prunedBytes, fmt.Errorf("write event: %w", err)
		}
		if _, err := tmpFile.WriteString("\n"); err != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
			return prunedCount, keptCount, prunedBytes, fmt.Errorf("write newline: %w", err)
		}
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return prunedCount, keptCount, prunedBytes, fmt.Errorf("close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return prunedCount, keptCount, prunedBytes, fmt.Errorf("rename temp file: %w", err)
	}

	return prunedCount, keptCount, prunedBytes, nil
}

// PruneBySize removes the oldest events until total size is under maxBytes.
// This is useful for keeping disk usage bounded regardless of event age.
//
// Index:
// - Purpose: Prune events by total size regardless of age
// - Flow: scan files → build event index → sort oldest first → rewrite affected files
// - SideEffects: reads event files; rewrites NDJSON files; deletes temp files
// - FailureModes: directory read errors, file I/O errors, JSON parse errors, context cancellation
// - Related: Prune, pruneEventFile
// - Keywords: prune_size, ndjson, max_bytes, rewrite, dry_run
func PruneBySize(ctx context.Context, obsDir string, maxBytes int64, dryRun bool) (*PruneResult, error) {
	if obsDir == "" {
		return nil, fmt.Errorf("observability directory not specified")
	}

	eventsDir := filepath.Join(obsDir, "events")
	if _, err := os.Stat(eventsDir); os.IsNotExist(err) {
		return &PruneResult{}, nil
	}

	// Collect all events with their timestamps and sizes
	type eventEntry struct {
		filePath string
		offset   int64
		size     int64
		ts       time.Time
		line     []byte
	}

	var allEvents []eventEntry
	var totalSize int64

	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		return nil, fmt.Errorf("read events directory: %w", err)
	}

	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if entry.IsDir() || filepath.Ext(entry.Name()) != ".ndjson" {
			continue
		}

		filePath := filepath.Join(eventsDir, entry.Name())
		f, err := os.Open(filePath)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		var offset int64
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				offset += 1
				continue
			}

			var event struct {
				Ts time.Time `json:"ts"`
			}
			lineSize := int64(len(line)) + 1

			if err := json.Unmarshal(line, &event); err == nil {
				allEvents = append(allEvents, eventEntry{
					filePath: filePath,
					offset:   offset,
					size:     lineSize,
					ts:       event.Ts,
					line:     append([]byte(nil), line...),
				})
			}
			totalSize += lineSize
			offset += lineSize
		}
		_ = f.Close()
	}

	result := &PruneResult{
		FilesProcessed: len(entries),
	}

	if totalSize <= maxBytes {
		result.EventsKept = int64(len(allEvents))
		return result, nil
	}

	// Sort by timestamp (oldest first)
	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].ts.Before(allEvents[j].ts)
	})

	// Determine which events to prune
	bytesToFree := totalSize - maxBytes
	var bytesFreed int64
	pruneUntil := 0

	for i, e := range allEvents {
		if bytesFreed >= bytesToFree {
			break
		}
		bytesFreed += e.size
		pruneUntil = i + 1
	}

	result.EventsPruned = int64(pruneUntil)
	result.EventsKept = int64(len(allEvents) - pruneUntil)
	result.BytesFreed = bytesFreed

	if dryRun {
		return result, nil
	}

	// Build set of events to keep per file
	keepEvents := make(map[string][]eventEntry)
	for i := pruneUntil; i < len(allEvents); i++ {
		e := allEvents[i]
		keepEvents[e.filePath] = append(keepEvents[e.filePath], e)
	}

	// Rewrite each affected file
	for filePath, events := range keepEvents {
		tmpPath := filePath + ".tmp"
		tmpFile, err := os.Create(tmpPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", filepath.Base(filePath), err))
			continue
		}

		for _, e := range events {
			if _, err := tmpFile.Write(e.line); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: write: %v", filepath.Base(filePath), err))
				break
			}
			if _, err := tmpFile.WriteString("\n"); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: write newline: %v", filepath.Base(filePath), err))
				break
			}
		}

		if err := tmpFile.Close(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: close: %v", filepath.Base(filePath), err))
			_ = os.Remove(tmpPath)
			continue
		}

		if err := os.Rename(tmpPath, filePath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: rename: %v", filepath.Base(filePath), err))
			_ = os.Remove(tmpPath)
		}
	}

	return result, nil
}
