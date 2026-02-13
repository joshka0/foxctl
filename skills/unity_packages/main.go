package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const skillName = "unity/packages"

// Input defines parameters for the unity/packages skill.
type Input struct {
	Operation   string `json:"operation"`
	PackageName string `json:"package_name"`
	Version     string `json:"version"`
	ProjectPath string `json:"project_path"`
}

func main() {
	skillmain.Main(skillName, run)
}

// run validates input, loads Unity manifest dependencies, and dispatches operations.
func run(_ context.Context, rc *skillmain.RunContext, in Input) error {
	if strings.TrimSpace(in.Operation) == "" {
		return skillerr.Arg(
			"operation is required",
			skillerr.WithHint("Use one of: list, add, remove, get."),
		)
	}

	projectPath, err := validateProjectPath(rc, in.ProjectPath)
	if err != nil {
		return err
	}

	result, err := oputil.NewSwitch(in.Operation).
		Case("list", func() (map[string]any, error) { return listPackages(projectPath) }).
		Case("add", func() (map[string]any, error) { return addPackage(projectPath, in) }).
		Case("remove", func() (map[string]any, error) { return removePackage(projectPath, in) }).
		Case("get", func() (map[string]any, error) { return getPackage(projectPath, in) }).
		Run()
	if err != nil {
		if _, ok := err.(*oputil.InvalidOpError); ok {
			return skillerr.Arg(err.Error(), skillerr.WithHint("Valid operations: list, add, remove, get."))
		}
		return err
	}

	return skillout.Emit(rc, skillName, result)
}

// manifestPath returns the Unity manifest path for a project.
func manifestPath(projectPath string) string {
	return filepath.Join(projectPath, "Packages", "manifest.json")
}

// validateProjectPath validates the provided project path points to a Unity project.
func validateProjectPath(rc *skillmain.RunContext, projectPath string) (string, error) {
	path := strings.TrimSpace(projectPath)
	if path == "" {
		path = rc.PathValidator.Workspace()
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(rc.PathValidator.Workspace(), path)
	}

	assetsPath := filepath.Join(path, "Assets")
	info, err := os.Stat(assetsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", skillerr.NotFound(
				fmt.Sprintf("assets directory not found at %q", assetsPath),
				skillerr.WithHint("Confirm this is a Unity project path and that an Assets/ directory exists."),
			)
		}
		return "", skillerr.WrapIO("stat assets directory", err)
	}
	if !info.IsDir() {
		return "", skillerr.Arg(
			fmt.Sprintf("assets path %q is not a directory", assetsPath),
			skillerr.WithHint("Unity project paths must have an Assets directory."),
		)
	}

	return path, nil
}

// loadManifest reads and decodes the manifest while preserving unknown top-level fields.
func loadManifest(projectPath string) (map[string]json.RawMessage, map[string]string, error) {
	manifestFile := manifestPath(projectPath)
	raw, err := os.ReadFile(manifestFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, skillerr.NotFound(
				fmt.Sprintf("manifest file not found at %q", manifestFile),
				skillerr.WithHint("Make sure this is a Unity project root containing Packages/manifest.json."),
			)
		}
		return nil, nil, skillerr.WrapIO("read manifest", err)
	}

	manifest := make(map[string]json.RawMessage)
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, nil, skillerr.WrapParse("parse manifest", err)
	}

	deps := make(map[string]string)
	if rawDeps, ok := manifest["dependencies"]; ok {
		if len(rawDeps) > 0 {
			if err := json.Unmarshal(rawDeps, &deps); err != nil {
				return nil, nil, skillerr.WrapParse("parse manifest dependencies", err)
			}
		}
	}

	return manifest, deps, nil
}

// writeManifest writes manifest bytes using the canonical formatting and newline.
func writeManifest(manifestPath string, manifest map[string]json.RawMessage) error {
	formatted, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return skillerr.WrapParse("format manifest", err)
	}

	formatted = append(formatted, '\n')
	if err := os.WriteFile(manifestPath, formatted, 0o644); err != nil {
		return skillerr.WrapIO("write manifest", err)
	}

	return nil
}

// listPackages lists package dependencies with sorted package names.
func listPackages(projectPath string) (map[string]any, error) {
	manifest, deps, err := loadManifest(projectPath)
	if err != nil {
		return nil, err
	}
	_ = manifest

	names := make([]string, 0, len(deps))
	for name := range deps {
		names = append(names, name)
	}
	sort.Strings(names)

	sortedDeps := make(map[string]string, len(deps))
	for _, name := range names {
		sortedDeps[name] = deps[name]
	}

	return map[string]any{
		"operation":     "list",
		"packages":      sortedDeps,
		"package_count": len(sortedDeps),
		"package_names": names,
		"manifest_path": manifestPath(projectPath),
	}, nil
}

func addPackage(projectPath string, in Input) (map[string]any, error) {
	if err := oputil.RequireForOp(in.Operation, in.PackageName, "package_name", "add"); err != nil {
		return nil, skillerr.Arg(err.Error(), skillerr.WithHint("Provide package_name for add operation."))
	}
	if err := oputil.RequireForOp(in.Operation, in.Version, "version", "add"); err != nil {
		return nil, skillerr.Arg(err.Error(), skillerr.WithHint("Provide version for add operation."))
	}

	manifest, deps, err := loadManifest(projectPath)
	if err != nil {
		return nil, err
	}

	deps[in.PackageName] = in.Version
	encodedDeps, err := json.Marshal(deps)
	if err != nil {
		return nil, skillerr.WrapParse("serialize dependencies", err)
	}
	manifest["dependencies"] = json.RawMessage(encodedDeps)

	if err := writeManifest(manifestPath(projectPath), manifest); err != nil {
		return nil, err
	}

	return map[string]any{
		"operation":    "add",
		"package_name": in.PackageName,
		"version":      in.Version,
		"summary":      fmt.Sprintf("Set %s to %s", in.PackageName, in.Version),
	}, nil
}

func removePackage(projectPath string, in Input) (map[string]any, error) {
	if err := oputil.RequireForOp(in.Operation, in.PackageName, "package_name", "remove"); err != nil {
		return nil, skillerr.Arg(err.Error(), skillerr.WithHint("Provide package_name for remove operation."))
	}

	manifest, deps, err := loadManifest(projectPath)
	if err != nil {
		return nil, err
	}

	if _, ok := deps[in.PackageName]; !ok {
		return nil, skillerr.NotFound(
			fmt.Sprintf("package %q not found", in.PackageName),
			skillerr.WithHint("Run operation=list to check available dependencies."),
		)
	}

	delete(deps, in.PackageName)
	encodedDeps, err := json.Marshal(deps)
	if err != nil {
		return nil, skillerr.WrapParse("serialize dependencies", err)
	}
	manifest["dependencies"] = json.RawMessage(encodedDeps)

	if err := writeManifest(manifestPath(projectPath), manifest); err != nil {
		return nil, err
	}

	return map[string]any{
		"operation":    "remove",
		"package_name": in.PackageName,
		"summary":      fmt.Sprintf("Removed %s", in.PackageName),
	}, nil
}

func getPackage(projectPath string, in Input) (map[string]any, error) {
	if err := oputil.RequireForOp(in.Operation, in.PackageName, "package_name", "get"); err != nil {
		return nil, skillerr.Arg(err.Error(), skillerr.WithHint("Provide package_name for get operation."))
	}

	_, deps, err := loadManifest(projectPath)
	if err != nil {
		return nil, err
	}

	version, ok := deps[in.PackageName]
	if !ok {
		return nil, skillerr.NotFound(
			fmt.Sprintf("package %q not found", in.PackageName),
			skillerr.WithHint("Run operation=list to check available dependencies."),
		)
	}

	return map[string]any{
		"operation":    "get",
		"package_name": in.PackageName,
		"version":      version,
	}, nil
}
