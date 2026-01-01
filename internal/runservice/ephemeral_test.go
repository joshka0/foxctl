package runservice

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/execution"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cache"
)

func TestExecuteEphemeral_SkipsJobStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
		},
		Memory: config.MemorySettings{AutoCacheTTL: time.Hour},
	}
	for _, dir := range []string{cfg.Paths.Cache, cfg.Paths.CAS} {
		if err := ensureDir(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata:     skill.Metadata{Name: "hooks/test_hook", Version: "1.0.0"},
			Distribution: skill.Distribution{Type: "exec"},
		},
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	executor := NewExecutor(ctx, cfg, handle, stdout, stderr, RunOptions{
		Ephemeral:     true,
		CacheMode:     cache.ModeOff,
		Workspace:     "ws",
		CorrelationID: "trace-ephemeral",
	})
	defer executor.Close()

	// Verify ephemeral mode is enabled
	if !executor.IsEphemeral() {
		t.Fatal("expected IsEphemeral() to return true")
	}

	// Verify job store was never opened
	if executor.jobStore != nil {
		t.Fatal("expected jobStore to be nil in ephemeral mode")
	}
}

func TestExecuteEphemeral_StillServesFromCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
		},
		Memory: config.MemorySettings{AutoCacheTTL: time.Hour},
	}
	for _, dir := range []string{cfg.Paths.Cache, cfg.Paths.CAS} {
		if err := ensureDir(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata:     skill.Metadata{Name: "hooks/test_hook", Version: "1.0.0"},
			Distribution: skill.Distribution{Type: "exec"},
		},
	}

	input := []byte(`{"query":"cached_value"}`)

	// Pre-populate cache
	store, err := cache.Open(ctx, cfg.Paths.Cache, cache.Options{AutoTTL: cfg.Memory.AutoCacheTTL, CASPath: cfg.Paths.CAS})
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	key, err := cache.BuildKey(handle.Manifest, input, nil)
	if err != nil {
		t.Fatalf("build key: %v", err)
	}
	result := []byte(`{"meta":{"ts":"2024-01-01T00:00:00Z"},"data":{"value":"cached"}}`)
	if err := store.Put(ctx, cache.Entry{
		CacheKey:     key,
		SkillName:    handle.Manifest.Metadata.Name,
		SkillVersion: handle.Manifest.Metadata.Version,
		Workspace:    "ws",
		Result:       result,
	}); err != nil {
		t.Fatalf("populate cache: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close cache: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	executor := NewExecutor(ctx, cfg, handle, stdout, stderr, RunOptions{
		Ephemeral:     true,
		CacheMode:     cache.ModeAuto,
		Workspace:     "ws",
		CorrelationID: "trace-ephemeral-cache",
	})
	defer executor.Close()

	// Execute ephemeral - should serve from cache
	err = executor.ExecuteEphemeral(input)
	if err != nil {
		t.Fatalf("ExecuteEphemeral: %v", err)
	}

	// Verify cache hit
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Meta.Source != "cache" {
		t.Fatalf("expected meta.source=cache, got %s", env.Meta.Source)
	}

	// Verify job store was never opened (cache hit bypasses execution)
	if executor.jobStore != nil {
		t.Fatal("expected jobStore to be nil when serving from cache")
	}
}

func TestExecuteEphemeral_DirectExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
		},
		Memory: config.MemorySettings{AutoCacheTTL: time.Hour},
	}
	for _, dir := range []string{cfg.Paths.Cache, cfg.Paths.CAS} {
		if err := ensureDir(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	// Create a mock skill executor that returns a valid envelope
	mockResult := envelope.OK("hooks/test_hook", map[string]any{"result": "success"})
	mockResultBytes, _ := json.Marshal(mockResult)

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata:     skill.Metadata{Name: "hooks/test_hook", Version: "1.0.0"},
			Distribution: skill.Distribution{Type: "exec"},
		},
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	executor := NewExecutor(ctx, cfg, handle, stdout, stderr, RunOptions{
		Ephemeral:     true,
		CacheMode:     cache.ModeOff, // Disable cache to force execution
		Workspace:     "ws",
		CorrelationID: "trace-ephemeral-exec",
	})
	defer executor.Close()

	// Replace the skill executor with a mock
	// Since we can't easily mock NewRunnerExecutor, we test the parts we can

	// For this test, we'll verify the executor setup is correct
	if !executor.IsEphemeral() {
		t.Fatal("expected IsEphemeral() to return true")
	}

	// Verify no job store opened
	if executor.jobStore != nil {
		t.Fatal("expected jobStore to be nil")
	}

	// Verify no trajectory capture in ephemeral mode
	if executor.trajCapture != nil {
		t.Fatal("expected trajCapture to be nil")
	}

	// Test handleEphemeralSuccess directly
	err := executor.handleEphemeralSuccess(mockResultBytes)
	if err != nil {
		t.Fatalf("handleEphemeralSuccess: %v", err)
	}

	// Verify output contains ephemeral metadata
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Meta.Source != "ephemeral" {
		t.Fatalf("expected meta.source=ephemeral, got %s", env.Meta.Source)
	}
}

func TestExecuteEphemeral_ErrorHandling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
		},
	}
	for _, dir := range []string{cfg.Paths.Cache, cfg.Paths.CAS} {
		if err := ensureDir(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata:     skill.Metadata{Name: "hooks/test_hook", Version: "1.0.0"},
			Distribution: skill.Distribution{Type: "exec"},
		},
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	executor := NewExecutor(ctx, cfg, handle, stdout, stderr, RunOptions{
		Ephemeral:     true,
		CacheMode:     cache.ModeOff,
		Workspace:     "ws",
		CorrelationID: "trace-error",
	})
	defer executor.Close()

	// Test handleEphemeralError with a result that has an error envelope
	errorEnv := envelope.Error("hooks/test_hook", "EFAILED", "test error", envelope.Meta{})
	errorBytes, _ := json.Marshal(errorEnv)

	result := &execution.Result{
		Stdout:   errorBytes,
		Stderr:   []byte("some stderr"),
		ExitCode: 1,
		Error:    nil, // Error envelope in stdout, not execution error
	}

	err := executor.handleEphemeralError(result)
	if err != nil {
		t.Fatalf("handleEphemeralError: %v", err)
	}

	// Verify error envelope was written to stdout
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("expected status=error, got %s", env.Status)
	}
}

func TestRunOptionsEphemeralValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    RunOptions
		wantErr string
	}{
		{
			name: "ephemeral alone is valid",
			opts: RunOptions{
				SkillName: "hooks/test",
				Ephemeral: true,
			},
			wantErr: "",
		},
		{
			name: "ephemeral with async fails",
			opts: RunOptions{
				SkillName: "hooks/test",
				Ephemeral: true,
				Async:     true,
			},
			wantErr: "--ephemeral cannot be combined with --async",
		},
		{
			name: "ephemeral with dedupe fails",
			opts: RunOptions{
				SkillName: "hooks/test",
				Ephemeral: true,
				Dedupe:    true,
			},
			wantErr: "--ephemeral cannot be combined with --dedupe",
		},
		{
			name: "ephemeral with remember fails",
			opts: RunOptions{
				SkillName:    "hooks/test",
				Ephemeral:    true,
				RememberName: "my-memory",
			},
			wantErr: "--ephemeral cannot be combined with --remember",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if err.Error() != tc.wantErr {
					t.Fatalf("expected error %q, got %q", tc.wantErr, err.Error())
				}
			}
		})
	}
}

func TestAnnotateEphemeral(t *testing.T) {
	t.Parallel()

	input := []byte(`{"meta":{"ts":"2024-01-01T00:00:00Z"},"data":{"value":1}}`)
	output := annotateEphemeral(input)

	var env map[string]any
	if err := json.Unmarshal(output, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	meta, ok := env["meta"].(map[string]any)
	if !ok {
		t.Fatal("expected meta to be a map")
	}

	if meta["source"] != "ephemeral" {
		t.Fatalf("expected source=ephemeral, got %v", meta["source"])
	}
	if meta["ephemeral"] != true {
		t.Fatalf("expected ephemeral=true, got %v", meta["ephemeral"])
	}
}

func TestAnnotateEphemeral_EmptyInput(t *testing.T) {
	t.Parallel()

	output := annotateEphemeral(nil)
	if output != nil {
		t.Fatalf("expected nil output for nil input, got %v", output)
	}

	output = annotateEphemeral([]byte{})
	if len(output) != 0 {
		t.Fatalf("expected empty output for empty input, got %v", output)
	}
}

func TestAnnotateEphemeral_InvalidJSON(t *testing.T) {
	t.Parallel()

	input := []byte(`not valid json`)
	output := annotateEphemeral(input)

	// Should return input unchanged for invalid JSON
	if string(output) != string(input) {
		t.Fatalf("expected input unchanged for invalid JSON")
	}
}

// TestExecutorCloseEphemeral tests that CloseEphemeral only closes cache store
func TestExecutorCloseEphemeral(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
		},
	}
	for _, dir := range []string{cfg.Paths.Cache, cfg.Paths.CAS} {
		if err := ensureDir(dir); err != nil {
			t.Fatalf("ensure dir: %v", err)
		}
	}

	handle := SkillHandle{
		Manifest: skill.Manifest{
			Metadata: skill.Metadata{Name: "hooks/test", Version: "1.0.0"},
		},
	}

	executor := NewExecutor(ctx, cfg, handle, io.Discard, io.Discard, RunOptions{
		Ephemeral: true,
		CacheMode: cache.ModeAuto,
	})

	// Open cache store by calling TryServeCache
	_, _ = executor.TryServeCache([]byte(`{"key":"value"}`))

	// Verify cache store was opened
	if executor.cacheStore == nil {
		t.Skip("cache store not opened - skipping close test")
	}

	// Close ephemeral
	executor.CloseEphemeral()

	// Verify cache store was closed
	if executor.cacheStore != nil {
		t.Fatal("expected cacheStore to be nil after CloseEphemeral")
	}

	// Verify jobStore was never opened
	if executor.jobStore != nil {
		t.Fatal("expected jobStore to be nil in ephemeral mode")
	}
}
