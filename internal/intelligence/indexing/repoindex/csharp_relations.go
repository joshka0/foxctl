package repoindex

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var csharpUsingRe = regexp.MustCompile(`(?m)^\s*(?:global\s+)?using\s+(?:static\s+)?([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*;`)

type csharpIndexedFile struct {
	RelPath    string
	PkgID      string
	FileNodeID string
	Project    string
	Usings     []string
}

type csharpProjectGraph struct {
	projects      map[string]csharpProject
	fileToProject map[string]string
}

type csharpProject struct {
	RelPath           string
	ProjectReferences []string
	CompileIncludes   []string
}

type csharpProjectXML struct {
	ItemGroups []struct {
		ProjectReferences []struct {
			Include string `xml:"Include,attr"`
		} `xml:"ProjectReference"`
		Compiles []struct {
			Include string `xml:"Include,attr"`
		} `xml:"Compile"`
	} `xml:"ItemGroup"`
}

func extractCSharpUsings(source []byte) []string {
	matches := csharpUsingRe.FindAllSubmatch(source, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value := strings.TrimSpace(string(match[1]))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func loadCSharpProjectGraph(repoRoot string, indexedFiles []string) csharpProjectGraph {
	graph := csharpProjectGraph{
		projects:      make(map[string]csharpProject),
		fileToProject: make(map[string]string),
	}
	projectFiles := findCSharpProjectFiles(repoRoot)
	for _, projectRelPath := range projectFiles {
		project, ok := readCSharpProject(repoRoot, projectRelPath)
		if !ok {
			continue
		}
		graph.projects[project.RelPath] = project
		for _, include := range project.CompileIncludes {
			if include == "" {
				continue
			}
			graph.fileToProject[include] = project.RelPath
		}
	}

	for _, fileRelPath := range indexedFiles {
		if _, ok := graph.fileToProject[fileRelPath]; ok {
			continue
		}
		if projectRelPath := nearestCSharpProject(projectFiles, fileRelPath); projectRelPath != "" {
			graph.fileToProject[fileRelPath] = projectRelPath
		}
	}

	return graph
}

func findCSharpProjectFiles(repoRoot string) []string {
	var files []string
	_ = filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", "obj", "node_modules", "vendor":
				if path != repoRoot {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".csproj") {
			if rel, ok := relPath(repoRoot, path); ok {
				files = append(files, rel)
			}
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func readCSharpProject(repoRoot, projectRelPath string) (csharpProject, bool) {
	content, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(projectRelPath)))
	if err != nil {
		return csharpProject{}, false
	}
	var doc csharpProjectXML
	if err := xml.Unmarshal(content, &doc); err != nil {
		return csharpProject{}, false
	}

	project := csharpProject{RelPath: filepath.ToSlash(projectRelPath)}
	projectDir := filepath.Dir(project.RelPath)
	if projectDir == "." {
		projectDir = ""
	}
	for _, group := range doc.ItemGroups {
		for _, ref := range group.ProjectReferences {
			if resolved := resolveCSharpProjectItem(projectDir, ref.Include); resolved != "" {
				project.ProjectReferences = append(project.ProjectReferences, resolved)
			}
		}
		for _, compile := range group.Compiles {
			if resolved := resolveCSharpProjectItem(projectDir, compile.Include); resolved != "" {
				project.CompileIncludes = append(project.CompileIncludes, resolved)
			}
		}
	}
	project.ProjectReferences = uniqueSortedStrings(project.ProjectReferences)
	project.CompileIncludes = uniqueSortedStrings(project.CompileIncludes)
	return project, true
}

func resolveCSharpProjectItem(projectDir, include string) string {
	include = strings.TrimSpace(include)
	if include == "" || strings.ContainsAny(include, "*?") {
		return ""
	}
	include = strings.ReplaceAll(include, "\\", "/")
	resolved := filepath.Clean(filepath.Join(filepath.FromSlash(projectDir), filepath.FromSlash(include)))
	if resolved == "." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) || resolved == ".." {
		return ""
	}
	return filepath.ToSlash(resolved)
}

func nearestCSharpProject(projectFiles []string, fileRelPath string) string {
	fileRelPath = filepath.ToSlash(fileRelPath)
	best := ""
	bestLen := -1
	for _, projectRelPath := range projectFiles {
		projectDir := filepath.Dir(filepath.ToSlash(projectRelPath))
		if projectDir == "." {
			projectDir = ""
		}
		if !pathUnderDir(fileRelPath, projectDir) {
			continue
		}
		if len(projectDir) > bestLen {
			best = projectRelPath
			bestLen = len(projectDir)
		}
	}
	return best
}

func pathUnderDir(path, dir string) bool {
	path = strings.Trim(filepath.ToSlash(path), "/")
	dir = strings.Trim(filepath.ToSlash(dir), "/")
	if dir == "" {
		return true
	}
	return path == dir || strings.HasPrefix(path, dir+"/")
}

func (g csharpProjectGraph) ProjectForFile(fileRelPath string) string {
	if g.fileToProject == nil {
		return ""
	}
	return g.fileToProject[filepath.ToSlash(fileRelPath)]
}

func (g csharpProjectGraph) References(projectRelPath string) []string {
	if g.projects == nil {
		return nil
	}
	project, ok := g.projects[filepath.ToSlash(projectRelPath)]
	if !ok {
		return nil
	}
	return project.ProjectReferences
}

func applyCSharpRelations(nodes map[string]Node, edges map[string]Edge, repoKey string, graph csharpProjectGraph, indexedFiles []csharpIndexedFile) {
	if len(indexedFiles) == 0 {
		return
	}

	filesByPkg := make(map[string][]string)
	pkgsByProject := make(map[string][]string)
	for _, file := range indexedFiles {
		if file.PkgID == "" {
			continue
		}
		filesByPkg[file.PkgID] = append(filesByPkg[file.PkgID], file.FileNodeID)
		if file.Project != "" {
			pkgsByProject[file.Project] = append(pkgsByProject[file.Project], file.PkgID)
		}
	}
	for pkgID := range filesByPkg {
		sort.Strings(filesByPkg[pkgID])
	}
	for project := range pkgsByProject {
		pkgsByProject[project] = uniqueSortedStrings(pkgsByProject[project])
	}

	for _, file := range indexedFiles {
		applyCSharpUsingRelations(nodes, edges, repoKey, filesByPkg, file)
		applyCSharpProjectReferenceRelations(nodes, edges, repoKey, graph, pkgsByProject, file)
	}
}

func applyCSharpUsingRelations(nodes map[string]Node, edges map[string]Edge, repoKey string, filesByPkg map[string][]string, file csharpIndexedFile) {
	for _, usingName := range file.Usings {
		for _, targetPkgID := range csharpUsingPackageCandidates(usingName) {
			if targetPkgID == file.PkgID {
				continue
			}
			targetPkgNodeID := PackageID(repoKey, targetPkgID)
			if _, ok := nodes[targetPkgNodeID]; !ok {
				continue
			}
			addEdge(edges, Edge{
				Src:    PackageID(repoKey, file.PkgID),
				Dst:    targetPkgNodeID,
				Type:   EdgeImports,
				Weight: 0.75,
				Meta:   csharpRelationMeta("using", usingName),
			})
			addEdge(edges, Edge{
				Src:    file.FileNodeID,
				Dst:    targetPkgNodeID,
				Type:   EdgeImports,
				Weight: 0.85,
				Meta:   csharpRelationMeta("using", usingName),
			})
			for _, targetFileNodeID := range filesByPkg[targetPkgID] {
				addEdge(edges, Edge{
					Src:    file.FileNodeID,
					Dst:    targetFileNodeID,
					Type:   EdgeImports,
					Weight: 0.9,
					Meta:   csharpRelationMeta("using", usingName),
				})
			}
			break
		}
	}
}

func applyCSharpProjectReferenceRelations(nodes map[string]Node, edges map[string]Edge, repoKey string, graph csharpProjectGraph, pkgsByProject map[string][]string, file csharpIndexedFile) {
	if file.Project == "" {
		return
	}
	for _, refProject := range graph.References(file.Project) {
		for _, targetPkgID := range pkgsByProject[refProject] {
			if targetPkgID == "" || targetPkgID == file.PkgID {
				continue
			}
			targetPkgNodeID := PackageID(repoKey, targetPkgID)
			if _, ok := nodes[targetPkgNodeID]; !ok {
				continue
			}
			addEdge(edges, Edge{
				Src:    PackageID(repoKey, file.PkgID),
				Dst:    targetPkgNodeID,
				Type:   EdgeImports,
				Weight: 0.65,
				Meta:   csharpRelationMeta("project_reference", refProject),
			})
			addEdge(edges, Edge{
				Src:    file.FileNodeID,
				Dst:    targetPkgNodeID,
				Type:   EdgeImports,
				Weight: 0.7,
				Meta:   csharpRelationMeta("project_reference", refProject),
			})
		}
	}
}

func csharpUsingPackageCandidates(usingName string) []string {
	usingName = strings.TrimSpace(usingName)
	if usingName == "" {
		return nil
	}
	parts := strings.Split(usingName, ".")
	out := make([]string, 0, len(parts))
	for len(parts) > 0 {
		out = append(out, csharpPkgPrefix+strings.Join(parts, "."))
		parts = parts[:len(parts)-1]
	}
	return out
}

func csharpRelationMeta(kind, value string) []byte {
	if kind == "" || value == "" {
		return nil
	}
	meta, err := json.Marshal(map[string]string{kind: value})
	if err != nil {
		return nil
	}
	return meta
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
