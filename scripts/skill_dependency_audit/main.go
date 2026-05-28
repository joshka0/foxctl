package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type goListPackage struct {
	Name       string
	ImportPath string
	Imports    []string
	Deps       []string
}

type skillReport struct {
	Skill                  string
	Classification         string
	DirectInternalImports  []string
	DirectBlockers         []string
	TransitiveInternal     int
	TransitiveIntelligence int
	Notes                  string
}

func main() {
	format := flag.String("format", "markdown", "Output format: markdown|tsv")
	flag.Parse()

	if *format != "markdown" && *format != "tsv" {
		fatalf("unsupported format %q", *format)
	}

	modulePath, err := goListModule()
	if err != nil {
		fatalf("go list module: %v", err)
	}
	pkgs, err := goListSkills()
	if err != nil {
		fatalf("go list skills: %v", err)
	}

	reports := buildReports(modulePath, pkgs)
	switch *format {
	case "markdown":
		printMarkdown(os.Stdout, reports)
	case "tsv":
		printTSV(os.Stdout, reports)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func buildReports(modulePath string, pkgs []goListPackage) []skillReport {
	var reports []skillReport
	for _, pkg := range pkgs {
		skill, ok := topLevelSkillName(modulePath, pkg)
		if !ok {
			continue
		}
		directInternal := filterInternal(modulePath, pkg.Imports)
		directBlockers := directBlockers(modulePath, directInternal)
		classification := classifySkill(modulePath, skill, directInternal, directBlockers)
		reports = append(reports, skillReport{
			Skill:                  skill,
			Classification:         classification,
			DirectInternalImports:  trimModulePrefix(modulePath, directInternal),
			DirectBlockers:         trimModulePrefix(modulePath, directBlockers),
			TransitiveInternal:     countPrefix(pkg.Deps, modulePath+"/internal/"),
			TransitiveIntelligence: countPrefix(pkg.Deps, modulePath+"/internal/intelligence/"),
			Notes:                  classificationNote(classification),
		})
	}
	sort.Slice(reports, func(i, j int) bool {
		return reports[i].Skill < reports[j].Skill
	})
	return reports
}

func topLevelSkillName(modulePath string, pkg goListPackage) (string, bool) {
	if pkg.Name != "main" {
		return "", false
	}
	prefix := modulePath + "/skills/"
	if !strings.HasPrefix(pkg.ImportPath, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(pkg.ImportPath, prefix)
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

func classifySkill(modulePath, skill string, directInternal, directBlockers []string) string {
	if strings.HasPrefix(skill, "hooks_") {
		return "core-hook"
	}
	if hasPrefix(directInternal, modulePath+"/internal/intelligence/") {
		return "core-intelligence"
	}
	if hasPrefix(directInternal, modulePath+"/internal/context/") {
		return "core-context"
	}
	if hasPrefix(directInternal, modulePath+"/internal/runtime/") {
		return "core-runtime"
	}
	if hasPrefix(directInternal, modulePath+"/internal/storage/") {
		return "core-storage"
	}
	if hasAnyPrefix(directInternal, modulePath+"/internal/interfaces/", modulePath+"/internal/providers/") {
		return "pack-with-client"
	}
	if len(directBlockers) > 0 {
		return "blocked-internal"
	}
	return "sdk-candidate"
}

func classificationNote(classification string) string {
	switch classification {
	case "core-hook":
		return "runtime hook; keep version-locked with foxctl"
	case "core-intelligence":
		return "direct intelligence import; keep core"
	case "core-context":
		return "direct context/memory import; keep core"
	case "core-runtime":
		return "direct runtime import; keep core"
	case "core-storage":
		return "direct storage import; keep core or add stable store seam"
	case "pack-with-client":
		return "extractable only if its provider/interface client moves with it"
	case "blocked-internal":
		return "needs an interface or package move before extraction"
	case "sdk-candidate":
		return "direct imports fit a future skillslib-core SDK"
	default:
		return "-"
	}
}

func directBlockers(modulePath string, imports []string) []string {
	var out []string
	for _, imp := range imports {
		if !isAllowedSDKImport(modulePath, imp) {
			out = append(out, imp)
		}
	}
	return out
}

func isAllowedSDKImport(modulePath, imp string) bool {
	allowedExact := map[string]bool{
		modulePath + "/internal/adapters/skillslib": true,
		modulePath + "/internal/domain/envelope":    true,
		modulePath + "/internal/domain/policy":      true,
		modulePath + "/internal/domain/skill":       true,
		modulePath + "/internal/platform/config":    true,
		modulePath + "/internal/platform/errors":    true,
		modulePath + "/internal/platform/workspace": true,
	}
	if allowedExact[imp] {
		return true
	}
	return strings.HasPrefix(imp, modulePath+"/internal/adapters/skillslib/")
}

func filterInternal(modulePath string, imports []string) []string {
	return filterPrefix(imports, modulePath+"/internal/")
}

func filterPrefix(values []string, prefix string) []string {
	var out []string
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func countPrefix(values []string, prefix string) int {
	count := 0
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			count++
		}
	}
	return count
}

func hasPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func hasAnyPrefix(values []string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if hasPrefix(values, prefix) {
			return true
		}
	}
	return false
}

func trimModulePrefix(modulePath string, imports []string) []string {
	out := make([]string, 0, len(imports))
	prefix := modulePath + "/"
	for _, imp := range imports {
		out = append(out, strings.TrimPrefix(imp, prefix))
	}
	return out
}

func summarize(values []string, max int) string {
	if len(values) == 0 {
		return "-"
	}
	if len(values) <= max {
		return strings.Join(values, ", ")
	}
	head := append([]string(nil), values[:max]...)
	return strings.Join(head, ", ") + fmt.Sprintf(", +%d more", len(values)-max)
}

func printMarkdown(w io.Writer, reports []skillReport) {
	fmt.Fprintln(w, "# Skill Dependency Audit")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Generated with `make skills-dependency-audit`.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Skill | Classification | Direct internal imports | Direct blockers | Transitive internal deps | Transitive intelligence deps | Notes |")
	fmt.Fprintln(w, "|-------|----------------|-------------------------|-----------------|--------------------------|------------------------------|-------|")
	for _, report := range reports {
		fmt.Fprintf(
			w,
			"| `%s` | `%s` | %s | %s | %d | %d | %s |\n",
			report.Skill,
			report.Classification,
			summarize(report.DirectInternalImports, 4),
			summarize(report.DirectBlockers, 4),
			report.TransitiveInternal,
			report.TransitiveIntelligence,
			report.Notes,
		)
	}
}

func printTSV(w io.Writer, reports []skillReport) {
	fmt.Fprintln(w, "skill\tclassification\tdirect_internal_imports\tdirect_blockers\ttransitive_internal_deps\ttransitive_intelligence_deps\tnotes")
	for _, report := range reports {
		fmt.Fprintf(
			w,
			"%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			report.Skill,
			report.Classification,
			summarize(report.DirectInternalImports, 4),
			summarize(report.DirectBlockers, 4),
			report.TransitiveInternal,
			report.TransitiveIntelligence,
			report.Notes,
		)
	}
}

func goListModule() (string, error) {
	out, err := runGo("list", "-m")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func goListSkills() ([]goListPackage, error) {
	out, err := runGo("list", "-json", "./skills/...")
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var pkgs []goListPackage
	for {
		var pkg goListPackage
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode go list json: %w", err)
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

func runGo(args ...string) ([]byte, error) {
	goBin := resolveGoBin()
	cmd := exec.Command(goBin, args...)
	cmd.Env = goCommandEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w\n%s", goBin, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func resolveGoBin() string {
	if goBin := strings.TrimSpace(os.Getenv("GO")); goBin != "" {
		return goBin
	}
	if goBin, err := exec.LookPath("go"); err == nil {
		return goBin
	}
	if _, err := os.Stat("/usr/local/go/bin/go"); err == nil {
		return "/usr/local/go/bin/go"
	}
	return "go"
}

func goCommandEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOROOT=") ||
			strings.HasPrefix(entry, "GOBIN=") ||
			strings.HasPrefix(entry, "GOTOOLDIR=") ||
			strings.HasPrefix(entry, "CGO_ENABLED=") {
			continue
		}
		out = append(out, entry)
	}
	return append(out, "CGO_ENABLED=0")
}
