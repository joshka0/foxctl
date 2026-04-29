package repl

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestYaegiSession_PromptBindingAndPersistence(t *testing.T) {
	s := newYaegiTestSession(t, map[string]any{"prompt": "hello"})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := s.Execute(ctx, "prompt")
	if err != nil {
		t.Fatalf("execute prompt failed: %v", err)
	}
	if got := metadataString(t, result.Metadata, "result"); got != `"hello"` {
		t.Fatalf("unexpected prompt result: %q", got)
	}

	if _, err := s.Execute(ctx, "x := 41"); err != nil {
		t.Fatalf("execute assignment failed: %v", err)
	}
	result, err = s.Execute(ctx, "x + 1")
	if err != nil {
		t.Fatalf("execute expression failed: %v", err)
	}
	if got := metadataString(t, result.Metadata, "result"); got != "42" {
		t.Fatalf("unexpected expression result: %q", got)
	}
}

func TestYaegiSession_StdoutAndErrorCapture(t *testing.T) {
	s := newYaegiTestSession(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cases := []struct {
		name              string
		code              string
		wantOK            bool
		wantStdout        string
		wantErrorContains string
	}{
		{
			name:   "import fmt",
			code:   `import "fmt"`,
			wantOK: true,
		},
		{
			name:       "stdout capture",
			code:       `fmt.Println("hi")`,
			wantOK:     true,
			wantStdout: "hi\n",
		},
		{
			name:              "runtime panic captured",
			code:              `panic("boom")`,
			wantOK:            false,
			wantErrorContains: "boom",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res, err := s.Execute(ctx, tc.code)
			if err != nil {
				t.Fatalf("execute failed: %v", err)
			}
			if got := metadataBool(t, res.Metadata, "ok"); got != tc.wantOK {
				t.Fatalf("unexpected ok value: got=%v want=%v metadata=%#v", got, tc.wantOK, res.Metadata)
			}
			if tc.wantStdout != "" {
				if got := metadataString(t, res.Metadata, "stdout"); got != tc.wantStdout {
					t.Fatalf("unexpected stdout: %q", got)
				}
			}
			if tc.wantErrorContains != "" {
				errText := metadataString(t, res.Metadata, "error")
				if !strings.Contains(errText, tc.wantErrorContains) {
					t.Fatalf("error text missing %q: %q", tc.wantErrorContains, errText)
				}
			}
		})
	}
}

func TestYaegiSession_SnapshotBestEffort(t *testing.T) {
	s := newYaegiTestSession(t, map[string]any{
		"prompt": "hello",
		"meta": map[string]any{
			"count": 2,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := s.Execute(ctx, "x := 7"); err != nil {
		t.Fatalf("execute x failed: %v", err)
	}
	if _, err := s.Execute(ctx, "fn := func() {}"); err != nil {
		t.Fatalf("execute fn failed: %v", err)
	}

	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	if got, ok := snapshot["prompt"].(string); !ok || got != "hello" {
		t.Fatalf("unexpected prompt snapshot: %#v", snapshot["prompt"])
	}
	if got, ok := snapshot["x"].(float64); !ok || got != 7 {
		t.Fatalf("unexpected x snapshot: %#v", snapshot["x"])
	}
	if _, ok := snapshot["fn"]; ok {
		t.Fatalf("expected fn to be absent from snapshot: %#v", snapshot["fn"])
	}
}

func TestYaegiSession_LifecycleAndClose(t *testing.T) {
	s := NewYaegiSession(YaegiOptions{MaxOutputBytes: 16 * 1024})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tests := []struct {
		name    string
		action  func() error
		wantErr error
	}{
		{
			name:    "execute before init",
			action:  func() error { _, err := s.Execute(ctx, "1 + 1"); return err },
			wantErr: errYaegiSessionNotInitialized,
		},
		{
			name:    "snapshot before init",
			action:  func() error { _, err := s.Snapshot(ctx); return err },
			wantErr: errYaegiSessionNotInitialized,
		},
		{
			name:    "init",
			action:  func() error { return s.Init(ctx, nil) },
			wantErr: nil,
		},
		{
			name:    "close",
			action:  func() error { return s.Close(ctx) },
			wantErr: nil,
		},
		{
			name:    "close idempotent",
			action:  func() error { return s.Close(ctx) },
			wantErr: nil,
		},
		{
			name:    "execute after close",
			action:  func() error { _, err := s.Execute(ctx, "1 + 1"); return err },
			wantErr: errYaegiSessionClosed,
		},
		{
			name:    "snapshot after close",
			action:  func() error { _, err := s.Snapshot(ctx); return err },
			wantErr: errYaegiSessionClosed,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.action()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("unexpected error: got=%v want=%v", err, tc.wantErr)
			}
		})
	}
}

func TestYaegiSession_InitValidation(t *testing.T) {
	tests := []struct {
		name             string
		state            map[string]any
		wantErrSubstring string
	}{
		{
			name:             "invalid identifier",
			state:            map[string]any{"bad-key": 1},
			wantErrSubstring: "valid Go identifier",
		},
		{
			name:             "non json serializable value",
			state:            map[string]any{"ch": make(chan int)},
			wantErrSubstring: "JSON-serializable",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			s := NewYaegiSession(YaegiOptions{})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			err := s.Init(ctx, tc.state)
			if err == nil {
				t.Fatal("expected init error")
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstring) {
				t.Fatalf("unexpected init error: %v", err)
			}
		})
	}
}

func newYaegiTestSession(t *testing.T, state map[string]any) *YaegiSession {
	t.Helper()

	s := NewYaegiSession(YaegiOptions{
		MaxOutputBytes: 16 * 1024,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Init(ctx, state); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer closeCancel()
		_ = s.Close(closeCtx)
	})
	return s
}

func metadataBool(t *testing.T, metadata map[string]any, key string) bool {
	t.Helper()
	value, ok := metadata[key]
	if !ok {
		t.Fatalf("metadata key %q missing", key)
	}
	typed, ok := value.(bool)
	if !ok {
		t.Fatalf("metadata key %q is not a bool: %#v", key, value)
	}
	return typed
}
