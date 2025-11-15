package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	memstore "github.com/jkatigb/agentctl/internal/storage/memory"
)

func TestOpenAPIImportCommandStoresSpec(t *testing.T) {
	cfg := setupOpenAPITestConfig(t)
	ctx := context.Background()

	// seed directories for cas/memory
	if _, err := cas.NewStore(cfg.Paths.CAS); err != nil {
		t.Fatalf("cas store: %v", err)
	}
	memStore, err := memstore.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("memory open: %v", err)
	}
	t.Cleanup(func() {
		if err := memStore.Close(); err != nil {
			t.Fatalf("memory close: %v", err)
		}
	})

	specPath := filepath.Join("..", "..", "..", "internal", "openapi", "loader", "testdata", "valid-3.0.json")
	cmd := newOpenAPICommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"import", specPath, "--as", "sample", "--workspace", cfg.Home})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("openapi import: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Command != "agentctl.openapi.import" {
		t.Fatalf("unexpected command %s", env.Command)
	}
	data := env.Data.(map[string]any)
	digest, ok := data["digest"].(string)
	if !ok || digest == "" {
		t.Fatalf("expected digest in response")
	}
	if _, err := memStore.Get(ctx, "sample", cfg.Home); err != nil {
		t.Fatalf("memory get: %v", err)
	}

	// ensure digest exists in CAS
	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open cas store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close cas store: %v", err)
		}
	})
	if _, _, err := store.Get(ctx, digest); err != nil {
		t.Fatalf("cas get: %v", err)
	}
}

func setupOpenAPITestConfig(t *testing.T) config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg, err := config.Load(context.Background())
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	dirs := []string{cfg.Home, cfg.Paths.CAS, cfg.Paths.Cache}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return cfg
}
