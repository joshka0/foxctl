package logging

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNewCreatesRedactingLogger(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := New(Config{Level: LevelDebug, Format: FormatJSON, Writer: buf})
	logger.Info().Str("token", "Bearer secret").Msg("hello")
	out := buf.String()
	if strings.Contains(out, "secret") {
		t.Fatalf("expected log output to be redacted, got %q", out)
	}
}

func TestContextHelpers(t *testing.T) {
	logger := Default()
	ctx := WithContext(context.Background(), logger)
	from := FromContext(ctx)
	if from.GetLevel() != logger.GetLevel() {
		t.Fatalf("expected context logger level %v got %v", logger.GetLevel(), from.GetLevel())
	}
}
