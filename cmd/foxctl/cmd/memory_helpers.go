package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
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

func resolveWorkspaceID(cfg config.Config, override string) string {
	root := resolveWorkspace(cfg, override)
	if strings.TrimSpace(override) != "" {
		return workspace.ExplicitIDOrSelector(root)
	}
	return workspace.ID(root)
}

// readMemoryPayload reads payload bytes from one of three sources: a file path, a direct data string, or standard input.
//
// If `file` is "-" the function reads from stdin; if `file` is a non-empty path it reads from that file. If `data` is non-empty it is returned as the payload. When neither `file` nor `data` is provided the function reads from stdin. If stdin is a terminal in cases where piping is expected, the function returns an error prompting the user to provide a file path, use `--data`, or pipe input into the appropriate stream.
//
// The function returns the payload bytes on success, or an error when reading fails or when stdin is a terminal and cannot be used.
func readMemoryPayload(cmd *cobra.Command, file, data string) ([]byte, error) {
	switch {
	case file == "-":
		in := cmd.InOrStdin()
		if isTerminalReader(in) {
			return nil, fmt.Errorf("stdin is a terminal; provide --file <path>, use --data, or pipe input into --file -")
		}
		return io.ReadAll(in)
	case file != "":
		return os.ReadFile(file)
	case data != "":
		return []byte(data), nil
	default:
		in := cmd.InOrStdin()
		if isTerminalReader(in) {
			return nil, fmt.Errorf("stdin is a terminal; provide --file <path>, use --data, or pipe input into stdin")
		}
		return io.ReadAll(in)
	}
}
