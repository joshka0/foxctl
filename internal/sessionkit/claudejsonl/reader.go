package claudejsonl

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	// DefaultBufferSize is the initial buffer size for the reader.
	DefaultBufferSize = 1024 * 1024 // 1MB
	// MaxLineSize is the maximum line size we support.
	MaxLineSize = 10 * 1024 * 1024 // 10MB
)

// Reader provides streaming access to a Claude Code JSONL file.
type Reader struct {
	scanner    *bufio.Scanner
	file       *os.File
	byteOffset int64
	lineNum    int
	err        error
}

// OpenReader opens a JSONL file for streaming.
// The caller must call Close() when done.
func OpenReader(path string) (*Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open JSONL: %w", err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, DefaultBufferSize), MaxLineSize)

	return &Reader{
		scanner: scanner,
		file:    file,
	}, nil
}

// NewReader creates a Reader from an io.Reader.
// The caller is responsible for closing the underlying reader if needed.
func NewReader(r io.Reader) *Reader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, DefaultBufferSize), MaxLineSize)

	return &Reader{
		scanner: scanner,
	}
}

// Close closes the underlying file if one was opened.
func (r *Reader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// ReadMessage represents a parsed message with metadata.
type ReadMessage struct {
	Message    *Message
	ByteOffset int64
	ByteLength int64
	LineNum    int
	Timestamp  time.Time
}

// Next reads and parses the next message from the JSONL file.
// Returns nil, nil when EOF is reached.
// Returns nil, error on parse or read errors.
func (r *Reader) Next() (*ReadMessage, error) {
	if r.err != nil {
		return nil, r.err
	}

	for r.scanner.Scan() {
		line := r.scanner.Bytes()
		lineLen := int64(len(line)) + 1 // +1 for newline

		r.lineNum++
		startOffset := r.byteOffset
		r.byteOffset += lineLen

		// Skip empty lines
		if len(line) == 0 {
			continue
		}

		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			// Skip malformed lines but continue reading
			continue
		}

		rm := &ReadMessage{
			Message:    &msg,
			ByteOffset: startOffset,
			ByteLength: lineLen,
			LineNum:    r.lineNum,
		}

		// Parse timestamp if available
		if msg.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
				rm.Timestamp = t
			} else if t, err := time.Parse(time.RFC3339Nano, msg.Timestamp); err == nil {
				rm.Timestamp = t
			}
		}

		return rm, nil
	}

	if err := r.scanner.Err(); err != nil {
		r.err = fmt.Errorf("scan JSONL: %w", err)
		return nil, r.err
	}

	return nil, nil // EOF
}

// Stream reads all messages and sends them to a channel.
// The channel is closed when reading completes.
// Errors are sent to the error channel, which is also closed on completion.
func (r *Reader) Stream() (<-chan *ReadMessage, <-chan error) {
	msgCh := make(chan *ReadMessage, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(msgCh)
		defer close(errCh)

		for {
			msg, err := r.Next()
			if err != nil {
				errCh <- err
				return
			}
			if msg == nil {
				return // EOF
			}
			msgCh <- msg
		}
	}()

	return msgCh, errCh
}

// ReadAll reads all messages from the file into a slice.
// For large files, prefer using Next() or Stream() for streaming access.
func (r *Reader) ReadAll() ([]*ReadMessage, error) {
	var messages []*ReadMessage

	for {
		msg, err := r.Next()
		if err != nil {
			return messages, err
		}
		if msg == nil {
			return messages, nil
		}
		messages = append(messages, msg)
	}
}

// ParseLine parses a single JSONL line into a Message.
// Useful for processing lines from other sources.
func ParseLine(line []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	return &msg, nil
}

// FileInfo returns information about the underlying file if one was opened.
func (r *Reader) FileInfo() (os.FileInfo, error) {
	if r.file == nil {
		return nil, fmt.Errorf("no file opened")
	}
	return r.file.Stat()
}

// ByteOffset returns the current byte offset in the file.
func (r *Reader) ByteOffset() int64 {
	return r.byteOffset
}

// LineNum returns the current line number (1-based).
func (r *Reader) LineNum() int {
	return r.lineNum
}
