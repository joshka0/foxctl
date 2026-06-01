package updater

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/quick"
)

func TestContainsGotchaDoesNotPanicAtBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   bool
	}{
		{source: "memory:gotch", want: false},
		{source: "memory:gotcha", want: true},
		{source: "memory:gotcha:legacy", want: true},
		{source: "session:gotcha", want: false},
	}

	for _, tt := range tests {
		if got := containsGotcha(tt.source); got != tt.want {
			t.Fatalf("containsGotcha(%q) = %v, want %v", tt.source, got, tt.want)
		}
	}
}

func TestContainsGotchaPropertyMatchesMemoryGotchaPrefix(t *testing.T) {
	t.Parallel()

	property := func(source string) bool {
		return containsGotcha(source) == strings.HasPrefix(source, "memory:gotcha")
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("containsGotcha property failed: %v", err)
	}
}

func TestInjectorAssignsHighPriorityOnlyForMemoryGotchas(t *testing.T) {
	t.Parallel()

	sender := &capturingSender{}
	injector := NewInjector(sender, InjectorConfig{Stream: "context-test"})

	if err := injector.Inject(context.Background(), "session-1", "workspace", ContextCandidate{
		ID:      "gotcha-1",
		Type:    "memory",
		Content: "careful with this edge case",
		Source:  "memory:gotcha",
		Score:   0.9,
		Query:   "edge case",
	}, "important"); err != nil {
		t.Fatalf("Inject(gotcha) error = %v", err)
	}

	if err := injector.Inject(context.Background(), "session-1", "workspace", ContextCandidate{
		ID:      "pattern-1",
		Type:    "memory",
		Content: "use the existing pattern",
		Source:  "memory:pattern",
		Score:   0.8,
		Query:   "pattern",
	}, "helpful"); err != nil {
		t.Fatalf("Inject(pattern) error = %v", err)
	}

	if len(sender.messages) != 2 {
		t.Fatalf("captured messages = %d, want 2", len(sender.messages))
	}
	if sender.messages[0].Priority != 1 {
		t.Fatalf("gotcha priority = %d, want 1", sender.messages[0].Priority)
	}
	if sender.messages[1].Priority != 0 {
		t.Fatalf("pattern priority = %d, want 0", sender.messages[1].Priority)
	}
}

type capturingSender struct {
	messages []ContextMessage
}

func (s *capturingSender) SendMessage(_ context.Context, _, _, _ string, payload []byte) error {
	var msg ContextMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return err
	}
	s.messages = append(s.messages, msg)
	return nil
}
