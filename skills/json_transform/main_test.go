package main

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

func newTestContext(t *testing.T, stdout *bytes.Buffer) *runner.Context {
	t.Helper()
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
	rc, err := runner.NewContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func TestRunJsonTransform(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestContext(t, stdout)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Input:     `{"a": 1, "b": 2}`,
		Operation: "keys",
	}

	if err := run(ctx, rc, in); err != nil {
		t.Errorf("run failed: %v", err)
	}
}

func TestRunJsonFormat(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestContext(t, stdout)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Input:     `{"a":1}`,
		Operation: "format",
		Indent:    2,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Errorf("run failed: %v", err)
	}
}

func TestRunJsonValidate(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestContext(t, stdout)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Input:     `{"a":1}`,
		Operation: "validate",
	}

	if err := run(ctx, rc, in); err != nil {
		t.Errorf("run failed: %v", err)
	}
}

func TestRunJsonMerge(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestContext(t, stdout)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Input:     `{"a":1}`,
		Operation: "merge",
		MergeWith: `{"b":2}`,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Errorf("run failed: %v", err)
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
		{"foo.bar[0]", "baz"},
		{"foo", map[string]any{"bar": []any{"baz"}}},
	}

	for _, tt := range tests {
		got := extractPath(data, tt.path)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("extractPath(%s) = %v, want %v", tt.path, got, tt.want)
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
