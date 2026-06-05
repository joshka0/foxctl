package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelpShowsAIAgentStartHereSection(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(nil)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	if err := rootCmd.Help(); err != nil {
		t.Fatalf("root help: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"Start here (for AI agents):",
		"foxctl skills",
		"foxctl skills get foxctl",
		"foxctl skills get <name>",
		"foxctl skills path [name]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("root help missing %q\n%s", want, got)
		}
	}

	usageIndex := strings.Index(got, "Usage:")
	startIndex := strings.Index(got, "Start here (for AI agents):")
	availableIndex := strings.Index(got, "Available Commands:")
	if usageIndex == -1 || startIndex == -1 || availableIndex == -1 {
		t.Fatalf("root help missing expected section ordering markers\n%s", got)
	}
	if !(usageIndex < startIndex && startIndex < availableIndex) {
		t.Fatalf("root help section order = Usage:%d Start:%d Available:%d\n%s", usageIndex, startIndex, availableIndex, got)
	}
}

func TestSubcommandHelpDoesNotShowAIAgentStartHereSection(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(nil)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	cmd, _, err := rootCmd.Find([]string{"skills", "get"})
	if err != nil {
		t.Fatalf("find skills get: %v", err)
	}
	if err := cmd.Help(); err != nil {
		t.Fatalf("skills get help: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "Start here (for AI agents):") {
		t.Fatalf("subcommand help should not include root start-here section:\n%s", got)
	}
	if !strings.Contains(got, "Usage:") {
		t.Fatalf("subcommand help missing usage:\n%s", got)
	}
}
