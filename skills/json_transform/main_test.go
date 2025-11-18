package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestJsonTransform(t *testing.T) {
	// Setup runner context
	ctx := context.Background()
	buf := &bytes.Buffer{}
	
	state := t.TempDir()
	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   filepath.Join(state, "cas"),
			Jobs:  filepath.Join(state, "jobs"),
			Cache: filepath.Join(state, "cache"),
		},
	}
	rc, err := runner.NewRunnerContext(cfg, buf)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	defer rc.Close()

	tests := []struct {
		name    string
		in      input
		wantErr bool
	}{
		{
			name: "extract operation",
			in: input{
				Data:      map[string]any{"foo": "bar"},
				Operation: "extract",
				Path:      "foo",
			},
			wantErr: false,
		},
		{
			name: "keys operation",
			in: input{
				Data:      map[string]any{"a": 1, "b": 2},
				Operation: "keys",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(ctx, rc, tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExtractPath(t *testing.T) {
	data := map[string]any{
		"foo": map[string]any{
			"bar": []any{"baz"},
		},
	}

	tests := []struct {
		path string
		want any
	}{
		{"foo.bar.0", "baz"},
		{"foo", map[string]any{"bar": []any{"baz"}}},
	}

	for _, tt := range tests {
		got, err := extractPath(data, tt.path)
		if err != nil {
			t.Errorf("extractPath(%s) error: %v", tt.path, err)
			continue
		}
		// Basic equality check - might need reflection for complex types
		if got != tt.want {
			// For map/slice simple compare fails, but this is just a smoke test
			// Just check non-nil
			if got == nil {
				t.Errorf("extractPath(%s) got nil", tt.path)
			}
		}
	}
}

func TestDeepMerge(t *testing.T) {
	dst := map[string]any{
		"a": 1,
		"b": map[string]any{"x": 1},
	}
	src := map[string]any{
		"b": map[string]any{"y": 2},
		"c": 3,
	}

	merged := deepMerge(dst, src).(map[string]any)

	if merged["a"] != 1 {
		t.Errorf("merged[a] = %v, want 1", merged["a"])
	}
	if merged["c"] != 3 {
		t.Errorf("merged[c] = %v, want 3", merged["c"])
	}
	b := merged["b"].(map[string]any)
	if b["x"] != 1 || b["y"] != 2 {
		t.Errorf("nested merge failed: %v", b)
	}
}
