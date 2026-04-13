package obsidian

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
)

// RepoGraphBuildOptions configures inbox-first repo graph draft generation.
type RepoGraphBuildOptions struct {
	Project                string
	WorkspaceRoot          string
	Folder                 string
	MaxPackages            int
	MaxFilesPerPackage     int
	MaxSymbolsPerPackage   int
	IncludePackagePrefixes []string
	ExcludePackagePrefixes []string
}

// RepoGraphBuildResult describes the generated draft bundle.
type RepoGraphBuildResult struct {
	RootNotePath  string   `json:"root_note_path"`
	PackageNotes  []string `json:"package_notes"`
	ConceptNotes  []string `json:"concept_notes,omitempty"`
	PackagesBuilt int      `json:"packages_built"`
	Folder        string   `json:"folder"`
}

// RepoGraphPromoteResult describes a reviewed promotion of a repo graph draft bundle.
type RepoGraphPromoteResult struct {
	SourceFolder string                `json:"source_folder"`
	TargetFolder string                `json:"target_folder"`
	Merged       []ReviewedMergeResult `json:"merged"`
}

// DefaultRepoGraphDraftFolder returns the inbox-first draft folder for a project repo graph bundle.
func DefaultRepoGraphDraftFolder(policy Policy, project string) string {
	project = safeSlug(project)
	if project == "" {
		project = "repo"
	}
	return filepath.ToSlash(filepath.Join(policy.InboxPrefix, "repo-graph", project))
}

// DefaultRepoGraphCanonicalFolder returns the canonical vault folder for a project repo graph bundle.
func DefaultRepoGraphCanonicalFolder(project string) string {
	project = safeSlug(project)
	if project == "" {
		project = "repo"
	}
	return filepath.ToSlash(filepath.Join("notes", "repo", project))
}

type packageDraft struct {
	node          repoindex.Node
	files         []repoindex.Node
	concepts      []repoindex.Node
	symbols       []repoindex.Node
	anchorPaths   []string
	relatedTitles []string
	relatedIDs    []string
	title         string
	path          string
	score         int
}

type conceptDraft struct {
	packageTitle string
	concept      repoindex.Node
	path         string
	title        string
}

// BuildRepoGraphDrafts creates an inbox-first Obsidian graph layer draft bundle from the repo index.
func BuildRepoGraphDrafts(ctx context.Context, writer *Writer, repo *repoindex.Store, opts RepoGraphBuildOptions) (RepoGraphBuildResult, error) {
	if writer == nil {
		return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: writer required")
	}
	if repo == nil {
		return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: repo index store required")
	}
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = filepath.Base(strings.TrimSpace(opts.WorkspaceRoot))
	}
	project = safeSlug(project)
	if project == "" {
		project = "repo"
	}
	if opts.MaxPackages <= 0 {
		opts.MaxPackages = 6
	}
	if opts.MaxFilesPerPackage <= 0 {
		opts.MaxFilesPerPackage = 8
	}
	if opts.MaxSymbolsPerPackage <= 0 {
		opts.MaxSymbolsPerPackage = 12
	}
	folder := strings.TrimSpace(opts.Folder)
	if folder == "" {
		folder = DefaultRepoGraphDraftFolder(writer.Policy, project)
	}
	if strings.TrimSpace(writer.VaultPath) != "" {
		if err := os.RemoveAll(filepath.Join(writer.VaultPath, filepath.FromSlash(folder))); err != nil {
			return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: clear draft folder: %w", err)
		}
	}

	packages, err := repo.ListNodesByKind(ctx, repoindex.NodePackage, opts.MaxPackages*100)
	if err != nil {
		return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: list packages: %w", err)
	}

	rootTitle := fmt.Sprintf("%s Repo Graph", strings.TrimSpace(opts.Project))
	if strings.TrimSpace(opts.Project) == "" {
		rootTitle = fmt.Sprintf("%s Repo Graph", filepath.Base(strings.TrimSpace(opts.WorkspaceRoot)))
	}
	if strings.TrimSpace(rootTitle) == "" {
		rootTitle = "Repo Graph"
	}
	rootWiki := rootTitle

	var drafts []packageDraft
	var conceptDrafts []conceptDraft
	packageTitleByID := map[string]string{}
	packagePaths := []string{}

	for _, pkg := range packages {
		if !packageAllowed(pkg, opts.IncludePackagePrefixes, opts.ExcludePackagePrefixes) {
			continue
		}
		fileBudget := opts.MaxFilesPerPackage
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(pkg.Pkg)), "k8s:") && fileBudget < 64 {
			fileBudget = 64
		}
		fileNodes, conceptNodes, relatedIDs, err := collectPackageGraph(ctx, repo, pkg, fileBudget)
		if err != nil {
			return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: collect package graph %s: %w", pkg.ID, err)
		}
		if len(fileNodes) == 0 {
			continue
		}
		packagePaths = append(packagePaths, normalizedPackagePath(pkg))
		symbolNodes, err := collectPackageSymbols(ctx, repo, fileNodes, opts.MaxSymbolsPerPackage)
		if err != nil {
			return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: collect package symbols %s: %w", pkg.ID, err)
		}
		if extraRelated, err := collectSymbolRelatedPackageIDs(ctx, repo, symbolNodes); err == nil {
			relatedIDs = append(relatedIDs, extraRelated...)
		}
		title := normalizedPackagePath(pkg)
		path := filepath.ToSlash(filepath.Join(folder, "packages", safeSlug(title)+".md"))
		drafts = append(drafts, packageDraft{
			node:       pkg,
			files:      fileNodes,
			concepts:   conceptNodes,
			symbols:    symbolNodes,
			relatedIDs: uniqueStrings(relatedIDs),
			title:      title,
			path:       path,
			score:      packageGraphScore(fileNodes, conceptNodes, symbolNodes, relatedIDs),
		})
		packageTitleByID[pkg.ID] = title
	}
	if len(drafts) == 0 {
		return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: no local packages with files found")
	}
	sort.SliceStable(drafts, func(i, j int) bool {
		if drafts[i].score != drafts[j].score {
			return drafts[i].score > drafts[j].score
		}
		return drafts[i].title < drafts[j].title
	})
	if len(drafts) > opts.MaxPackages {
		drafts = drafts[:opts.MaxPackages]
	}

	trimPrefix := commonPackagePrefix(packagePaths)
	repoTrimPrefix := repoScopedTrimPrefix(packagePaths, opts.WorkspaceRoot)
	for i := range drafts {
		drafts[i].title = friendlyPackageTitle(drafts[i].node, trimPrefix, repoTrimPrefix)
		drafts[i].path = filepath.ToSlash(filepath.Join(folder, "packages", safeSlug(drafts[i].title)+".md"))
		drafts[i].anchorPaths = buildPackageDraftAnchorPaths(drafts[i])
		packageTitleByID[drafts[i].node.ID] = drafts[i].title
	}
	for _, draft := range drafts {
		for _, concept := range limitConceptDrafts(draft.concepts, 6) {
			title := conceptDraftTitle(draft.title, concept)
			if title == "" {
				title = strings.TrimSpace(concept.File)
			}
			if title == "" {
				continue
			}
			conceptDrafts = append(conceptDrafts, conceptDraft{
				packageTitle: draft.title,
				concept:      concept,
				title:        title,
				path:         filepath.ToSlash(filepath.Join(folder, "concepts", safeSlug(title)+".md")),
			})
		}
	}
	conceptNodes, err := repo.ListNodesByKind(ctx, repoindex.NodeConcept, opts.MaxPackages*200)
	if err != nil {
		return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: list concepts: %w", err)
	}
	for _, concept := range conceptNodes {
		if !infraConceptAllowed(concept) {
			continue
		}
		if !packageAllowed(concept, opts.IncludePackagePrefixes, opts.ExcludePackagePrefixes) {
			continue
		}
		pkgTitle := packageTitleByID[repoindex.PackageID(repo.RepoKey(), concept.Pkg)]
		if strings.TrimSpace(pkgTitle) == "" {
			pkgTitle = friendlyPackageTitle(concept, trimPrefix, repoTrimPrefix)
		}
		title := conceptDraftTitle(pkgTitle, concept)
		if title == "" {
			continue
		}
		conceptDrafts = append(conceptDrafts, conceptDraft{
			packageTitle: pkgTitle,
			concept:      concept,
			title:        title,
			path:         filepath.ToSlash(filepath.Join(folder, "concepts", safeSlug(title)+".md")),
		})
	}
	conceptDrafts = dedupeConceptDrafts(conceptDrafts)

	for i := range drafts {
		drafts[i].relatedTitles = resolveImportedPackageTitles(drafts[i].relatedIDs, packageTitleByID)
		body := renderPackageGraphNote(rootWiki, drafts[i])
		if err := writer.CreateNote(ctx, drafts[i].path, body, true); err != nil {
			return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: write package note %s: %w", drafts[i].path, err)
		}
	}
	for _, draft := range conceptDrafts {
		body := renderConceptGraphNote(rootWiki, draft)
		if err := writer.CreateNote(ctx, draft.path, body, true); err != nil {
			return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: write concept note %s: %w", draft.path, err)
		}
	}

	rootPath := filepath.ToSlash(filepath.Join(folder, "index.md"))
	rootBody := renderRepoGraphRoot(rootTitle, drafts, conceptDrafts, opts.WorkspaceRoot)
	if err := writer.CreateNote(ctx, rootPath, rootBody, true); err != nil {
		return RepoGraphBuildResult{}, fmt.Errorf("obsidian graph: write root note: %w", err)
	}

	result := RepoGraphBuildResult{
		RootNotePath:  rootPath,
		PackagesBuilt: len(drafts),
		Folder:        folder,
	}
	for _, draft := range drafts {
		result.PackageNotes = append(result.PackageNotes, draft.path)
	}
	for _, draft := range conceptDrafts {
		result.ConceptNotes = append(result.ConceptNotes, draft.path)
	}
	return result, nil
}

// PromoteRepoGraphDrafts review-merges an inbox-first graph bundle into a canonical target folder.
func PromoteRepoGraphDrafts(ctx context.Context, writer *Writer, sourceFolder, targetFolder string) (RepoGraphPromoteResult, error) {
	if writer == nil {
		return RepoGraphPromoteResult{}, fmt.Errorf("obsidian graph: writer required")
	}
	if strings.TrimSpace(writer.VaultPath) == "" {
		return RepoGraphPromoteResult{}, fmt.Errorf("obsidian graph: vault path required")
	}
	sourceFolder = normalizeVaultPath(sourceFolder)
	targetFolder = normalizeVaultPath(targetFolder)
	root := filepath.Join(writer.VaultPath, filepath.FromSlash(sourceFolder))
	entries := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(writer.VaultPath, path)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return RepoGraphPromoteResult{}, fmt.Errorf("obsidian graph: walk source folder: %w", err)
	}
	sort.Strings(entries)
	result := RepoGraphPromoteResult{
		SourceFolder: sourceFolder,
		TargetFolder: targetFolder,
	}
	for _, draftPath := range entries {
		rel := strings.TrimPrefix(draftPath, sourceFolder)
		rel = strings.TrimPrefix(rel, "/")
		targetPath := filepath.ToSlash(filepath.Join(targetFolder, rel))
		merge, err := writer.ReviewMergeDraft(ctx, draftPath, targetPath, "")
		if err != nil {
			return RepoGraphPromoteResult{}, fmt.Errorf("obsidian graph: promote %s: %w", draftPath, err)
		}
		result.Merged = append(result.Merged, merge)
	}
	return result, nil
}

func collectPackageGraph(ctx context.Context, repo *repoindex.Store, pkg repoindex.Node, maxFiles int) ([]repoindex.Node, []repoindex.Node, []string, error) {
	edges, err := repo.GetOutgoingEdges(ctx, pkg.ID, []repoindex.EdgeType{repoindex.EdgeContains, repoindex.EdgeImports}, 200)
	if err != nil {
		return nil, nil, nil, err
	}
	containedIDs := []string{}
	importedPkgs := []string{}
	for _, edge := range edges {
		switch edge.Type {
		case repoindex.EdgeContains:
			containedIDs = append(containedIDs, edge.Dst)
		case repoindex.EdgeImports:
			importedPkgs = append(importedPkgs, edge.Dst)
		}
	}
	incoming, err := repo.GetIncomingEdges(ctx, pkg.ID, []repoindex.EdgeType{repoindex.EdgeImports}, 200)
	if err == nil {
		for _, edge := range incoming {
			importedPkgs = append(importedPkgs, edge.Src)
		}
	}
	nodes, err := repo.GetNodes(ctx, uniqueStrings(containedIDs))
	if err != nil {
		return nil, nil, nil, err
	}
	allFiles := make([]repoindex.Node, 0, len(nodes))
	concepts := make([]repoindex.Node, 0, len(nodes))
	for _, node := range nodes {
		switch node.Kind {
		case repoindex.NodeFile:
			allFiles = append(allFiles, node)
		case repoindex.NodeConcept:
			concepts = append(concepts, node)
		}
	}
	sort.SliceStable(allFiles, func(i, j int) bool {
		return allFiles[i].File < allFiles[j].File
	})
	sort.SliceStable(concepts, func(i, j int) bool {
		if concepts[i].Name != concepts[j].Name {
			return concepts[i].Name < concepts[j].Name
		}
		return concepts[i].File < concepts[j].File
	})
	if len(allFiles) > 0 {
		fileConceptIDs := []string{}
		for _, file := range allFiles {
			outgoing, err := repo.GetOutgoingEdges(ctx, file.ID, []repoindex.EdgeType{repoindex.EdgeContains, repoindex.EdgeTouchesResource}, 200)
			if err != nil {
				return nil, nil, nil, err
			}
			for _, edge := range outgoing {
				fileConceptIDs = append(fileConceptIDs, edge.Dst)
			}
		}
		if len(fileConceptIDs) > 0 {
			nodes, err := repo.GetNodes(ctx, uniqueStrings(fileConceptIDs))
			if err != nil {
				return nil, nil, nil, err
			}
			for _, node := range nodes {
				if node.Kind == repoindex.NodeConcept {
					concepts = append(concepts, node)
				}
			}
			concepts = dedupeRepoNodes(concepts)
			sort.SliceStable(concepts, func(i, j int) bool {
				if concepts[i].Name != concepts[j].Name {
					return concepts[i].Name < concepts[j].Name
				}
				return concepts[i].File < concepts[j].File
			})
		}
	}
	files := allFiles
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	if len(concepts) > maxFiles {
		concepts = concepts[:maxFiles]
	}
	return files, concepts, importedPkgs, nil
}

func collectPackageSymbols(ctx context.Context, repo *repoindex.Store, files []repoindex.Node, maxSymbols int) ([]repoindex.Node, error) {
	symbolIDs := []string{}
	for _, file := range files {
		edges, err := repo.GetOutgoingEdges(ctx, file.ID, []repoindex.EdgeType{repoindex.EdgeContains}, 200)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			symbolIDs = append(symbolIDs, edge.Dst)
		}
	}
	nodes, err := repo.GetNodes(ctx, uniqueStrings(symbolIDs))
	if err != nil {
		return nil, err
	}
	symbols := make([]repoindex.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == repoindex.NodeSymbol {
			symbols = append(symbols, node)
		}
	}
	sort.SliceStable(symbols, func(i, j int) bool {
		if symbols[i].File != symbols[j].File {
			return symbols[i].File < symbols[j].File
		}
		return symbols[i].Name < symbols[j].Name
	})
	if len(symbols) > maxSymbols {
		symbols = symbols[:maxSymbols]
	}
	return symbols, nil
}

func collectSymbolRelatedPackageIDs(ctx context.Context, repo *repoindex.Store, symbols []repoindex.Node) ([]string, error) {
	ids := []string{}
	for _, symbol := range symbols {
		outgoing, err := repo.GetOutgoingEdges(ctx, symbol.ID, []repoindex.EdgeType{repoindex.EdgeCalls, repoindex.EdgeRefersTo}, 40)
		if err == nil {
			targetIDs := []string{}
			for _, edge := range outgoing {
				targetIDs = append(targetIDs, edge.Dst)
			}
			nodes, err := repo.GetNodes(ctx, uniqueStrings(targetIDs))
			if err == nil {
				for _, node := range nodes {
					if node.Kind == repoindex.NodeSymbol && node.Pkg != "" && node.Pkg != symbol.Pkg {
						ids = append(ids, repoindex.PackageID(repo.RepoKey(), node.Pkg))
					}
				}
			}
		}
		incoming, err := repo.GetIncomingEdges(ctx, symbol.ID, []repoindex.EdgeType{repoindex.EdgeCalls, repoindex.EdgeRefersTo}, 40)
		if err == nil {
			sourceIDs := []string{}
			for _, edge := range incoming {
				sourceIDs = append(sourceIDs, edge.Src)
			}
			nodes, err := repo.GetNodes(ctx, uniqueStrings(sourceIDs))
			if err == nil {
				for _, node := range nodes {
					if node.Kind == repoindex.NodeSymbol && node.Pkg != "" && node.Pkg != symbol.Pkg {
						ids = append(ids, repoindex.PackageID(repo.RepoKey(), node.Pkg))
					}
				}
			}
		}
	}
	return uniqueStrings(ids), nil
}

func resolveImportedPackageTitles(importedIDs []string, byID map[string]string) []string {
	out := []string{}
	for _, value := range importedIDs {
		if title, ok := byID[value]; ok {
			out = append(out, title)
		}
	}
	return uniqueStrings(out)
}

func renderRepoGraphRoot(title string, drafts []packageDraft, conceptDrafts []conceptDraft, workspaceRoot string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + title + "\n")
	b.WriteString("type: map\n")
	b.WriteString("status: draft\n")
	b.WriteString("trust: reviewed\n")
	b.WriteString(fmt.Sprintf("updated: %s\n", time.Now().UTC().Format("2006-01-02")))
	b.WriteString("---\n\n")
	b.WriteString("# " + title + "\n\n")
	if trimmed := strings.TrimSpace(workspaceRoot); trimmed != "" {
		b.WriteString("- Workspace: `" + trimmed + "`\n")
	}
	b.WriteString("- Generated from repo index into inbox-first graph drafts.\n\n")
	b.WriteString("## Packages\n\n")
	for _, draft := range drafts {
		b.WriteString("- [[" + draft.title + "]]\n")
	}
	if len(conceptDrafts) > 0 {
		b.WriteString("\n## Concepts\n\n")
		for _, draft := range conceptDrafts {
			b.WriteString("- [[" + draft.title + "]]\n")
		}
	}
	b.WriteString("\n## Notes\n\n")
	b.WriteString("- Review package notes before merging into canonical repo maps.\n")
	b.WriteString("- Paths and symbols are attached in frontmatter for retrieval.\n")
	return b.String()
}

func renderPackageGraphNote(rootWiki string, draft packageDraft) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + draft.title + "\n")
	b.WriteString("type: investigation\n")
	b.WriteString("status: draft\n")
	b.WriteString("trust: reviewed\n")
	b.WriteString("paths:\n")
	for _, file := range draft.files {
		if strings.TrimSpace(file.File) != "" {
			b.WriteString("  - " + filepath.ToSlash(file.File) + "\n")
		}
	}
	if len(draft.anchorPaths) > 0 {
		b.WriteString("primary_anchor_path: " + filepath.ToSlash(draft.anchorPaths[0]) + "\n")
		b.WriteString("impl_anchor_paths:\n")
		b.WriteString("  - " + filepath.ToSlash(draft.anchorPaths[0]) + "\n")
	}
	if len(draft.anchorPaths) > 0 {
		b.WriteString("anchor_paths:\n")
		for _, pathValue := range draft.anchorPaths {
			b.WriteString("  - " + filepath.ToSlash(pathValue) + "\n")
		}
		if len(draft.anchorPaths) > 1 {
			b.WriteString("support_anchor_paths:\n")
			for _, pathValue := range draft.anchorPaths[1:] {
				b.WriteString("  - " + filepath.ToSlash(pathValue) + "\n")
			}
		}
	}
	if len(draft.symbols) > 0 || len(draft.concepts) > 0 {
		b.WriteString("symbols:\n")
		seen := map[string]struct{}{}
		for _, symbol := range draft.symbols {
			name := strings.TrimSpace(symbol.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			b.WriteString("  - " + name + "\n")
		}
		for _, concept := range draft.concepts {
			name := strings.TrimSpace(concept.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			b.WriteString("  - " + name + "\n")
		}
	}
	b.WriteString(fmt.Sprintf("updated: %s\n", time.Now().UTC().Format("2006-01-02")))
	b.WriteString("---\n\n")
	b.WriteString("# " + draft.title + "\n\n")
	b.WriteString("- Back to [[" + rootWiki + "]]\n\n")
	b.WriteString("## Files\n\n")
	for _, file := range draft.files {
		label := file.File
		if label == "" {
			label = file.Name
		}
		b.WriteString("- `" + label + "`\n")
	}
	if len(draft.symbols) > 0 {
		b.WriteString("\n## Symbols\n\n")
		for _, symbol := range draft.symbols {
			label := symbol.Name
			if symbol.File != "" {
				label += " (`" + symbol.File + "`)"
			}
			b.WriteString("- " + label + "\n")
		}
	}
	if len(draft.concepts) > 0 {
		b.WriteString("\n## Concepts\n\n")
		for _, concept := range draft.concepts {
			label := strings.TrimSpace(concept.Name)
			if label == "" {
				label = strings.TrimSpace(concept.File)
			}
			if concept.File != "" {
				label += " (`" + concept.File + "`)"
			}
			b.WriteString("- " + label + "\n")
		}
	}
	if len(draft.relatedTitles) > 0 {
		b.WriteString("\n## Related Packages\n\n")
		for _, title := range draft.relatedTitles {
			b.WriteString("- [[" + title + "]]\n")
		}
	}
	return b.String()
}

func renderConceptGraphNote(rootWiki string, draft conceptDraft) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + draft.title + "\n")
	b.WriteString("type: investigation\n")
	b.WriteString("status: draft\n")
	b.WriteString("trust: reviewed\n")
	if strings.TrimSpace(draft.concept.File) != "" {
		b.WriteString("paths:\n")
		b.WriteString("  - " + filepath.ToSlash(draft.concept.File) + "\n")
		b.WriteString("primary_anchor_path: " + filepath.ToSlash(draft.concept.File) + "\n")
		b.WriteString("resource_anchor_paths:\n")
		b.WriteString("  - " + filepath.ToSlash(draft.concept.File) + "\n")
		b.WriteString("anchor_paths:\n")
		b.WriteString("  - " + filepath.ToSlash(draft.concept.File) + "\n")
	}
	b.WriteString("symbols:\n")
	b.WriteString("  - " + draft.title + "\n")
	b.WriteString(fmt.Sprintf("updated: %s\n", time.Now().UTC().Format("2006-01-02")))
	b.WriteString("---\n\n")
	b.WriteString("# " + draft.title + "\n\n")
	b.WriteString("- Back to [[" + rootWiki + "]]\n")
	if strings.TrimSpace(draft.packageTitle) != "" {
		b.WriteString("- Package: [[" + draft.packageTitle + "]]\n")
	}
	b.WriteString("\n## Resource\n\n")
	if strings.TrimSpace(draft.concept.File) != "" {
		b.WriteString("- File: `" + filepath.ToSlash(draft.concept.File) + "`\n")
	}
	if strings.TrimSpace(draft.concept.Summary) != "" {
		b.WriteString("- Summary: " + draft.concept.Summary + "\n")
	}
	return b.String()
}

func buildPackageDraftAnchorPaths(draft packageDraft) []string {
	if len(draft.files) == 0 {
		return nil
	}
	titleTerms := packageDraftTitleTerms(draft.title)
	type scoredFile struct {
		path  string
		score float64
	}
	scored := make([]scoredFile, 0, len(draft.files))
	for _, file := range draft.files {
		pathValue := strings.TrimSpace(filepath.ToSlash(file.File))
		if pathValue == "" {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(pathValue), filepath.Ext(pathValue)))
		score := packageDraftTitleAffinity(base, titleTerms)*2.0 + packageDraftRoleScore(base)
		score += packageDraftSymbolFileScore(pathValue, draft.symbols)
		score -= float64(strings.Count(pathValue, "/")) * 0.03
		scored = append(scored, scoredFile{path: pathValue, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].path < scored[j].path
	})
	if len(scored) > 2 {
		scored = scored[:2]
	}
	out := make([]string, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.path)
	}
	return uniqueStrings(out)
}

func packageDraftTitleTerms(title string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	for _, field := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(title)), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		field = strings.TrimSpace(field)
		if len(field) < 3 {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out
}

func packageDraftTitleAffinity(base string, titleTerms []string) float64 {
	if base == "" || len(titleTerms) == 0 {
		return 0
	}
	best := 0.0
	for _, term := range titleTerms {
		switch {
		case base == term:
			if best < 1.0 {
				best = 1.0
			}
		case strings.Contains(base, term), strings.Contains(term, base):
			if best < 0.55 {
				best = 0.55
			}
		}
	}
	return best
}

func packageDraftRoleScore(base string) float64 {
	switch {
	case strings.HasSuffix(base, "_exec"):
		return 0.45
	case base == "ets":
		return 0.4
	}
	switch base {
	case "main", "config", "store", "index", "indexer", "runtime", "router", "provider", "plugin", "registry":
		return 0.35
	default:
		return 0
	}
}

func packageDraftSymbolFileScore(pathValue string, symbols []repoindex.Node) float64 {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(pathValue), filepath.Ext(pathValue)))
	if base == "" || len(symbols) == 0 {
		return 0
	}
	score := 0.0
	for _, symbol := range symbols {
		if strings.TrimSpace(filepath.ToSlash(symbol.File)) != pathValue {
			continue
		}
		name := strings.TrimSpace(symbol.Name)
		if name == "" {
			continue
		}
		parts := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
		})
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			switch {
			case last == base:
				score += 0.8
			case strings.Contains(last, base), strings.Contains(base, last):
				score += 0.4
			default:
				score += 0.1
			}
		}
	}
	return score
}

func friendlyPackageTitle(node repoindex.Node, trimPrefix, repoTrimPrefix string) string {
	raw := normalizedPackagePath(node)
	raw = strings.TrimPrefix(raw, trimPrefix)
	raw = strings.TrimPrefix(raw, repoTrimPrefix)
	raw = strings.Trim(raw, "/")
	if raw == "" {
		raw = firstNonEmpty(node.Name, node.File, "package")
	}
	return strings.ReplaceAll(raw, "/", " ")
}

func normalizedPackagePath(node repoindex.Node) string {
	raw := strings.TrimSpace(node.Pkg)
	for _, prefix := range []string{"go:", "ts:local:", "ts:npm:", "ex:"} {
		raw = strings.TrimPrefix(raw, prefix)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = strings.TrimSpace(node.Name)
	}
	return strings.Trim(raw, "/")
}

func packageAllowed(node repoindex.Node, includes, excludes []string) bool {
	path := strings.ToLower(normalizedPackagePath(node))
	if path == "" {
		return false
	}
	allowed := len(includes) == 0
	for _, include := range includes {
		include = strings.ToLower(strings.Trim(strings.TrimSpace(include), "/"))
		if include != "" && strings.HasPrefix(path, include) {
			allowed = true
			break
		}
	}
	if !allowed {
		return false
	}
	for _, exclude := range excludes {
		exclude = strings.ToLower(strings.Trim(strings.TrimSpace(exclude), "/"))
		if exclude != "" && strings.HasPrefix(path, exclude) {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func commonPackagePrefix(items []string) string {
	if len(items) == 0 {
		return ""
	}
	segments := strings.Split(strings.Trim(items[0], "/"), "/")
	if len(segments) == 0 {
		return ""
	}
	prefix := []string{}
	for i := 0; i < len(segments); i++ {
		part := segments[i]
		matches := true
		for _, item := range items[1:] {
			parts := strings.Split(strings.Trim(item, "/"), "/")
			if i >= len(parts) || parts[i] != part {
				matches = false
				break
			}
		}
		if !matches {
			break
		}
		prefix = append(prefix, part)
	}
	if len(prefix) == 0 {
		return ""
	}
	return strings.Join(prefix, "/") + "/"
}

func repoScopedTrimPrefix(items []string, workspaceRoot string) string {
	repoName := safeSlug(filepath.Base(strings.TrimSpace(workspaceRoot)))
	if repoName == "" {
		return ""
	}
	repoName = strings.ReplaceAll(repoName, "-", "")
	best := ""
	for _, item := range items {
		parts := strings.Split(strings.Trim(item, "/"), "/")
		for i, part := range parts {
			normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(part)), "-", "")
			if normalized != strings.ToLower(repoName) {
				continue
			}
			prefix := strings.Join(parts[:i+1], "/") + "/"
			if len(prefix) > len(best) {
				best = prefix
			}
			break
		}
	}
	return best
}

func packageGraphScore(files, concepts, symbols []repoindex.Node, relatedIDs []string) int {
	return len(files)*5 + len(concepts)*4 + len(symbols)*2 + len(uniqueStrings(relatedIDs))*3
}

func limitConceptDrafts(items []repoindex.Node, maxItems int) []repoindex.Node {
	if maxItems <= 0 {
		maxItems = 20
	}
	if len(items) <= maxItems {
		return items
	}
	return items[:maxItems]
}

func dedupeConceptDrafts(items []conceptDraft) []conceptDraft {
	seen := map[string]struct{}{}
	out := make([]conceptDraft, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item.title)) + "::" + strings.ToLower(strings.TrimSpace(item.concept.File))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func conceptDraftTitle(packageTitle string, concept repoindex.Node) string {
	base := strings.TrimSpace(concept.Name)
	if base == "" {
		return ""
	}
	pkg := strings.TrimSpace(packageTitle)
	if pkg == "" {
		return base
	}
	return base + " in " + pkg
}

func dedupeRepoNodes(items []repoindex.Node) []repoindex.Node {
	seen := map[string]struct{}{}
	out := make([]repoindex.Node, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		out = append(out, item)
	}
	return out
}

func infraConceptAllowed(node repoindex.Node) bool {
	pkg := strings.ToLower(strings.TrimSpace(node.Pkg))
	return strings.HasPrefix(pkg, "k8s:") || strings.HasPrefix(pkg, "tf:") || strings.HasPrefix(pkg, "sh:")
}
