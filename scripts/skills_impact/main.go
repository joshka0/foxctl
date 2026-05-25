package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type goListPackage struct {
	ImportPath   string
	Dir          string
	Deps         []string
	TestImports  []string
	XTestImports []string
}

type impactReport struct {
	BaseRef        string          `json:"base_ref,omitempty"`
	HeadRef        string          `json:"head_ref,omitempty"`
	ChangedFiles   []string        `json:"changed_files"`
	ChangedPkgs    []string        `json:"changed_packages,omitempty"`
	GlobalTriggers []string        `json:"global_triggers,omitempty"`
	Packages       []packageImpact `json:"packages,omitempty"`
	Skills         []skillImpact   `json:"skills"`
}

type packageImpact struct {
	ImportPath string   `json:"import_path"`
	Dir        string   `json:"dir,omitempty"`
	Reasons    []string `json:"reasons"`
}

type skillImpact struct {
	Name       string   `json:"name"`
	ImportPath string   `json:"import_path,omitempty"`
	Dir        string   `json:"dir,omitempty"`
	Reasons    []string `json:"reasons"`
}

func main() {
	var (
		baseRef  string
		headRef  string
		format   string
		files    string
		mode     string
		worktree bool
	)
	flag.StringVar(&baseRef, "base-ref", "", "Base git ref for changed file detection (required unless --files is provided)")
	flag.StringVar(&headRef, "head-ref", "HEAD", "Head git ref for changed file detection")
	flag.StringVar(&format, "format", "text", "Output format: text|json|names")
	flag.StringVar(&mode, "mode", "skills", "Impact mode: skills|packages")
	flag.StringVar(&files, "files", "", "Optional comma-separated explicit changed files (skips git diff)")
	flag.BoolVar(&worktree, "worktree", false, "Use current unstaged, staged, and untracked working tree files")
	flag.Parse()

	changedFiles, err := resolveChangedFiles(strings.TrimSpace(baseRef), strings.TrimSpace(headRef), strings.TrimSpace(files), worktree)
	if err != nil {
		fatalf("%v", err)
	}

	allPkgs, err := goList("./...")
	if err != nil {
		fatalf("go list all packages: %v", err)
	}
	integrationPkgs, err := goListWithTags("integration", "./tests/integration/...")
	if err == nil {
		allPkgs = mergePackages(allPkgs, integrationPkgs)
	}
	skillPkgs, err := goList("./skills/...")
	if err != nil {
		fatalf("go list skill packages: %v", err)
	}

	report, err := buildImpactReport(strings.TrimSpace(baseRef), strings.TrimSpace(headRef), changedFiles, allPkgs, skillPkgs)
	if err != nil {
		fatalf("build impact report: %v", err)
	}

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fatalf("encode json: %v", err)
		}
	case "names":
		var names []string
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "skills":
			names = make([]string, 0, len(report.Skills))
			for _, skill := range report.Skills {
				names = append(names, skill.Name)
			}
		case "packages":
			names = make([]string, 0, len(report.Packages))
			for _, pkg := range report.Packages {
				names = append(names, pkg.ImportPath)
			}
		default:
			fatalf("unsupported mode %q", mode)
		}
		fmt.Println(strings.Join(names, " "))
	case "text":
		switch strings.ToLower(strings.TrimSpace(mode)) {
		case "skills":
			printSkillsReportText(report)
		case "packages":
			printPackageReportText(report)
		default:
			fatalf("unsupported mode %q", mode)
		}
	default:
		fatalf("unsupported format %q", format)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func resolveChangedFiles(baseRef, headRef, explicit string, worktree bool) ([]string, error) {
	if explicit != "" {
		parts := strings.Split(explicit, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			part = normalizeSlash(strings.TrimSpace(part))
			if part != "" {
				out = append(out, part)
			}
		}
		sort.Strings(out)
		return dedupeStrings(out), nil
	}
	if worktree {
		return resolveWorktreeFiles()
	}
	if baseRef == "" {
		return nil, fmt.Errorf("--base-ref is required unless --files is provided")
	}
	args := []string{"diff", "--name-only", "--diff-filter=ACMR", baseRef + "..." + firstNonEmpty(headRef, "HEAD")}
	cmd := exec.Command("git", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	var out []string
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := normalizeSlash(strings.TrimSpace(scanner.Text()))
		if line != "" {
			out = append(out, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan git diff output: %w", err)
	}
	sort.Strings(out)
	return dedupeStrings(out), nil
}

func resolveWorktreeFiles() ([]string, error) {
	commands := [][]string{
		{"diff", "--name-only"},
		{"diff", "--cached", "--name-only"},
		{"ls-files", "--others", "--exclude-standard"},
	}
	var out []string
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		scanner := bufio.NewScanner(bytes.NewReader(output))
		for scanner.Scan() {
			line := normalizeSlash(strings.TrimSpace(scanner.Text()))
			if line != "" {
				out = append(out, line)
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("scan git %s output: %w", strings.Join(args, " "), err)
		}
	}
	sort.Strings(out)
	return dedupeStrings(out), nil
}

func goList(pattern string) ([]goListPackage, error) {
	cmd := exec.Command("go", "list", "-json", pattern)
	return decodeGoListOutput(cmd)
}

func goListWithTags(tags, pattern string) ([]goListPackage, error) {
	args := []string{"list"}
	if strings.TrimSpace(tags) != "" {
		args = append(args, "-tags="+strings.TrimSpace(tags))
	}
	args = append(args, "-json", pattern)
	cmd := exec.Command("go", args...)
	return decodeGoListOutput(cmd)
}

func decodeGoListOutput(cmd *exec.Cmd) ([]goListPackage, error) {
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var out []goListPackage
	for {
		var pkg goListPackage
		if err := decoder.Decode(&pkg); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		if strings.TrimSpace(pkg.ImportPath) == "" || strings.TrimSpace(pkg.Dir) == "" {
			continue
		}
		pkg.Dir = normalizeSlash(pkg.Dir)
		out = append(out, pkg)
	}
	return out, nil
}

func mergePackages(groups ...[]goListPackage) []goListPackage {
	index := map[string]goListPackage{}
	for _, group := range groups {
		for _, pkg := range group {
			key := strings.TrimSpace(pkg.ImportPath)
			if key == "" {
				continue
			}
			existing, ok := index[key]
			if !ok {
				existing = goListPackage{ImportPath: pkg.ImportPath, Dir: pkg.Dir}
			}
			if existing.Dir == "" {
				existing.Dir = pkg.Dir
			}
			deps := append(existing.Deps[:0:0], existing.Deps...)
			deps = append(deps, pkg.Deps...)
			sort.Strings(deps)
			existing.Deps = dedupeStrings(deps)
			testImports := append(existing.TestImports[:0:0], existing.TestImports...)
			testImports = append(testImports, pkg.TestImports...)
			sort.Strings(testImports)
			existing.TestImports = dedupeStrings(testImports)
			xTestImports := append(existing.XTestImports[:0:0], existing.XTestImports...)
			xTestImports = append(xTestImports, pkg.XTestImports...)
			sort.Strings(xTestImports)
			existing.XTestImports = dedupeStrings(xTestImports)
			index[key] = existing
		}
	}
	out := make([]goListPackage, 0, len(index))
	for _, pkg := range index {
		out = append(out, pkg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ImportPath < out[j].ImportPath })
	return out
}

func buildImpactReport(baseRef, headRef string, changedFiles []string, allPkgs, skillPkgs []goListPackage) (impactReport, error) {
	repoRoot, err := os.Getwd()
	if err != nil {
		return impactReport{}, err
	}
	return buildImpactReportWithRepoRoot(normalizeSlash(repoRoot), baseRef, headRef, changedFiles, allPkgs, skillPkgs), nil
}

func buildImpactReportWithRepoRoot(repoRoot, baseRef, headRef string, changedFiles []string, allPkgs, skillPkgs []goListPackage) impactReport {
	dirToImport := make(map[string]string, len(allPkgs))
	for _, pkg := range allPkgs {
		dirToImport[pkg.Dir] = pkg.ImportPath
	}

	globalTriggers := make([]string, 0)
	globalAll := false
	directSkillReasons := map[string][]string{}
	changedPkgReasons := map[string][]string{}

	for _, file := range changedFiles {
		if trigger := globalSkillTrigger(file); trigger != "" {
			globalAll = true
			globalTriggers = append(globalTriggers, trigger)
		}
		if skillName := directSkillForPath(file); skillName != "" {
			directSkillReasons[skillName] = append(directSkillReasons[skillName], "changed file "+file)
		}
		if importPath := nearestPackageImport(file, repoRoot, dirToImport); importPath != "" {
			changedPkgReasons[importPath] = append(changedPkgReasons[importPath], "depends on changed package "+importPath)
		}
	}

	changedPkgs := make([]string, 0, len(changedPkgReasons))
	for importPath := range changedPkgReasons {
		changedPkgs = append(changedPkgs, importPath)
	}
	sort.Strings(changedPkgs)
	sort.Strings(globalTriggers)
	globalTriggers = dedupeStrings(globalTriggers)

	impactedPackages := buildImpactedPackages(allPkgs, changedPkgReasons, globalTriggers, globalAll)
	impacted := buildImpactedSkills(skillPkgs, directSkillReasons, changedPkgReasons, globalTriggers, globalAll)

	return impactReport{
		BaseRef:        baseRef,
		HeadRef:        headRef,
		ChangedFiles:   changedFiles,
		ChangedPkgs:    changedPkgs,
		GlobalTriggers: globalTriggers,
		Packages:       impactedPackages,
		Skills:         impacted,
	}
}

func buildImpactedPackages(allPkgs []goListPackage, changedPkgReasons map[string][]string, globalTriggers []string, globalAll bool) []packageImpact {
	impactedPackages := make([]packageImpact, 0)
	for _, pkg := range allPkgs {
		reasons := collectPackageImpactReasons(pkg, changedPkgReasons, globalTriggers, globalAll)
		if len(reasons) == 0 {
			continue
		}
		impactedPackages = append(impactedPackages, packageImpact{
			ImportPath: pkg.ImportPath,
			Dir:        pkg.Dir,
			Reasons:    reasons,
		})
	}
	sort.Slice(impactedPackages, func(i, j int) bool { return impactedPackages[i].ImportPath < impactedPackages[j].ImportPath })
	return impactedPackages
}

func collectPackageImpactReasons(pkg goListPackage, changedPkgReasons map[string][]string, globalTriggers []string, globalAll bool) []string {
	reasonSet := map[string]struct{}{}
	if globalAll {
		for _, trigger := range globalTriggers {
			reasonSet["global trigger "+trigger] = struct{}{}
		}
		return sortedReasonSet(reasonSet)
	}
	for _, reason := range changedPkgReasons[pkg.ImportPath] {
		reasonSet[reason] = struct{}{}
	}
	for _, dep := range packageTestDependencyImports(pkg) {
		for _, reason := range changedPkgReasons[dep] {
			reasonSet[reason] = struct{}{}
		}
	}
	return sortedReasonSet(reasonSet)
}

func packageTestDependencyImports(pkg goListPackage) []string {
	deps := append(pkg.Deps[:0:0], pkg.Deps...)
	deps = append(deps, pkg.TestImports...)
	deps = append(deps, pkg.XTestImports...)
	sort.Strings(deps)
	return dedupeStrings(deps)
}

func buildImpactedSkills(skillPkgs []goListPackage, directSkillReasons, changedPkgReasons map[string][]string, globalTriggers []string, globalAll bool) []skillImpact {
	impacted := make([]skillImpact, 0)
	for _, pkg := range skillPkgs {
		name := skillNameFromImportPath(pkg.ImportPath)
		if name == "" {
			continue
		}
		reasons := collectSkillImpactReasons(pkg, name, directSkillReasons, changedPkgReasons, globalTriggers, globalAll)
		if len(reasons) == 0 {
			continue
		}
		impacted = append(impacted, skillImpact{
			Name:       name,
			ImportPath: pkg.ImportPath,
			Dir:        pkg.Dir,
			Reasons:    reasons,
		})
	}
	sort.Slice(impacted, func(i, j int) bool { return impacted[i].Name < impacted[j].Name })
	return impacted
}

func collectSkillImpactReasons(pkg goListPackage, name string, directSkillReasons, changedPkgReasons map[string][]string, globalTriggers []string, globalAll bool) []string {
	reasonSet := map[string]struct{}{}
	if globalAll {
		for _, trigger := range globalTriggers {
			reasonSet["global trigger "+trigger] = struct{}{}
		}
	} else {
		for _, dep := range pkg.Deps {
			for _, reason := range changedPkgReasons[dep] {
				reasonSet[reason] = struct{}{}
			}
		}
		for _, reason := range changedPkgReasons[pkg.ImportPath] {
			reasonSet[reason] = struct{}{}
		}
	}
	for _, reason := range directSkillReasons[name] {
		reasonSet[reason] = struct{}{}
	}
	return sortedReasonSet(reasonSet)
}

func sortedReasonSet(reasonSet map[string]struct{}) []string {
	if len(reasonSet) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(reasonSet))
	for reason := range reasonSet {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return reasons
}

func globalSkillTrigger(path string) string {
	switch normalizeSlash(strings.TrimSpace(path)) {
	case "go.mod", "go.sum", "go.work", "go.work.sum":
		return path
	default:
		return ""
	}
}

func directSkillForPath(path string) string {
	path = normalizeSlash(strings.TrimSpace(path))
	if !strings.HasPrefix(path, "skills/") {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ""
	}
	name := strings.TrimSpace(parts[1])
	if name == "" || name == "." {
		return ""
	}
	return name
}

func nearestPackageImport(path, repoRoot string, dirToImport map[string]string) string {
	path = normalizeSlash(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	dir := filepath.Dir(filepath.Join(repoRoot, filepath.FromSlash(path)))
	dir = normalizeSlash(dir)
	for {
		if importPath, ok := dirToImport[dir]; ok {
			return importPath
		}
		if dir == repoRoot {
			return ""
		}
		parent := normalizeSlash(filepath.Dir(filepath.FromSlash(dir)))
		if parent == dir || parent == "." || parent == "" {
			return ""
		}
		dir = parent
	}
}

func skillNameFromImportPath(importPath string) string {
	importPath = strings.TrimSpace(importPath)
	if importPath == "" {
		return ""
	}
	idx := strings.Index(importPath, "/skills/")
	if idx < 0 {
		return ""
	}
	rest := importPath[idx+len("/skills/"):]
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

func normalizeSlash(s string) string {
	return filepath.ToSlash(strings.TrimSpace(s))
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func printSkillsReportText(report impactReport) {
	fmt.Printf("Changed files: %d\n", len(report.ChangedFiles))
	for _, file := range report.ChangedFiles {
		fmt.Printf("- %s\n", file)
	}
	if len(report.ChangedPkgs) > 0 {
		fmt.Printf("\nChanged packages: %d\n", len(report.ChangedPkgs))
		for _, pkg := range report.ChangedPkgs {
			fmt.Printf("- %s\n", pkg)
		}
	}
	if len(report.GlobalTriggers) > 0 {
		fmt.Printf("\nGlobal skill triggers:\n")
		for _, trigger := range report.GlobalTriggers {
			fmt.Printf("- %s\n", trigger)
		}
	}
	fmt.Printf("\nImpacted skills: %d\n", len(report.Skills))
	for _, skill := range report.Skills {
		fmt.Printf("- %s\n", skill.Name)
		for _, reason := range skill.Reasons {
			fmt.Printf("  reason: %s\n", reason)
		}
	}
}

func printPackageReportText(report impactReport) {
	fmt.Printf("Changed files: %d\n", len(report.ChangedFiles))
	for _, file := range report.ChangedFiles {
		fmt.Printf("- %s\n", file)
	}
	if len(report.ChangedPkgs) > 0 {
		fmt.Printf("\nChanged packages: %d\n", len(report.ChangedPkgs))
		for _, pkg := range report.ChangedPkgs {
			fmt.Printf("- %s\n", pkg)
		}
	}
	if len(report.GlobalTriggers) > 0 {
		fmt.Printf("\nGlobal package triggers:\n")
		for _, trigger := range report.GlobalTriggers {
			fmt.Printf("- %s\n", trigger)
		}
	}
	fmt.Printf("\nImpacted packages: %d\n", len(report.Packages))
	for _, pkg := range report.Packages {
		fmt.Printf("- %s\n", pkg.ImportPath)
		for _, reason := range pkg.Reasons {
			fmt.Printf("  reason: %s\n", reason)
		}
	}
}
