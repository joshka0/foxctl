package cmd

import (
	"context"

	"github.com/jkatigb/agentctl/internal/platform/config"
)

func loadConfig(ctx context.Context, opts ...config.Option) (config.Config, error) {
	if cfg, ok := config.FromContext(ctx); ok {
		return cfg, nil
	}
	if configPath != "" && len(opts) == 0 {
		opts = append(opts, config.WithConfigFile(configPath))
	}
	return config.Load(ctx, opts...)
}
