package memorycmd

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/jkatigb/agentctl/internal/cache"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	errs "github.com/jkatigb/agentctl/internal/errors"
	memstore "github.com/jkatigb/agentctl/internal/memory"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/spf13/cobra"
)

// WithConfig loads configuration once for a command invocation and exposes it to the callback.
//
// If a config has already been attached to the command context it is reused, otherwise the helper
// loads it and updates the command context to avoid duplicate loads for nested helpers.
func WithConfig(cmd *cobra.Command, fn func(context.Context, config.Config) error) error {
	ctx := cmd.Context()
	if cfg, ok := config.FromContext(ctx); ok {
		return fn(ctx, cfg)
	}

	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx = config.WithContext(ctx, cfg)
	cmd.SetContext(ctx)
	return fn(ctx, cfg)
}

// WithCacheStore opens the cache store for the provided configuration and ensures cleanup.
func WithCacheStore(ctx context.Context, cfg config.Config, fn func(storage.CacheStore) error) error {
	store, err := cache.Open(ctx, cfg.Paths.Cache, cache.Options{
		AutoTTL: cfg.Memory.AutoCacheTTL,
		CASPath: cfg.Paths.CAS,
	})
	if err != nil {
		return fmt.Errorf("open cache store: %w", err)
	}
	defer func() {
		errs.Ignore(store.Close(), "close cache store helper")
	}()
	return fn(store)
}

// WithMemoryStore opens the named memory store for the provided configuration and ensures cleanup.
func WithMemoryStore(ctx context.Context, cfg config.Config, fn func(storage.MemoryStore) error) error {
	store, err := memstore.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer func() {
		errs.Ignore(store.Close(), "close memory store helper")
	}()
	return fn(store)
}

// WriteOK renders a success envelope with the provided command name and payload.
func WriteOK(out io.Writer, command string, data any) error {
	return envelope.Write(out, envelope.OK(command, data))
}

// WriteEnvelope writes a serialized JSON envelope to the output, ensuring it is newline terminated.
func WriteEnvelope(out io.Writer, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	_, err := out.Write(data)
	return err
}
