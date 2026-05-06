package repoindex

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"

	docparser "github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex/parser"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/platform/fsutil"
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
)

const (
	goPkgPrefix         = "go:"
	pythonPkgPrefix     = "py:"
	rustPkgPrefix       = "rs:"
	csharpPkgPrefix     = "cs:"
	tsLocalPrefix       = "ts:local:"
	tsNpmPrefix         = "ts:npm:"
	elixirPkgPrefix     = "ex:"
	maxRollupEntries    = 6
	maxRollupSummaryLen = 160
)

// Builder constructs the repo graph index.
type Builder struct {
	store      *Store
	repoRoot   string
	registry   *symbol.ExtractorRegistry
	tsResolver *tsResolver
}

// NewBuilder creates a builder for the given repo index store.
func NewBuilder(store *Store, repoRoot string) *Builder {
	return &Builder{
		store:      store,
		repoRoot:   repoRoot,
		registry:   symbol.DefaultRegistry(),
		tsResolver: newTSResolver(repoRoot),
	}
}

// Build builds and replaces the repo graph index.
//
// Index:
// - Purpose: Build repo graph nodes/edges for enabled languages and persist to store
// - Flow: validate opts → buildGo/TS/Elixir → rollups → comment edges → replace store → set meta
// - SideEffects: reads workspace files; runs language tooling; writes repoindex SQLite
// - FailureModes: package load errors, file read errors, store write errors
// - Related: buildGo, buildTS, buildElixir, applyCommentEdges, Store.ReplaceAll
// - Keywords: repo_index_build, repoindex, nodes, edges, repo_key, schema_version, buildGo, buildTS, buildElixir, ReplaceAll
func (b *Builder) Build(ctx context.Context, opts BuildOptions) (BuildResult, error) {
	result := BuildResult{}
	start := time.Now()
	report := func(phase, message string) {
		if opts.Progress == nil {
			return
		}
		opts.Progress(BuildProgress{
			Phase:     phase,
			Message:   message,
			ElapsedMs: time.Since(start).Milliseconds(),
			Time:      time.Now().UTC(),
			Result:    result,
		})
	}
	if opts.RepoRoot == "" {
		opts.RepoRoot = b.repoRoot
	}
	if opts.RepoRoot == "" {
		return result, fmt.Errorf("repoindex: repo root is required")
	}
	if opts.RepoKey == "" {
		opts.RepoKey = repoKey(opts.RepoRoot)
	}
	if len(opts.Patterns) == 0 {
		opts.Patterns = []string{"./..."}
	}
	if !opts.IncludeGo && !opts.IncludePython && !opts.IncludeRust && !opts.IncludeCSharp && !opts.IncludeTypescript && !opts.IncludeElixir && !opts.IncludeTerraform && !opts.IncludeKubernetes && !opts.IncludeShell {
		return result, fmt.Errorf("repoindex: at least one language or config family must be enabled")
	}
	report("start", "initialized repoindex build")

	nodes := make(map[string]Node)
	edges := make(map[string]Edge)
	var pending []pendingNameEdge
	var pendingFileSymbols []pendingFileSymbolEdge
	var locators []LocatorEntry

	if opts.IncludeGo {
		report("go", "building Go package graph")
		if err := b.buildGo(ctx, opts, nodes, edges, &result, &locators); err != nil {
			return result, err
		}
		report("go", "finished Go package graph")
	}
	if opts.IncludePython {
		report("python", "building Python graph")
		if err := b.buildPython(ctx, opts, nodes, edges, &result, &pending, &locators); err != nil {
			return result, err
		}
		report("python", "finished Python graph")
	}
	if opts.IncludeRust {
		report("rust", "building Rust graph")
		if err := b.buildRust(ctx, opts, nodes, edges, &result, &pending, &locators); err != nil {
			return result, err
		}
		report("rust", "finished Rust graph")
	}
	if opts.IncludeCSharp {
		report("csharp", "building C# graph")
		if err := b.buildCSharp(ctx, opts, nodes, edges, &result, &pending, &locators); err != nil {
			return result, err
		}
		report("csharp", "finished C# graph")
	}
	if opts.IncludeTypescript {
		report("typescript", "building TypeScript graph")
		if err := b.buildTS(ctx, opts, nodes, edges, &result, &pending, &pendingFileSymbols, &locators); err != nil {
			return result, err
		}
		report("typescript", "finished TypeScript graph")
	}
	if opts.IncludeElixir {
		report("elixir", "building Elixir graph")
		if err := b.buildElixir(ctx, opts, nodes, edges, &result, &pending, &locators); err != nil {
			return result, err
		}
		report("elixir", "finished Elixir graph")
	}
	if opts.IncludeTerraform {
		report("terraform", "building Terraform graph")
		if err := b.buildTerraform(ctx, opts, nodes, edges, &result); err != nil {
			return result, err
		}
		report("terraform", "finished Terraform graph")
	}
	if opts.IncludeKubernetes {
		report("kubernetes", "building Kubernetes graph")
		if err := b.buildKubernetes(ctx, opts, nodes, edges, &result); err != nil {
			return result, err
		}
		report("kubernetes", "finished Kubernetes graph")
	}
	if opts.IncludeShell {
		report("shell", "building shell graph")
		if err := b.buildShell(ctx, opts, nodes, edges, &result); err != nil {
			return result, err
		}
		report("shell", "finished shell graph")
	}

	report("resolve", "resolving pending graph edges")
	applyPendingNameEdges(nodes, edges, pending)
	applyPendingFileSymbolEdges(nodes, edges, pendingFileSymbols)

	report("rollups", "building package and repo rollups")
	localPackages := collectLocalPackages(nodes, opts.RepoKey)
	applyPackageRollups(nodes, localPackages)
	applyRepoRollup(nodes, edges, opts.RepoKey, opts.RepoRoot, localPackages)
	applyCommentEdges(nodes, edges, opts.RepoKey)
	if opts.IncludeSemanticAnchors {
		report("semantic_anchors", "building semantic anchor graph edges")
		if err := applySemanticAnchorEdges(ctx, opts, nodes, edges); err != nil {
			return result, err
		}
		report("semantic_anchors", "finished semantic anchor graph edges")
	}
	if opts.IncludeCoChange {
		report("cochange", "building empirical co-change graph edges")
		if err := applyCoChangeEdges(ctx, opts, nodes, edges); err != nil {
			return result, err
		}
		report("cochange", "finished empirical co-change graph edges")
	}

	result.Nodes = len(nodes)
	result.Edges = len(edges)

	nodeList := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		nodeList = append(nodeList, node)
	}
	edgeList := make([]Edge, 0, len(edges))
	for _, edge := range edges {
		edgeList = append(edgeList, edge)
	}

	if opts.DryRun {
		report("done", "dry run complete")
		return result, nil
	}

	report("persist", "replacing repoindex store")
	if err := b.store.ReplaceAll(ctx, nodeList, edgeList); err != nil {
		return result, err
	}
	report("persist", "upserting symbol locators")
	for _, loc := range locators {
		if err := b.store.UpsertLocator(ctx, loc); err != nil {
			return result, fmt.Errorf("repoindex: upsert locator: %w", err)
		}
	}

	snapshot := ResolveGitSnapshot(ctx, opts.RepoRoot)
	report("persist", "updating file state")
	if err := b.store.ReplaceFileStates(ctx, buildFileStates(opts.RepoRoot, nodeList, snapshot.HeadSHA)); err != nil {
		return result, fmt.Errorf("repoindex: replace file state: %w", err)
	}

	meta := IndexMetaFromGitSnapshot(IndexMeta{
		RepoRoot:      opts.RepoRoot,
		SchemaVersion: schemaVersion,
		IndexedAt:     time.Now().UTC(),
		Languages:     buildLanguages(opts),
	}, snapshot)
	if err := b.store.SetMeta(ctx, meta); err != nil {
		return result, err
	}

	report("done", "repoindex build complete")
	return result, nil
}

func buildFileStates(repoRoot string, nodes []Node, headSHA string) []FileState {
	seen := map[string]struct{}{}
	states := make([]FileState, 0)
	for _, node := range nodes {
		if node.Kind != NodeFile {
			continue
		}
		pathValue := filepath.ToSlash(strings.TrimSpace(node.File))
		if pathValue == "" {
			continue
		}
		if _, ok := seen[pathValue]; ok {
			continue
		}
		seen[pathValue] = struct{}{}
		state, ok := fileStateForPath(repoRoot, pathValue, headSHA)
		if ok {
			states = append(states, state)
		}
	}
	sort.SliceStable(states, func(i, j int) bool {
		return states[i].Path < states[j].Path
	})
	return states
}

func fileStateForPath(repoRoot, pathValue, headSHA string) (FileState, bool) {
	pathValue = filepath.ToSlash(strings.TrimSpace(pathValue))
	if pathValue == "" {
		return FileState{}, false
	}
	fullPath := filepath.Join(repoRoot, filepath.FromSlash(pathValue))
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		return FileState{}, false
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return FileState{}, false
	}
	return FileState{
		Path:            pathValue,
		ContentHash:     symbol.ComputeDigest(content),
		SizeBytes:       info.Size(),
		MTimeUnix:       info.ModTime().Unix(),
		Language:        languageForPath(pathValue),
		IndexedAt:       time.Now().UTC(),
		LastSeenHeadSHA: headSHA,
	}, true
}

func reportBuildProgress(opts BuildOptions, phase, message string, result BuildResult) {
	if opts.Progress == nil {
		return
	}
	opts.Progress(BuildProgress{
		Phase:   phase,
		Message: message,
		Time:    time.Now().UTC(),
		Result:  result,
	})
}

func (b *Builder) buildGo(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult, locators *[]LocatorEntry) error {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     opts.RepoRoot,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Tests:   opts.IncludeTests,
	}
	reportBuildProgress(opts, "go_load_packages", "loading Go packages", *result)
	pkgs, err := packages.Load(cfg, opts.Patterns...)
	if err != nil {
		return fmt.Errorf("repoindex: load go packages: %w", err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		return fmt.Errorf("repoindex: go packages load errors")
	}
	reportBuildProgress(opts, "go_load_packages", fmt.Sprintf("loaded %d Go packages", len(pkgs)), *result)

	for _, pkg := range pkgs {
		pkgID := goPackageID(pkg.PkgPath)
		pkgNodeID := PackageID(opts.RepoKey, pkgID)
		addNode(nodes, Node{
			ID:        pkgNodeID,
			Kind:      NodePackage,
			Pkg:       pkgID,
			Name:      pkg.PkgPath,
			UpdatedAt: time.Now().UTC(),
		})
		result.Packages++

		for _, imp := range pkg.Imports {
			impID := goPackageID(imp.PkgPath)
			impNodeID := PackageID(opts.RepoKey, impID)
			addNode(nodes, Node{
				ID:        impNodeID,
				Kind:      NodePackage,
				Pkg:       impID,
				Name:      imp.PkgPath,
				UpdatedAt: time.Now().UTC(),
			})
			addEdge(edges, Edge{
				Src:    pkgNodeID,
				Dst:    impNodeID,
				Type:   EdgeImports,
				Weight: 1.0,
				Meta:   importMeta(imp.PkgPath),
			})
		}

		for _, filePath := range pkg.GoFiles {
			if !opts.IncludeTests && fsutil.IsTestFile(filePath) {
				continue
			}
			fileRelPath, ok := relPath(opts.RepoRoot, filePath)
			if !ok {
				continue
			}
			content, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("repoindex: read go file %s: %w", filePath, err)
			}
			lineCount := countLines(content)
			fileNodeID := FileID(opts.RepoKey, pkgID, fileRelPath)
			fileNode := Node{
				ID:        fileNodeID,
				Kind:      NodeFile,
				Pkg:       pkgID,
				File:      fileRelPath,
				Name:      filepath.Base(fileRelPath),
				SpanStart: 1,
				SpanEnd:   lineCount,
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
			result.Files++

			extractor := b.registry.Get("go")
			if extractor == nil {
				continue
			}
			syms, err := extractor.Extract(ctx, fileRelPath, content)
			if err != nil {
				return fmt.Errorf("repoindex: extract go symbols %s: %w", fileRelPath, err)
			}
			for _, sym := range syms {
				sym.Key = goSymbolKeyFromName(sym.Name, sym.FilePath)
				addSymbol(ctx, opts, nodes, edges, pkgID, fileNode.ID, sym, locators)
				result.Symbols++
			}
		}
	}

	if err := b.addGoReferenceEdges(opts, pkgs, nodes, edges); err != nil {
		return err
	}

	return nil
}

func (b *Builder) buildTS(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult, pending *[]pendingNameEdge, pendingFileSymbols *[]pendingFileSymbolEdge, locators *[]LocatorEntry) error {
	extractor := b.registry.Get("typescript")
	if extractor == nil {
		return fmt.Errorf("repoindex: no typescript extractor registered")
	}

	exclude := []string{
		"node_modules/**",
		"vendor/**",
		"dist/**",
		"build/**",
		"out/**",
		"coverage/**",
		".pnpm-patches/**",
		".turbo/**",
		".next/**",
		".expo/**",
		"tmp/**",
		"temp/**",
		".git/**",
		"**/*.d.ts",
	}

	patterns := []string{"**/*.ts", "**/*.tsx"}
	files := make(map[string]bool)
	for _, pattern := range patterns {
		paths, err := fsutil.FindFilesRespectingGitignore(opts.RepoRoot, pattern, exclude)
		if err != nil {
			return fmt.Errorf("repoindex: find typescript files: %w", err)
		}
		for _, path := range paths {
			files[path] = true
		}
	}

	var fileList []string
	for path := range files {
		fileList = append(fileList, path)
	}
	sort.Strings(fileList)

	for _, fileRelPath := range fileList {
		if !opts.IncludeTests && fsutil.IsTestFile(fileRelPath) {
			continue
		}
		absPath := filepath.Join(opts.RepoRoot, fileRelPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("repoindex: read typescript file %s: %w", fileRelPath, err)
		}

		moduleRoot, err := b.tsResolver.ModuleRoot(absPath)
		if err != nil {
			return fmt.Errorf("repoindex: module root %s: %w", fileRelPath, err)
		}
		moduleRelPath, ok := relPath(opts.RepoRoot, moduleRoot)
		if !ok {
			moduleRelPath = "."
		}
		pkgID := tsLocalPrefix + moduleRelPath
		pkgNodeID := PackageID(opts.RepoKey, pkgID)

		addNode(nodes, Node{
			ID:        pkgNodeID,
			Kind:      NodePackage,
			Pkg:       pkgID,
			Name:      moduleRelPath,
			UpdatedAt: time.Now().UTC(),
		})

		lineCount := countLines(content)
		fileNodeID := FileID(opts.RepoKey, pkgID, fileRelPath)
		fileNode := Node{
			ID:        fileNodeID,
			Kind:      NodeFile,
			Pkg:       pkgID,
			File:      fileRelPath,
			Name:      filepath.Base(fileRelPath),
			SpanStart: 1,
			SpanEnd:   lineCount,
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
		result.Files++

		syms, err := extractor.Extract(ctx, fileRelPath, content)
		if err != nil {
			return fmt.Errorf("repoindex: extract typescript symbols %s: %w", fileRelPath, err)
		}
		for _, sym := range syms {
			sym.Key = symbol.TSSymbolKey(sym.Name, isExportedSymbol(sym), filepath.Base(fileRelPath))
			srcID := SymbolID(opts.RepoKey, pkgID, sym.EffectiveID())
			addSymbol(ctx, opts, nodes, edges, pkgID, fileNode.ID, sym, locators)
			result.Symbols++

			callNames, err := extractor.ExtractCalls(ctx, sym, content)
			if err == nil && len(callNames) > 0 && pending != nil {
				callNames = capList(callNames, 50)
				for _, callName := range callNames {
					callName = strings.TrimSpace(callName)
					if callName == "" {
						continue
					}
					*pending = append(*pending, pendingNameEdge{
						SrcID:      srcID,
						SrcPkg:     pkgID,
						SrcFile:    fileRelPath,
						TargetName: callName,
						Type:       EdgeCalls,
						Weight:     0.9, // heuristic for TS (not type-checked)
					})
				}
			}

			refNames, err := symbol.ExtractTypeScriptReferences(ctx, sym, content)
			if err == nil && len(refNames) > 0 && pending != nil {
				refNames = capList(refNames, 100)
				for _, refName := range refNames {
					refName = strings.TrimSpace(refName)
					if refName == "" {
						continue
					}
					*pending = append(*pending, pendingNameEdge{
						SrcID:      srcID,
						SrcPkg:     pkgID,
						SrcFile:    fileRelPath,
						TargetName: refName,
						Type:       EdgeRefersTo,
						Weight:     0.8,
					})
				}
			}
		}

		imports := extractTSImports(fileRelPath, content)
		sort.Strings(imports)
		for _, imp := range imports {
			impPkg := b.tsResolver.ResolveImportPackage(absPath, imp)
			if impPkg == "" {
				continue
			}
			impNodeID := PackageID(opts.RepoKey, impPkg)
			addNode(nodes, Node{
				ID:        impNodeID,
				Kind:      NodePackage,
				Pkg:       impPkg,
				Name:      strings.TrimPrefix(strings.TrimPrefix(impPkg, tsLocalPrefix), tsNpmPrefix),
				UpdatedAt: time.Now().UTC(),
			})
			addEdge(edges, Edge{
				Src:    pkgNodeID,
				Dst:    impNodeID,
				Type:   EdgeImports,
				Weight: 0.7,
				Meta:   importMeta(imp),
			})

			if targetFile := b.tsResolver.ResolveImportFile(absPath, imp); targetFile != "" {
				targetRelPath, ok := relPath(opts.RepoRoot, targetFile)
				if ok && files[targetRelPath] {
					targetPkgID := b.tsResolver.packageForFile(targetFile)
					if targetPkgID != "" {
						targetFileNodeID := FileID(opts.RepoKey, targetPkgID, targetRelPath)
						addEdge(edges, Edge{
							Src:    fileNode.ID,
							Dst:    targetFileNodeID,
							Type:   EdgeImports,
							Weight: 0.85,
							Meta:   importMeta(imp),
						})
					}
				}
			}
		}

		if pendingFileSymbols != nil {
			bindings := extractTSImportBindings(fileRelPath, content)
			for _, binding := range bindings {
				targetFile := b.tsResolver.ResolveImportFile(absPath, binding.ImportPath)
				if targetFile == "" {
					continue
				}
				targetRelPath, ok := relPath(opts.RepoRoot, targetFile)
				if !ok || !files[targetRelPath] {
					continue
				}
				targetPkgID := b.tsResolver.packageForFile(targetFile)
				if targetPkgID == "" {
					continue
				}
				*pendingFileSymbols = append(*pendingFileSymbols, pendingFileSymbolEdge{
					SrcID:      fileNode.ID,
					TargetPkg:  targetPkgID,
					TargetFile: targetRelPath,
					TargetName: binding.TargetName,
					Type:       EdgeUsesSymbol,
					Weight:     0.95,
				})
			}
		}
	}

	return nil
}

func (b *Builder) buildPython(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult, pending *[]pendingNameEdge, locators *[]LocatorEntry) error {
	extractor := b.registry.Get("python")
	if extractor == nil {
		return fmt.Errorf("repoindex: no python extractor registered")
	}

	exclude := []string{
		"__pycache__/**",
		".pytest_cache/**",
		".venv/**",
		"venv/**",
		"node_modules/**",
		"vendor/**",
		"dist/**",
		"build/**",
		".git/**",
	}

	files, err := fsutil.FindFilesRespectingGitignore(opts.RepoRoot, "**/*.py", exclude)
	if err != nil {
		return fmt.Errorf("repoindex: find python files: %w", err)
	}
	sort.Strings(files)

	seenPackages := make(map[string]bool)
	for _, fileRelPath := range files {
		if !opts.IncludeTests && fsutil.IsTestFile(fileRelPath) {
			continue
		}
		absPath := filepath.Join(opts.RepoRoot, fileRelPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("repoindex: read python file %s: %w", fileRelPath, err)
		}

		pkgID := pythonModuleID(fileRelPath)
		pkgNodeID := PackageID(opts.RepoKey, pkgID)
		addNode(nodes, Node{
			ID:        pkgNodeID,
			Kind:      NodePackage,
			Pkg:       pkgID,
			Name:      strings.TrimPrefix(pkgID, pythonPkgPrefix),
			UpdatedAt: time.Now().UTC(),
		})
		if !seenPackages[pkgID] {
			result.Packages++
			seenPackages[pkgID] = true
		}

		lineCount := countLines(content)
		fileNodeID := FileID(opts.RepoKey, pkgID, fileRelPath)
		fileNode := Node{
			ID:        fileNodeID,
			Kind:      NodeFile,
			Pkg:       pkgID,
			File:      fileRelPath,
			Name:      filepath.Base(fileRelPath),
			SpanStart: 1,
			SpanEnd:   lineCount,
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
		result.Files++

		syms, err := extractor.Extract(ctx, fileRelPath, content)
		if err != nil {
			return fmt.Errorf("repoindex: extract python symbols %s: %w", fileRelPath, err)
		}
		for _, sym := range syms {
			sym.Key = symbol.PythonSymbolKey(sym.Name)
			srcID := SymbolID(opts.RepoKey, pkgID, sym.EffectiveID())
			addSymbol(ctx, opts, nodes, edges, pkgID, fileNode.ID, sym, locators)
			result.Symbols++

			callNames, err := extractor.ExtractCalls(ctx, sym, content)
			if err == nil && len(callNames) > 0 && pending != nil {
				callNames = capList(callNames, 50)
				for _, callName := range callNames {
					callName = strings.TrimSpace(callName)
					if callName == "" {
						continue
					}
					*pending = append(*pending, pendingNameEdge{
						SrcID:      srcID,
						SrcPkg:     pkgID,
						SrcFile:    fileRelPath,
						TargetName: callName,
						Type:       EdgeCalls,
						Weight:     0.9,
					})
				}
			}
		}
	}

	return nil
}

func (b *Builder) buildRust(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult, pending *[]pendingNameEdge, locators *[]LocatorEntry) error {
	extractor := b.registry.Get("rust")
	if extractor == nil {
		return fmt.Errorf("repoindex: no rust extractor registered")
	}

	exclude := []string{
		"target/**",
		".cargo/**",
		"node_modules/**",
		"vendor/**",
		"dist/**",
		"build/**",
		".git/**",
	}

	files, err := fsutil.FindFilesRespectingGitignore(opts.RepoRoot, "**/*.rs", exclude)
	if err != nil {
		return fmt.Errorf("repoindex: find rust files: %w", err)
	}
	sort.Strings(files)

	seenPackages := make(map[string]bool)
	for _, fileRelPath := range files {
		if !opts.IncludeTests && fsutil.IsTestFile(fileRelPath) {
			continue
		}
		absPath := filepath.Join(opts.RepoRoot, fileRelPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("repoindex: read rust file %s: %w", fileRelPath, err)
		}

		pkgID := rustModuleID(fileRelPath)
		pkgNodeID := PackageID(opts.RepoKey, pkgID)
		addNode(nodes, Node{
			ID:        pkgNodeID,
			Kind:      NodePackage,
			Pkg:       pkgID,
			Name:      strings.TrimPrefix(pkgID, rustPkgPrefix),
			UpdatedAt: time.Now().UTC(),
		})
		if !seenPackages[pkgID] {
			result.Packages++
			seenPackages[pkgID] = true
		}

		lineCount := countLines(content)
		fileNodeID := FileID(opts.RepoKey, pkgID, fileRelPath)
		fileNode := Node{
			ID:        fileNodeID,
			Kind:      NodeFile,
			Pkg:       pkgID,
			File:      fileRelPath,
			Name:      filepath.Base(fileRelPath),
			SpanStart: 1,
			SpanEnd:   lineCount,
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
		result.Files++

		syms, err := extractor.Extract(ctx, fileRelPath, content)
		if err != nil {
			return fmt.Errorf("repoindex: extract rust symbols %s: %w", fileRelPath, err)
		}
		for _, sym := range syms {
			sym.Key = symbol.RustSymbolKey(sym.Name, isExportedSymbol(sym), filepath.Base(fileRelPath))
			srcID := SymbolID(opts.RepoKey, pkgID, sym.EffectiveID())
			addSymbol(ctx, opts, nodes, edges, pkgID, fileNode.ID, sym, locators)
			result.Symbols++

			callNames, err := extractor.ExtractCalls(ctx, sym, content)
			if err == nil && len(callNames) > 0 && pending != nil {
				callNames = capList(callNames, 50)
				for _, callName := range callNames {
					callName = strings.TrimSpace(callName)
					if callName == "" {
						continue
					}
					*pending = append(*pending, pendingNameEdge{
						SrcID:      srcID,
						SrcPkg:     pkgID,
						SrcFile:    fileRelPath,
						TargetName: callName,
						Type:       EdgeCalls,
						Weight:     0.9,
					})
				}
			}
		}
	}

	return nil
}

func (b *Builder) buildCSharp(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult, pending *[]pendingNameEdge, locators *[]LocatorEntry) error {
	extractor := b.registry.Get("csharp")
	if extractor == nil {
		return fmt.Errorf("repoindex: no csharp extractor registered")
	}

	exclude := []string{
		"bin/**",
		"obj/**",
		"node_modules/**",
		"vendor/**",
		"dist/**",
		"build/**",
		".git/**",
		"**/*.g.cs",
		"**/*.designer.cs",
	}

	files, err := fsutil.FindFilesRespectingGitignore(opts.RepoRoot, "**/*.cs", exclude)
	if err != nil {
		return fmt.Errorf("repoindex: find csharp files: %w", err)
	}
	sort.Strings(files)

	seenPackages := make(map[string]bool)
	projectGraph := loadCSharpProjectGraph(opts.RepoRoot, files)
	indexedFiles := make([]csharpIndexedFile, 0, len(files))
	for _, fileRelPath := range files {
		if !opts.IncludeTests && fsutil.IsTestFile(fileRelPath) {
			continue
		}
		absPath := filepath.Join(opts.RepoRoot, fileRelPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("repoindex: read csharp file %s: %w", fileRelPath, err)
		}

		pkgID := csharpPackageID(fileRelPath, content)
		pkgNodeID := PackageID(opts.RepoKey, pkgID)
		addNode(nodes, Node{
			ID:        pkgNodeID,
			Kind:      NodePackage,
			Pkg:       pkgID,
			Name:      strings.TrimPrefix(pkgID, csharpPkgPrefix),
			UpdatedAt: time.Now().UTC(),
		})
		if !seenPackages[pkgID] {
			result.Packages++
			seenPackages[pkgID] = true
		}

		lineCount := countLines(content)
		fileNodeID := FileID(opts.RepoKey, pkgID, fileRelPath)
		fileNode := Node{
			ID:        fileNodeID,
			Kind:      NodeFile,
			Pkg:       pkgID,
			File:      fileRelPath,
			Name:      filepath.Base(fileRelPath),
			SpanStart: 1,
			SpanEnd:   lineCount,
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
		result.Files++
		indexedFiles = append(indexedFiles, csharpIndexedFile{
			RelPath:    fileRelPath,
			PkgID:      pkgID,
			FileNodeID: fileNode.ID,
			Project:    projectGraph.ProjectForFile(fileRelPath),
			Usings:     extractCSharpUsings(content),
		})

		syms, err := extractor.Extract(ctx, fileRelPath, content)
		if err != nil {
			return fmt.Errorf("repoindex: extract csharp symbols %s: %w", fileRelPath, err)
		}
		for _, sym := range syms {
			sym.Key = symbol.CSharpSymbolKey(sym.Name, isExportedSymbol(sym), filepath.Base(fileRelPath))
			srcID := SymbolID(opts.RepoKey, pkgID, sym.EffectiveID())
			addSymbol(ctx, opts, nodes, edges, pkgID, fileNode.ID, sym, locators)
			result.Symbols++

			callNames, err := extractor.ExtractCalls(ctx, sym, content)
			if err == nil && len(callNames) > 0 && pending != nil {
				callNames = capList(callNames, 50)
				for _, callName := range callNames {
					callName = strings.TrimSpace(callName)
					if callName == "" {
						continue
					}
					*pending = append(*pending, pendingNameEdge{
						SrcID:      srcID,
						SrcPkg:     pkgID,
						SrcFile:    fileRelPath,
						TargetName: callName,
						Type:       EdgeCalls,
						Weight:     0.85,
					})
				}
			}
		}
	}

	applyCSharpRelations(nodes, edges, opts.RepoKey, projectGraph, indexedFiles)

	return nil
}

func (b *Builder) buildElixir(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult, pending *[]pendingNameEdge, locators *[]LocatorEntry) error {
	extractor := b.registry.Get("elixir")
	if extractor == nil {
		return fmt.Errorf("repoindex: no elixir extractor registered")
	}

	exclude := []string{
		"_build/**",
		"deps/**",
		"node_modules/**",
		".git/**",
		"cover/**",
		"priv/static/**",
	}

	patterns := []string{"**/*.ex", "**/*.exs"}
	files := make(map[string]bool)
	for _, pattern := range patterns {
		paths, err := fsutil.FindFilesRespectingGitignore(opts.RepoRoot, pattern, exclude)
		if err != nil {
			return fmt.Errorf("repoindex: find elixir files: %w", err)
		}
		for _, path := range paths {
			files[path] = true
		}
	}

	var fileList []string
	for path := range files {
		fileList = append(fileList, path)
	}
	sort.Strings(fileList)

	seenPackages := make(map[string]bool)

	for _, fileRelPath := range fileList {
		if !opts.IncludeTests && fsutil.IsTestFile(fileRelPath) {
			continue
		}
		absPath := filepath.Join(opts.RepoRoot, fileRelPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("repoindex: read elixir file %s: %w", fileRelPath, err)
		}

		// Determine package from directory structure or module name
		pkgID := elixirPackageID(fileRelPath)
		pkgNodeID := PackageID(opts.RepoKey, pkgID)

		addNode(nodes, Node{
			ID:        pkgNodeID,
			Kind:      NodePackage,
			Pkg:       pkgID,
			Name:      strings.TrimPrefix(pkgID, elixirPkgPrefix),
			UpdatedAt: time.Now().UTC(),
		})
		if !seenPackages[pkgID] {
			result.Packages++
			seenPackages[pkgID] = true
		}

		lineCount := countLines(content)
		fileNodeID := FileID(opts.RepoKey, pkgID, fileRelPath)
		fileNode := Node{
			ID:        fileNodeID,
			Kind:      NodeFile,
			Pkg:       pkgID,
			File:      fileRelPath,
			Name:      filepath.Base(fileRelPath),
			SpanStart: 1,
			SpanEnd:   lineCount,
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
		result.Files++

		syms, err := extractor.Extract(ctx, fileRelPath, content)
		if err != nil {
			return fmt.Errorf("repoindex: extract elixir symbols %s: %w", fileRelPath, err)
		}
		for _, sym := range syms {
			sym.Key = symbol.ElixirSymbolKey(sym.Name)
			srcID := SymbolID(opts.RepoKey, pkgID, sym.EffectiveID())
			addSymbol(ctx, opts, nodes, edges, pkgID, fileNode.ID, sym, locators)
			result.Symbols++

			// Elixir call graph is mostly "module references" (alias/use/remote calls).
			refNames, err := extractor.ExtractCalls(ctx, sym, content)
			if err == nil && len(refNames) > 0 && pending != nil {
				refNames = capList(refNames, 50)
				for _, refName := range refNames {
					refName = strings.TrimSpace(refName)
					if refName == "" {
						continue
					}
					*pending = append(*pending, pendingNameEdge{
						SrcID:      srcID,
						SrcPkg:     pkgID,
						SrcFile:    fileRelPath,
						TargetName: refName,
						Type:       EdgeRefersTo,
						Weight:     0.85, // heuristic for Elixir refs
					})
				}
			}
		}

		relations := extractElixirFileRelations(content)
		if pending != nil && len(relations) > 0 {
			for _, relation := range relations {
				*pending = append(*pending, pendingNameEdge{
					SrcID:      fileNode.ID,
					SrcPkg:     pkgID,
					SrcFile:    fileRelPath,
					TargetName: relation.TargetName,
					Type:       relation.Type,
					Weight:     relation.Weight,
				})
			}
		}
	}

	return nil
}

// elixirPackageID determines the package ID for an Elixir file.
// Uses the directory path as the package identifier.
func elixirPackageID(filePath string) string {
	dir := filepath.Dir(filePath)
	if dir == "." || dir == "" {
		return elixirPkgPrefix + "root"
	}
	return elixirPkgPrefix + filepath.ToSlash(dir)
}

func (b *Builder) addGoReferenceEdges(opts BuildOptions, pkgs []*packages.Package, nodes map[string]Node, edges map[string]Edge) error {
	if len(pkgs) == 0 {
		return nil
	}
	for _, pkg := range pkgs {
		if pkg == nil || pkg.TypesInfo == nil || pkg.Fset == nil {
			continue
		}
		pkgID := goPackageID(pkg.PkgPath)
		for _, file := range pkg.Syntax {
			if file == nil {
				continue
			}
			filePos := pkg.Fset.Position(file.Pos())
			if filePos.Filename == "" {
				continue
			}
			fileRelPath, ok := relPath(opts.RepoRoot, filePos.Filename)
			if !ok {
				continue
			}
			fileNodeID := FileID(opts.RepoKey, pkgID, fileRelPath)
			if _, ok := nodes[fileNodeID]; !ok {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				name := goFuncDeclSymbolName(fn)
				if name == "" {
					continue
				}
				srcID := SymbolID(opts.RepoKey, pkgID, goSymbolKeyFromName(name, fileRelPath).String())
				if _, ok := nodes[srcID]; !ok {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch expr := n.(type) {
					case *ast.CallExpr:
						dstID := goCallTargetNodeID(opts, pkg, pkg.TypesInfo, expr, nodes)
						if dstID != "" {
							addEdge(edges, Edge{
								Src:    srcID,
								Dst:    dstID,
								Type:   EdgeCalls,
								Weight: 1.0,
							})
						}
					case *ast.Ident:
						dstID := goObjectNodeID(opts, pkg, pkg.TypesInfo.Uses[expr], nodes)
						if dstID != "" {
							addEdge(edges, Edge{
								Src:    srcID,
								Dst:    dstID,
								Type:   EdgeRefersTo,
								Weight: 1.0,
							})
						}
					case *ast.SelectorExpr:
						dstID := goObjectNodeID(opts, pkg, goSelectorObject(pkg.TypesInfo, expr), nodes)
						if dstID != "" {
							addEdge(edges, Edge{
								Src:    srcID,
								Dst:    dstID,
								Type:   EdgeRefersTo,
								Weight: 1.0,
							})
						}
					}
					return true
				})
			}
			addGoFileRootReferenceEdges(opts, pkg, file, fileNodeID, nodes, edges)
		}
	}
	return nil
}

func addGoFileRootReferenceEdges(opts BuildOptions, pkg *packages.Package, file *ast.File, fileNodeID string, nodes map[string]Node, edges map[string]Edge) {
	if pkg == nil || pkg.TypesInfo == nil || file == nil || fileNodeID == "" {
		return
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, value := range valueSpec.Values {
				if value == nil {
					continue
				}
				ast.Inspect(value, func(n ast.Node) bool {
					switch expr := n.(type) {
					case *ast.Ident:
						dstID := goObjectNodeID(opts, pkg, pkg.TypesInfo.Uses[expr], nodes)
						if dstID != "" {
							addEdge(edges, Edge{
								Src:    fileNodeID,
								Dst:    dstID,
								Type:   EdgeRefersTo,
								Weight: 1.0,
							})
						}
					case *ast.SelectorExpr:
						dstID := goObjectNodeID(opts, pkg, goSelectorObject(pkg.TypesInfo, expr), nodes)
						if dstID != "" {
							addEdge(edges, Edge{
								Src:    fileNodeID,
								Dst:    dstID,
								Type:   EdgeRefersTo,
								Weight: 1.0,
							})
						}
					}
					return true
				})
			}
		}
	}
}

func goFuncDeclSymbolName(fn *ast.FuncDecl) string {
	if fn == nil || fn.Name == nil {
		return ""
	}
	name := fn.Name.Name
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return name
	}
	recvName := goRecvTypeNameFromExpr(fn.Recv.List[0].Type)
	if recvName == "" {
		return name
	}
	return recvName + "." + name
}

func goRecvTypeNameFromExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return goRecvTypeNameFromExpr(t.X)
	default:
		return ""
	}
}

func goCallTargetNodeID(opts BuildOptions, pkg *packages.Package, info *types.Info, call *ast.CallExpr, nodes map[string]Node) string {
	if call == nil || info == nil {
		return ""
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return goObjectNodeID(opts, pkg, info.Uses[fun], nodes)
	case *ast.SelectorExpr:
		return goObjectNodeID(opts, pkg, goSelectorObject(info, fun), nodes)
	default:
		return ""
	}
}

func goSelectorObject(info *types.Info, sel *ast.SelectorExpr) types.Object {
	if info == nil || sel == nil {
		return nil
	}
	if info.Selections != nil {
		if selection, ok := info.Selections[sel]; ok && selection != nil {
			return selection.Obj()
		}
	}
	if info.Uses != nil {
		if obj, ok := info.Uses[sel.Sel]; ok {
			return obj
		}
	}
	return nil
}

func goObjectNodeID(opts BuildOptions, pkg *packages.Package, obj types.Object, nodes map[string]Node) string {
	if pkg == nil || pkg.Fset == nil || obj == nil || obj.Pkg() == nil {
		return ""
	}
	if !goObjectIsPackageSymbol(obj) {
		return ""
	}
	pos := pkg.Fset.Position(obj.Pos())
	if pos.Filename == "" {
		return ""
	}
	fileRelPath, ok := relPath(opts.RepoRoot, pos.Filename)
	if !ok {
		return ""
	}
	name := goObjectSymbolName(obj)
	if name == "" {
		return ""
	}
	pkgPath := obj.Pkg().Path()
	if pkgPath == "" {
		return ""
	}
	nodeID := SymbolID(opts.RepoKey, goPackageID(pkgPath), goSymbolKeyFromName(name, fileRelPath).String())
	if _, ok := nodes[nodeID]; !ok {
		return ""
	}
	return nodeID
}

// goSymbolKeyFromName returns a SymbolKey for a Go symbol given its name and file path.
func goSymbolKeyFromName(name, fileRelPath string) symbol.SymbolKey {
	name = strings.TrimSpace(name)
	if name == "init" {
		return symbol.GoInitSymbolKey(filepath.Base(fileRelPath))
	}
	r, _ := utf8.DecodeRuneInString(name)
	if unicode.IsUpper(r) {
		return symbol.GoSymbolKey(name)
	}
	return symbol.GoNonExportedSymbolKey(name, filepath.Base(fileRelPath))
}

func goObjectIsPackageSymbol(obj types.Object) bool {
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	switch o := obj.(type) {
	case *types.Func:
		if sig, ok := o.Type().(*types.Signature); ok && sig.Recv() != nil {
			return true
		}
		return obj.Parent() == obj.Pkg().Scope()
	case *types.TypeName, *types.Var, *types.Const:
		return obj.Parent() == obj.Pkg().Scope()
	default:
		return false
	}
}

func goObjectSymbolName(obj types.Object) string {
	if obj == nil {
		return ""
	}
	if fn, ok := obj.(*types.Func); ok {
		if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
			recvName := goRecvTypeName(sig.Recv().Type())
			if recvName != "" {
				return recvName + "." + fn.Name()
			}
		}
		return fn.Name()
	}
	return obj.Name()
}

func goRecvTypeName(t types.Type) string {
	switch v := t.(type) {
	case *types.Pointer:
		return goRecvTypeName(v.Elem())
	case *types.Named:
		return v.Obj().Name()
	default:
		return ""
	}
}

func addSymbol(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, pkgID string, fileID string, sym symbol.Symbol, locators *[]LocatorEntry) {
	name := sym.Name
	if name == "" {
		return
	}
	spanStart := sym.StartLine
	spanEnd := sym.EndLine
	if spanEnd == 0 {
		spanEnd = spanStart
	}
	if spanStart == 0 {
		spanStart = spanEnd
	}
	if spanStart == 0 {
		spanStart = 1
	}

	nodeID := SymbolID(opts.RepoKey, pkgID, sym.EffectiveID())
	node := Node{
		ID:        nodeID,
		Kind:      NodeSymbol,
		Pkg:       pkgID,
		File:      sym.FilePath,
		Name:      sym.Name,
		Signature: sym.Signature,
		SpanStart: spanStart,
		SpanEnd:   spanEnd,
		Exported:  isExportedSymbol(sym),
		Hash:      sym.BodyDigest,
		UpdatedAt: time.Now().UTC(),
	}
	parsed := docparser.Parse(sym.Documentation)
	node.Doc = parsed.Doc
	if parsed.Index.Purpose != "" && node.Summary == "" {
		node.Summary = parsed.Index.Purpose
	}
	if !parsed.Index.Empty() {
		meta, err := json.Marshal(parsed.Index)
		if err == nil {
			node.Meta = meta
		}
	}
	lang := languageFromPackageID(pkgID)
	pkg := symbolutil.DeriveSymbolPackage(sym.FilePath, lang)
	applySymbolSummary(ctx, opts, &node, sym.ID, sym.EffectiveID(), pkg)
	addNode(nodes, node)
	addEdge(edges, Edge{
		Src:    fileID,
		Dst:    node.ID,
		Type:   EdgeContains,
		Weight: 1.0,
	})
	if locators != nil {
		*locators = append(*locators, LocatorEntry{
			SymbolKey: sym.EffectiveID(),
			Pkg:       pkgID,
			FilePath:  sym.FilePath,
			Name:      sym.Name,
			Kind:      string(sym.Kind),
			Exported:  isExportedSymbol(sym),
			SpanStart: spanStart,
			SpanEnd:   spanEnd,
			BodyHash:  sym.BodyDigest,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
}

func applyFileSummary(ctx context.Context, opts BuildOptions, node *Node, relPath string) {
	if node == nil || opts.SummaryProvider == nil {
		return
	}
	if node.Summary != "" {
		return
	}
	summary, err := opts.SummaryProvider.Summary(ctx, relPath)
	if err != nil {
		return
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return
	}
	node.Summary = summary
}

func applySymbolSummary(ctx context.Context, opts BuildOptions, node *Node, symbolID, symbolKey, pkg string) {
	if node == nil || opts.SymbolSummaryProvider == nil {
		return
	}
	if node.Summary != "" {
		return
	}
	if symbolID == "" {
		return
	}
	summary, err := opts.SymbolSummaryProvider.Summary(ctx, symbolID, symbolKey, pkg)
	if err != nil {
		return
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return
	}
	node.Summary = summary
}

func collectLocalPackages(nodes map[string]Node, repoKey string) []string {
	set := make(map[string]struct{})
	for _, node := range nodes {
		if node.Kind != NodeFile {
			continue
		}
		if node.Pkg == "" {
			continue
		}
		set[PackageID(repoKey, node.Pkg)] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func applyPackageRollups(nodes map[string]Node, packageIDs []string) {
	if len(packageIDs) == 0 {
		return
	}
	pkgFiles := make(map[string][]Node)
	for _, node := range nodes {
		if node.Kind != NodeFile {
			continue
		}
		if node.Pkg == "" {
			continue
		}
		pkgFiles[node.Pkg] = append(pkgFiles[node.Pkg], node)
	}

	for _, pkgNodeID := range packageIDs {
		pkgNode, ok := nodes[pkgNodeID]
		if !ok {
			continue
		}
		files := pkgFiles[pkgNode.Pkg]
		if len(files) == 0 {
			continue
		}
		if pkgNode.Summary != "" {
			continue
		}
		summary := buildPackageSummary(pkgNode, files)
		if summary == "" {
			continue
		}
		pkgNode.Summary = summary
		nodes[pkgNodeID] = pkgNode
	}
}

func applyRepoRollup(nodes map[string]Node, edges map[string]Edge, repoKey, repoRoot string, packageIDs []string) {
	if len(packageIDs) == 0 {
		return
	}

	packages := make([]Node, 0, len(packageIDs))
	for _, pkgNodeID := range packageIDs {
		pkgNode, ok := nodes[pkgNodeID]
		if !ok {
			continue
		}
		packages = append(packages, pkgNode)
	}
	if len(packages) == 0 {
		return
	}

	repoName := filepath.Base(repoRoot)
	if repoName == "" {
		repoName = repoRoot
	}
	summary := buildRepoSummary(repoName, packages)
	if summary == "" {
		return
	}

	repoNode := Node{
		ID:        repoNodeID(repoKey),
		Kind:      NodeConcept,
		Name:      repoName,
		Summary:   summary,
		UpdatedAt: time.Now().UTC(),
	}
	addNode(nodes, repoNode)

	for _, pkgNode := range packages {
		addEdge(edges, Edge{
			Src:    repoNode.ID,
			Dst:    pkgNode.ID,
			Type:   EdgeContains,
			Weight: 1.0,
		})
	}
}

func buildPackageSummary(pkgNode Node, files []Node) string {
	displayName := strings.TrimSpace(pkgNode.Name)
	if displayName == "" {
		displayName = pkgNode.Pkg
	}
	sorted := make([]Node, 0, len(files))
	sorted = append(sorted, files...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].File < sorted[j].File
	})

	entries := make([]rollupEntry, 0, maxRollupEntries)
	for _, fileNode := range sorted {
		summary := strings.TrimSpace(fileNode.Summary)
		if summary == "" {
			continue
		}
		name := fileNode.Name
		if name == "" {
			name = filepath.Base(fileNode.File)
		}
		entries = append(entries, rollupEntry{name: name, summary: summary})
		if len(entries) >= maxRollupEntries {
			break
		}
	}

	rollup := fmt.Sprintf("Package %s contains %d files.", displayName, len(files))
	if len(entries) == 0 {
		return rollup
	}
	return rollup + " Key files: " + formatRollupEntries(entries)
}

func buildRepoSummary(repoName string, packages []Node) string {
	sorted := make([]Node, 0, len(packages))
	sorted = append(sorted, packages...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	entries := make([]rollupEntry, 0, maxRollupEntries)
	for _, pkgNode := range sorted {
		summary := strings.TrimSpace(pkgNode.Summary)
		if summary == "" {
			continue
		}
		name := strings.TrimSpace(pkgNode.Name)
		if name == "" {
			name = pkgNode.Pkg
		}
		entries = append(entries, rollupEntry{name: name, summary: summary})
		if len(entries) >= maxRollupEntries {
			break
		}
	}

	rollup := fmt.Sprintf("Repo %s contains %d packages.", repoName, len(packages))
	if len(entries) == 0 {
		return rollup
	}
	return rollup + " Key packages: " + formatRollupEntries(entries)
}

func formatRollupEntries(entries []rollupEntry) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		summary := truncateSummary(entry.summary, maxRollupSummaryLen)
		parts = append(parts, fmt.Sprintf("%s - %s", entry.name, summary))
	}
	return strings.Join(parts, "; ")
}

func truncateSummary(value string, max int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max < 4 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

type rollupEntry struct {
	name    string
	summary string
}

func repoNodeID(repoKey string) string {
	return NamespacedID(repoKey, "repo:root")
}

func addNode(nodes map[string]Node, node Node) {
	if node.ID == "" {
		return
	}
	if existing, ok := nodes[node.ID]; ok {
		nodes[node.ID] = mergeNode(existing, node)
		return
	}
	nodes[node.ID] = node
}

func addEdge(edges map[string]Edge, edge Edge) {
	if edge.Src == "" || edge.Dst == "" || edge.Type == "" {
		return
	}
	key := fmt.Sprintf("%s|%s|%s", edge.Src, edge.Dst, edge.Type)
	if existing, ok := edges[key]; ok {
		if edge.Weight > existing.Weight {
			existing.Weight = edge.Weight
		}
		if len(edge.Meta) > 0 {
			existing.Meta = edge.Meta
		}
		edges[key] = existing
		return
	}
	edges[key] = edge
}

func mergeNode(a, b Node) Node {
	if a.Kind == "" {
		a.Kind = b.Kind
	}
	if a.Pkg == "" {
		a.Pkg = b.Pkg
	}
	if a.File == "" {
		a.File = b.File
	}
	if a.Name == "" {
		a.Name = b.Name
	}
	if a.Signature == "" {
		a.Signature = b.Signature
	}
	if a.SpanStart == 0 {
		a.SpanStart = b.SpanStart
	}
	if a.SpanEnd == 0 {
		a.SpanEnd = b.SpanEnd
	}
	if !a.Exported {
		a.Exported = b.Exported
	}
	if a.Doc == "" {
		a.Doc = b.Doc
	}
	if a.Summary == "" {
		a.Summary = b.Summary
	}
	if len(a.Meta) == 0 {
		a.Meta = b.Meta
	}
	if a.Hash == "" {
		a.Hash = b.Hash
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = b.UpdatedAt
	}
	return a
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := 1
	for _, b := range content {
		if b == '\n' {
			count++
		}
	}
	return count
}

func isExportedName(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

func isExportedSymbol(sym symbol.Symbol) bool {
	if sym.Language == "typescript" {
		signature := strings.TrimSpace(sym.Signature)
		if strings.HasPrefix(signature, "export ") {
			return true
		}
		if sym.Name == "default" {
			return true
		}
	}
	if sym.Language == "elixir" {
		signature := strings.TrimSpace(sym.Signature)
		// Modules are always exported
		if strings.HasPrefix(signature, "defmodule ") {
			return true
		}
		// Public functions
		if strings.HasPrefix(signature, "def ") {
			return true
		}
		// Public macros
		if strings.HasPrefix(signature, "defmacro ") {
			return true
		}
		// Public types
		if strings.HasPrefix(signature, "@type ") {
			return true
		}
		// Callbacks are part of behaviours (public API)
		if strings.HasPrefix(signature, "@callback ") {
			return true
		}
		// Private: defp, defmacrop, @typep
		return false
	}
	if sym.Language == "python" {
		name := strings.TrimSpace(sym.Name)
		if name == "" {
			return false
		}
		return !strings.HasPrefix(name, "_")
	}
	if sym.Language == "rust" {
		signature := strings.TrimSpace(sym.Signature)
		return strings.HasPrefix(signature, "pub ") || strings.HasPrefix(signature, "pub(")
	}
	if sym.Language == "csharp" {
		signature := strings.TrimSpace(sym.Signature)
		return strings.HasPrefix(signature, "public ") || strings.HasPrefix(signature, "protected ") || strings.HasPrefix(signature, "internal ")
	}
	name := strings.TrimSpace(sym.Name)
	if sym.Language == "go" && sym.Kind == symbol.KindMethod {
		if idx := strings.LastIndex(name, "."); idx >= 0 && idx+1 < len(name) {
			name = name[idx+1:]
		}
	}
	return isExportedName(name)
}

func relPath(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	if strings.HasPrefix(rel, "..") {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func goPackageID(importPath string) string {
	return goPkgPrefix + importPath
}

func languageFromPackageID(pkgID string) string {
	switch {
	case strings.HasPrefix(pkgID, goPkgPrefix):
		return "go"
	case strings.HasPrefix(pkgID, tsLocalPrefix):
		return "typescript"
	case strings.HasPrefix(pkgID, tsNpmPrefix):
		return "typescript"
	case strings.HasPrefix(pkgID, pythonPkgPrefix):
		return "python"
	case strings.HasPrefix(pkgID, rustPkgPrefix):
		return "rust"
	case strings.HasPrefix(pkgID, csharpPkgPrefix):
		return "csharp"
	case strings.HasPrefix(pkgID, elixirPkgPrefix):
		return "elixir"
	default:
		return ""
	}
}

func languageForPath(pathValue string) string {
	switch strings.ToLower(filepath.Ext(filepath.ToSlash(pathValue))) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".cs":
		return "csharp"
	case ".ts", ".tsx", ".js", ".jsx":
		return "typescript"
	case ".ex", ".exs":
		return "elixir"
	case ".tf", ".tfvars":
		return "terraform"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return ""
	}
}

func importMeta(path string) []byte {
	if path == "" {
		return nil
	}
	meta, err := json.Marshal(map[string]string{"import": path})
	if err != nil {
		return nil
	}
	return meta
}

func buildLanguages(opts BuildOptions) []string {
	languages := make([]string, 0, 7)
	if opts.IncludeGo {
		languages = append(languages, "go")
	}
	if opts.IncludePython {
		languages = append(languages, "python")
	}
	if opts.IncludeRust {
		languages = append(languages, "rust")
	}
	if opts.IncludeCSharp {
		languages = append(languages, "csharp")
	}
	if opts.IncludeTypescript {
		languages = append(languages, "typescript")
	}
	if opts.IncludeElixir {
		languages = append(languages, "elixir")
	}
	if opts.IncludeTerraform {
		languages = append(languages, "terraform")
	}
	if opts.IncludeKubernetes {
		languages = append(languages, "kubernetes")
	}
	if opts.IncludeShell {
		languages = append(languages, "shell")
	}
	return languages
}

func pythonModuleID(filePath string) string {
	trimmed := strings.TrimSpace(filepath.ToSlash(filePath))
	if trimmed == "" {
		return pythonPkgPrefix + "root"
	}
	withoutExt := strings.TrimSuffix(trimmed, filepath.Ext(trimmed))
	withoutExt = strings.TrimSuffix(withoutExt, "/__init__")
	if withoutExt == "" || withoutExt == "." {
		return pythonPkgPrefix + "root"
	}
	return pythonPkgPrefix + withoutExt
}

func rustModuleID(filePath string) string {
	trimmed := strings.TrimSpace(filepath.ToSlash(filePath))
	if trimmed == "" {
		return rustPkgPrefix + "root"
	}
	withoutExt := strings.TrimSuffix(trimmed, filepath.Ext(trimmed))
	withoutExt = strings.TrimSuffix(withoutExt, "/mod")
	if withoutExt == "" || withoutExt == "." {
		return rustPkgPrefix + "root"
	}
	return rustPkgPrefix + withoutExt
}

func csharpPackageID(filePath string, content []byte) string {
	if ns := csharpNamespace(content); ns != "" {
		return csharpPkgPrefix + ns
	}
	trimmed := strings.TrimSpace(filepath.ToSlash(filePath))
	dir := filepath.Dir(trimmed)
	if dir == "." || dir == "" {
		return csharpPkgPrefix + "root"
	}
	return csharpPkgPrefix + dir
}

func csharpNamespace(content []byte) string {
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "namespace ") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "namespace "))
			value = strings.Trim(value, " ;{")
			if value != "" {
				return value
			}
		}
	}
	return ""
}
