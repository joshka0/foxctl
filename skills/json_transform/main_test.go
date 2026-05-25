package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer) *skillmain.RunContext {
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
	rc, err := skillmain.BuildRunContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func TestRunJsonTransform(t *testing.T) {
	ctx := context.Background()
	stdout := &bytes.Buffer{}
	rc := newTestRunnerContext(t, stdout)
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
	rc := newTestRunnerContext(t, stdout)
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
	rc := newTestRunnerContext(t, stdout)
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
	rc := newTestRunnerContext(t, stdout)
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

func TestExtractPathRejectsMalformedArrayIndexes(t *testing.T) {
	data := map[string]any{
		"items": []any{"zero", "one"},
	}

	for _, path := range []string{
		"items[1x]",
		"items[+1]",
		"items[ 1]",
		"items[]",
		"items[x]",
		"items[-1]",
	} {
		if got := extractPath(data, path); got != nil {
			t.Fatalf("extractPath(%q)=%v, want nil for malformed index", path, got)
		}
	}
}

func TestExtractPathGeneratedValidArrayIndexes(t *testing.T) {
	values := []any{"zero", "one", "two", "three", "four"}
	data := map[string]any{"items": values}
	cfg := &quick.Config{MaxCount: 100}

	err := quick.Check(func(rawIndex uint8) bool {
		index := int(rawIndex) % len(values)
		path := fmt.Sprintf("items[%d]", index)
		got := extractPath(data, path)
		if !reflect.DeepEqual(got, values[index]) {
			t.Logf("extractPath(%q)=%v want %v", path, got, values[index])
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("valid array index property failed: %v", err)
	}
}

func TestCollectAllKeysReturnsSortedStablePaths(t *testing.T) {
	indexed := make([]any, 12)
	for i := range indexed {
		indexed[i] = map[string]any{"value": i}
	}
	data := map[string]any{
		"z": 1,
		"a": map[string]any{
			"m": 2,
			"b": 3,
		},
		"arr": []any{
			map[string]any{"y": 4, "x": 5},
		},
		"indexed": indexed,
	}

	got := collectAllKeys(data, "")
	if !sort.StringsAreSorted(got) {
		t.Fatalf("all keys should be sorted for stable output, got %v", got)
	}

	want := []string{
		"a", "a.b", "a.m",
		"arr", "arr[0].x", "arr[0].y",
		"indexed", "indexed[0].value", "indexed[10].value", "indexed[11].value", "indexed[1].value",
		"indexed[2].value", "indexed[3].value", "indexed[4].value", "indexed[5].value", "indexed[6].value",
		"indexed[7].value", "indexed[8].value", "indexed[9].value",
		"z",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectAllKeys=%v want %v", got, want)
	}
}

func TestKeysOperationReturnsSortedTopLevelKeys(t *testing.T) {
	got := keysOperation(map[string]any{
		"z": 1,
		"a": 2,
		"m": 3,
	})

	want := []string{"a", "m", "z"}
	if !reflect.DeepEqual(got["keys"], want) {
		t.Fatalf("keys=%v want %v", got["keys"], want)
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
