package cmd

import (
	"context"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func loadConfig(ctx context.Context, opts ...config.Option) (config.Config, error) {
	if cfg, ok := config.FromContext(ctx); ok && len(opts) == 0 {
		return cfg, nil
	}
	if configPath != "" {
		opts = append(opts, config.WithConfigFile(configPath))
	}
	return config.Load(ctx, opts...)
}
