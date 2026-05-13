package cmd

import (
	"bytes"
	"testing"

	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

func TestBenchmarkManifestValidateCommandUsesDefaultManifest(t *testing.T) {
	t.Parallel()

	cmd := newBenchmarkManifestValidateCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	env, err := protocol.DecodeEnvelope(out.Bytes())
	if err != nil {
		t.Fatalf("DecodeEnvelope() error = %v\n%s", err, out.String())
	}
	data := env.Data.(map[string]any)
	if got := data["suite"]; got != "foxctl-benchmark-suite" {
		t.Fatalf("suite=%v", got)
	}
	if got := int(data["category_count"].(float64)); got != 7 {
		t.Fatalf("category_count=%d want 7", got)
	}
	if got := int(data["benchmark_count"].(float64)); got < 10 {
		t.Fatalf("benchmark_count=%d want at least 10", got)
	}
}

func TestBenchmarkManifestPackagesCommandReturnsDefaultGoBenchPackages(t *testing.T) {
	t.Parallel()

	cmd := newBenchmarkManifestPackagesCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	env, err := protocol.DecodeEnvelope(out.Bytes())
	if err != nil {
		t.Fatalf("DecodeEnvelope() error = %v\n%s", err, out.String())
	}
	data := env.Data.(map[string]any)
	rawPackages := data["packages"].([]any)
	packages := make([]string, 0, len(rawPackages))
	for _, raw := range rawPackages {
		packages = append(packages, raw.(string))
	}
	for _, want := range []string{
		"./internal/runtime/engine",
		"./internal/intelligence/indexing/repoindex",
		"./internal/rlm",
		"./internal/tooling/shellreduce",
	} {
		if !containsBenchmarkString(packages, want) {
			t.Fatalf("packages=%v missing %s", packages, want)
		}
	}
}

func TestBenchmarkManifestListRejectsUnsupportedGate(t *testing.T) {
	t.Parallel()

	cmd := newBenchmarkManifestListCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--gate", "live"})
	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want written envelope error")
	}
	env, err := protocol.DecodeEnvelope(out.Bytes())
	if err != nil {
		t.Fatalf("DecodeEnvelope() error = %v\n%s", err, out.String())
	}
	if env.Status != "error" {
		t.Fatalf("status=%s want error", env.Status)
	}
}

func TestBenchmarkManifestCommandContainsExpectedSubcommands(t *testing.T) {
	t.Parallel()

	cmd := newBenchmarkCommand()
	if cmd.Use != "benchmark" {
		t.Fatalf("Use=%q", cmd.Use)
	}
	manifest := findSubcommand(cmd, "manifest")
	if manifest == nil {
		t.Fatal("manifest subcommand missing")
	}
	for _, name := range []string{"validate", "list", "packages"} {
		if findSubcommand(manifest, name) == nil {
			t.Fatalf("%s subcommand missing", name)
		}
	}
}

func findSubcommand(cmd interface{ Commands() []*cobra.Command }, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

func containsBenchmarkString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
