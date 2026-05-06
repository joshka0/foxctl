package repoindex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/platform/fsutil"
	"gopkg.in/yaml.v3"
)

const (
	terraformPkgPrefix  = "tf:"
	kubernetesPkgPrefix = "k8s:"
	shellPkgPrefix      = "sh:"
)

var (
	terraformBlockRe           = regexp.MustCompile(`(?m)^\s*(resource|data|module|variable|output|provider)\s+"([^"]+)"(?:\s+"([^"]+)")?\s*\{`)
	terraformLocalsRe          = regexp.MustCompile(`(?m)^\s*locals\s*\{`)
	terraformLocalAssignRe     = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]+)\s*=`)
	terraformSourceRe          = regexp.MustCompile(`(?m)^\s*source\s*=\s*"([^"]+)"`)
	terraformVarRefRe          = regexp.MustCompile(`\bvar\.([A-Za-z0-9_]+)\b`)
	terraformLocalRefRe        = regexp.MustCompile(`\blocal\.([A-Za-z0-9_]+)\b`)
	terraformModuleRefRe       = regexp.MustCompile(`\bmodule\.([A-Za-z0-9_-]+)\b`)
	terraformModuleOutputRefRe = regexp.MustCompile(`\bmodule\.([A-Za-z0-9_-]+)\.([A-Za-z0-9_]+)\b`)
	terraformOutputRefRe       = regexp.MustCompile(`\boutput\.([A-Za-z0-9_]+)\b`)
	terraformDataRefRe         = regexp.MustCompile(`\bdata\.([A-Za-z0-9_]+)\.([A-Za-z0-9_]+)\b`)
	terraformResourceRefRe     = regexp.MustCompile(`\b([a-z][A-Za-z0-9_]*)\.([A-Za-z0-9_]+)\b`)
	helmTemplateKindRe         = regexp.MustCompile(`(?m)^\s*kind:\s*([A-Za-z0-9]+)\s*$`)
	helmTemplateAPIVerRe       = regexp.MustCompile(`(?m)^\s*apiVersion:\s*([A-Za-z0-9./-]+)\s*$`)
	helmTemplateNameRe         = regexp.MustCompile(`(?m)^\s*name:\s*([^\s{][^\n#]*)$`)
	shellEnvVarRe              = regexp.MustCompile(`\$\{?([A-Z_][A-Z0-9_]*)\}?`)
)

func (b *Builder) buildTerraform(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult) error {
	exclude := []string{
		".terraform/**",
		".git/**",
		"node_modules/**",
		"vendor/**",
	}
	files, err := fsutil.FindFilesRespectingGitignore(opts.RepoRoot, "**/*.tf", exclude)
	if err != nil {
		return fmt.Errorf("repoindex: find terraform files: %w", err)
	}
	sort.Strings(files)
	seenPackages := map[string]bool{}
	allBlocks := make([]terraformBlock, 0)
	for _, fileRelPath := range files {
		absPath := filepath.Join(opts.RepoRoot, fileRelPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("repoindex: read terraform file %s: %w", fileRelPath, err)
		}
		pkgID, pkgNodeID := ensureInfraPackageNode(nodes, terraformPkgPrefix, opts.RepoKey, fileRelPath)
		if !seenPackages[pkgID] {
			result.Packages++
			seenPackages[pkgID] = true
		}
		fileNodeID := addInfraFileNode(opts, nodes, edges, pkgID, pkgNodeID, fileRelPath, content)
		result.Files++
		blocks, added := addTerraformConcepts(opts, nodes, edges, fileNodeID, fileRelPath, content)
		allBlocks = append(allBlocks, blocks...)
		result.Symbols += added
	}
	addTerraformModuleSourceEdges(opts, nodes, edges, allBlocks)
	addTerraformReferenceEdges(opts, nodes, edges, allBlocks)
	addTerraformCrossModuleEdges(opts, nodes, edges, allBlocks)
	return nil
}

func (b *Builder) buildKubernetes(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult) error {
	exclude := []string{
		".git/**",
		"node_modules/**",
		"vendor/**",
		".foxctl/**",
	}
	fileSet := map[string]struct{}{}
	for _, pattern := range []string{"**/*.yaml", "**/*.yml", "**/*.tpl"} {
		paths, err := fsutil.FindFilesRespectingGitignore(opts.RepoRoot, pattern, exclude)
		if err != nil {
			return fmt.Errorf("repoindex: find kubernetes files: %w", err)
		}
		for _, path := range paths {
			fileSet[path] = struct{}{}
		}
	}
	fileList := make([]string, 0, len(fileSet))
	for path := range fileSet {
		fileList = append(fileList, path)
	}
	sort.Strings(fileList)
	seenPackages := map[string]bool{}
	chartRefs := make([]helmChartRef, 0)
	appRefs := make([]argoApplicationRef, 0)
	for _, fileRelPath := range fileList {
		absPath := filepath.Join(opts.RepoRoot, fileRelPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("repoindex: read kubernetes file %s: %w", fileRelPath, err)
		}
		chartRef, hasChart := extractHelmChartRef(opts.RepoKey, fileRelPath, content)
		if hasChart {
			chartRefs = append(chartRefs, chartRef)
		}
		apps := extractArgoApplicationRefs(opts.RepoKey, fileRelPath, content)
		if len(apps) > 0 {
			appRefs = append(appRefs, apps...)
		}
		concepts := extractKubernetesConcepts(opts.RepoKey, fileRelPath, content)
		if len(concepts) == 0 && isHelmTemplateFile(fileRelPath) {
			concepts = extractHelmTemplateConcepts(opts.RepoKey, fileRelPath, content)
		}
		pkgID, pkgNodeID := ensureInfraPackageNode(nodes, kubernetesPkgPrefix, opts.RepoKey, fileRelPath)
		if !seenPackages[pkgID] {
			result.Packages++
			seenPackages[pkgID] = true
		}
		fileNodeID := addInfraFileNode(opts, nodes, edges, pkgID, pkgNodeID, fileRelPath, content)
		result.Files++
		if hasChart {
			addNode(nodes, chartRef.node)
			addEdge(edges, Edge{
				Src:    pkgNodeID,
				Dst:    chartRef.node.ID,
				Type:   EdgeContains,
				Weight: 1.0,
			})
			addEdge(edges, Edge{
				Src:    fileNodeID,
				Dst:    chartRef.node.ID,
				Type:   EdgeContains,
				Weight: 1.0,
			})
			result.Symbols++
		}
		for _, app := range apps {
			addNode(nodes, app.node)
			addEdge(edges, Edge{
				Src:    fileNodeID,
				Dst:    app.node.ID,
				Type:   EdgeContains,
				Weight: 1.0,
			})
			result.Symbols++
		}
		for _, concept := range concepts {
			addNode(nodes, concept.node)
			addEdge(edges, Edge{
				Src:    fileNodeID,
				Dst:    concept.node.ID,
				Type:   EdgeTouchesResource,
				Weight: 1.0,
				Meta:   concept.meta,
			})
			result.Symbols++
		}
	}
	linkHelmCharts(nodes, edges, chartRefs)
	linkArgoApplications(nodes, edges, chartRefs, appRefs, opts.RepoKey)
	return nil
}

func (b *Builder) buildShell(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult) error {
	exclude := []string{
		".git/**",
		"node_modules/**",
		"vendor/**",
		".foxctl/**",
	}
	files, err := fsutil.FindFilesRespectingGitignore(opts.RepoRoot, "**/*.sh", exclude)
	if err != nil {
		return fmt.Errorf("repoindex: find shell files: %w", err)
	}
	sort.Strings(files)
	seenPackages := map[string]bool{}
	for _, fileRelPath := range files {
		absPath := filepath.Join(opts.RepoRoot, fileRelPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("repoindex: read shell file %s: %w", fileRelPath, err)
		}
		pkgID, pkgNodeID := ensureInfraPackageNode(nodes, shellPkgPrefix, opts.RepoKey, fileRelPath)
		if !seenPackages[pkgID] {
			result.Packages++
			seenPackages[pkgID] = true
		}
		fileNodeID := addInfraFileNode(opts, nodes, edges, pkgID, pkgNodeID, fileRelPath, content)
		result.Files++
		result.Symbols += addShellConcepts(opts, nodes, edges, fileNodeID, fileRelPath, content)
	}
	return nil
}

func ensureInfraPackageNode(nodes map[string]Node, prefix, repoKey, fileRelPath string) (string, string) {
	pkgID := infraPackageID(prefix, fileRelPath)
	pkgNodeID := PackageID(repoKey, pkgID)
	display := strings.TrimPrefix(pkgID, prefix)
	if display == "" || display == "." {
		display = "root"
	}
	kindLabel := infraKindLabel(prefix)
	summaryLabel := infraPackageSummaryLabel(prefix, display)
	addNode(nodes, Node{
		ID:        pkgNodeID,
		Kind:      NodePackage,
		Pkg:       pkgID,
		Name:      display,
		Summary:   fmt.Sprintf("%s %s.", kindLabel, summaryLabel),
		UpdatedAt: time.Now().UTC(),
	})
	return pkgID, pkgNodeID
}

func addInfraFileNode(opts BuildOptions, nodes map[string]Node, edges map[string]Edge, pkgID, pkgNodeID, fileRelPath string, content []byte) string {
	fileNodeID := FileID(opts.RepoKey, pkgID, fileRelPath)
	kindLabel := infraKindLabelFromPkgID(pkgID)
	pkgDisplay := infraPackageDisplay(pkgID)
	docLabel := infraFileDocLabel(kindLabel, pkgDisplay)
	fileNode := Node{
		ID:        fileNodeID,
		Kind:      NodeFile,
		Pkg:       pkgID,
		File:      fileRelPath,
		Name:      filepath.Base(fileRelPath),
		Doc:       fmt.Sprintf("%s path %s package %s.", docLabel, filepath.ToSlash(fileRelPath), pkgDisplay),
		SpanStart: 1,
		SpanEnd:   countLines(content),
		Hash:      symbol.ComputeDigest(content),
		UpdatedAt: time.Now().UTC(),
	}
	if strings.TrimSpace(fileNode.Summary) == "" {
		fileNode.Summary = fmt.Sprintf("%s %s in package %s.", docLabel, filepath.Base(fileRelPath), pkgDisplay)
	}
	addNode(nodes, fileNode)
	addEdge(edges, Edge{
		Src:    pkgNodeID,
		Dst:    fileNode.ID,
		Type:   EdgeContains,
		Weight: 1.0,
	})
	return fileNodeID
}

func addTerraformConcepts(opts BuildOptions, nodes map[string]Node, edges map[string]Edge, fileNodeID, fileRelPath string, content []byte) ([]terraformBlock, int) {
	pkgID := infraPackageID(terraformPkgPrefix, fileRelPath)
	blocks := parseTerraformBlocks(pkgID, fileRelPath, content)
	added := 0
	for _, block := range blocks {
		node := Node{
			ID:        terraformConceptNodeID(opts.RepoKey, block.pkgID, block.conceptID),
			Kind:      NodeConcept,
			Pkg:       block.pkgID,
			File:      block.fileRelPath,
			Name:      block.displayName,
			Summary:   block.summary,
			SpanStart: block.startLine,
			SpanEnd:   block.startLine,
			UpdatedAt: time.Now().UTC(),
		}
		meta, _ := json.Marshal(map[string]any{
			"block_type": block.blockType,
			"type":       block.typeName,
			"name":       block.name,
			"file":       block.fileRelPath,
		})
		addNode(nodes, node)
		addEdge(edges, Edge{
			Src:    fileNodeID,
			Dst:    node.ID,
			Type:   EdgeTouchesResource,
			Weight: 1.0,
			Meta:   meta,
		})
		added++
	}
	return blocks, added
}

func addShellConcepts(opts BuildOptions, nodes map[string]Node, edges map[string]Edge, fileNodeID, fileRelPath string, content []byte) int {
	commands, envVars := extractShellConcepts(content)
	added := 0
	for _, cmd := range commands {
		node := Node{
			ID:        NamespacedID(opts.RepoKey, ConceptCommand+cmd),
			Kind:      NodeConcept,
			Pkg:       infraPackageID(shellPkgPrefix, fileRelPath),
			File:      fileRelPath,
			Name:      cmd,
			Summary:   fmt.Sprintf("Shell command %s referenced by %s.", cmd, fileRelPath),
			UpdatedAt: time.Now().UTC(),
		}
		addNode(nodes, node)
		addEdge(edges, Edge{
			Src:    fileNodeID,
			Dst:    node.ID,
			Type:   EdgeCalls,
			Weight: 0.7,
		})
		added++
	}
	for _, envVar := range envVars {
		node := Node{
			ID:        NamespacedID(opts.RepoKey, ConceptEnvVar+envVar),
			Kind:      NodeConcept,
			Pkg:       infraPackageID(shellPkgPrefix, fileRelPath),
			File:      fileRelPath,
			Name:      envVar,
			Summary:   fmt.Sprintf("Shell environment variable %s referenced by %s.", envVar, fileRelPath),
			UpdatedAt: time.Now().UTC(),
		}
		addNode(nodes, node)
		addEdge(edges, Edge{
			Src:    fileNodeID,
			Dst:    node.ID,
			Type:   EdgeRefersTo,
			Weight: 0.5,
		})
		added++
	}
	return added
}

type kubeConcept struct {
	node Node
	meta json.RawMessage
}

type helmChartRef struct {
	dir    string
	pkgID  string
	node   Node
	values []string
}

type argoApplicationRef struct {
	node       Node
	pkgID      string
	fileRel    string
	chartPath  string
	valueFiles []string
}

type terraformBlock struct {
	blockType   string
	typeName    string
	name        string
	pkgID       string
	fileRelPath string
	startLine   int
	body        string
	conceptID   string
	displayName string
	summary     string
}

func extractKubernetesConcepts(repoKey, fileRelPath string, content []byte) []kubeConcept {
	decoder := yaml.NewDecoder(strings.NewReader(string(content)))
	out := []kubeConcept{}
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			break
		}
		if len(node.Content) == 0 {
			continue
		}
		doc := node.Content[0]
		apiVersion := yamlLookup(doc, "apiVersion")
		kind := yamlLookup(doc, "kind")
		if apiVersion == "" || kind == "" {
			continue
		}
		metadata := yamlMapping(doc, "metadata")
		name := yamlLookupNode(metadata, "name")
		if name == "" {
			name = filepath.Base(strings.TrimSuffix(fileRelPath, filepath.Ext(fileRelPath)))
		}
		namespace := yamlLookupNode(metadata, "namespace")
		resourceKey := filepath.ToSlash(fileRelPath) + ":" + strings.ToLower(kind) + ":" + name
		if namespace != "" {
			resourceKey = filepath.ToSlash(fileRelPath) + ":" + strings.ToLower(kind) + ":" + namespace + "/" + name
		}
		summary := fmt.Sprintf("Kubernetes %s %s (%s).", kind, name, apiVersion)
		display := kind + "/" + name
		if namespace != "" {
			display = kind + "/" + namespace + "/" + name
		}
		meta, _ := json.Marshal(map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"name":       name,
			"namespace":  namespace,
			"file":       fileRelPath,
		})
		out = append(out, kubeConcept{
			node: Node{
				ID:        NamespacedID(repoKey, ConceptResource+resourceKey),
				Kind:      NodeConcept,
				Pkg:       infraPackageID(kubernetesPkgPrefix, fileRelPath),
				File:      fileRelPath,
				Name:      display,
				Summary:   summary,
				SpanStart: doc.Line,
				SpanEnd:   doc.Line,
				UpdatedAt: time.Now().UTC(),
			},
			meta: meta,
		})
	}
	return out
}

func extractHelmChartRef(repoKey, fileRelPath string, content []byte) (helmChartRef, bool) {
	if filepath.Base(fileRelPath) != "Chart.yaml" {
		return helmChartRef{}, false
	}
	var chart struct {
		APIVersion  string   `yaml:"apiVersion"`
		Name        string   `yaml:"name"`
		Version     string   `yaml:"version"`
		AppVersion  string   `yaml:"appVersion"`
		Type        string   `yaml:"type"`
		Description string   `yaml:"description"`
		Keywords    []string `yaml:"keywords"`
	}
	if err := yaml.Unmarshal(content, &chart); err != nil {
		return helmChartRef{}, false
	}
	name := strings.TrimSpace(chart.Name)
	if name == "" {
		return helmChartRef{}, false
	}
	dir := filepath.ToSlash(filepath.Dir(fileRelPath))
	pkgID := infraPackageID(kubernetesPkgPrefix, fileRelPath)
	summary := fmt.Sprintf("Helm chart %s in %s.", name, dir)
	if strings.TrimSpace(chart.Description) != "" {
		summary = summary + " " + strings.TrimSpace(chart.Description)
	}
	node := Node{
		ID:        NamespacedID(repoKey, ConceptChart+pkgID+":"+strings.ToLower(name)),
		Kind:      NodeConcept,
		Pkg:       pkgID,
		File:      fileRelPath,
		Name:      "chart " + name,
		Doc:       fmt.Sprintf("Helm chart path %s.", dir),
		Summary:   summary,
		SpanStart: 1,
		SpanEnd:   1,
		UpdatedAt: time.Now().UTC(),
	}
	return helmChartRef{
		dir:    dir,
		pkgID:  pkgID,
		node:   node,
		values: []string{filepath.ToSlash(filepath.Join(dir, "values.yaml"))},
	}, true
}

func extractArgoApplicationRefs(repoKey, fileRelPath string, content []byte) []argoApplicationRef {
	decoder := yaml.NewDecoder(strings.NewReader(string(content)))
	out := []argoApplicationRef{}
	for {
		var node yaml.Node
		if err := decoder.Decode(&node); err != nil {
			break
		}
		if len(node.Content) == 0 {
			continue
		}
		doc := node.Content[0]
		if yamlLookup(doc, "kind") != "Application" {
			continue
		}
		if !strings.Contains(strings.ToLower(yamlLookup(doc, "apiVersion")), "argoproj.io/") {
			continue
		}
		metadata := yamlMapping(doc, "metadata")
		name := yamlLookupNode(metadata, "name")
		if name == "" {
			name = filepath.Base(strings.TrimSuffix(fileRelPath, filepath.Ext(fileRelPath)))
		}
		namespace := yamlLookupNode(metadata, "namespace")
		spec := yamlMapping(doc, "spec")
		chartPath := ""
		valueFiles := []string{}
		if source := yamlMapping(spec, "source"); source != nil {
			chartPath = strings.TrimSpace(yamlLookupNode(source, "path"))
			if helmNode := yamlMapping(source, "helm"); helmNode != nil {
				valueFiles = append(valueFiles, yamlSequenceValues(yamlMapping(helmNode, "valueFiles"))...)
			}
		}
		if sources := yamlSequenceNode(yamlMapping(spec, "sources")); len(sources) > 0 {
			for _, source := range sources {
				if chartPath == "" {
					chartPath = strings.TrimSpace(yamlLookupNode(source, "path"))
				}
				if helmNode := yamlMapping(source, "helm"); helmNode != nil {
					valueFiles = append(valueFiles, yamlSequenceValues(yamlMapping(helmNode, "valueFiles"))...)
				}
			}
		}
		nodeID := NamespacedID(repoKey, ConceptApp+filepath.ToSlash(fileRelPath)+":"+strings.ToLower(name))
		display := "application " + name
		if namespace != "" {
			display = "application " + namespace + "/" + name
		}
		out = append(out, argoApplicationRef{
			node: Node{
				ID:        nodeID,
				Kind:      NodeConcept,
				Pkg:       infraPackageID(kubernetesPkgPrefix, fileRelPath),
				File:      fileRelPath,
				Name:      display,
				Summary:   fmt.Sprintf("ArgoCD Application %s targets chart path %s.", name, firstNonEmptyString(chartPath, "(unspecified)")),
				SpanStart: doc.Line,
				SpanEnd:   doc.Line,
				UpdatedAt: time.Now().UTC(),
			},
			pkgID:      infraPackageID(kubernetesPkgPrefix, fileRelPath),
			fileRel:    fileRelPath,
			chartPath:  filepath.ToSlash(strings.TrimSpace(chartPath)),
			valueFiles: append([]string(nil), valueFiles...),
		})
	}
	return out
}

func extractHelmTemplateConcepts(repoKey, fileRelPath string, content []byte) []kubeConcept {
	apiVersion := ""
	if match := helmTemplateAPIVerRe.FindStringSubmatch(string(content)); len(match) > 1 {
		apiVersion = strings.TrimSpace(match[1])
	}
	kind := ""
	if match := helmTemplateKindRe.FindStringSubmatch(string(content)); len(match) > 1 {
		kind = strings.TrimSpace(match[1])
	}
	if apiVersion == "" || kind == "" {
		return nil
	}
	name := strings.TrimSuffix(filepath.Base(fileRelPath), filepath.Ext(fileRelPath))
	if match := helmTemplateNameRe.FindStringSubmatch(string(content)); len(match) > 1 {
		candidate := strings.TrimSpace(match[1])
		if !strings.Contains(candidate, "{{") {
			name = candidate
		}
	}
	resourceKey := filepath.ToSlash(fileRelPath) + ":" + strings.ToLower(kind) + ":" + name
	return []kubeConcept{{
		node: Node{
			ID:        NamespacedID(repoKey, ConceptResource+resourceKey),
			Kind:      NodeConcept,
			Pkg:       infraPackageID(kubernetesPkgPrefix, fileRelPath),
			File:      fileRelPath,
			Name:      kind + "/" + name,
			Summary:   fmt.Sprintf("Kubernetes %s template %s (%s).", kind, name, apiVersion),
			SpanStart: 1,
			SpanEnd:   1,
			UpdatedAt: time.Now().UTC(),
		},
	}}
}

func linkHelmCharts(nodes map[string]Node, edges map[string]Edge, charts []helmChartRef) {
	if len(charts) == 0 {
		return
	}
	fileNodes := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == NodeFile {
			fileNodes = append(fileNodes, node)
		}
	}
	for _, chart := range charts {
		for _, node := range fileNodes {
			if !strings.HasPrefix(filepath.ToSlash(node.File), chart.dir+"/") {
				continue
			}
			if node.ID == chart.node.ID {
				continue
			}
			addEdge(edges, Edge{
				Src:    chart.node.ID,
				Dst:    node.ID,
				Type:   EdgeContains,
				Weight: 0.9,
			})
		}
	}
}

func linkArgoApplications(nodes map[string]Node, edges map[string]Edge, charts []helmChartRef, apps []argoApplicationRef, repoKey string) {
	if len(apps) == 0 {
		return
	}
	chartByDir := map[string]helmChartRef{}
	for _, chart := range charts {
		chartByDir[filepath.ToSlash(chart.dir)] = chart
	}
	fileNodes := map[string]string{}
	for _, node := range nodes {
		if node.Kind == NodeFile && strings.TrimSpace(node.File) != "" {
			fileNodes[filepath.ToSlash(node.File)] = node.ID
		}
	}
	for _, app := range apps {
		resolvedChartDir := ""
		if chart := resolveArgoChartRef(chartByDir, app.fileRel, app.chartPath); chart != nil {
			resolvedChartDir = chart.dir
			addEdge(edges, Edge{
				Src:    app.node.ID,
				Dst:    chart.node.ID,
				Type:   EdgeImports,
				Weight: 1.0,
			})
		}
		for _, valueFile := range normalizeArgoValueFiles(app.fileRel, resolvedChartDir, app.chartPath, app.valueFiles) {
			if targetID := fileNodes[valueFile]; targetID != "" {
				addEdge(edges, Edge{
					Src:    app.node.ID,
					Dst:    targetID,
					Type:   EdgeRefersTo,
					Weight: 0.8,
				})
			}
		}
		_ = repoKey
	}
}

func resolveArgoChartRef(charts map[string]helmChartRef, appFileRel, chartPath string) *helmChartRef {
	chartPath = filepath.ToSlash(strings.TrimSpace(chartPath))
	if chartPath == "" {
		return nil
	}
	if chart, ok := charts[chartPath]; ok {
		return &chart
	}
	parentDir := filepath.ToSlash(filepath.Dir(filepath.Dir(appFileRel)))
	if parentDir != "." && parentDir != "" {
		candidate := filepath.ToSlash(filepath.Clean(filepath.Join(parentDir, chartPath)))
		if chart, ok := charts[candidate]; ok {
			return &chart
		}
	}
	base := filepath.Base(chartPath)
	var match *helmChartRef
	for dir, chart := range charts {
		if filepath.Base(dir) != base {
			continue
		}
		if match != nil {
			return nil
		}
		c := chart
		match = &c
	}
	return match
}

func normalizeArgoValueFiles(fileRelPath, resolvedChartDir, chartPath string, values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = filepath.ToSlash(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		candidates := []string{
			value,
			filepath.ToSlash(filepath.Join(chartPath, value)),
			filepath.ToSlash(filepath.Join(resolvedChartDir, value)),
			filepath.ToSlash(filepath.Join(filepath.Dir(fileRelPath), value)),
		}
		for _, candidate := range candidates {
			candidate = filepath.ToSlash(filepath.Clean(strings.TrimSpace(candidate)))
			if candidate == "." || candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
		}
	}
	return out
}

func isHelmTemplateFile(fileRelPath string) bool {
	rel := filepath.ToSlash(strings.TrimSpace(fileRelPath))
	return strings.Contains(rel, "/templates/") && (strings.HasSuffix(rel, ".yaml") || strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".tpl"))
}

func terraformConceptMeta(blockType, typeName, name string) (conceptID, displayName, summary string) {
	switch blockType {
	case "resource", "data":
		conceptID = blockType + ":" + typeName + "." + name
		displayName = blockType + " " + typeName + "." + name
		summary = fmt.Sprintf("Terraform %s %s %s.", blockType, typeName, name)
	case "provider":
		conceptID = blockType + ":" + typeName
		displayName = blockType + " " + typeName
		summary = fmt.Sprintf("Terraform provider %s.", typeName)
	default:
		conceptID = blockType + ":" + typeName
		displayName = blockType + " " + typeName
		if name != "" {
			conceptID += "." + name
			displayName = blockType + " " + name
		}
		summary = fmt.Sprintf("Terraform %s %s.", blockType, firstNonEmptyString(name, typeName))
	}
	return conceptID, displayName, summary
}

func terraformConceptNodeID(repoKey, pkgID, conceptID string) string {
	return NamespacedID(repoKey, ConceptResource+pkgID+":"+conceptID)
}

func infraPackageIDFromDir(prefix, dir string) string {
	dir = filepath.ToSlash(filepath.Clean(strings.TrimSpace(dir)))
	if dir == "." || dir == "" {
		return prefix + "root"
	}
	return prefix + dir
}

func infraKindLabel(prefix string) string {
	switch prefix {
	case terraformPkgPrefix:
		return "Terraform"
	case kubernetesPkgPrefix:
		return "Kubernetes"
	case shellPkgPrefix:
		return "Shell"
	default:
		return "Infrastructure"
	}
}

func infraKindLabelFromPkgID(pkgID string) string {
	switch {
	case strings.HasPrefix(pkgID, terraformPkgPrefix):
		return "Terraform"
	case strings.HasPrefix(pkgID, kubernetesPkgPrefix):
		return "Kubernetes"
	case strings.HasPrefix(pkgID, shellPkgPrefix):
		return "Shell"
	default:
		return "Infrastructure"
	}
}

func infraPackageDisplay(pkgID string) string {
	switch {
	case strings.HasPrefix(pkgID, terraformPkgPrefix):
		return strings.TrimPrefix(pkgID, terraformPkgPrefix)
	case strings.HasPrefix(pkgID, kubernetesPkgPrefix):
		return strings.TrimPrefix(pkgID, kubernetesPkgPrefix)
	case strings.HasPrefix(pkgID, shellPkgPrefix):
		return strings.TrimPrefix(pkgID, shellPkgPrefix)
	default:
		return pkgID
	}
}

func infraPackageSummaryLabel(prefix, display string) string {
	switch prefix {
	case terraformPkgPrefix:
		if display == "root" {
			return "root package"
		}
		if strings.HasPrefix(display, "modules/") {
			return "module package " + display
		}
		return "package " + display
	default:
		return "package " + display
	}
}

func infraFileDocLabel(kindLabel, pkgDisplay string) string {
	if kindLabel == "Terraform" && strings.HasPrefix(pkgDisplay, "modules/") {
		return "Terraform module file"
	}
	return kindLabel + " file"
}

func parseTerraformBlocks(pkgID, fileRelPath string, content []byte) []terraformBlock {
	text := string(content)
	blocks := make([]terraformBlock, 0)

	matches := terraformBlockRe.FindAllStringSubmatchIndex(text, -1)
	for _, match := range matches {
		blockType := strings.TrimSpace(text[match[2]:match[3]])
		typeName := strings.TrimSpace(text[match[4]:match[5]])
		name := ""
		if match[6] >= 0 && match[7] >= 0 {
			name = strings.TrimSpace(text[match[6]:match[7]])
		}
		openBrace := strings.Index(text[match[0]:match[1]], "{")
		if openBrace < 0 {
			continue
		}
		openIdx := match[0] + openBrace
		closeIdx := terraformBlockEnd(text, openIdx)
		body := ""
		if closeIdx > openIdx+1 {
			body = text[openIdx+1 : closeIdx]
		}
		conceptID, displayName, summary := terraformConceptMeta(blockType, typeName, name)
		blocks = append(blocks, terraformBlock{
			blockType:   blockType,
			typeName:    typeName,
			name:        name,
			pkgID:       pkgID,
			fileRelPath: fileRelPath,
			startLine:   lineForOffset(content, match[0]),
			body:        body,
			conceptID:   conceptID,
			displayName: displayName,
			summary:     summary,
		})
	}

	localMatches := terraformLocalsRe.FindAllStringIndex(text, -1)
	for _, match := range localMatches {
		openBrace := strings.Index(text[match[0]:match[1]], "{")
		if openBrace < 0 {
			continue
		}
		openIdx := match[0] + openBrace
		closeIdx := terraformBlockEnd(text, openIdx)
		if closeIdx <= openIdx+1 {
			continue
		}
		body := text[openIdx+1 : closeIdx]
		assignments := terraformLocalAssignRe.FindAllStringSubmatchIndex(body, -1)
		for i, assign := range assignments {
			localName := strings.TrimSpace(body[assign[2]:assign[3]])
			bodyStart := assign[0]
			bodyEnd := len(body)
			if i+1 < len(assignments) {
				bodyEnd = assignments[i+1][0]
			}
			localBody := strings.TrimSpace(body[bodyStart:bodyEnd])
			conceptID, displayName, summary := terraformConceptMeta("local", localName, "")
			blocks = append(blocks, terraformBlock{
				blockType:   "local",
				typeName:    localName,
				pkgID:       pkgID,
				fileRelPath: fileRelPath,
				startLine:   lineForOffset(content, openIdx+1+bodyStart),
				body:        localBody,
				conceptID:   conceptID,
				displayName: displayName,
				summary:     summary,
			})
		}
	}

	return blocks
}

func terraformBlockEnd(text string, openIdx int) int {
	if openIdx < 0 || openIdx >= len(text) || text[openIdx] != '{' {
		return openIdx
	}
	depth := 0
	inString := false
	escaped := false
	for i := openIdx; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(text)
}

func addTerraformModuleSourceEdges(opts BuildOptions, nodes map[string]Node, edges map[string]Edge, blocks []terraformBlock) {
	for _, block := range blocks {
		if block.blockType != "module" {
			continue
		}
		matches := terraformSourceRe.FindStringSubmatch(block.body)
		if len(matches) < 2 {
			continue
		}
		source := strings.TrimSpace(matches[1])
		if !strings.HasPrefix(source, "./") && !strings.HasPrefix(source, "../") {
			continue
		}
		baseDir := filepath.Dir(block.fileRelPath)
		targetDir := filepath.Clean(filepath.Join(baseDir, source))
		targetPkgID := infraPackageIDFromDir(terraformPkgPrefix, targetDir)
		targetPkgNodeID := PackageID(opts.RepoKey, targetPkgID)
		addNode(nodes, Node{
			ID:        targetPkgNodeID,
			Kind:      NodePackage,
			Pkg:       targetPkgID,
			Name:      strings.TrimPrefix(targetPkgID, terraformPkgPrefix),
			UpdatedAt: time.Now().UTC(),
		})
		addEdge(edges, Edge{
			Src:    terraformConceptNodeID(opts.RepoKey, block.pkgID, block.conceptID),
			Dst:    targetPkgNodeID,
			Type:   EdgeImports,
			Weight: 1.0,
			Meta:   importMeta(source),
		})
	}
}

func addTerraformCrossModuleEdges(opts BuildOptions, nodes map[string]Node, edges map[string]Edge, blocks []terraformBlock) {
	varTargets := make(map[string]string)
	outputTargets := make(map[string]string)
	moduleTargets := make(map[string]string)
	for _, block := range blocks {
		switch block.blockType {
		case "variable":
			varTargets[block.pkgID+"|"+block.typeName] = terraformConceptNodeID(opts.RepoKey, block.pkgID, block.conceptID)
		case "output":
			outputTargets[block.pkgID+"|"+block.typeName] = terraformConceptNodeID(opts.RepoKey, block.pkgID, block.conceptID)
		}
	}

	for _, block := range blocks {
		if block.blockType != "module" {
			continue
		}
		sourceMatches := terraformSourceRe.FindStringSubmatch(block.body)
		if len(sourceMatches) < 2 {
			continue
		}
		source := strings.TrimSpace(sourceMatches[1])
		if !strings.HasPrefix(source, "./") && !strings.HasPrefix(source, "../") {
			continue
		}
		baseDir := filepath.Dir(block.fileRelPath)
		targetDir := filepath.Clean(filepath.Join(baseDir, source))
		targetPkgID := infraPackageIDFromDir(terraformPkgPrefix, targetDir)
		moduleName := firstNonEmptyString(block.name, block.typeName)
		moduleTargets[block.pkgID+"|"+moduleName] = targetPkgID
		targetPkgNodeID := PackageID(opts.RepoKey, targetPkgID)
		srcConceptID := terraformConceptNodeID(opts.RepoKey, block.pkgID, block.conceptID)
		srcPkgNodeID := PackageID(opts.RepoKey, block.pkgID)

		addEdge(edges, Edge{
			Src:    srcPkgNodeID,
			Dst:    targetPkgNodeID,
			Type:   EdgeImports,
			Weight: 0.9,
			Meta:   importMeta(source),
		})

		for _, inputName := range terraformModuleInputRefs(block.body) {
			targetID := varTargets[targetPkgID+"|"+inputName]
			if targetID == "" {
				continue
			}
			addEdge(edges, Edge{
				Src:    srcConceptID,
				Dst:    targetID,
				Type:   EdgeRefersTo,
				Weight: 0.85,
			})
		}
	}

	for _, block := range blocks {
		srcConceptID := terraformConceptNodeID(opts.RepoKey, block.pkgID, block.conceptID)
		for _, ref := range terraformModuleOutputRefs(block.body) {
			targetPkgID := moduleTargets[block.pkgID+"|"+ref.moduleName]
			if targetPkgID == "" {
				continue
			}
			targetID := outputTargets[targetPkgID+"|"+ref.outputName]
			if targetID == "" || targetID == srcConceptID {
				continue
			}
			if _, ok := nodes[targetID]; !ok {
				continue
			}
			addEdge(edges, Edge{
				Src:    srcConceptID,
				Dst:    targetID,
				Type:   EdgeRefersTo,
				Weight: 0.9,
			})
		}
	}
}

func addTerraformReferenceEdges(opts BuildOptions, nodes map[string]Node, edges map[string]Edge, blocks []terraformBlock) {
	for _, block := range blocks {
		srcID := terraformConceptNodeID(opts.RepoKey, block.pkgID, block.conceptID)
		for _, targetID := range terraformReferenceTargets(opts.RepoKey, block.pkgID, block.body) {
			if targetID == "" || targetID == srcID {
				continue
			}
			if _, ok := nodes[targetID]; !ok {
				continue
			}
			addEdge(edges, Edge{
				Src:    srcID,
				Dst:    targetID,
				Type:   EdgeRefersTo,
				Weight: 0.8,
			})
		}
	}
}

func terraformReferenceTargets(repoKey, pkgID, body string) []string {
	targets := make([]string, 0)
	seen := map[string]struct{}{}
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		targets = append(targets, id)
	}

	for _, match := range terraformVarRefRe.FindAllStringSubmatch(body, -1) {
		if len(match) > 1 {
			add(terraformConceptNodeID(repoKey, pkgID, "variable:"+strings.TrimSpace(match[1])))
		}
	}
	for _, match := range terraformLocalRefRe.FindAllStringSubmatch(body, -1) {
		if len(match) > 1 {
			add(terraformConceptNodeID(repoKey, pkgID, "local:"+strings.TrimSpace(match[1])))
		}
	}
	for _, match := range terraformModuleRefRe.FindAllStringSubmatch(body, -1) {
		if len(match) > 1 {
			add(terraformConceptNodeID(repoKey, pkgID, "module:"+strings.TrimSpace(match[1])))
		}
	}
	for _, match := range terraformOutputRefRe.FindAllStringSubmatch(body, -1) {
		if len(match) > 1 {
			add(terraformConceptNodeID(repoKey, pkgID, "output:"+strings.TrimSpace(match[1])))
		}
	}
	for _, match := range terraformDataRefRe.FindAllStringSubmatch(body, -1) {
		if len(match) > 2 {
			add(terraformConceptNodeID(repoKey, pkgID, "data:"+strings.TrimSpace(match[1])+"."+strings.TrimSpace(match[2])))
		}
	}
	for _, match := range terraformResourceRefRe.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 {
			continue
		}
		prefix := strings.TrimSpace(match[1])
		name := strings.TrimSpace(match[2])
		switch prefix {
		case "var", "local", "module", "data", "each", "count", "path", "self", "terraform", "output":
			continue
		}
		add(terraformConceptNodeID(repoKey, pkgID, "resource:"+prefix+"."+name))
	}

	return targets
}

func terraformModuleInputRefs(body string) []string {
	assignments := terraformLocalAssignRe.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(assignments))
	seen := map[string]struct{}{}
	for _, match := range assignments {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" || isTerraformReservedModuleArg(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

type terraformModuleOutputRef struct {
	moduleName string
	outputName string
}

func terraformModuleOutputRefs(body string) []terraformModuleOutputRef {
	matches := terraformModuleOutputRefRe.FindAllStringSubmatch(body, -1)
	out := make([]terraformModuleOutputRef, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		moduleName := strings.TrimSpace(match[1])
		outputName := strings.TrimSpace(match[2])
		if moduleName == "" || outputName == "" {
			continue
		}
		key := moduleName + "|" + outputName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, terraformModuleOutputRef{
			moduleName: moduleName,
			outputName: outputName,
		})
	}
	return out
}

func isTerraformReservedModuleArg(name string) bool {
	switch strings.TrimSpace(name) {
	case "source", "version", "providers", "count", "for_each", "depends_on":
		return true
	default:
		return false
	}
}

func infraPackageID(prefix, fileRelPath string) string {
	dir := filepath.ToSlash(filepath.Dir(strings.TrimSpace(fileRelPath)))
	if dir == "." || dir == "" {
		return prefix + "root"
	}
	return prefix + dir
}

func lineForOffset(content []byte, offset int) int {
	if offset <= 0 {
		return 1
	}
	line := 1
	for i := 0; i < offset && i < len(content); i++ {
		if content[i] == '\n' {
			line++
		}
	}
	return line
}

func extractShellConcepts(content []byte) ([]string, []string) {
	commandSet := map[string]struct{}{}
	envSet := map[string]struct{}{}
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, match := range shellEnvVarRe.FindAllStringSubmatch(line, -1) {
			if len(match) > 1 {
				envSet[match[1]] = struct{}{}
			}
		}
		for _, segment := range splitShellSegments(line) {
			cmd := firstShellCommand(segment)
			if cmd == "" {
				continue
			}
			commandSet[cmd] = struct{}{}
		}
	}
	return sortedStringKeys(commandSet), sortedStringKeys(envSet)
}

func splitShellSegments(line string) []string {
	replacer := strings.NewReplacer("&&", "\n", "||", "\n", "|", "\n", ";", "\n")
	parts := strings.Split(replacer.Replace(line), "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstShellCommand(segment string) string {
	tokens := strings.Fields(strings.TrimSpace(segment))
	if len(tokens) == 0 {
		return ""
	}
	i := 0
	for i < len(tokens) {
		token := strings.TrimSpace(tokens[i])
		if token == "" {
			i++
			continue
		}
		if strings.Contains(token, "=") && !strings.HasPrefix(token, "=") && !strings.HasPrefix(token, "$") {
			i++
			continue
		}
		if token == "env" || token == "sudo" {
			i++
			continue
		}
		if isShellKeyword(token) || token == "." {
			return ""
		}
		return filepath.Base(token)
	}
	return ""
}

func isShellKeyword(token string) bool {
	switch token {
	case "if", "then", "fi", "for", "do", "done", "while", "case", "esac", "function", "select", "until", "elif", "else", "in":
		return true
	default:
		return false
	}
}

func yamlLookup(doc *yaml.Node, key string) string {
	return yamlLookupNode(yamlMapping(doc, key), "")
}

func yamlMapping(doc *yaml.Node, key string) *yaml.Node {
	if doc == nil || doc.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if strings.EqualFold(strings.TrimSpace(doc.Content[i].Value), key) {
			return doc.Content[i+1]
		}
	}
	return nil
}

func yamlLookupNode(node *yaml.Node, key string) string {
	if node == nil {
		return ""
	}
	if key == "" {
		return strings.TrimSpace(node.Value)
	}
	if node.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if strings.EqualFold(strings.TrimSpace(node.Content[i].Value), key) {
			return strings.TrimSpace(node.Content[i+1].Value)
		}
	}
	return ""
}

func yamlSequenceValues(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		value := strings.TrimSpace(item.Value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func yamlSequenceNode(node *yaml.Node) []*yaml.Node {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]*yaml.Node, 0, len(node.Content))
	for _, item := range node.Content {
		if item != nil {
			out = append(out, item)
		}
	}
	return out
}

func sortedStringKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
