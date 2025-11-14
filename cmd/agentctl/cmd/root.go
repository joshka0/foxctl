// Package cmd wires up the Cobra commands exposed by the agentctl binary.
package cmd

import (
	"context"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/logging"
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
}
