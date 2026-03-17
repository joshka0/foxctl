// Package cmd wires up the Cobra commands exposed by the agentctl binary.
package cmd

import (
	"context"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/logging"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:           "agentctl",
	Short:         "agentctl Core Profile v1 CLI",
	Long:          "agentctl is a CLI for running skills, managing CAS artifacts, and executing OpenAPI jobs.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

var configPath string

// Execute runs the root command with the supplied context.
func Execute(ctx context.Context) error {
	rootCmd.SetContext(ctx)
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to a config file (default: ~/.agentctl/config.yaml)")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if _, ok := config.FromContext(cmd.Context()); ok {
			return nil
		}

		var opts []config.Option
		if configPath != "" {
			opts = append(opts, config.WithConfigFile(configPath))
		}
		if flag := cmd.Flags().Lookup("workspace"); flag != nil && flag.Changed {
			opts = append(opts, config.WithWorkspacePath(flag.Value.String()))
		}
		cfg, err := config.Load(cmd.Context(), opts...)
		if err != nil {
			return err
		}
		logger := logging.New(logging.Config{
			Level:  logging.ParseLevel(cfg.Logging.Level),
			Format: logging.ParseFormat(cfg.Logging.Format),
		})
		ctx := config.WithContext(cmd.Context(), cfg)
		ctx = logging.WithContext(ctx, logger)
		cmd.SetContext(ctx)
		return nil
	}
}

func initConfig() {
	viper.SetEnvPrefix("agentctl")
	viper.AutomaticEnv()

	// Load .env files from multiple locations (later files override earlier ones)
	// 1. ~/.agentctl/.env (global)
	// 2. Git root .env (found by walking up)
	// 3. ./.env (current directory)
	if home, err := os.UserHomeDir(); err == nil {
		_ = godotenv.Load(filepath.Join(home, ".agentctl", ".env"))
	}

	// Walk up to find git root and load .env from there
	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for dir != "/" && dir != "." {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				_ = godotenv.Load(filepath.Join(dir, ".env"))
				break
			}
			dir = filepath.Dir(dir)
		}
	}

	_ = godotenv.Load() // loads .env from current directory (highest priority)
}
