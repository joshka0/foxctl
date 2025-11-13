package cmd

import (
	"context"
	"io"
	"os"

	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	memstore "github.com/jkatigb/agentctl/internal/memory"
	"github.com/jkatigb/agentctl/internal/workspace"
	"github.com/spf13/cobra"
)

func resolveWorkspace(cfg config.Config, override string) string {
	if override != "" {
		return workspace.Normalize(override)
	}
	if !cfg.Memory.AutoLoadWorkspace {
		if wd, err := os.Getwd(); err == nil {
			return workspace.Normalize(wd)
		}
		return ""
	}
	return workspace.Detect("")
}

func readMemoryPayload(cmd *cobra.Command, file, data string) ([]byte, error) {
	switch {
	case file == "-":
		return io.ReadAll(cmd.InOrStdin())
	case file != "":
		return os.ReadFile(file)
	case data != "":
		return []byte(data), nil
	default:
		return io.ReadAll(cmd.InOrStdin())
	}
}

func withCacheStore(ctx context.Context, cfg config.Config, fn func(*cache.Store) error) error {
	store, err := cache.Open(ctx, cfg.Paths.Cache, cache.Options{
		AutoTTL: cfg.Memory.AutoCacheTTL,
		CASPath: cfg.Paths.CAS,
	})
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return fn(store)
}

func withMemoryStore(ctx context.Context, cfg config.Config, fn func(*memstore.Store) error) error {
	store, err := memstore.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return fn(store)
}
