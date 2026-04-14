package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFilteredToolExecutor_BlocksDisallowed(t *testing.T) {
	inner := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return "ok:" + name, nil
		},
		Tools: []ToolDef{
			{Name: "a"},
			{Name: "b"},
		},
	}

	exec := NewFilteredToolExecutor(inner, []string{"b"})
	if exec == nil {
		t.Fatalf("expected executor")
	}

	if _, err := exec.Execute(context.Background(), "a", nil); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected not allowed error, got %v", err)
	}

	out, err := exec.Execute(context.Background(), "b", nil)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if out != "ok:b" {
		t.Fatalf("unexpected output %q", out)
	}

	defs := exec.List()
	if len(defs) != 1 || defs[0].Name != "b" {
		t.Fatalf("unexpected defs %#v", defs)
	}
}

func TestFilterToolDefs(t *testing.T) {
	defs := []ToolDef{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}
	filtered := FilterToolDefs(defs, []string{"c", "a"})
	if len(filtered) != 2 || filtered[0].Name != "a" || filtered[1].Name != "c" {
		t.Fatalf("unexpected filtered %#v", filtered)
	}
}
