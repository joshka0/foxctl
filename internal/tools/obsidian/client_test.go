package obsidian

import "testing"

func TestNewClientAndCLICommandSpec(t *testing.T) {
	client, err := NewClient(Config{
		Target:    VaultTarget{Name: "AgentCTL Vault"},
		Transport: TransportCLI,
		CLIPath:   "/Applications/Obsidian.app/Contents/MacOS/obsidian",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	spec, err := client.CLICommandSpec()
	if err != nil {
		t.Fatalf("CLICommandSpec: %v", err)
	}
	if spec.Binary == "" {
		t.Fatalf("expected binary")
	}
	if spec.Vault.Name != "AgentCTL Vault" {
		t.Fatalf("vault name=%q", spec.Vault.Name)
	}
}
