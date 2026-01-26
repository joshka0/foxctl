package repoindex

import "regexp"

var (
	importFromRe    = regexp.MustCompile(`(?m)^\s*(?:import|export)\s+[^;]*\sfrom\s+["']([^"']+)["']`)
	importBareRe    = regexp.MustCompile(`(?m)^\s*import\s+["']([^"']+)["']`)
	dynamicImportRe = regexp.MustCompile(`(?m)import\(\s*["']([^"']+)["']\s*\)`)
	requireImportRe = regexp.MustCompile(`(?m)require\(\s*["']([^"']+)["']\s*\)`)
)

func extractTSImports(source string) []string {
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
	return result
}
