package obsidian

import (
	"testing"
	"time"
)

func TestResolveVaultTarget(t *testing.T) {
	target, err := ResolveVaultTarget("Obsidian Vault", "")
	if err != nil {
		t.Fatalf("ResolveVaultTarget by name: %v", err)
	}
	if target.Name != "Obsidian Vault" {
		t.Fatalf("name=%q", target.Name)
	}

	target, err = ResolveVaultTarget("", "/tmp/vault")
	if err != nil {
		t.Fatalf("ResolveVaultTarget by path: %v", err)
	}
	if target.Path != "/tmp/vault" {
		t.Fatalf("path=%q", target.Path)
	}

	if _, err := ResolveVaultTarget("name", "/tmp/vault"); err == nil {
		t.Fatalf("expected error when both name and path are set")
	}
}

func TestConfigValidate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Target = VaultTarget{Name: "Obsidian Vault"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate default cli config: %v", err)
	}

	cfg.Transport = TransportMCP
	cfg.MCPServerName = ""
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected mcp validation error")
	}

	cfg = DefaultConfig()
	cfg.Target = VaultTarget{Name: "Obsidian Vault"}
	cfg.PostCreateDelay = -1 * time.Second
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected delay validation error")
	}
}
