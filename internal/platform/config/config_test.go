package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg, err := Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	expectedHome := filepath.Join(tmp, ".agentctl")
	if cfg.Home != expectedHome {
		t.Fatalf("expected home %s got %s", expectedHome, cfg.Home)
	}
	if cfg.Paths.CAS != filepath.Join(expectedHome, "cas") {
		t.Fatalf("unexpected cas path: %s", cfg.Paths.CAS)
	}
	if cfg.InlineOutputKB != DefaultInlineOutputKB {
		t.Fatalf("expected inline_output_kb default %d got %d", DefaultInlineOutputKB, cfg.InlineOutputKB)
	}
	if cfg.MaxCaptureKB != DefaultMaxCaptureKB {
		t.Fatalf("expected max_capture_kb default %d got %d", DefaultMaxCaptureKB, cfg.MaxCaptureKB)
	}
	if cfg.Memory.AutoCacheTTL.Hours() != 24 {
		t.Fatalf("expected auto cache ttl 24h got %s", cfg.Memory.AutoCacheTTL)
	}
	if cfg.Cache.DefaultMode != "off" {
		t.Fatalf("expected default cache mode off got %s", cfg.Cache.DefaultMode)
	}
	if cfg.Logging.Level != "info" {
		t.Fatalf("expected default logging level info got %s", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "text" {
		t.Fatalf("expected default logging format text got %s", cfg.Logging.Format)
	}
	if cfg.Logging.Output != "" {
		t.Fatalf("expected default logging output empty got %s", cfg.Logging.Output)
	}
	expectedPluginPath := filepath.Join(expectedHome, "plugins")
	if len(cfg.OpenAPI.PluginPath) != 1 || cfg.OpenAPI.PluginPath[0] != expectedPluginPath {
		t.Fatalf("expected default plugin path %s got %v", expectedPluginPath, cfg.OpenAPI.PluginPath)
	}
}

func TestLoadWithConfigFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	home := filepath.Join(tmp, ".agentctl")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfgFile := filepath.Join(home, "config.yaml")
	content := []byte("inline_output_kb: 1024\nlogging:\n  level: warn\n  format: json\n  output: /tmp/agentctl-json.log\npaths:\n  cas: custom/cas\nopenapi:\n  plugin_path: plugins:/opt/agentctl/plugins\n")
	if err := os.WriteFile(cfgFile, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(context.Background(), WithConfigFile(cfgFile))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.InlineOutputKB != 1024 {
		t.Fatalf("expected inline_output_kb override, got %d", cfg.InlineOutputKB)
	}
	expectedCAS := filepath.Join(cfg.Home, "custom/cas")
	if cfg.Paths.CAS != expectedCAS {
		t.Fatalf("expected cas path %s got %s", expectedCAS, cfg.Paths.CAS)
	}
	if cfg.Logging.Level != "warn" {
		t.Fatalf("expected logging level warn got %s", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Fatalf("expected logging format json got %s", cfg.Logging.Format)
	}
	if cfg.Logging.Output != "/tmp/agentctl-json.log" {
		t.Fatalf("expected logging output /tmp/agentctl-json.log got %s", cfg.Logging.Output)
	}
	expectedPluginPaths := []string{
		filepath.Join(cfg.Home, "plugins"),
		"/opt/agentctl/plugins",
	}
	if diff := cmpSlices(expectedPluginPaths, cfg.OpenAPI.PluginPath); diff != "" {
		t.Fatalf("unexpected plugin paths: %s", diff)
	}
}

func TestLoadWithEnvOverridesAndTildePaths(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	t.Setenv("AGENTCTL_INLINE_OUTPUT_KB", "512")
	t.Setenv("AGENTCTL_EMBEDDING_API_KEY", "embed-key")
	t.Setenv("AGENTCTL_PATHS_CAS", "~/custom/cas")
	t.Setenv("AGENTCTL_LOGGING_LEVEL", "DEBUG")
	t.Setenv("AGENTCTL_LOGGING_FORMAT", "JSON")
	t.Setenv("AGENTCTL_LOGGING_OUTPUT", "/tmp/env-agentctl.log")
	t.Setenv("AGENTCTL_OPENAPI_PLUGIN_PATH", "~/plugins:/usr/local/agentctl/plugins")

	cfg, err := Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.InlineOutputKB != 512 {
		t.Fatalf("expected inline_output_kb env override, got %d", cfg.InlineOutputKB)
	}
	if cfg.Embedding.APIKey != "embed-key" {
		t.Fatalf("expected embedding api key override, got %q", cfg.Embedding.APIKey)
	}
	expectedCAS := filepath.Join(tmp, "custom/cas")
	if cfg.Paths.CAS != expectedCAS {
		t.Fatalf("expected cas path %s got %s", expectedCAS, cfg.Paths.CAS)
	}
	if cfg.Logging.Level != "debug" {
		t.Fatalf("expected logging level debug got %s", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Fatalf("expected logging format json got %s", cfg.Logging.Format)
	}
	if cfg.Logging.Output != "/tmp/env-agentctl.log" {
		t.Fatalf("expected logging output /tmp/env-agentctl.log got %s", cfg.Logging.Output)
	}
	expectedPluginPaths := []string{
		filepath.Join(tmp, "plugins"),
		"/usr/local/agentctl/plugins",
	}
	if diff := cmpSlices(expectedPluginPaths, cfg.OpenAPI.PluginPath); diff != "" {
		t.Fatalf("unexpected plugin paths: %s", diff)
	}
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Home: "/tmp"}
	ctx = WithContext(ctx, cfg)
	stored, ok := FromContext(ctx)
	if !ok {
		t.Fatalf("expected config in context")
	}
	if stored.Home != cfg.Home {
		t.Fatalf("expected home %s got %s", cfg.Home, stored.Home)
	}
}

func TestInvalidInlineOutputFallsBackToDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	home := filepath.Join(tmp, ".agentctl")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfgFile := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("inline_output_kb: -64\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(context.Background(), WithConfigFile(cfgFile))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.InlineOutputKB != DefaultInlineOutputKB {
		t.Fatalf("expected inline_output_kb default %d got %d", DefaultInlineOutputKB, cfg.InlineOutputKB)
	}
}

func cmpSlices(expected, actual []string) string {
	if len(expected) != len(actual) {
		return fmt.Sprintf("length mismatch expected %d got %d (%v)", len(expected), len(actual), actual)
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return fmt.Sprintf("at %d expected %s got %s", i, expected[i], actual[i])
		}
	}
	return ""
}
