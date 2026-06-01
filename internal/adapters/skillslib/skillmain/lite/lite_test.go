package lite

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillcas"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
)

type testInput struct {
	Path string `json:"path" validate:"required"`
	Name string `json:"name,omitempty"`
}

func TestMainWithCodeSuccess(t *testing.T) {
	stdin := strings.NewReader(`{"path":"/tmp","name":"test"}`)
	var stdout bytes.Buffer

	runCalled := false
	run := func(ctx context.Context, rc *RunContext, in testInput) error {
		runCalled = true
		if in.Path != "/tmp" {
			t.Errorf("expected path /tmp, got %s", in.Path)
		}
		return Emit(rc, "test/cmd", map[string]string{"result": "ok"})
	}

	code := mainWithCode("test/cmd", run, stdin, &stdout)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stdout=%s", code, stdout.String())
	}
	if !runCalled {
		t.Fatal("run function was not called")
	}
	out := stdout.String()
	if !strings.Contains(out, `"status":"ok"`) {
		t.Errorf("expected ok envelope, got: %s", out)
	}
	if !strings.Contains(out, `"result":"ok"`) {
		t.Errorf("expected data result, got: %s", out)
	}
}

func TestMainWithCodeValidationError(t *testing.T) {
	stdin := strings.NewReader(`{}`)
	var stdout bytes.Buffer

	run := func(ctx context.Context, rc *RunContext, in testInput) error {
		return errors.New("should not reach run")
	}

	code := mainWithCode("test/cmd", run, stdin, &stdout)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d; stdout=%s", code, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"status":"error"`) {
		t.Errorf("expected error envelope, got: %s", out)
	}
	if !strings.Contains(out, "path is required") {
		t.Errorf("expected validation message, got: %s", out)
	}
}

func TestMainWithCodeParseError(t *testing.T) {
	stdin := strings.NewReader(`{not json`)
	var stdout bytes.Buffer

	run := func(ctx context.Context, rc *RunContext, in testInput) error {
		return errors.New("should not reach run")
	}

	code := mainWithCode("test/cmd", run, stdin, &stdout)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d; stdout=%s", code, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"status":"error"`) {
		t.Errorf("expected error envelope, got: %s", out)
	}
}

func TestMainWithCodeRunError(t *testing.T) {
	stdin := strings.NewReader(`{"path":"/tmp"}`)
	var stdout bytes.Buffer

	run := func(ctx context.Context, rc *RunContext, in testInput) error {
		return errors.New("boom")
	}

	code := mainWithCode("test/cmd", run, stdin, &stdout)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d; stdout=%s", code, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"status":"error"`) {
		t.Errorf("expected error envelope, got: %s", out)
	}
}

func TestMainWithCodeSkillError(t *testing.T) {
	stdin := strings.NewReader(`{"path":"/tmp"}`)
	var stdout bytes.Buffer

	run := func(ctx context.Context, rc *RunContext, in testInput) error {
		return skillerr.Arg("bad input")
	}

	code := mainWithCode("test/cmd", run, stdin, &stdout)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d; stdout=%s", code, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"code":"EARG"`) {
		t.Errorf("expected EARG code, got: %s", out)
	}
}

func TestMainWithCodeHookUnknownFields(t *testing.T) {
	stdin := strings.NewReader(`{"path":"/tmp","extra_field":"value"}`)
	var stdout bytes.Buffer

	runCalled := false
	run := func(ctx context.Context, rc *RunContext, in testInput) error {
		runCalled = true
		return nil
	}

	code := mainWithCode("hooks/test", run, stdin, &stdout)
	if code != 0 {
		t.Fatalf("expected exit code 0 for hook with unknown fields, got %d; stdout=%s", code, stdout.String())
	}
	if !runCalled {
		t.Fatal("run function was not called")
	}
}

func TestMainWithCodeNonHookUnknownFields(t *testing.T) {
	stdin := strings.NewReader(`{"path":"/tmp","extra_field":"value"}`)
	var stdout bytes.Buffer

	run := func(ctx context.Context, rc *RunContext, in testInput) error {
		return errors.New("should not reach run")
	}

	code := mainWithCode("test/cmd", run, stdin, &stdout)
	if code != 1 {
		t.Fatalf("expected exit code 1 for unknown fields, got %d; stdout=%s", code, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"status":"error"`) {
		t.Errorf("expected error envelope, got: %s", out)
	}
	expectedHint := "Unknown field in input; check field names match the skill's expected parameters (e.g., 'scope' not 'scopes')"
	if !strings.Contains(out, expectedHint) {
		t.Errorf("expected unknown field hint %q, got: %s", expectedHint, out)
	}
}

func TestMainWithCodeEmptyInput(t *testing.T) {
	stdin := strings.NewReader(``)
	var stdout bytes.Buffer

	run := func(ctx context.Context, rc *RunContext, in testInput) error {
		return errors.New("should not reach run")
	}

	code := mainWithCode("test/cmd", run, stdin, &stdout)
	if code != 1 {
		t.Fatalf("expected exit code 1 (validation fails on zero-value), got %d; stdout=%s", code, stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"status":"error"`) {
		t.Errorf("expected error envelope, got: %s", out)
	}
}

func TestRunContextInlineLimitAndShouldTruncate(t *testing.T) {
	tests := []struct {
		name     string
		inlineKB int
		noCAS    bool
		size     int
		want     bool
	}{
		{name: "below limit", inlineKB: 2, size: 2048, want: false},
		{name: "above limit", inlineKB: 2, size: 2049, want: true},
		{name: "no cas disables truncation", inlineKB: 2, noCAS: true, size: 2049, want: false},
		{name: "zero limit disables truncation", inlineKB: 0, size: 1, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &RunContext{InlineKB: tt.inlineKB, NoCAS: tt.noCAS}
			if got := rc.InlineLimit(); got != tt.inlineKB*1024 {
				t.Fatalf("InlineLimit() = %d, want %d", got, tt.inlineKB*1024)
			}
			if got := rc.ShouldTruncate(tt.size); got != tt.want {
				t.Fatalf("ShouldTruncate(%d) = %v, want %v", tt.size, got, tt.want)
			}
		})
	}
}

type liteFakeCASWriter struct{}

func (liteFakeCASWriter) PutArtifact(_ context.Context, r io.Reader, kind string, _ []string) (skillcas.Artifact, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return skillcas.Artifact{}, err
	}
	return skillcas.Artifact{Digest: "sha256:lite", Size: int64(len(data)), Kind: kind}, nil
}

func TestRunContextCASCapability(t *testing.T) {
	rc := &RunContext{
		Config: LiteConfig{CAS: LiteCASPolicy{Store: true, Expose: "hint"}},
		Stdout: &bytes.Buffer{},
	}

	if rc.ShouldStoreCAS() {
		t.Fatal("ShouldStoreCAS should be false without an injected writer")
	}
	if rc.CASExposePolicy() != skillcas.ExposePolicyHint {
		t.Fatalf("CASExposePolicy() = %q, want hint", rc.CASExposePolicy())
	}

	rc.CASWriter = liteFakeCASWriter{}
	if !rc.ShouldStoreCAS() {
		t.Fatal("ShouldStoreCAS should be true with store enabled and an injected writer")
	}

	artifact, err := rc.PutArtifact(context.Background(), strings.NewReader("lite content"), "text/plain", nil)
	if err != nil {
		t.Fatalf("PutArtifact returned error: %v", err)
	}
	if artifact.Size != int64(len("lite content")) || artifact.Kind != "text/plain" {
		t.Fatalf("artifact = %+v, want text/plain artifact with size", artifact)
	}

	rc.NoCAS = true
	if rc.ShouldStoreCAS() {
		t.Fatal("ShouldStoreCAS should be false when NoCAS is set")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("FOXCTL_HOME", "")
	t.Setenv("FOXCTL_INLINE_OUTPUT_KB", "")
	t.Setenv("FOXCTL_PATHS_CAS", "")
	t.Setenv("FOXCTL_PATHS_CACHE", "")
	t.Setenv("FOXCTL_CAS_STORE", "")
	t.Setenv("FOXCTL_CAS_EXPOSE", "")
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Chdir(t.TempDir())

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	wantHome := filepath.Join(tmpDir, ".foxctl")
	if cfg.Home != wantHome {
		t.Fatalf("Home = %q, want %q", cfg.Home, wantHome)
	}
	if cfg.InlineOutputKB != defaultInlineOutputKB {
		t.Fatalf("InlineOutputKB = %d, want %d", cfg.InlineOutputKB, defaultInlineOutputKB)
	}
	if cfg.Paths.CAS != filepath.Join(wantHome, "cas") {
		t.Fatalf("Paths.CAS = %q", cfg.Paths.CAS)
	}
	if cfg.Paths.Cache != filepath.Join(wantHome, "cache") {
		t.Fatalf("Paths.Cache = %q", cfg.Paths.Cache)
	}
	if !cfg.CAS.Store {
		t.Fatal("CAS.Store should default true")
	}
	if cfg.CAS.Expose != "off" {
		t.Fatalf("CAS.Expose = %q, want off", cfg.CAS.Expose)
	}
}

func TestLoadConfigFileAndEnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Chdir(t.TempDir())

	foxctlHome := filepath.Join(tmpDir, "foxctl-home")
	if err := os.MkdirAll(foxctlHome, 0o755); err != nil {
		t.Fatalf("mkdir foxctl home: %v", err)
	}
	t.Setenv("FOXCTL_HOME", foxctlHome)
	if err := os.WriteFile(filepath.Join(foxctlHome, "config.yaml"), []byte(`
inline_output_kb: 64
paths:
  cas: file-cas
  cache: ~/file-cache
cas:
  store: false
  expose: digest
search:
  exa_api_key: file-exa
  tavily_api_key: file-tavily
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("FOXCTL_INLINE_OUTPUT_KB", "128")
	t.Setenv("FOXCTL_PATHS_CAS", "env-cas")
	t.Setenv("FOXCTL_CAS_STORE", "true")
	t.Setenv("FOXCTL_CAS_EXPOSE", "hint")
	t.Setenv("EXA_API_KEY", "env-exa")
	t.Setenv("TAVILY_API_KEY", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.InlineOutputKB != 128 {
		t.Fatalf("InlineOutputKB = %d, want env override 128", cfg.InlineOutputKB)
	}
	if cfg.Paths.CAS != filepath.Join(foxctlHome, "env-cas") {
		t.Fatalf("Paths.CAS = %q", cfg.Paths.CAS)
	}
	if cfg.Paths.Cache != filepath.Join(tmpDir, "file-cache") {
		t.Fatalf("Paths.Cache = %q", cfg.Paths.Cache)
	}
	if !cfg.CAS.Store {
		t.Fatal("CAS.Store should be true from env override")
	}
	if cfg.CAS.Expose != "hint" {
		t.Fatalf("CAS.Expose = %q, want hint", cfg.CAS.Expose)
	}
	if cfg.Search.ExaAPIKey != "env-exa" {
		t.Fatalf("Search.ExaAPIKey = %q, want env-exa", cfg.Search.ExaAPIKey)
	}
	if cfg.Search.TavilyAPIKey != "file-tavily" {
		t.Fatalf("Search.TavilyAPIKey = %q, want file-tavily", cfg.Search.TavilyAPIKey)
	}
}

func TestLoadConfigDotEnvPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	userHome := filepath.Join(tmpDir, "home")
	foxctlHome := filepath.Join(tmpDir, "foxctl-home")
	projectDir := filepath.Join(tmpDir, "project")
	for _, dir := range []string{filepath.Join(userHome, ".foxctl"), foxctlHome, projectDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	t.Setenv("HOME", userHome)
	t.Setenv("FOXCTL_HOME", foxctlHome)
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	t.Chdir(projectDir)

	if err := os.WriteFile(filepath.Join(userHome, ".foxctl", ".env"), []byte("EXA_API_KEY=global\nTAVILY_API_KEY=global\n"), 0o644); err != nil {
		t.Fatalf("write global env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(foxctlHome, ".env"), []byte("EXA_API_KEY=custom\n"), 0o644); err != nil {
		t.Fatalf("write custom env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env"), []byte("EXA_API_KEY=project\n"), 0o644); err != nil {
		t.Fatalf("write project env: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Search.ExaAPIKey != "project" {
		t.Fatalf("Search.ExaAPIKey = %q, want project", cfg.Search.ExaAPIKey)
	}
	if cfg.Search.TavilyAPIKey != "global" {
		t.Fatalf("Search.TavilyAPIKey = %q, want global", cfg.Search.TavilyAPIKey)
	}
}

func TestBootstrap(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// config.Load defaults home to ~/.foxctl; NewPathValidator uses EvalSymlinks
	// on allowed roots, so the directory must exist.
	if err := os.MkdirAll(filepath.Join(tmpDir, ".foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir .foxctl: %v", err)
	}

	var stdout bytes.Buffer
	rc, err := Bootstrap(context.Background(), &stdout)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	if rc == nil {
		t.Fatal("expected non-nil RunContext")
	}
	if rc.Workspace == "" {
		t.Error("expected non-empty workspace")
	}
	if rc.AgentID == "" {
		t.Error("expected non-empty agent ID")
	}
	if rc.Validator == nil {
		t.Error("expected non-nil validator")
	}
	if rc.PathValidator == nil {
		t.Error("expected non-nil path validator")
	}
}

func TestResolvePath(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := os.MkdirAll(filepath.Join(tmpDir, ".foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir .foxctl: %v", err)
	}

	var stdout bytes.Buffer
	rc, err := Bootstrap(context.Background(), &stdout)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}

	ws, resolved, err := ResolvePath(rc, "")
	if err != nil {
		t.Fatalf("ResolvePath empty failed: %v", err)
	}
	if ws == "" {
		t.Error("expected non-empty workspace")
	}
	if resolved != ws {
		t.Errorf("expected resolved == workspace for empty candidate, got %q vs %q", resolved, ws)
	}
}

func TestValidatePath(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := os.MkdirAll(filepath.Join(tmpDir, ".foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir .foxctl: %v", err)
	}

	var stdout bytes.Buffer
	rc, err := Bootstrap(context.Background(), &stdout)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}

	// Valid path inside workspace
	resolved, err := ValidatePath(rc, rc.Workspace)
	if err != nil {
		t.Errorf("ValidatePath workspace failed: %v", err)
	}
	if resolved == "" {
		t.Error("expected non-empty resolved path")
	}

	// Invalid path outside workspace
	_, err = ValidatePath(rc, "/etc/passwd")
	if err == nil {
		t.Error("expected error for path outside workspace")
	}
}

func TestGuardCallPassthrough(t *testing.T) {
	ctx := context.Background()
	called := false
	err := GuardCall(ctx, BreakerHTTP, func(ctx context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if !called {
		t.Error("fn was not called")
	}
}

func TestGuardCallErrorPassthrough(t *testing.T) {
	ctx := context.Background()
	expectedErr := errors.New("expected")
	err := GuardCall(ctx, BreakerLLMProvider, func(ctx context.Context) error {
		return expectedErr
	})
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestResolvePaths(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	if err := os.MkdirAll(filepath.Join(tmpDir, ".foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir .foxctl: %v", err)
	}

	var stdout bytes.Buffer
	rc, err := Bootstrap(context.Background(), &stdout)
	if err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}

	testFile := filepath.Join(rc.Workspace, "lite-test-foo.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("setup file: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(testFile) })

	resolved, err := ResolvePaths(rc, "", []string{"lite-test-*.txt"})
	if err != nil {
		t.Fatalf("ResolvePaths failed: %v", err)
	}
	if len(resolved) == 0 {
		t.Error("expected at least one resolved path from glob")
	}
}

func TestFatal(t *testing.T) {
	var buf bytes.Buffer
	err := Fatal(&buf, "test/cmd", skillerr.Runtime("something broke"))
	if err != nil {
		t.Fatalf("Fatal returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"status":"error"`) {
		t.Errorf("expected error envelope, got: %s", out)
	}
	if !strings.Contains(out, `"code":"ERUNTIME"`) {
		t.Errorf("expected ERUNTIME code, got: %s", out)
	}
}
