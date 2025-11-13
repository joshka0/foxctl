package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewLoggerFormats(t *testing.T) {
	tests := []struct {
		name       string
		cfg        Config
		wantFormat Format
	}{
		{
			name:       "text default",
			cfg:        Config{Level: LevelInfo, Format: FormatText},
			wantFormat: FormatText,
		},
		{
			name:       "json",
			cfg:        Config{Level: LevelDebug, Format: FormatJSON},
			wantFormat: FormatJSON,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.cfg.Writer = &buf

			logger := New(tt.cfg)
			if logger == nil {
				t.Fatal("logger is nil")
			}

			logger.Info("test message", "key", "value")
			out := buf.String()
			if out == "" {
				t.Fatal("expected log output")
			}

			if tt.wantFormat == FormatJSON {
				var decoded map[string]any
				if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
					t.Fatalf("expected json output: %v", err)
				}
				if decoded["msg"] != "test message" {
					t.Fatalf("unexpected msg: %v", decoded["msg"])
				}
			} else if !strings.Contains(out, "test message") {
				t.Fatalf("expected text log to contain message, got %q", out)
			}
		})
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{Level: LevelWarn, Format: FormatText, Writer: &buf})

	logger.Info("info")
	if buf.Len() != 0 {
		t.Fatalf("expected warn logger to filter info logs")
	}

	logger.Warn("warn")
	if buf.Len() == 0 {
		t.Fatalf("expected warn log to be emitted")
	}
}

func TestContextHelpers(t *testing.T) {
	logger := New(Config{Level: LevelInfo, Format: FormatText})
	ctx := WithContext(context.Background(), logger)
	got := FromContext(ctx)
	if got == nil {
		t.Fatalf("expected logger in context")
	}
	if got != logger {
		t.Fatalf("expected same logger instance")
	}

	if val := FromContext(context.Background()); val != nil {
		t.Fatalf("expected nil when logger missing")
	}
}

func TestParseLevelAndFormat(t *testing.T) {
	if ParseLevel("WARN") != LevelWarn {
		t.Fatalf("expected warn level")
	}
	if ParseLevel("unknown") != LevelInfo {
		t.Fatalf("expected default info")
	}
	if ParseFormat("json") != FormatJSON {
		t.Fatalf("expected json format")
	}
	if ParseFormat("other") != FormatText {
		t.Fatalf("expected text default")
	}
}

func TestDefaultAndDiscard(t *testing.T) {
	if Default() == nil {
		t.Fatalf("default logger should not be nil")
	}
	discard := Discard()
	if discard == nil {
		t.Fatalf("discard logger should not be nil")
	}
	var buf bytes.Buffer
	logger := New(Config{Level: LevelInfo, Format: FormatText, Writer: &buf})
	logger.Info("test message", "job_id", "abc123")
	if !strings.Contains(buf.String(), "job_id") {
		t.Fatalf("expected structured text output")
	}
}
