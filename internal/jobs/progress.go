package jobs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// ProgressWriter writes progress events to a job's progress.ndjson file.
type ProgressWriter struct {
	file *os.File
	enc  *json.Encoder
	mu   sync.Mutex
}

// NewProgressWriter creates a progress writer for the given job directory.
func NewProgressWriter(jobDir string) (*ProgressWriter, error) {
	progressPath := filepath.Join(jobDir, "progress.ndjson")
	f, err := os.Create(progressPath)
	if err != nil {
		return nil, fmt.Errorf("progress: create file: %w", err)
	}
	return &ProgressWriter{
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

// Write emits a progress event.
func (pw *ProgressWriter) Write(event ProgressEvent) error {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	if err := pw.enc.Encode(event); err != nil {
		return fmt.Errorf("progress: encode: %w", err)
	}

	// Flush immediately for real-time streaming
	return pw.file.Sync()
}

// WriteMessage emits a simple message progress event.
func (pw *ProgressWriter) WriteMessage(message string) error {
	return pw.Write(ProgressEvent{Message: message})
}

// WritePercent emits a progress event with percentage completion.
func (pw *ProgressWriter) WritePercent(percent float64, message string) error {
	return pw.Write(ProgressEvent{
		Percent: percent,
		Message: message,
	})
}

// Close closes the progress writer.
func (pw *ProgressWriter) Close() error {
	pw.mu.Lock()
	defer pw.mu.Unlock()
	return pw.file.Close()
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

// TailProgress streams progress events from a job, following new writes.
func (s *Store) TailProgress(ctx context.Context, jobID string, follow bool) (retErr error) {
	jobDir := s.jobDir(jobID)
	progressPath := filepath.Join(jobDir, "progress.ndjson")

	// Check if job exists
	if _, err := s.Get(ctx, jobID); err != nil {
		return err
	}

	// Open or wait for progress file
	var f *os.File
	var err error
	for {
		f, err = os.Open(progressPath)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("progress: open: %w", err)
		}
		if !follow {
			return fmt.Errorf("progress: no progress file yet")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// Retry
		}
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			if retErr == nil {
				retErr = fmt.Errorf("progress: close: %w", closeErr)
			} else {
				retErr = fmt.Errorf("%v; close error: %w", retErr, closeErr)
			}
		}
	}()

	scanner := bufio.NewScanner(f)
	for {
		// Read available lines
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			return fmt.Errorf("progress: scan: %w", err)
		}

		// Check if we should continue following
		if !follow {
			return nil
		}

		// Check if job is complete
		job, err := s.Get(ctx, jobID)
		if err != nil {
			return err
		}

		if job.State == StateOK || job.State == StateError || job.State == StateCanceled {
			// Job finished, read any remaining lines and exit
			for scanner.Scan() {
				fmt.Println(scanner.Text())
			}
			return nil
		}

		// Wait before checking for new content
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// Continue
		}
	}
}

// WaitForCompletion blocks until the job reaches a terminal state.
func (s *Store) WaitForCompletion(ctx context.Context, jobID string, pollInterval time.Duration) (Job, error) {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		job, err := s.Get(ctx, jobID)
		if err != nil {
			return Job{}, err
		}

		// Check if job is in terminal state
		switch job.State {
		case StateOK, StateError, StateCanceled:
			return job, nil
		}

		// Wait for next poll
		select {
		case <-ctx.Done():
			return Job{}, ctx.Err()
		case <-ticker.C:
			// Continue polling
		}
	}
}
