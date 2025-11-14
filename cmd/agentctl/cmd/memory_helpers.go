package cmd

import (
	"io"
	"os"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
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
