package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ProgressEvent represents a single progress update in NDJSON format.
type ProgressEvent struct {
	Timestamp time.Time              `json:"ts"`
	Message   string                 `json:"message,omitempty"`
	Percent   float64                `json:"percent,omitempty"`
	Meta      map[string]interface{} `json:"meta,omitempty"`
}

type progressWriter struct {
	file   *os.File
	enc    *json.Encoder
	mu     sync.Mutex
	closed bool
}

func newProgressWriter(jobDir string) (*progressWriter, error) {
	progressPath := filepath.Join(jobDir, "progress.ndjson")
	f, err := os.Create(progressPath)
	if err != nil {
		return nil, fmt.Errorf("progress: create file: %w", err)
	}
	return &progressWriter{file: f, enc: json.NewEncoder(f)}, nil
}

func (pw *progressWriter) Write(event ProgressEvent) error {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if pw.closed {
		return fmt.Errorf("progress: writer closed")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if err := pw.enc.Encode(event); err != nil {
		return fmt.Errorf("progress: encode: %w", err)
	}
	return pw.file.Sync()
}

func (pw *progressWriter) WriteMessage(message string) error {
	return pw.Write(ProgressEvent{Message: message})
}

func (pw *progressWriter) Close() error {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	if pw.closed {
		return nil
	}
	pw.closed = true
	return pw.file.Close()
}
