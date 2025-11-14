package jobs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ProgressEvent represents a single progress update in NDJSON format.
type ProgressEvent struct {
	Timestamp time.Time              `json:"ts"`
	Message   string                 `json:"message,omitempty"`
	Percent   float64                `json:"percent,omitempty"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
}

// ProgressReader reads progress events from a job's progress.ndjson file.
type ProgressReader struct {
	file    *os.File
	scanner *bufio.Scanner
}

// OpenProgressReader opens a progress reader for the given job directory.
func OpenProgressReader(jobDir string) (*ProgressReader, error) {
	progressPath := filepath.Join(jobDir, "progress.ndjson")
	f, err := os.Open(progressPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("progress: no progress file")
		}
		return nil, fmt.Errorf("progress: open file: %w", err)
	}
	return &ProgressReader{
		file:    f,
		scanner: bufio.NewScanner(f),
	}, nil
}

// Next reads the next progress event. Returns io.EOF when done.
func (pr *ProgressReader) Next() (ProgressEvent, error) {
	if !pr.scanner.Scan() {
		if err := pr.scanner.Err(); err != nil {
			return ProgressEvent{}, err
		}
		return ProgressEvent{}, io.EOF
	}

	var event ProgressEvent
	if err := json.Unmarshal(pr.scanner.Bytes(), &event); err != nil {
		return ProgressEvent{}, fmt.Errorf("progress: decode: %w", err)
	}
	return event, nil
}

// Close closes the progress reader.
func (pr *ProgressReader) Close() error {
	return pr.file.Close()
}
