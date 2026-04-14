package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/joshka0/foxctl/internal/v2/core/events"
)

// MarshalEventsJSONL encodes events to canonical JSONL bytes.
func MarshalEventsJSONL(list []events.Event) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for i := range list {
		if err := enc.Encode(list[i]); err != nil {
			return nil, fmt.Errorf("encode event[%d]: %w", i, err)
		}
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}

// AssertEventsJSONLMatchesFile compares events against a golden JSONL fixture.
func AssertEventsJSONLMatchesFile(t *testing.T, list []events.Event, goldenPath string) {
	t.Helper()

	got, err := MarshalEventsJSONL(list)
	if err != nil {
		t.Fatalf("marshal jsonl: %v", err)
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v", goldenPath, err)
	}

	want = bytes.TrimSpace(want)
	if !bytes.Equal(want, got) {
		t.Fatalf("golden mismatch %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, got, want)
	}
}
