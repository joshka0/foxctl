package repoindex

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"

	docparser "github.com/jkatigb/agentctl/internal/indexing/repoindex/parser"
	"github.com/jkatigb/agentctl/internal/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/platform/fsutil"
	"github.com/jkatigb/agentctl/internal/platform/symbolutil"
)

const (
	goPkgPrefix         = "go:"
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
	if !opts.IncludeGo && !opts.IncludeTypescript && !opts.IncludeElixir {
		return result, fmt.Errorf("repoindex: at least one language must be enabled")
	}

	nodes := make(map[string]Node)
	edges := make(map[string]Edge)
	var pending []pendingNameEdge
	var locators []LocatorEntry

	if opts.IncludeGo {
		if err := b.buildGo(ctx, opts, nodes, edges, &result, &locators); err != nil {
			return result, err
		}
	}
	if opts.IncludeTypescript {
		if err := b.buildTS(ctx, opts, nodes, edges, &result, &pending, &locators); err != nil {
			return result, err
		}
	}
	if opts.IncludeElixir {
		if err := b.buildElixir(ctx, opts, nodes, edges, &result, &pending, &locators); err != nil {
			return result, err
		}
	}

	applyPendingNameEdges(nodes, edges, pending)

	localPackages := collectLocalPackages(nodes, opts.RepoKey)
	applyPackageRollups(nodes, localPackages)
	applyRepoRollup(nodes, edges, opts.RepoKey, opts.RepoRoot, localPackages)
	applyCommentEdges(nodes, edges, opts.RepoKey)

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
		return result, nil
	}

	if err := b.store.ReplaceAll(ctx, nodeList, edgeList); err != nil {
		return result, err
	}
	for _, loc := range locators {
		if err := b.store.UpsertLocator(ctx, loc); err != nil {
			return result, fmt.Errorf("repoindex: upsert locator: %w", err)
		}
	}

	meta := IndexMeta{
		RepoRoot:      opts.RepoRoot,
		HeadSHA:       resolveGitHead(ctx, opts.RepoRoot),
		SchemaVersion: schemaVersion,
		IndexedAt:     time.Now().UTC(),
	}
	if err := b.store.SetMeta(ctx, meta); err != nil {
		return result, err
	}

	return result, nil
}

func (b *Builder) buildGo(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult, locators *[]LocatorEntry) error {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     opts.RepoRoot,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Tests:   opts.IncludeTests,
	}
	pkgs, err := packages.Load(cfg, opts.Patterns...)
	if err != nil {
		return fmt.Errorf("repoindex: load go packages: %w", err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		return fmt.Errorf("repoindex: go packages load errors")
	}

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

func (b *Builder) buildTS(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge, result *BuildResult, pending *[]pendingNameEdge, locators *[]LocatorEntry) error {
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
		".git/**",
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
		}

		imports := extractTSImports(string(content))
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
		}
	}

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
		}
	}
	return nil
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
	return isExportedName(sym.Name)
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
	case strings.HasPrefix(pkgID, elixirPkgPrefix):
		return "elixir"
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

func resolveGitHead(ctx context.Context, repoRoot string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
