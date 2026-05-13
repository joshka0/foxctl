package benchmarks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultManifestPath = "configs/benchmarks/foxctl.json"

type Manifest struct {
	Version     int             `json:"version"`
	Suite       string          `json:"suite"`
	Description string          `json:"description"`
	Categories  []Category      `json:"categories"`
	Benchmarks  []BenchmarkSpec `json:"benchmarks"`
}

type Category struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Owner       string `json:"owner"`
	Description string `json:"description"`
}

type BenchmarkSpec struct {
	ID              string   `json:"id"`
	Category        string   `json:"category"`
	Status          string   `json:"status"`
	Runner          string   `json:"runner"`
	DefaultGate     bool     `json:"default_gate"`
	ExtendedGate    bool     `json:"extended_gate"`
	RequiresNetwork bool     `json:"requires_network"`
	RequiresLLM     bool     `json:"requires_llm"`
	Commands        []string `json:"commands"`
	Packages        []string `json:"packages,omitempty"`
	Fixtures        []string `json:"fixtures"`
	Metrics         []string `json:"metrics"`
	Artifacts       []string `json:"artifacts"`
	Notes           string   `json:"notes,omitempty"`
}

type Gate string

const (
	GateAll      Gate = "all"
	GateDefault  Gate = "default"
	GateExtended Gate = "extended"
)

var allowedStatuses = map[string]struct{}{
	"implemented":  {},
	"in_progress":  {},
	"planned":      {},
	"experimental": {},
}

var allowedRunners = map[string]struct{}{
	"go-test":       {},
	"go-test-bench": {},
	"shell-script":  {},
	"make-eval":     {},
	"foxctl-eval":   {},
	"npm-script":    {},
}

func LoadManifest(path string) (Manifest, string, error) {
	resolved, err := ResolveManifestPath(path)
	if err != nil {
		return Manifest{}, "", err
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("read benchmark manifest: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, "", fmt.Errorf("decode benchmark manifest: %w", err)
	}
	return manifest, resolved, nil
}

func ResolveManifestPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultManifestPath
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	if abs, err := filepath.Abs(path); err == nil {
		if _, statErr := os.Stat(abs); statErr == nil {
			return abs, nil
		}
	}
	root, err := findRepoRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, path), nil
}

func ValidateManifest(manifest Manifest) error {
	if manifest.Version <= 0 {
		return fmt.Errorf("manifest version must be positive")
	}
	if strings.TrimSpace(manifest.Suite) == "" {
		return fmt.Errorf("manifest suite is required")
	}
	if len(manifest.Categories) == 0 {
		return fmt.Errorf("manifest must define at least one category")
	}
	categoryIDs := make(map[string]struct{}, len(manifest.Categories))
	for i, category := range manifest.Categories {
		id := strings.TrimSpace(category.ID)
		if id == "" {
			return fmt.Errorf("category[%d] id is required", i)
		}
		if _, exists := categoryIDs[id]; exists {
			return fmt.Errorf("duplicate category id %q", id)
		}
		categoryIDs[id] = struct{}{}
		if strings.TrimSpace(category.Name) == "" {
			return fmt.Errorf("category %q name is required", id)
		}
		if err := validateStatus("category "+id, category.Status); err != nil {
			return err
		}
	}

	if len(manifest.Benchmarks) == 0 {
		return fmt.Errorf("manifest must define at least one benchmark")
	}
	benchmarkIDs := make(map[string]struct{}, len(manifest.Benchmarks))
	coveredCategories := make(map[string]struct{}, len(manifest.Categories))
	for i, benchmark := range manifest.Benchmarks {
		if err := validateBenchmarkSpec(i, benchmark, categoryIDs); err != nil {
			return err
		}
		id := strings.TrimSpace(benchmark.ID)
		if _, exists := benchmarkIDs[id]; exists {
			return fmt.Errorf("duplicate benchmark id %q", id)
		}
		benchmarkIDs[id] = struct{}{}
		coveredCategories[strings.TrimSpace(benchmark.Category)] = struct{}{}
	}
	for categoryID := range categoryIDs {
		if _, ok := coveredCategories[categoryID]; !ok {
			return fmt.Errorf("category %q has no benchmark coverage", categoryID)
		}
	}
	return nil
}

func SpecsForGate(manifest Manifest, gate Gate) []BenchmarkSpec {
	out := make([]BenchmarkSpec, 0, len(manifest.Benchmarks))
	for _, spec := range manifest.Benchmarks {
		if !specMatchesGate(spec, gate) {
			continue
		}
		out = append(out, spec)
	}
	return out
}

func GoBenchmarkPackages(manifest Manifest, gate Gate) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, spec := range SpecsForGate(manifest, gate) {
		if strings.TrimSpace(spec.Runner) != "go-test-bench" {
			continue
		}
		for _, pkg := range spec.Packages {
			pkg = strings.TrimSpace(pkg)
			if pkg == "" {
				continue
			}
			if _, ok := seen[pkg]; ok {
				continue
			}
			seen[pkg] = struct{}{}
			out = append(out, pkg)
		}
	}
	return out
}

func CategoryStatuses(manifest Manifest) map[string]string {
	out := make(map[string]string, len(manifest.Categories))
	for _, category := range manifest.Categories {
		out[strings.TrimSpace(category.ID)] = strings.TrimSpace(category.Status)
	}
	return out
}

func ValidGate(value string) (Gate, error) {
	switch Gate(strings.ToLower(strings.TrimSpace(value))) {
	case "", GateDefault:
		return GateDefault, nil
	case GateAll:
		return GateAll, nil
	case GateExtended:
		return GateExtended, nil
	default:
		return "", fmt.Errorf("unsupported benchmark gate %q", value)
	}
}

func validateBenchmarkSpec(index int, spec BenchmarkSpec, categories map[string]struct{}) error {
	id := strings.TrimSpace(spec.ID)
	if id == "" {
		return fmt.Errorf("benchmark[%d] id is required", index)
	}
	category := strings.TrimSpace(spec.Category)
	if category == "" {
		return fmt.Errorf("benchmark %q category is required", id)
	}
	if _, ok := categories[category]; !ok {
		return fmt.Errorf("benchmark %q references unknown category %q", id, category)
	}
	if err := validateStatus("benchmark "+id, spec.Status); err != nil {
		return err
	}
	runner := strings.TrimSpace(spec.Runner)
	if _, ok := allowedRunners[runner]; !ok {
		return fmt.Errorf("benchmark %q uses unsupported runner %q", id, runner)
	}
	if spec.DefaultGate == spec.ExtendedGate {
		return fmt.Errorf("benchmark %q must set exactly one of default_gate or extended_gate", id)
	}
	if spec.DefaultGate && spec.RequiresNetwork {
		return fmt.Errorf("benchmark %q cannot require network in the default gate", id)
	}
	if spec.DefaultGate && spec.RequiresLLM {
		return fmt.Errorf("benchmark %q cannot require an LLM in the default gate", id)
	}
	if len(nonEmpty(spec.Commands)) == 0 {
		return fmt.Errorf("benchmark %q must define at least one command", id)
	}
	if runner == "go-test-bench" && len(nonEmpty(spec.Packages)) == 0 {
		return fmt.Errorf("benchmark %q must define Go benchmark packages", id)
	}
	if len(nonEmpty(spec.Fixtures)) == 0 {
		return fmt.Errorf("benchmark %q must define fixtures", id)
	}
	if len(nonEmpty(spec.Metrics)) == 0 {
		return fmt.Errorf("benchmark %q must define metrics", id)
	}
	if len(nonEmpty(spec.Artifacts)) == 0 {
		return fmt.Errorf("benchmark %q must define artifacts", id)
	}
	return nil
}

func validateStatus(scope, status string) error {
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("%s status is required", scope)
	}
	if _, ok := allowedStatuses[status]; !ok {
		return fmt.Errorf("%s uses unsupported status %q", scope, status)
	}
	return nil
}

func specMatchesGate(spec BenchmarkSpec, gate Gate) bool {
	switch gate {
	case GateAll:
		return true
	case GateExtended:
		return spec.ExtendedGate
	default:
		return spec.DefaultGate
	}
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repository root from %s", cwd)
		}
		dir = parent
	}
}

func SortedCategoryIDs(manifest Manifest) []string {
	ids := make([]string, 0, len(manifest.Categories))
	for _, category := range manifest.Categories {
		ids = append(ids, strings.TrimSpace(category.ID))
	}
	sort.Strings(ids)
	return ids
}
