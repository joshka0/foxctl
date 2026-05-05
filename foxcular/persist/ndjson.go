// Package persist provides optional persistence sinks for foxcular events.
//
// This package is separate from the core foxcular package to avoid pulling
// file I/O or database dependencies into core usage. Import it only when
// persistent event storage is needed.
package persist

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/joshka0/foxcular"
)

// NDJSONSink writes events as newline-delimited JSON to a file. Each accepted
// event is written as one valid JSON object per line. Events should already be
// redacted by the client before reaching this sink.
type NDJSONSink struct {
	mu   sync.Mutex
	file *os.File
	w    *bufio.Writer
	path string
}

// NDJSONOption configures an NDJSONSink.
type NDJSONOption func(*ndjsonOpts)

type ndjsonOpts struct{}

// NewNDJSONSink creates an NDJSONSink that appends to the file at path.
// The file is created if it does not exist.
func NewNDJSONSink(path string, _ ...NDJSONOption) (*NDJSONSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("ndjson: open %s: %w", path, err)
	}
	return &NDJSONSink{
		file: f,
		w:    bufio.NewWriter(f),
		path: path,
	}, nil
}

// Send writes one event as a JSON line. Returns an error if the event cannot
// be serialized or written.
func (s *NDJSONSink) Send(_ context.Context, event *foxcular.Event) error {
	if event == nil {
		return nil
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("ndjson: marshal event: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(data); err != nil {
		return fmt.Errorf("ndjson: write: %w", err)
	}
	if err := s.w.WriteByte('\n'); err != nil {
		return fmt.Errorf("ndjson: newline: %w", err)
	}
	return nil
}

// Flush writes any buffered data to the underlying file.
func (s *NDJSONSink) Flush(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w != nil {
		if err := s.w.Flush(); err != nil {
			return fmt.Errorf("ndjson: flush buffer: %w", err)
		}
	}
	if s.file != nil {
		if err := s.file.Sync(); err != nil {
			return fmt.Errorf("ndjson: sync file: %w", err)
		}
	}
	return nil
}

// Close flushes remaining data and closes the file. Safe to call multiple times.
func (s *NDJSONSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w != nil {
		_ = s.w.Flush()
		s.w = nil
	}
	if s.file != nil {
		err := s.file.Close()
		s.file = nil
		return err
	}
	return nil
}

// Path returns the file path this sink writes to.
func (s *NDJSONSink) Path() string {
	return s.path
}

// ReadNDJSON reads all events from an NDJSON file at path. Returns an error
// if the file cannot be opened or any line fails to parse.
func ReadNDJSON(path string) ([]*foxcular.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ndjson: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var events []*foxcular.Event
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event foxcular.Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("ndjson: parse line %d: %w", lineNum, err)
		}
		events = append(events, &event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ndjson: scan: %w", err)
	}
	return events, nil
}
