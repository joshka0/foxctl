package obsidian

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ReadOptions configures an Obsidian note read.
type ReadOptions struct {
	BinaryPath string
	VaultName  string
	VaultPath  string
	NotePath   string
}

// ReadResult is the result of reading a vault note.
type ReadResult struct {
	VaultName string `json:"vault_name"`
	NotePath  string `json:"note_path"`
	Content   string `json:"content"`
}

// Read returns the full note contents via the official Obsidian CLI.
func Read(ctx context.Context, opts ReadOptions) (*ReadResult, error) {
	if strings.TrimSpace(opts.NotePath) == "" {
		return nil, fmt.Errorf("obsidian: note path required")
	}

	binary := resolveBinary(opts.BinaryPath)
	vaultName, err := ResolveVaultName(ctx, binary, opts.VaultName, opts.VaultPath)
	if err != nil {
		return nil, err
	}

	stdout, stderr, err := runCLI(ctx, binary, "read", "vault="+vaultName, "path="+opts.NotePath)
	if err != nil {
		return nil, formatCLIError("read", err, stderr)
	}
	raw := sanitizeCLIReadOutput(strings.TrimRight(string(stdout), "\n"))
	if looksLikeMissingNoteOutput(raw) {
		return nil, fmt.Errorf("obsidian: read %s: %w", opts.NotePath, os.ErrNotExist)
	}

	return &ReadResult{
		VaultName: vaultName,
		NotePath:  opts.NotePath,
		Content:   raw,
	}, nil
}

func resolveBinary(binary string) string {
	if strings.TrimSpace(binary) != "" {
		return binary
	}
	return "obsidian"
}

func ResolveVaultName(ctx context.Context, binary, explicitName, vaultPath string) (string, error) {
	if trimmed := strings.TrimSpace(explicitName); trimmed != "" {
		return trimmed, nil
	}
	if strings.TrimSpace(vaultPath) == "" {
		return "", fmt.Errorf("obsidian: vault name or vault path required")
	}

	target := filepath.Clean(vaultPath)
	if absTarget, absErr := filepath.Abs(target); absErr == nil {
		target = filepath.Clean(absTarget)
	}
	stdout, stderr, err := runCLI(ctx, binary, "vaults", "verbose")
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(stdout)), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) < 2 {
				continue
			}
			name := strings.TrimSpace(fields[0])
			path := filepath.Clean(strings.TrimSpace(fields[1]))
			if absPath, absErr := filepath.Abs(path); absErr == nil {
				path = filepath.Clean(absPath)
			}
			if path == target {
				return name, nil
			}
		}
	}

	// Fallback to the directory basename when the registry lookup is unavailable.
	if base := strings.TrimSpace(filepath.Base(target)); base != "" && base != "." && base != string(filepath.Separator) {
		return base, nil
	}
	if err != nil {
		return "", formatCLIError("vaults", err, stderr)
	}
	return "", fmt.Errorf("obsidian: could not resolve vault name for path %s", vaultPath)
}

func runCLI(ctx context.Context, binary string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func formatCLIError(op string, err error, stderr []byte) error {
	msg := strings.TrimSpace(string(stderr))
	if msg != "" {
		return fmt.Errorf("obsidian: %s: %w: %s", op, err, msg)
	}
	return fmt.Errorf("obsidian: %s: %w", op, err)
}

func looksLikeMissingNoteOutput(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(lower, "error: file \"") && strings.Contains(lower, "not found")
}

func sanitizeCLIReadOutput(text string) string {
	lines := strings.Split(text, "\n")
	start := 0
	for start < len(lines) {
		line := strings.TrimSpace(lines[start])
		if line == "" {
			start++
			continue
		}
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "#") {
			break
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "loading updated app package") ||
			strings.Contains(lower, "your obsidian installer is out of date") {
			start++
			continue
		}
		if strings.HasPrefix(lower, "error: file \"") && strings.Contains(lower, "not found") {
			break
		}
		break
	}
	return strings.TrimLeft(strings.Join(lines[start:], "\n"), "\n")
}
