package obsidian

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadUsesExplicitVaultName(t *testing.T) {
	bin := writeFakeCLI(t, `#!/bin/sh
case "$1" in
  read)
    printf '%s\n' 'read:'"$2"'|'"$3"
    ;;
  *)
    exit 1
    ;;
esac
`)

	result, err := Read(context.Background(), ReadOptions{
		BinaryPath: bin,
		VaultName:  "Obsidian Vault",
		NotePath:   "foxctl-lab/example.md",
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if result.VaultName != "Obsidian Vault" {
		t.Fatalf("vault=%q", result.VaultName)
	}
	if result.Content != "read:vault=Obsidian Vault|path=foxctl-lab/example.md" {
		t.Fatalf("content=%q", result.Content)
	}
}

func TestReadResolvesVaultNameFromPath(t *testing.T) {
	bin := writeFakeCLI(t, `#!/bin/sh
case "$1 $2" in
  "vaults verbose")
    printf '%s\n' 'AgentCTL Test Vault	/Users/joshka/Documents/AgentCTL Test Vault'
    ;;
  "read vault=AgentCTL Test Vault")
    printf '%s\n' '# note'
    ;;
  *)
    exit 1
    ;;
esac
`)

	result, err := Read(context.Background(), ReadOptions{
		BinaryPath: bin,
		VaultPath:  "/Users/joshka/Documents/AgentCTL Test Vault",
		NotePath:   "Welcome.md",
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if result.VaultName != "AgentCTL Test Vault" {
		t.Fatalf("vault=%q", result.VaultName)
	}
}

func TestReadTreatsMissingFilePayloadAsNotExist(t *testing.T) {
	bin := writeFakeCLI(t, `#!/bin/sh
case "$1" in
  read)
    printf '%s\n' 'Error: File "notes/patterns/missing.md" not found.'
    ;;
  *)
    exit 1
    ;;
esac
`)

	_, err := Read(context.Background(), ReadOptions{
		BinaryPath: bin,
		VaultName:  "Obsidian Vault",
		NotePath:   "notes/patterns/missing.md",
	})
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected not-exist style error, got %v", err)
	}
}

func TestReadSanitizesInstallerNoise(t *testing.T) {
	bin := writeFakeCLI(t, `#!/bin/sh
case "$1" in
  read)
    cat <<'EOF'
2026-03-10 13:08:05 Loading updated app package /Users/joshka/Library/Application Support/obsidian/obsidian-1.12.4.asar
Your Obsidian installer is out of date. Please download the latest installer which includes better CLI support: https://obsidian.md/download
---
title: Example
---

# Example
EOF
    ;;
  *)
    exit 1
    ;;
esac
`)

	result, err := Read(context.Background(), ReadOptions{
		BinaryPath: bin,
		VaultName:  "Obsidian Vault",
		NotePath:   "notes/example.md",
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if strings.HasPrefix(result.Content, "2026-03-10") || !strings.HasPrefix(result.Content, "---") {
		t.Fatalf("expected sanitized markdown, got %q", result.Content)
	}
}

func writeFakeCLI(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "obsidian")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return path
}
