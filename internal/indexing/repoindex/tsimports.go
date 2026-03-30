package repoindex

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	importFromRe    = regexp.MustCompile(`(?m)^\s*(?:import|export)\s+[^\n;]*\sfrom\s+["']([^"']+)["']`)
	importBareRe    = regexp.MustCompile(`(?m)^\s*import\s+["']([^"']+)["']`)
	dynamicImportRe = regexp.MustCompile(`(?m)import\(\s*["']([^"']+)["']\s*\)`)
	requireImportRe = regexp.MustCompile(`(?m)require\(\s*["']([^"']+)["']\s*\)`)

	tsDefaultImportRe = regexp.MustCompile(`(?m)^\s*import\s+(?:type\s+)?([A-Za-z_$][A-Za-z0-9_$]*)\s*(?:,\s*\{([^}]*)\})?\s*from\s+["']([^"']+)["']`)
	tsNamedImportRe   = regexp.MustCompile(`(?m)^\s*import\s+(?:type\s+)?\{([^}]*)\}\s*from\s+["']([^"']+)["']`)
)

type tsImportBinding struct {
	ImportPath string
	TargetName string
}

func extractTSImports(filePath string, source []byte) []string {
	if imports, ok := extractTSImportsWithTreeSitter(filePath, source); ok {
		return imports
	}
	return extractTSImportsRegex(string(source))
}

func extractTSImportBindings(filePath string, source []byte) []tsImportBinding {
	if bindings, ok := extractTSImportBindingsWithTreeSitter(filePath, source); ok {
		return bindings
	}
	return extractTSImportBindingsRegex(string(source))
}

func extractTSImportsRegex(source string) []string {
	imports := make(map[string]struct{})

	for _, match := range importFromRe.FindAllStringSubmatch(source, -1) {
		addTSImport(imports, match[1])
	}
	for _, match := range importBareRe.FindAllStringSubmatch(source, -1) {
		addTSImport(imports, match[1])
	}
	for _, match := range dynamicImportRe.FindAllStringSubmatch(source, -1) {
		addTSImport(imports, match[1])
	}
	for _, match := range requireImportRe.FindAllStringSubmatch(source, -1) {
		addTSImport(imports, match[1])
	}

	return flattenTSImports(imports)
}

func addTSImport(set map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	set[value] = struct{}{}
}

func flattenTSImports(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func isTSImportableFile(filePath string) bool {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs":
		return !strings.HasSuffix(strings.ToLower(filePath), ".d.ts")
	default:
		return false
	}
}

func extractTSImportBindingsRegex(source string) []tsImportBinding {
	bindings := make([]tsImportBinding, 0)

	for _, match := range tsDefaultImportRe.FindAllStringSubmatch(source, -1) {
		if len(match) < 4 {
			continue
		}
		importPath := strings.TrimSpace(match[3])
		if importPath == "" {
			continue
		}
		if strings.TrimSpace(match[1]) != "" {
			bindings = append(bindings, tsImportBinding{ImportPath: importPath, TargetName: "default"})
		}
		bindings = append(bindings, parseTSNamedImportBindings(importPath, match[2])...)
	}

	for _, match := range tsNamedImportRe.FindAllStringSubmatch(source, -1) {
		if len(match) < 3 {
			continue
		}
		importPath := strings.TrimSpace(match[2])
		if importPath == "" {
			continue
		}
		bindings = append(bindings, parseTSNamedImportBindings(importPath, match[1])...)
	}

	return uniqueTSImportBindings(bindings)
}

func parseTSNamedImportBindings(importPath, raw string) []tsImportBinding {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]tsImportBinding, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "type "))
		if part == "" || strings.HasPrefix(part, "*") {
			continue
		}
		targetName := part
		if before, _, ok := strings.Cut(part, " as "); ok {
			targetName = before
		}
		targetName = strings.TrimSpace(targetName)
		if targetName == "" {
			continue
		}
		out = append(out, tsImportBinding{ImportPath: importPath, TargetName: targetName})
	}
	return out
}

func uniqueTSImportBindings(bindings []tsImportBinding) []tsImportBinding {
	if len(bindings) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(bindings))
	out := make([]tsImportBinding, 0, len(bindings))
	for _, binding := range bindings {
		key := strconv.Quote(binding.ImportPath) + "::" + binding.TargetName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, binding)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ImportPath == out[j].ImportPath {
			return out[i].TargetName < out[j].TargetName
		}
		return out[i].ImportPath < out[j].ImportPath
	})
	return out
}
