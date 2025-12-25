package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

func TestOK(t *testing.T) {
	data := map[string]string{"key": "value"}
	env := OK("test.command", data)

	if env.Version != envelope.Version {
		t.Fatalf("expected version %d, got %d", envelope.Version, env.Version)
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("expected status ok, got %s", env.Status)
	}
	if env.Command != "test.command" {
		t.Fatalf("expected command test.command, got %s", env.Command)
	}
	if env.Meta.TS == "" {
		t.Fatal("expected non-empty timestamp")
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}
}

func TestOKWithOptions(t *testing.T) {
	env := OK("test.cmd", nil,
		WithSource("run"),
		WithWorkspace("/tmp/ws"),
		WithSkillVersion("1.0.0"),
		WithJobID("job-123"),
		WithRunner("wasi"),
		WithDuration(100),
	)

	if env.Meta.Source != "run" {
		t.Fatalf("expected source run, got %s", env.Meta.Source)
	}
	if env.Meta.Workspace != "/tmp/ws" {
		t.Fatalf("expected workspace /tmp/ws, got %s", env.Meta.Workspace)
	}
	if env.Meta.SkillVer != "1.0.0" {
		t.Fatalf("expected skill_version 1.0.0, got %s", env.Meta.SkillVer)
	}
	if env.Meta.JobID != "job-123" {
		t.Fatalf("expected job_id job-123, got %s", env.Meta.JobID)
	}
	if env.Meta.Runner != "wasi" {
		t.Fatalf("expected runner wasi, got %s", env.Meta.Runner)
	}
	if env.Meta.DurationMS != 100 {
		t.Fatalf("expected duration_ms 100, got %d", env.Meta.DurationMS)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}
}

func TestError(t *testing.T) {
	data := map[string]string{"detail": "more info"}
	env := Error("test.fail", ErrorCodeEARG, "invalid argument", data)

	if env.Version != envelope.Version {
		t.Fatalf("expected version %d, got %d", envelope.Version, env.Version)
	}
	if env.Status != envelope.StatusError {
		t.Fatalf("expected status error, got %s", env.Status)
	}
	if env.Command != "test.fail" {
		t.Fatalf("expected command test.fail, got %s", env.Command)
	}
	if env.Error.Code != "EARG" {
		t.Fatalf("expected error code EARG, got %s", env.Error.Code)
	}
	if env.Error.Message != "invalid argument" {
		t.Fatalf("expected error message 'invalid argument', got %s", env.Error.Message)
	}
	if env.Meta.TS == "" {
		t.Fatal("expected non-empty timestamp")
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}
}

func TestErrorCodes(t *testing.T) {
	codes := []ErrorCode{
		ErrorCodeEARG,
		ErrorCodeEOpenAPI,
		ErrorCodeEAuth,
		ErrorCodeEPagination,
		ErrorCodeERateLimit,
		ErrorCodeERuntime,
		ErrorCodeERuntimeRestart,
		ErrorCodeEOutputTooLarge,
		ErrorCodeEPolicy,
		ErrorCodeENotFound,
		ErrorCodeETimeout,
		ErrorCodeESkillDown,
		ErrorCodeEParse,
		ErrorCodeEEnvelope,
		ErrorCodeEIO,
		ErrorCodeECanceled,
	}

	for _, code := range codes {
		env := Error("test", code, "test error", nil)
		if env.Error.Code != string(code) {
			t.Fatalf("expected error code %s, got %s", code, env.Error.Code)
		}
		if err := Validate(env); err != nil {
			t.Fatalf("validation failed for code %s: %v", code, err)
		}
	}
}

func TestValidate(t *testing.T) {
	t.Run("valid ok envelope", func(t *testing.T) {
		env := OK("test", nil)
		if err := Validate(env); err != nil {
			t.Fatalf("expected valid envelope: %v", err)
		}
	})

	t.Run("valid error envelope", func(t *testing.T) {
		env := Error("test", ErrorCodeEARG, "bad arg", nil)
		if err := Validate(env); err != nil {
			t.Fatalf("expected valid envelope: %v", err)
		}
	})

	t.Run("invalid version", func(t *testing.T) {
		env := OK("test", nil)
		env.Version = 99
		if err := Validate(env); err == nil {
			t.Fatal("expected validation error for invalid version")
		}
	})

	t.Run("missing timestamp", func(t *testing.T) {
		env := OK("test", nil)
		env.Meta.TS = ""
		if err := Validate(env); err == nil {
			t.Fatal("expected validation error for missing timestamp")
		}
	})

	t.Run("error without code", func(t *testing.T) {
		env := Error("test", "", "msg", nil)
		if err := Validate(env); err == nil {
			t.Fatal("expected validation error for missing error code")
		}
	})

	t.Run("error without message", func(t *testing.T) {
		env := Error("test", ErrorCodeEARG, "", nil)
		if err := Validate(env); err == nil {
			t.Fatal("expected validation error for missing error message")
		}
	})

	t.Run("rejects http summary below error range", func(t *testing.T) {
		data := HTTPErrorData{Summary: HTTPSummary{StatusCode: 200}}
		env := HTTPError("test.http", "unexpected", data)
		if err := Validate(env); err == nil || !strings.Contains(err.Error(), "status_code=200") {
			t.Fatalf("expected status code validation error, got %v", err)
		}
	})

	t.Run("rejects structured summary map", func(t *testing.T) {
		data := &ErrorData{Summary: map[string]any{"status_code": 201}}
		env := Error("test.struct", ErrorCodeEARG, "oops", data)
		if err := Validate(env); err == nil || !strings.Contains(err.Error(), "status_code=201") {
			t.Fatalf("expected structured status code error, got %v", err)
		}
	})

	t.Run("enforces cas digest for map data", func(t *testing.T) {
		data := map[string]string{"artifact": "sha256:deadbeef"}
		env := OK("test.cas", data)
		env.Meta.CASDigest = "sha256:beaded"
		if err := Validate(env); err == nil || !strings.Contains(err.Error(), "meta.cas_digest") {
			t.Fatalf("expected cas digest mismatch error, got %v", err)
		}
	})
}

func TestWrite(t *testing.T) {
	buf := &bytes.Buffer{}
	env := OK("test.write", map[string]int{"count": 42})

	if err := Write(buf, env); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected non-empty buffer")
	}

	// Verify it's valid JSON
	var decoded envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// Verify it ends with newline (json.Encoder behavior)
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatal("expected output to end with newline")
	}
}

func TestMustWrite(t *testing.T) {
	buf := &bytes.Buffer{}
	env := OK("test.mustwrite", nil)

	if err := MustWrite(buf, env); err != nil {
		t.Fatalf("MustWrite failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected MustWrite to write data")
	}
}

func TestWriteOK(t *testing.T) {
	buf := &bytes.Buffer{}
	data := map[string]string{"status": "healthy"}

	if err := WriteOK(buf, "health.check", data, WithSource("run")); err != nil {
		t.Fatalf("WriteOK failed: %v", err)
	}

	var decoded envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if decoded.Status != envelope.StatusOK {
		t.Fatalf("expected status ok, got %s", decoded.Status)
	}
	if decoded.Command != "health.check" {
		t.Fatalf("expected command health.check, got %s", decoded.Command)
	}
	if decoded.Meta.Source != "run" {
		t.Fatalf("expected source run, got %s", decoded.Meta.Source)
	}
}

func TestAnnotateRunBytesPreservesEmptySlices(t *testing.T) {
	type recallOutput struct {
		Matches []string `json:"matches"`
		Status  string   `json:"status"`
	}

	env := envelope.OK("session/recall", recallOutput{
		Matches: []string{},
		Status:  "ok",
	})

	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}

	annotated := AnnotateRunBytes(raw, "/workspace/path", "1.2.3")

	var decoded struct {
		Data recallOutput `json:"data"`
		Meta struct {
			Workspace string `json:"workspace"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(annotated, &decoded); err != nil {
		t.Fatalf("unmarshal annotated: %v", err)
	}

	if decoded.Data.Matches == nil {
		t.Fatal("expected matches to be empty slice, got nil")
	}
	if len(decoded.Data.Matches) != 0 {
		t.Fatalf("expected zero matches, got %d", len(decoded.Data.Matches))
	}
	if decoded.Meta.Workspace != "/workspace/path" {
		t.Fatalf("expected workspace annotation, got %q", decoded.Meta.Workspace)
	}
}

func TestWriteError(t *testing.T) {
	buf := &bytes.Buffer{}

	if err := WriteError(buf, "test.fail", ErrorCodeEAuth, "auth failed", nil, WithSource("run")); err != nil {
		t.Fatalf("WriteError failed: %v", err)
	}

	var decoded envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if decoded.Status != envelope.StatusError {
		t.Fatalf("expected status error, got %s", decoded.Status)
	}
	if decoded.Error.Code != "EAUTH" {
		t.Fatalf("expected error code EAUTH, got %s", decoded.Error.Code)
	}
	if decoded.Error.Message != "auth failed" {
		t.Fatalf("expected message 'auth failed', got %s", decoded.Error.Message)
	}
}

func TestWriteInvalidEnvelope(t *testing.T) {
	buf := &bytes.Buffer{}
	env := OK("test", nil)
	env.Version = 999 // Invalid version

	if err := Write(buf, env); err == nil {
		t.Fatal("expected Write to fail for invalid envelope")
	}

	// Buffer should be empty since validation failed
	if buf.Len() > 0 {
		t.Fatal("expected empty buffer after validation failure")
	}
}

func TestAnnotateRun(t *testing.T) {
	env := OK("test.cmd", map[string]string{"key": "val"})
	annotated := AnnotateRun(env, "/tmp/workspace", "v1.2.3")

	if annotated.Meta.Source != "run" {
		t.Fatalf("expected source run, got %s", annotated.Meta.Source)
	}
	if annotated.Meta.Workspace != "/tmp/workspace" {
		t.Fatalf("expected workspace /tmp/workspace, got %s", annotated.Meta.Workspace)
	}
	if annotated.Meta.SkillVer != "v1.2.3" {
		t.Fatalf("expected skill_version v1.2.3, got %s", annotated.Meta.SkillVer)
	}
	if err := Validate(annotated); err != nil {
		t.Fatalf("validation failed: %v", err)
	}
}

func TestAnnotateRunEmptyFields(t *testing.T) {
	env := OK("test.cmd", nil)
	annotated := AnnotateRun(env, "", "")

	if annotated.Meta.Source != "run" {
		t.Fatalf("expected source run, got %s", annotated.Meta.Source)
	}
	if annotated.Meta.Workspace != "" {
		t.Fatalf("expected empty workspace, got %s", annotated.Meta.Workspace)
	}
	if annotated.Meta.SkillVer != "" {
		t.Fatalf("expected empty skill_version, got %s", annotated.Meta.SkillVer)
	}
}

func TestAnnotateCacheHit(t *testing.T) {
	env := OK("test.cmd", map[string]int{"count": 5})
	annotated, err := AnnotateCacheHit(env, "cache-key-abc", "/tmp/ws", "v2.0.0")
	if err != nil {
		t.Fatalf("AnnotateCacheHit failed: %v", err)
	}

	if annotated.Meta.Source != "cache" {
		t.Fatalf("expected source cache, got %s", annotated.Meta.Source)
	}
	if annotated.Meta.CacheKey != "cache-key-abc" {
		t.Fatalf("expected cache_key cache-key-abc, got %s", annotated.Meta.CacheKey)
	}
	if annotated.Meta.Workspace != "/tmp/ws" {
		t.Fatalf("expected workspace /tmp/ws, got %s", annotated.Meta.Workspace)
	}
	if annotated.Meta.SkillVer != "v2.0.0" {
		t.Fatalf("expected skill_version v2.0.0, got %s", annotated.Meta.SkillVer)
	}
}

func TestAnnotateCacheHitBytes(t *testing.T) {
	env := OK("test.cmd", map[string]string{"result": "data"}, WithDuration(100))
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	annotated, err := AnnotateCacheHitBytes(data, "key123", "/workspace", "v1.0")
	if err != nil {
		t.Fatalf("AnnotateCacheHitBytes failed: %v", err)
	}

	var decoded envelope.Envelope
	if err := json.Unmarshal(annotated, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Meta.Source != "cache" {
		t.Fatalf("expected source cache, got %s", decoded.Meta.Source)
	}
	if decoded.Meta.CacheKey != "key123" {
		t.Fatalf("expected cache_key key123, got %s", decoded.Meta.CacheKey)
	}
	if decoded.Meta.DurationMS != 100 {
		t.Fatal("expected duration to be preserved")
	}
}

func TestAnnotateCacheHitBytesInvalidJSON(t *testing.T) {
	_, err := AnnotateCacheHitBytes([]byte("not json"), "key", "", "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestAnnotateRunBytes(t *testing.T) {
	env := OK("test", map[string]bool{"success": true})
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	annotated := AnnotateRunBytes(data, "/path/to/ws", "v3.0.0")

	var decoded envelope.Envelope
	if err := json.Unmarshal(annotated, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Meta.Source != "run" {
		t.Fatalf("expected source run, got %s", decoded.Meta.Source)
	}
	if decoded.Meta.Workspace != "/path/to/ws" {
		t.Fatalf("expected workspace /path/to/ws, got %s", decoded.Meta.Workspace)
	}
	if decoded.Meta.SkillVer != "v3.0.0" {
		t.Fatalf("expected skill_version v3.0.0, got %s", decoded.Meta.SkillVer)
	}
}

func TestAnnotateRunBytesInvalidJSON(t *testing.T) {
	// Should return original bytes on error
	invalid := []byte("not json")
	result := AnnotateRunBytes(invalid, "/ws", "v1")

	if !bytes.Equal(result, invalid) {
		t.Fatal("expected original bytes to be returned on parse error")
	}
}

func TestSummarizeForMemory(t *testing.T) {
	tests := []struct {
		name     string
		env      envelope.Envelope
		expected string
	}{
		{
			name:     "with workspace",
			env:      OK("fs/read", nil, WithWorkspace("/home/user/project")),
			expected: "fs/read (project)",
		},
		{
			name:     "without workspace",
			env:      OK("http/get", nil),
			expected: "http/get",
		},
		{
			name:     "with nested workspace path",
			env:      OK("text/grep", nil, WithWorkspace("/home/user/repos/myapp/src")),
			expected: "text/grep (src)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SummarizeForMemory(tt.env)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestSummarizeForMemoryBytes(t *testing.T) {
	env := OK("repo/index", nil, WithWorkspace("/repos/agentctl"))
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	summary := SummarizeForMemoryBytes(data)
	expected := "repo/index (agentctl)"
	if summary != expected {
		t.Fatalf("expected %q, got %q", expected, summary)
	}
}

func TestSummarizeForMemoryBytesInvalidJSON(t *testing.T) {
	summary := SummarizeForMemoryBytes([]byte("not json"))
	if summary != "" {
		t.Fatalf("expected empty string for invalid JSON, got %q", summary)
	}
}

func TestWithCASDigest(t *testing.T) {
	digest := "sha256:abc123"
	env := OK("test", nil, WithCASDigest(digest))

	if env.Meta.CASDigest != digest {
		t.Fatalf("expected cas_digest %s, got %s", digest, env.Meta.CASDigest)
	}
}

func TestWithMemoryRef(t *testing.T) {
	ref := &envelope.MemoryRef{
		Name:      "test-memory",
		Type:      "result",
		Workspace: "ws1",
	}
	env := OK("test", nil, WithMemoryRef(ref))

	if env.Meta.Memory == nil {
		t.Fatal("expected memory ref to be set")
	}
	if env.Meta.Memory.Name != "test-memory" {
		t.Fatalf("expected memory name test-memory, got %s", env.Meta.Memory.Name)
	}
	if env.Meta.Memory.Type != "result" {
		t.Fatalf("expected memory type result, got %s", env.Meta.Memory.Type)
	}
	if env.Meta.Memory.Workspace != "ws1" {
		t.Fatalf("expected memory workspace ws1, got %s", env.Meta.Memory.Workspace)
	}
}

func TestWithMetaMutatorOption(t *testing.T) {
	env := OK("test", nil, WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "custom"
		m.DurationMS = 999
		seq := 5
		m.Seq = &seq
	}))

	if env.Meta.Source != "custom" {
		t.Fatalf("expected source custom, got %s", env.Meta.Source)
	}
	if env.Meta.DurationMS != 999 {
		t.Fatalf("expected duration_ms 999, got %d", env.Meta.DurationMS)
	}
	if env.Meta.Seq == nil || *env.Meta.Seq != 5 {
		t.Fatal("expected seq to be 5")
	}
}

func TestMultipleOptions(t *testing.T) {
	env := OK("test", map[string]int{"n": 1},
		WithSource("run"),
		WithWorkspace("/ws"),
		WithSkillVersion("v1"),
		WithJobID("job1"),
		WithCacheKey("key1"),
		WithCASDigest("sha256:abc"),
		WithRunner("exec"),
		WithDuration(50),
		WithData(map[string]int{"n": 2}), // Should override initial data
	)

	if env.Meta.Source != "run" {
		t.Fatal("expected source run")
	}
	if env.Meta.Workspace != "/ws" {
		t.Fatal("expected workspace /ws")
	}
	if env.Meta.SkillVer != "v1" {
		t.Fatal("expected skill_version v1")
	}
	if env.Meta.JobID != "job1" {
		t.Fatal("expected job_id job1")
	}
	if env.Meta.CacheKey != "key1" {
		t.Fatal("expected cache_key key1")
	}
	if env.Meta.CASDigest != "sha256:abc" {
		t.Fatal("expected cas_digest sha256:abc")
	}
	if env.Meta.Runner != "exec" {
		t.Fatal("expected runner exec")
	}
	if env.Meta.DurationMS != 50 {
		t.Fatal("expected duration_ms 50")
	}

	// Verify data was overridden
	data, ok := env.Data.(map[string]int)
	if !ok || data["n"] != 2 {
		t.Fatal("expected data to be overridden to n=2")
	}
}

func TestMustValidate(t *testing.T) {
	t.Run("valid envelope does not panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MustValidate panicked on valid envelope: %v", r)
			}
		}()
		env := OK("test", nil)
		MustValidate(env)
	})

	t.Run("invalid envelope panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("MustValidate should have panicked on invalid envelope")
			}
		}()
		env := OK("test", nil)
		env.Version = 999
		MustValidate(env)
	})
}

func TestIsOK(t *testing.T) {
	env := OK("test", nil)
	if !IsOK(env) {
		t.Fatal("expected IsOK to return true for ok envelope")
	}

	errEnv := Error("test", ErrorCodeEARG, "error", nil)
	if IsOK(errEnv) {
		t.Fatal("expected IsOK to return false for error envelope")
	}
}

func TestIsError(t *testing.T) {
	env := Error("test", ErrorCodeEAuth, "auth failed", nil)
	if !IsError(env) {
		t.Fatal("expected IsError to return true for error envelope")
	}

	okEnv := OK("test", nil)
	if IsError(okEnv) {
		t.Fatal("expected IsError to return false for ok envelope")
	}
}

func TestGetErrorCode(t *testing.T) {
	env := Error("test", ErrorCodeETimeout, "timed out", nil)
	code := GetErrorCode(env)
	if code != ErrorCodeETimeout {
		t.Fatalf("expected error code ETIMEOUT, got %s", code)
	}

	okEnv := OK("test", nil)
	code = GetErrorCode(okEnv)
	if code != "" {
		t.Fatalf("expected empty error code for ok envelope, got %s", code)
	}
}

func TestTimestampIsSet(t *testing.T) {
	before := time.Now().Add(-1 * time.Second) // Allow 1 second slack
	env := OK("test", nil)
	after := time.Now().Add(2 * time.Second) // Allow 2 second slack

	if env.Meta.TS == "" {
		t.Fatal("expected timestamp to be set")
	}

	// Parse and verify it's within reasonable bounds
	ts, err := time.Parse(time.RFC3339, env.Meta.TS)
	if err != nil {
		t.Fatalf("failed to parse timestamp: %v", err)
	}

	if ts.Before(before) || ts.After(after) {
		t.Fatalf("timestamp %v is outside expected range [%v, %v]", ts, before, after)
	}
}

func TestErrorWithOptions(t *testing.T) {
	env := Error("test.error", ErrorCodeEPolicy, "policy violation",
		map[string]string{"rule": "network.egress"},
		WithSource("run"),
		WithWorkspace("/project"),
	)

	if env.Error.Code != "EPOLICY" {
		t.Fatalf("expected error code EPOLICY, got %s", env.Error.Code)
	}
	if env.Meta.Source != "run" {
		t.Fatalf("expected source run, got %s", env.Meta.Source)
	}
	if env.Meta.Workspace != "/project" {
		t.Fatalf("expected workspace /project, got %s", env.Meta.Workspace)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}
}

// Tests for validation extensions

func TestValidateCASDigest(t *testing.T) {
	t.Run("valid artifact and cas_digest", func(t *testing.T) {
		digest := "sha256:abc123"
		env := OK("test", map[string]any{
			"artifact": digest,
		}, WithCASDigest(digest))

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass: %v", err)
		}
	})

	t.Run("artifact without cas_digest", func(t *testing.T) {
		env := OK("test", map[string]any{
			"artifact": "sha256:abc123",
		})

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass when artifact exists without cas_digest: %v", err)
		}
	})

	t.Run("mismatched artifact and cas_digest", func(t *testing.T) {
		env := OK("test", map[string]any{
			"artifact": "sha256:abc123",
		}, WithCASDigest("sha256:different"))

		if err := Validate(env); err == nil {
			t.Fatal("validation should fail when artifact and cas_digest don't match")
		}
	})

	t.Run("artifact without sha256 prefix", func(t *testing.T) {
		env := OK("test", map[string]any{
			"artifact": "baddigest",
		}, WithCASDigest("baddigest"))

		if err := Validate(env); err == nil {
			t.Fatal("validation should fail when artifact doesn't have sha256: prefix")
		}
	})

	t.Run("no artifact field", func(t *testing.T) {
		env := OK("test", map[string]string{"key": "value"})

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass when no artifact field: %v", err)
		}
	})

	t.Run("artifact map string string", func(t *testing.T) {
		digest := "sha256:def456"
		env := OK("test", map[string]string{
			"artifact": digest,
		}, WithCASDigest(digest))

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass for map[string]string artifact: %v", err)
		}
	})

	t.Run("artifact map string string mismatch", func(t *testing.T) {
		env := OK("test", map[string]string{
			"artifact": "sha256:def456",
		}, WithCASDigest("sha256:notmatch"))

		if err := Validate(env); err == nil {
			t.Fatal("validation should fail when map[string]string artifact mismatches cas_digest")
		}
	})
}

func TestValidateCacheMetadata(t *testing.T) {
	t.Run("cache source with cache_key", func(t *testing.T) {
		env := OK("test", nil,
			WithSource("cache"),
			WithCacheKey("key123"),
		)

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass: %v", err)
		}
	})

	t.Run("cache source without cache_key", func(t *testing.T) {
		env := OK("test", nil, WithSource("cache"))

		if err := Validate(env); err == nil {
			t.Fatal("validation should fail when source is cache but cache_key is empty")
		}
	})

	t.Run("non-cache source", func(t *testing.T) {
		env := OK("test", nil, WithSource("run"))

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass for non-cache source: %v", err)
		}
	})
}

func TestValidateMemoryMetadata(t *testing.T) {
	t.Run("memory source with reference", func(t *testing.T) {
		env := OK("test", nil,
			WithSource("memory"),
			WithMemoryRef(&envelope.MemoryRef{Name: "recent", Type: "auto"}),
		)

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass for memory source with reference: %v", err)
		}
	})

	t.Run("memory source without reference", func(t *testing.T) {
		env := OK("test", nil, WithSource("memory"))

		if err := Validate(env); err == nil {
			t.Fatal("validation should fail when memory source lacks memory reference")
		}
	})

	t.Run("memory source missing name", func(t *testing.T) {
		env := OK("test", nil,
			WithSource("memory"),
			WithMemoryRef(&envelope.MemoryRef{Name: "", Type: "auto"}),
		)

		if err := Validate(env); err == nil {
			t.Fatal("validation should fail when memory reference name is empty")
		}
	})

	t.Run("memory source missing type", func(t *testing.T) {
		env := OK("test", nil,
			WithSource("memory"),
			WithMemoryRef(&envelope.MemoryRef{Name: "recent", Type: ""}),
		)

		if err := Validate(env); err == nil {
			t.Fatal("validation should fail when memory reference type is empty")
		}
	})
}

func TestValidateErrorStatusCode(t *testing.T) {
	t.Run("error envelope with valid status code", func(t *testing.T) {
		env := Error("test", ErrorCodeERuntime, "server error", map[string]any{
			"summary": map[string]any{
				"status_code": 500,
			},
		})

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass: %v", err)
		}
	})

	t.Run("error envelope with 4xx status code", func(t *testing.T) {
		env := Error("test", ErrorCodeEARG, "bad request", map[string]any{
			"summary": map[string]any{
				"status_code": 400,
			},
		})

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass: %v", err)
		}
	})

	t.Run("error envelope with 2xx status code", func(t *testing.T) {
		env := Error("test", ErrorCodeERuntime, "error", map[string]any{
			"summary": map[string]any{
				"status_code": 200,
			},
		})

		if err := Validate(env); err == nil {
			t.Fatal("validation should fail when error envelope has 2xx status code")
		}
	})

	t.Run("error envelope without status code", func(t *testing.T) {
		env := Error("test", ErrorCodeERuntime, "error", map[string]string{
			"detail": "some detail",
		})

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass when no status_code field: %v", err)
		}
	})

	t.Run("ok envelope with any status code", func(t *testing.T) {
		env := OK("test", map[string]any{
			"summary": map[string]any{
				"status_code": 200,
			},
		})

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass for ok envelope: %v", err)
		}
	})

	t.Run("http error data summary struct", func(t *testing.T) {
		env := HTTPError("test", "bad gateway", HTTPErrorData{
			Summary: HTTPSummary{
				StatusCode: 502,
			},
		})

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass for HTTPErrorData summary: %v", err)
		}
	})

	t.Run("http error data summary invalid", func(t *testing.T) {
		env := HTTPError("test", "bad request", HTTPErrorData{
			Summary: HTTPSummary{
				StatusCode: 200,
			},
		})

		if err := Validate(env); err == nil {
			t.Fatal("validation should fail for HTTPErrorData with 2xx status")
		}
	})

	t.Run("summary map string string", func(t *testing.T) {
		env := Error("test", ErrorCodeERuntime, "server error", map[string]any{
			"summary": map[string]string{
				"status_code": "503",
			},
		})

		if err := Validate(env); err != nil {
			t.Fatalf("validation should pass for summary map[string]string: %v", err)
		}
	})
}

// Tests for typed error helpers

func TestValidationError(t *testing.T) {
	env := ValidationError("test.validate", "invalid field", ValidationErrorData{
		Field:  "email",
		Value:  "not-an-email",
		Reason: "must be a valid email address",
		Hint:   "use format: user@example.com",
	})

	if env.Status != envelope.StatusError {
		t.Fatal("expected error status")
	}
	if env.Error.Code != "EARG" {
		t.Fatalf("expected EARG code, got %s", env.Error.Code)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	data, ok := env.Data.(ValidationErrorData)
	if !ok {
		t.Fatal("expected ValidationErrorData")
	}
	if data.Field != "email" {
		t.Fatalf("expected field email, got %s", data.Field)
	}
}

func TestHTTPError(t *testing.T) {
	env := HTTPError("http.get", "request failed", HTTPErrorData{
		Summary: HTTPSummary{
			StatusCode: 401,
			Method:     "GET",
			URL:        "https://api.example.com/users",
		},
		Hint: "check your API key",
	})

	if env.Status != envelope.StatusError {
		t.Fatal("expected error status")
	}
	if env.Error.Code != "EAUTH" {
		t.Fatalf("expected EAUTH code for 401, got %s", env.Error.Code)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	data, ok := env.Data.(HTTPErrorData)
	if !ok {
		t.Fatal("expected HTTPErrorData")
	}
	if data.Summary.StatusCode != 401 {
		t.Fatalf("expected status code 401, got %d", data.Summary.StatusCode)
	}
}

func TestHTTPStatusToErrorCode(t *testing.T) {
	tests := []struct {
		statusCode int
		expected   ErrorCode
	}{
		{401, ErrorCodeEAuth},
		{403, ErrorCodeEAuth},
		{404, ErrorCodeENotFound},
		{408, ErrorCodeETimeout},
		{429, ErrorCodeERateLimit},
		{400, ErrorCodeEARG},
		{422, ErrorCodeEARG},
		{500, ErrorCodeERuntime},
		{502, ErrorCodeERuntime},
		{503, ErrorCodeERuntime},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.statusCode), func(t *testing.T) {
			code := httpStatusToErrorCode(tt.statusCode)
			if code != tt.expected {
				t.Fatalf("expected %s for status %d, got %s", tt.expected, tt.statusCode, code)
			}
		})
	}
}

func TestAuthError(t *testing.T) {
	env := AuthError("auth.login", "authentication failed", "check your credentials")

	if env.Error.Code != "EAUTH" {
		t.Fatalf("expected EAUTH code, got %s", env.Error.Code)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	data, ok := env.Data.(ErrorData)
	if !ok {
		t.Fatal("expected ErrorData")
	}
	if data.Hint != "check your credentials" {
		t.Fatalf("expected hint, got %s", data.Hint)
	}
}

func TestNotFoundError(t *testing.T) {
	env := NotFoundError("resource.get", "skill", "foo/bar")

	if env.Error.Code != "ENOTFOUND" {
		t.Fatalf("expected ENOTFOUND code, got %s", env.Error.Code)
	}
	if env.Error.Message != "skill not found" {
		t.Fatalf("unexpected message: %s", env.Error.Message)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	data, ok := env.Data.(ErrorData)
	if !ok {
		t.Fatal("expected ErrorData")
	}
	if data.Context["resource"] != "skill" {
		t.Fatal("expected resource in context")
	}
	if data.Context["identifier"] != "foo/bar" {
		t.Fatal("expected identifier in context")
	}
}

func TestTimeoutError(t *testing.T) {
	env := TimeoutError("job.wait", "skill execution", "30s")

	if env.Error.Code != "ETIMEOUT" {
		t.Fatalf("expected ETIMEOUT code, got %s", env.Error.Code)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	data, ok := env.Data.(ErrorData)
	if !ok {
		t.Fatal("expected ErrorData")
	}
	if data.Context["operation"] != "skill execution" {
		t.Fatal("expected operation in context")
	}
	if data.Context["duration"] != "30s" {
		t.Fatal("expected duration in context")
	}
}

func TestRateLimitError(t *testing.T) {
	env := RateLimitError("api.call", "rate limit exceeded", "60s")

	if env.Error.Code != "ERATELIMIT" {
		t.Fatalf("expected ERATELIMIT code, got %s", env.Error.Code)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	data, ok := env.Data.(ErrorData)
	if !ok {
		t.Fatal("expected ErrorData")
	}
	if data.Context["retry_after"] != "60s" {
		t.Fatal("expected retry_after in context")
	}
}

func TestPolicyError(t *testing.T) {
	env := PolicyError("skill.run", "network.egress", "egress to example.com not allowed")

	if env.Error.Code != "EPOLICY" {
		t.Fatalf("expected EPOLICY code, got %s", env.Error.Code)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	data, ok := env.Data.(ErrorData)
	if !ok {
		t.Fatal("expected ErrorData")
	}
	if data.Context["policy"] != "network.egress" {
		t.Fatal("expected policy in context")
	}
}

func TestErrorWithData(t *testing.T) {
	env := ErrorWithData("test.error", ErrorCodeEIO, "I/O error", ErrorData{
		Detail: "failed to write file",
		Hint:   "check disk space",
		Context: map[string]any{
			"path": "/tmp/file.txt",
		},
	})

	if env.Error.Code != "EIO" {
		t.Fatalf("expected EIO code, got %s", env.Error.Code)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	data, ok := env.Data.(ErrorData)
	if !ok {
		t.Fatal("expected ErrorData")
	}
	if data.Detail != "failed to write file" {
		t.Fatalf("unexpected detail: %s", data.Detail)
	}
	if data.Hint != "check disk space" {
		t.Fatalf("unexpected hint: %s", data.Hint)
	}
}
