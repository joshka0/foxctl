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

	"github.com/jkatigb/agentctl/internal/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/platform/fsutil"
	"gopkg.in/yaml.v3"
)

const (
	terraformPkgPrefix  = "tf:"
	kubernetesPkgPrefix = "k8s:"
	shellPkgPrefix      = "sh:"
)

var (
	terraformBlockRe = regexp.MustCompile(`(?m)^\s*(resource|data|module|variable|output|provider)\s+"([^"]+)"(?:\s+"([^"]+)")?\s*\{`)
	shellEnvVarRe    = regexp.MustCompile(`\$\{?([A-Z_][A-Z0-9_]*)\}?`)
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
		fileNodeID := addInfraFileNode(ctx, opts, nodes, edges, pkgID, pkgNodeID, fileRelPath, content)
		result.Files++
		result.Symbols += addTerraformConcepts(opts, nodes, edges, fileNodeID, fileRelPath, content)
	}
	return nil
}

func (b *Builder) buildKubernetes(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult) error {
	exclude := []string{
		".git/**",
		"node_modules/**",
		"vendor/**",
		".agentctl/**",
	}
	fileSet := map[string]struct{}{}
	for _, pattern := range []string{"**/*.yaml", "**/*.yml"} {
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
	for _, fileRelPath := range fileList {
		absPath := filepath.Join(opts.RepoRoot, fileRelPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("repoindex: read kubernetes file %s: %w", fileRelPath, err)
		}
		concepts := extractKubernetesConcepts(opts.RepoKey, fileRelPath, content)
		if len(concepts) == 0 {
			continue
		}
		pkgID, pkgNodeID := ensureInfraPackageNode(nodes, kubernetesPkgPrefix, opts.RepoKey, fileRelPath)
		if !seenPackages[pkgID] {
			result.Packages++
			seenPackages[pkgID] = true
		}
		fileNodeID := addInfraFileNode(ctx, opts, nodes, edges, pkgID, pkgNodeID, fileRelPath, content)
		result.Files++
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
	return nil
}

func (b *Builder) buildShell(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult) error {
	exclude := []string{
		".git/**",
		"node_modules/**",
		"vendor/**",
		".agentctl/**",
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
		fileNodeID := addInfraFileNode(ctx, opts, nodes, edges, pkgID, pkgNodeID, fileRelPath, content)
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
	addNode(nodes, Node{
		ID:        pkgNodeID,
		Kind:      NodePackage,
		Pkg:       pkgID,
		Name:      display,
		UpdatedAt: time.Now().UTC(),
	})
	return pkgID, pkgNodeID
}

func addInfraFileNode(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, pkgID, pkgNodeID, fileRelPath string, content []byte) string {
	fileNodeID := FileID(opts.RepoKey, pkgID, fileRelPath)
	fileNode := Node{
		ID:        fileNodeID,
		Kind:      NodeFile,
		Pkg:       pkgID,
		File:      fileRelPath,
		Name:      filepath.Base(fileRelPath),
		SpanStart: 1,
		SpanEnd:   countLines(content),
		Hash:      symbol.ComputeDigest(content),
		UpdatedAt: time.Now().UTC(),
	}
	applyFileSummary(ctx, opts, &fileNode, fileRelPath)
	addNode(nodes, fileNode)
	addEdge(edges, Edge{
		Src:    pkgNodeID,
		Dst:    fileNode.ID,
		Type:   EdgeContains,
		Weight: 1.0,
	})
	return fileNodeID
}

func addTerraformConcepts(opts BuildOptions, nodes map[string]Node, edges map[string]Edge, fileNodeID, fileRelPath string, content []byte) int {
	matches := terraformBlockRe.FindAllStringSubmatchIndex(string(content), -1)
	added := 0
	for _, match := range matches {
		blockType := strings.TrimSpace(string(content[match[2]:match[3]]))
		typeName := strings.TrimSpace(string(content[match[4]:match[5]]))
		name := ""
		if match[6] >= 0 && match[7] >= 0 {
			name = strings.TrimSpace(string(content[match[6]:match[7]]))
		}
		line := lineForOffset(content, match[0])
		conceptID, displayName, summary := terraformConceptMeta(blockType, typeName, name)
		node := Node{
			ID:        NamespacedID(opts.RepoKey, ConceptResource+conceptID),
			Kind:      NodeConcept,
			Name:      displayName,
			Summary:   summary,
			SpanStart: line,
			SpanEnd:   line,
			UpdatedAt: time.Now().UTC(),
		}
		meta, _ := json.Marshal(map[string]any{
			"block_type": blockType,
			"type":       typeName,
			"name":       name,
			"file":       fileRelPath,
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
	return added
}

func addShellConcepts(opts BuildOptions, nodes map[string]Node, edges map[string]Edge, fileNodeID, fileRelPath string, content []byte) int {
	commands, envVars := extractShellConcepts(content)
	added := 0
	for _, cmd := range commands {
		node := Node{
			ID:        NamespacedID(opts.RepoKey, ConceptCommand+cmd),
			Kind:      NodeConcept,
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
		resourceKey := strings.ToLower(kind) + ":" + name
		if namespace != "" {
			resourceKey = strings.ToLower(kind) + ":" + namespace + "/" + name
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
