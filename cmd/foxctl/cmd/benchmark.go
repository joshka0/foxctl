package cmd

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/tooling/benchmarks"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newBenchmarkCommand())
}

func newBenchmarkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Inspect foxctl benchmark suite metadata",
	}
	cmd.AddCommand(newBenchmarkManifestCommand())
	return cmd
}

func newBenchmarkManifestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Inspect the benchmark manifest",
	}
	cmd.AddCommand(
		newBenchmarkManifestValidateCommand(),
		newBenchmarkManifestListCommand(),
		newBenchmarkManifestPackagesCommand(),
	)
	return cmd
}

func newBenchmarkManifestValidateCommand() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the benchmark manifest contract",
		RunE: func(cmd *cobra.Command, _ []string) error {
			manifest, resolved, err := benchmarks.LoadManifest(path)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.benchmark.manifest.validate", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"path": path,
				}, protocol.WithSource("cli"))
			}
			if err := benchmarks.ValidateManifest(manifest); err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.benchmark.manifest.validate", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"path": resolved,
				}, protocol.WithSource("cli"))
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.benchmark.manifest.validate", map[string]any{
				"path":             resolved,
				"suite":            manifest.Suite,
				"category_count":   len(manifest.Categories),
				"benchmark_count":  len(manifest.Benchmarks),
				"category_status":  benchmarks.CategoryStatuses(manifest),
				"default_packages": benchmarks.GoBenchmarkPackages(manifest, benchmarks.GateDefault),
			}, protocol.WithSource("cli"))
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "Benchmark manifest path")
	return cmd
}

func newBenchmarkManifestListCommand() *cobra.Command {
	var path string
	var gateValue string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List benchmark specs in the manifest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			gate, err := benchmarks.ValidGate(gateValue)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.benchmark.manifest.list", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"gate": gateValue,
				}, protocol.WithSource("cli"))
			}
			manifest, resolved, err := benchmarks.LoadManifest(path)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.benchmark.manifest.list", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"path": path,
				}, protocol.WithSource("cli"))
			}
			if err := benchmarks.ValidateManifest(manifest); err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.benchmark.manifest.list", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"path": resolved,
				}, protocol.WithSource("cli"))
			}
			specs := benchmarks.SpecsForGate(manifest, gate)
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.benchmark.manifest.list", map[string]any{
				"path":       resolved,
				"gate":       gate,
				"count":      len(specs),
				"benchmarks": specs,
			}, protocol.WithSource("cli"))
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "Benchmark manifest path")
	cmd.Flags().StringVar(&gateValue, "gate", string(benchmarks.GateDefault), "Benchmark gate: default, extended, or all")
	return cmd
}

func newBenchmarkManifestPackagesCommand() *cobra.Command {
	var path string
	var gateValue string
	cmd := &cobra.Command{
		Use:   "packages",
		Short: "List Go benchmark packages for a benchmark gate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			gate, err := benchmarks.ValidGate(gateValue)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.benchmark.manifest.packages", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"gate": gateValue,
				}, protocol.WithSource("cli"))
			}
			manifest, resolved, err := benchmarks.LoadManifest(path)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.benchmark.manifest.packages", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"path": path,
				}, protocol.WithSource("cli"))
			}
			if err := benchmarks.ValidateManifest(manifest); err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.benchmark.manifest.packages", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"path": resolved,
				}, protocol.WithSource("cli"))
			}
			packages := benchmarks.GoBenchmarkPackages(manifest, gate)
			if len(packages) == 0 {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.benchmark.manifest.packages", protocol.ErrorCodeENotFound, fmt.Sprintf("no Go benchmark packages for gate %q", gate), map[string]any{
					"path": resolved,
					"gate": gate,
				}, protocol.WithSource("cli"))
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.benchmark.manifest.packages", map[string]any{
				"path":     resolved,
				"gate":     gate,
				"packages": packages,
			}, protocol.WithSource("cli"))
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "Benchmark manifest path")
	cmd.Flags().StringVar(&gateValue, "gate", string(benchmarks.GateDefault), "Benchmark gate: default, extended, or all")
	return cmd
}
