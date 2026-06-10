// Package cmd wires up the Cobra commands exposed by the foxctl binary.
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/joho/godotenv"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:           "foxctl",
	Short:         "foxctl Core Profile v1 CLI",
	Long:          "foxctl is a CLI for running skills, managing CAS artifacts, and executing OpenAPI jobs.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

const rootAgentStartHere = `Start here (for AI agents):
  foxctl skills
  foxctl skills search "<task>"
  foxctl skills get foxctl
  foxctl skills get <name>
  foxctl skills path [name]`

var (
	configPath     string
	initConfigOnce sync.Once
)

// Execute runs the root command with the supplied context.
func Execute(ctx context.Context) error {
	rootCmd.SetContext(ctx)
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.SetHelpFunc(foxctlHelp)
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to a config file (default: ~/.foxctl/config.yaml)")
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

func foxctlHelp(cmd *cobra.Command, _ []string) {
	if cmd.CommandPath() != "foxctl" {
		writeDefaultHelp(cmd)
		return
	}
	writeRootHelp(cmd)
}

func writeDefaultHelp(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	if desc := strings.TrimRight(cmd.Long, " \t\r\n"); desc != "" {
		fmt.Fprintln(out, desc)
		fmt.Fprintln(out)
	} else if desc := strings.TrimRight(cmd.Short, " \t\r\n"); desc != "" {
		fmt.Fprintln(out, desc)
		fmt.Fprintln(out)
	}
	if cmd.Runnable() || cmd.HasSubCommands() {
		fmt.Fprint(out, cmd.UsageString())
	}
}

func writeRootHelp(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	if desc := strings.TrimRight(cmd.Long, " \t\r\n"); desc != "" {
		fmt.Fprintln(out, desc)
		fmt.Fprintln(out)
	} else if desc := strings.TrimRight(cmd.Short, " \t\r\n"); desc != "" {
		fmt.Fprintln(out, desc)
		fmt.Fprintln(out)
	}

	if cmd.Runnable() || cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out, "Usage:")
		if cmd.Runnable() {
			fmt.Fprintf(out, "  %s\n", cmd.UseLine())
		}
		if cmd.HasAvailableSubCommands() {
			fmt.Fprintf(out, "  %s [command]\n", cmd.CommandPath())
		}
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, rootAgentStartHere)

	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Available Commands:")
		for _, subcmd := range cmd.Commands() {
			if subcmd.IsAvailableCommand() || subcmd.Name() == "help" {
				fmt.Fprintf(out, "  %-*s %s\n", cmd.NamePadding(), subcmd.Name(), subcmd.Short)
			}
		}
	}
	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Flags:")
		fmt.Fprintln(out, strings.TrimRight(cmd.LocalFlags().FlagUsages(), " \t\r\n"))
	}
	if cmd.HasAvailableInheritedFlags() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Global Flags:")
		fmt.Fprintln(out, strings.TrimRight(cmd.InheritedFlags().FlagUsages(), " \t\r\n"))
	}
	if cmd.HasHelpSubCommands() {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Additional help topics:")
		for _, subcmd := range cmd.Commands() {
			if subcmd.IsAdditionalHelpTopicCommand() {
				fmt.Fprintf(out, "  %-*s %s\n", cmd.CommandPathPadding(), subcmd.CommandPath(), subcmd.Short)
			}
		}
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "Use %q for more information about a command.\n", cmd.CommandPath()+" [command] --help")
	}
}

func initConfig() {
	initConfigOnce.Do(func() {
		viper.SetEnvPrefix("foxctl")
		viper.AutomaticEnv()

		// Load .env files from multiple locations (later files override earlier ones)
		// 1. ~/.foxctl/.env (global)
		// 2. Git root .env (found by walking up)
		// 3. ./.env (current directory)
		if home, err := os.UserHomeDir(); err == nil {
			_ = godotenv.Load(filepath.Join(home, ".foxctl", ".env"))
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
	})
}
