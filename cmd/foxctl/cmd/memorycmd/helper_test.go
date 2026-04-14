package memorycmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/spf13/cobra"
)

func TestWithConfigUsesExisting(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cfg := config.Config{Home: "/tmp/test"}
	ctx := config.WithContext(context.Background(), cfg)
	cmd.SetContext(ctx)

	if err := WithConfig(cmd, func(inner context.Context, loaded config.Config) error {
		if loaded.Home != cfg.Home {
			t.Fatalf("expected home %s, got %s", cfg.Home, loaded.Home)
		}
		if inner != ctx {
			t.Fatalf("expected context reuse")
		}
		return nil
	}); err != nil {
		t.Fatalf("withConfig existing: %v", err)
	}
}

func TestWithConfigLoadsWhenMissing(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(context.Background())
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := WithConfig(cmd, func(_ context.Context, cfg config.Config) error {
		if cfg.Home == "" {
			t.Fatalf("expected home to be set")
		}
		loaded, ok := config.FromContext(cmd.Context())
		if !ok {
			t.Fatalf("expected config attached to command context")
		}
		if loaded.Home != cfg.Home {
			t.Fatalf("context config mismatch")
		}
		return nil
	}); err != nil {
		t.Fatalf("withConfig load: %v", err)
	}
}

func TestWithCacheAndMemoryStore(t *testing.T) {
	cfg := setupHelperConfig(t)
	ctx := context.Background()

	if err := WithCacheStore(ctx, cfg, func(store storage.CacheStore) error {
		if _, err := store.Stats(ctx); err != nil {
			t.Fatalf("cache stats: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("withCacheStore: %v", err)
	}

	if err := WithMemoryStore(ctx, cfg, func(store storage.MemoryStore) error {
		if _, err := store.Stats(ctx); err != nil {
			t.Fatalf("memory stats: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("withMemoryStore: %v", err)
	}
}

func TestWriteHelpers(t *testing.T) {
	buf := &bytes.Buffer{}
	payload := map[string]string{"status": "ok"}
	if err := WriteOK(buf, "foxctl.test", payload); err != nil {
		t.Fatalf("write ok: %v", err)
	}
	var env envelope.Envelope
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Command != "foxctl.test" {
		t.Fatalf("unexpected command %s", env.Command)
	}

	buf.Reset()
	raw := []byte(`{"version":1,"status":"ok"}`)
	if err := WriteEnvelope(buf, raw); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Fatalf("expected newline termination")
	}
}

func setupHelperConfig(t *testing.T) config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	dirs := []string{cfg.Home, cfg.Paths.CAS, cfg.Paths.Jobs, cfg.Paths.Cache, cfg.Paths.Skills}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return cfg
}
