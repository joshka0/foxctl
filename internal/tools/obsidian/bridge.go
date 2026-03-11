package obsidian

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// DocsBridgeReconcileOptions configures repo docs <-> vault bridge reconciliation.
type DocsBridgeReconcileOptions struct {
	Project            string
	WorkspaceRoot      string
	DocsRoot           string
	Folder             string
	MaxMatches         int
	IncludeDocPrefixes []string
	ExcludeDocPrefixes []string
	SearchProvider     DocsBridgeSearchProvider
}

// DocsBridgeDocResult describes one reconciled repo doc and its bridge draft.
type DocsBridgeDocResult struct {
	DocPath                 string   `json:"doc_path"`
	Title                   string   `json:"title"`
	ExistingVaultRefs       []string `json:"existing_vault_refs,omitempty"`
	ExistingRepoDocBackrefs []string `json:"existing_repo_doc_backrefs,omitempty"`
	SuggestedVaultRefs      []string `json:"suggested_vault_refs,omitempty"`
	DraftPath               string   `json:"draft_path"`
}

// DocsBridgeReconcileResult describes the generated docs-bridge draft bundle.
type DocsBridgeReconcileResult struct {
	RootNotePath   string                `json:"root_note_path"`
	DocNotes       []DocsBridgeDocResult `json:"doc_notes"`
	DocsScanned    int                   `json:"docs_scanned"`
	DocsWithLinks  int                   `json:"docs_with_links"`
	DocsMissing    int                   `json:"docs_missing_links"`
	Folder         string                `json:"folder"`
	WorkspaceRoot  string                `json:"workspace_root"`
	DocsRoot       string                `json:"docs_root"`
	CanonicalNotes int                   `json:"canonical_notes"`
}

// DocsBridgeApplyOptions configures applying reviewed bridge draft frontmatter patches.
type DocsBridgeApplyOptions struct {
	Project       string
	WorkspaceRoot string
	DraftPath     string
	DocPath       string
	MaxLinks      int
}

// DocsBridgeApplyResult describes the files patched from a reviewed bridge draft.
type DocsBridgeApplyResult struct {
	DraftPath         string   `json:"draft_path"`
	RepoDocPath       string   `json:"repo_doc_path"`
	VaultRefsApplied  []string `json:"vault_refs_applied,omitempty"`
	VaultNotesPatched []string `json:"vault_notes_patched,omitempty"`
	RepoDocUpdated    bool     `json:"repo_doc_updated"`
	VaultNotesUpdated int      `json:"vault_notes_updated"`
}

// DocsBridgeBatchApplyOptions configures applying reviewed bridge drafts in bulk.
type DocsBridgeBatchApplyOptions struct {
	Project            string
	WorkspaceRoot      string
	Folder             string
	RequireStatus      string
	RequireTrust       string
	IncludeDocPrefixes []string
	ExcludeDocPrefixes []string
	MaxLinks           int
	MaxDrafts          int
}

// DocsBridgeBatchSkip describes a skipped bridge draft and why it was not applied.
type DocsBridgeBatchSkip struct {
	DraftPath string `json:"draft_path"`
	DocPath   string `json:"doc_path,omitempty"`
	Reason    string `json:"reason"`
}

// DocsBridgeBatchApplyResult describes a batch bridge apply run.
type DocsBridgeBatchApplyResult struct {
	Folder     string                  `json:"folder"`
	Considered int                     `json:"considered"`
	Matched    int                     `json:"matched"`
	Applied    []DocsBridgeApplyResult `json:"applied,omitempty"`
	Skipped    []DocsBridgeBatchSkip   `json:"skipped,omitempty"`
}

// DocsBridgeReportOptions configures reporting over bridge drafts.
type DocsBridgeReportOptions struct {
	Project            string
	WorkspaceRoot      string
	Folder             string
	IncludeDocPrefixes []string
	ExcludeDocPrefixes []string
}

// DocsBridgeDraftStatus reports the current review/apply state of a bridge draft.
type DocsBridgeDraftStatus struct {
	DraftPath          string   `json:"draft_path"`
	DocPath            string   `json:"doc_path"`
	Status             string   `json:"status,omitempty"`
	Trust              string   `json:"trust,omitempty"`
	SuggestedVaultRefs []string `json:"suggested_vault_refs,omitempty"`
	RepoDocAppliedRefs []string `json:"repo_doc_applied_refs,omitempty"`
	VaultNotesPatched  []string `json:"vault_notes_patched,omitempty"`
	MissingVaultNotes  []string `json:"missing_vault_notes,omitempty"`
	State              string   `json:"state"`
}

// DocsBridgeReportResult summarizes draft/review/apply state across a bridge folder.
type DocsBridgeReportResult struct {
	Folder   string                  `json:"folder"`
	Total    int                     `json:"total"`
	Draft    int                     `json:"draft"`
	Reviewed int                     `json:"reviewed"`
	Applied  int                     `json:"applied"`
	Partial  int                     `json:"partial"`
	Pending  int                     `json:"pending"`
	Entries  []DocsBridgeDraftStatus `json:"entries,omitempty"`
}

// DocsBridgeTidyOptions configures archival of fully applied bridge drafts.
type DocsBridgeTidyOptions struct {
	Project            string
	WorkspaceRoot      string
	Folder             string
	ArchiveFolder      string
	IncludeDocPrefixes []string
	ExcludeDocPrefixes []string
	MaxDrafts          int
}

// DocsBridgeTidySkip describes a draft skipped during tidy/archive.
type DocsBridgeTidySkip struct {
	DraftPath string `json:"draft_path"`
	DocPath   string `json:"doc_path,omitempty"`
	Reason    string `json:"reason"`
}

// DocsBridgeTidyResult describes archived bridge drafts.
type DocsBridgeTidyResult struct {
	Folder        string                  `json:"folder"`
	ArchiveFolder string                  `json:"archive_folder"`
	Considered    int                     `json:"considered"`
	Archived      []DocsBridgeDraftStatus `json:"archived,omitempty"`
	Skipped       []DocsBridgeTidySkip    `json:"skipped,omitempty"`
}

type bridgeDoc struct {
	RelPath     string
	Title       string
	FrontValues map[string]string
	FrontLists  map[string][]string
	Body        string
}

type bridgeVaultNote struct {
	Path        string
	Title       string
	FrontValues map[string]string
	FrontLists  map[string][]string
	Body        string
}

type bridgeSuggestion struct {
	Path  string
	Title string
	Score int
}

// DocsBridgeSearchHit is a ranked canonical vault candidate returned by an external search provider.
type DocsBridgeSearchHit struct {
	Path  string
	Title string
	Score int
}

// DocsBridgeSearchProvider supplies lexical/semantic ranked canonical note candidates for bridge reconciliation.
type DocsBridgeSearchProvider interface {
	SearchBridgeCandidates(ctx context.Context, query string, limit int) ([]DocsBridgeSearchHit, error)
}

// DefaultDocsBridgeDraftFolder returns the inbox-first draft folder for repo-doc bridge notes.
func DefaultDocsBridgeDraftFolder(policy Policy, project string) string {
	project = safeSlug(project)
	if project == "" {
		project = "repo"
	}
	return filepath.ToSlash(filepath.Join(policy.InboxPrefix, "docs-bridge", project))
}

// DefaultDocsBridgeArchiveFolder returns the archive folder for fully applied bridge drafts.
func DefaultDocsBridgeArchiveFolder(policy Policy, project string) string {
	project = safeSlug(project)
	if project == "" {
		project = "repo"
	}
	return filepath.ToSlash(filepath.Join(policy.OpsPrefix, "docs-bridge-applied", project))
}

// ReconcileDocsBridge scans repo docs and canonical vault notes, then creates draft bridge notes
// with backlink and metadata suggestions in the vault inbox.
func ReconcileDocsBridge(ctx context.Context, writer *Writer, opts DocsBridgeReconcileOptions) (DocsBridgeReconcileResult, error) {
	if writer == nil {
		return DocsBridgeReconcileResult{}, fmt.Errorf("obsidian bridge: writer required")
	}
	workspaceRoot := mustAbs(strings.TrimSpace(opts.WorkspaceRoot))
	if workspaceRoot == "" {
		return DocsBridgeReconcileResult{}, fmt.Errorf("obsidian bridge: workspace root required")
	}
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = filepath.Base(workspaceRoot)
	}
	if strings.TrimSpace(project) == "" {
		project = "repo"
	}
	docsRoot := strings.TrimSpace(opts.DocsRoot)
	if docsRoot == "" {
		docsRoot = filepath.Join(workspaceRoot, "docs")
	} else {
		docsRoot = mustAbs(docsRoot)
	}
	info, err := os.Stat(docsRoot)
	if err != nil {
		return DocsBridgeReconcileResult{}, fmt.Errorf("obsidian bridge: stat docs root: %w", err)
	}
	if !info.IsDir() {
		return DocsBridgeReconcileResult{}, fmt.Errorf("obsidian bridge: docs root is not a directory: %s", docsRoot)
	}
	if opts.MaxMatches <= 0 {
		opts.MaxMatches = 5
	}
	folder := strings.TrimSpace(opts.Folder)
	if folder == "" {
		folder = DefaultDocsBridgeDraftFolder(writer.Policy, project)
	}
	if strings.TrimSpace(writer.VaultPath) != "" {
		draftFolder, err := safeVaultChildPath(writer.VaultPath, folder)
		if err != nil {
			return DocsBridgeReconcileResult{}, err
		}
		if err := os.RemoveAll(draftFolder); err != nil {
			return DocsBridgeReconcileResult{}, fmt.Errorf("obsidian bridge: clear draft folder: %w", err)
		}
	}

	if len(opts.ExcludeDocPrefixes) == 0 {
		opts.ExcludeDocPrefixes = []string{"docs/archive/"}
	}
	docs, err := loadBridgeDocs(workspaceRoot, docsRoot, opts.IncludeDocPrefixes, opts.ExcludeDocPrefixes)
	if err != nil {
		return DocsBridgeReconcileResult{}, err
	}
	vaultNotes, err := loadCanonicalBridgeVaultNotes(writer.VaultPath, writer.Policy)
	if err != nil {
		return DocsBridgeReconcileResult{}, err
	}

	result := DocsBridgeReconcileResult{
		Folder:         folder,
		WorkspaceRoot:  workspaceRoot,
		DocsRoot:       docsRoot,
		CanonicalNotes: len(vaultNotes),
	}

	for _, doc := range docs {
		existingVaultRefs := uniqueStrings(doc.FrontLists["vault_refs"])
		existingBackrefs := notesReferencingDoc(vaultNotes, doc.RelPath)
		suggestions := suggestVaultNotes(ctx, doc, vaultNotes, opts.MaxMatches, project, opts.SearchProvider)
		suggestedPaths := make([]string, 0, len(suggestions))
		for _, suggestion := range suggestions {
			suggestedPaths = append(suggestedPaths, suggestion.Path)
		}
		if len(existingVaultRefs) > 0 || len(existingBackrefs) > 0 || len(suggestedPaths) > 0 {
			result.DocsWithLinks++
		} else {
			result.DocsMissing++
		}
		draftPath := filepath.ToSlash(filepath.Join(folder, safeSlug(doc.RelPath)+".md"))
		body := renderDocsBridgeDraft(project, doc, existingVaultRefs, existingBackrefs, suggestions)
		if err := writer.CreateNote(ctx, draftPath, body, true); err != nil {
			return DocsBridgeReconcileResult{}, fmt.Errorf("obsidian bridge: write draft note %s: %w", draftPath, err)
		}
		result.DocNotes = append(result.DocNotes, DocsBridgeDocResult{
			DocPath:                 doc.RelPath,
			Title:                   doc.Title,
			ExistingVaultRefs:       existingVaultRefs,
			ExistingRepoDocBackrefs: existingBackrefs,
			SuggestedVaultRefs:      suggestedPaths,
			DraftPath:               draftPath,
		})
	}

	sort.SliceStable(result.DocNotes, func(i, j int) bool {
		return result.DocNotes[i].DocPath < result.DocNotes[j].DocPath
	})
	result.DocsScanned = len(result.DocNotes)

	rootPath := filepath.ToSlash(filepath.Join(folder, "index.md"))
	rootBody := renderDocsBridgeRoot(project, docsRoot, result.DocNotes)
	if err := writer.CreateNote(ctx, rootPath, rootBody, true); err != nil {
		return DocsBridgeReconcileResult{}, fmt.Errorf("obsidian bridge: write root note: %w", err)
	}
	result.RootNotePath = rootPath

	return result, nil
}

// ApplyDocsBridgeDraft patches only frontmatter list fields from a reviewed bridge draft:
// - repo doc `vault_refs`
// - canonical vault note `repo_docs`
func ApplyDocsBridgeDraft(ctx context.Context, writer *Writer, opts DocsBridgeApplyOptions) (DocsBridgeApplyResult, error) {
	if writer == nil {
		return DocsBridgeApplyResult{}, fmt.Errorf("obsidian bridge: writer required")
	}
	workspaceRoot := mustAbs(strings.TrimSpace(opts.WorkspaceRoot))
	if workspaceRoot == "" {
		return DocsBridgeApplyResult{}, fmt.Errorf("obsidian bridge: workspace root required")
	}
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = filepath.Base(workspaceRoot)
	}
	if project == "" {
		project = "repo"
	}
	draftPath := normalizeVaultPath(opts.DraftPath)
	if draftPath == "" {
		docPath := filepath.ToSlash(strings.TrimSpace(opts.DocPath))
		if docPath == "" {
			return DocsBridgeApplyResult{}, fmt.Errorf("obsidian bridge: draft path or doc path required")
		}
		draftPath = filepath.ToSlash(filepath.Join(DefaultDocsBridgeDraftFolder(writer.Policy, project), safeSlug(docPath)+".md"))
	}
	if !hasVaultPrefix(draftPath, writer.Policy.InboxPrefix) {
		return DocsBridgeApplyResult{}, fmt.Errorf("obsidian bridge: draft path must be under %s", writer.Policy.InboxPrefix)
	}
	draftText, err := writer.Read(ctx, draftPath)
	if err != nil {
		return DocsBridgeApplyResult{}, err
	}
	draft := parseBridgeMarkdown(draftPath, []byte(draftText))
	docPaths := uniqueStrings(draft.FrontLists["repo_docs"])
	if len(docPaths) == 0 {
		return DocsBridgeApplyResult{}, fmt.Errorf("obsidian bridge: draft %s does not declare repo_docs", draftPath)
	}
	docPath := filepath.ToSlash(strings.TrimSpace(docPaths[0]))
	suggested := uniqueStrings(draft.FrontLists["suggested_vault_refs"])
	if len(suggested) == 0 {
		suggested = uniqueStrings(draft.FrontLists["vault_refs"])
	}
	if opts.MaxLinks > 0 && len(suggested) > opts.MaxLinks {
		suggested = suggested[:opts.MaxLinks]
	}

	result := DocsBridgeApplyResult{
		DraftPath:        draftPath,
		RepoDocPath:      docPath,
		VaultRefsApplied: append([]string{}, suggested...),
	}

	repoDocFullPath := filepath.Join(workspaceRoot, filepath.FromSlash(docPath))
	docBody, err := os.ReadFile(repoDocFullPath)
	if err != nil {
		return DocsBridgeApplyResult{}, fmt.Errorf("obsidian bridge: read repo doc %s: %w", docPath, err)
	}
	updatedDoc, changed := mergeMarkdownFrontmatterList(string(docBody), "vault_refs", suggested)
	if changed {
		if err := os.WriteFile(repoDocFullPath, []byte(updatedDoc), 0o644); err != nil {
			return DocsBridgeApplyResult{}, fmt.Errorf("obsidian bridge: write repo doc %s: %w", docPath, err)
		}
		result.RepoDocUpdated = true
	}

	for _, vaultRef := range suggested {
		vaultRef = normalizeVaultPath(vaultRef)
		if vaultRef == "" || !isCanonicalVaultPath(vaultRef, writer.Policy) {
			continue
		}
		text, err := writer.Read(ctx, vaultRef)
		if err != nil {
			return DocsBridgeApplyResult{}, fmt.Errorf("obsidian bridge: read vault note %s: %w", vaultRef, err)
		}
		updated, changed := mergeMarkdownFrontmatterList(text, "repo_docs", []string{docPath})
		if !changed {
			result.VaultNotesPatched = append(result.VaultNotesPatched, vaultRef)
			continue
		}
		if err := writer.writeNoteDirectOrCLI(ctx, vaultRef, updated, true); err != nil {
			return DocsBridgeApplyResult{}, fmt.Errorf("obsidian bridge: write vault note %s: %w", vaultRef, err)
		}
		result.VaultNotesPatched = append(result.VaultNotesPatched, vaultRef)
		result.VaultNotesUpdated++
	}
	return result, nil
}

// ApplyDocsBridgeDrafts applies reviewed bridge drafts in bulk using frontmatter-based filters.
func ApplyDocsBridgeDrafts(ctx context.Context, writer *Writer, opts DocsBridgeBatchApplyOptions) (DocsBridgeBatchApplyResult, error) {
	if writer == nil {
		return DocsBridgeBatchApplyResult{}, fmt.Errorf("obsidian bridge: writer required")
	}
	workspaceRoot := mustAbs(strings.TrimSpace(opts.WorkspaceRoot))
	if workspaceRoot == "" {
		return DocsBridgeBatchApplyResult{}, fmt.Errorf("obsidian bridge: workspace root required")
	}
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = filepath.Base(workspaceRoot)
	}
	if project == "" {
		project = "repo"
	}
	folder := normalizeVaultPath(opts.Folder)
	if folder == "" {
		folder = DefaultDocsBridgeDraftFolder(writer.Policy, project)
	}
	root := filepath.Join(writer.VaultPath, filepath.FromSlash(folder))
	result := DocsBridgeBatchApplyResult{Folder: folder}
	entries := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
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
		return DocsBridgeBatchApplyResult{}, fmt.Errorf("obsidian bridge: walk draft folder: %w", err)
	}
	sort.Strings(entries)
	for _, draftPath := range entries {
		result.Considered++
		text, err := writer.Read(ctx, draftPath)
		if err != nil {
			result.Skipped = append(result.Skipped, DocsBridgeBatchSkip{DraftPath: draftPath, Reason: err.Error()})
			continue
		}
		draft := parseBridgeMarkdown(draftPath, []byte(text))
		docPath := ""
		if docs := uniqueStrings(draft.FrontLists["repo_docs"]); len(docs) > 0 {
			docPath = filepath.ToSlash(strings.TrimSpace(docs[0]))
		}
		if filepath.Base(draftPath) == "index.md" || docPath == "" {
			result.Skipped = append(result.Skipped, DocsBridgeBatchSkip{DraftPath: draftPath, DocPath: docPath, Reason: "not an applyable bridge draft"})
			continue
		}
		if !bridgeDocAllowed(docPath, opts.IncludeDocPrefixes, opts.ExcludeDocPrefixes) {
			result.Skipped = append(result.Skipped, DocsBridgeBatchSkip{DraftPath: draftPath, DocPath: docPath, Reason: "doc path filtered"})
			continue
		}
		requiredStatus := strings.TrimSpace(opts.RequireStatus)
		if requiredStatus != "" && !strings.EqualFold(strings.TrimSpace(draft.FrontValues["status"]), requiredStatus) {
			result.Skipped = append(result.Skipped, DocsBridgeBatchSkip{DraftPath: draftPath, DocPath: docPath, Reason: "status mismatch"})
			continue
		}
		requiredTrust := strings.TrimSpace(opts.RequireTrust)
		if requiredTrust != "" && !strings.EqualFold(strings.TrimSpace(draft.FrontValues["trust"]), requiredTrust) {
			result.Skipped = append(result.Skipped, DocsBridgeBatchSkip{DraftPath: draftPath, DocPath: docPath, Reason: "trust mismatch"})
			continue
		}
		result.Matched++
		applied, err := ApplyDocsBridgeDraft(ctx, writer, DocsBridgeApplyOptions{
			Project:       project,
			WorkspaceRoot: workspaceRoot,
			DraftPath:     draftPath,
			MaxLinks:      opts.MaxLinks,
		})
		if err != nil {
			result.Skipped = append(result.Skipped, DocsBridgeBatchSkip{DraftPath: draftPath, DocPath: docPath, Reason: err.Error()})
			continue
		}
		result.Applied = append(result.Applied, applied)
		if opts.MaxDrafts > 0 && len(result.Applied) >= opts.MaxDrafts {
			break
		}
	}
	return result, nil
}

// ReportDocsBridgeDrafts reports which bridge drafts are raw, reviewed, pending, partial, or fully applied.
func ReportDocsBridgeDrafts(ctx context.Context, writer *Writer, opts DocsBridgeReportOptions) (DocsBridgeReportResult, error) {
	if writer == nil {
		return DocsBridgeReportResult{}, fmt.Errorf("obsidian bridge: writer required")
	}
	workspaceRoot := mustAbs(strings.TrimSpace(opts.WorkspaceRoot))
	if workspaceRoot == "" {
		return DocsBridgeReportResult{}, fmt.Errorf("obsidian bridge: workspace root required")
	}
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = filepath.Base(workspaceRoot)
	}
	if project == "" {
		project = "repo"
	}
	folder := normalizeVaultPath(opts.Folder)
	if folder == "" {
		folder = DefaultDocsBridgeDraftFolder(writer.Policy, project)
	}
	root := filepath.Join(writer.VaultPath, filepath.FromSlash(folder))
	entries := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
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
		return DocsBridgeReportResult{}, fmt.Errorf("obsidian bridge: walk draft folder: %w", err)
	}
	sort.Strings(entries)
	report := DocsBridgeReportResult{Folder: folder}
	for _, draftPath := range entries {
		text, err := writer.Read(ctx, draftPath)
		if err != nil {
			continue
		}
		draft := parseBridgeMarkdown(draftPath, []byte(text))
		docPaths := uniqueStrings(draft.FrontLists["repo_docs"])
		if filepath.Base(draftPath) == "index.md" || len(docPaths) == 0 {
			continue
		}
		docPath := filepath.ToSlash(strings.TrimSpace(docPaths[0]))
		if !bridgeDocAllowed(docPath, opts.IncludeDocPrefixes, opts.ExcludeDocPrefixes) {
			continue
		}
		suggested := uniqueStrings(draft.FrontLists["suggested_vault_refs"])
		if len(suggested) == 0 {
			suggested = uniqueStrings(draft.FrontLists["vault_refs"])
		}
		entry := DocsBridgeDraftStatus{
			DraftPath:          draftPath,
			DocPath:            docPath,
			Status:             strings.TrimSpace(draft.FrontValues["status"]),
			Trust:              strings.TrimSpace(draft.FrontValues["trust"]),
			SuggestedVaultRefs: suggested,
		}
		repoDocFullPath := filepath.Join(workspaceRoot, filepath.FromSlash(docPath))
		if body, err := os.ReadFile(repoDocFullPath); err == nil {
			docNote := parseBridgeMarkdown(repoDocFullPath, body)
			for _, ref := range suggested {
				if containsString(docNote.FrontLists["vault_refs"], ref) {
					entry.RepoDocAppliedRefs = append(entry.RepoDocAppliedRefs, ref)
				}
			}
		}
		for _, ref := range suggested {
			ref = normalizeVaultPath(ref)
			if ref == "" || !isCanonicalVaultPath(ref, writer.Policy) {
				continue
			}
			text, err := writer.Read(ctx, ref)
			if err != nil {
				entry.MissingVaultNotes = append(entry.MissingVaultNotes, ref)
				continue
			}
			note := parseBridgeMarkdown(ref, []byte(text))
			if containsString(note.FrontLists["repo_docs"], docPath) {
				entry.VaultNotesPatched = append(entry.VaultNotesPatched, ref)
			}
		}
		entry.State = classifyBridgeDraftState(entry)
		report.Total++
		switch entry.State {
		case "draft":
			report.Draft++
		case "reviewed":
			report.Reviewed++
		case "applied":
			report.Applied++
		case "partial":
			report.Partial++
		default:
			report.Pending++
		}
		report.Entries = append(report.Entries, entry)
	}
	return report, nil
}

// TidyDocsBridgeDrafts archives fully applied bridge drafts out of the inbox.
func TidyDocsBridgeDrafts(ctx context.Context, writer *Writer, opts DocsBridgeTidyOptions) (DocsBridgeTidyResult, error) {
	if writer == nil {
		return DocsBridgeTidyResult{}, fmt.Errorf("obsidian bridge: writer required")
	}
	if strings.TrimSpace(writer.VaultPath) == "" {
		return DocsBridgeTidyResult{}, fmt.Errorf("obsidian bridge: vault path required for tidy")
	}
	workspaceRoot := mustAbs(strings.TrimSpace(opts.WorkspaceRoot))
	if workspaceRoot == "" {
		return DocsBridgeTidyResult{}, fmt.Errorf("obsidian bridge: workspace root required")
	}
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = filepath.Base(workspaceRoot)
	}
	if project == "" {
		project = "repo"
	}
	folder := normalizeVaultPath(opts.Folder)
	if folder == "" {
		folder = DefaultDocsBridgeDraftFolder(writer.Policy, project)
	}
	archiveFolder := normalizeVaultPath(opts.ArchiveFolder)
	if archiveFolder == "" {
		archiveFolder = DefaultDocsBridgeArchiveFolder(writer.Policy, project)
	}
	report, err := ReportDocsBridgeDrafts(ctx, writer, DocsBridgeReportOptions{
		Project:            project,
		WorkspaceRoot:      workspaceRoot,
		Folder:             folder,
		IncludeDocPrefixes: opts.IncludeDocPrefixes,
		ExcludeDocPrefixes: opts.ExcludeDocPrefixes,
	})
	if err != nil {
		return DocsBridgeTidyResult{}, err
	}
	result := DocsBridgeTidyResult{
		Folder:        folder,
		ArchiveFolder: archiveFolder,
	}
	for _, entry := range report.Entries {
		result.Considered++
		if entry.State != "applied" {
			result.Skipped = append(result.Skipped, DocsBridgeTidySkip{
				DraftPath: entry.DraftPath,
				DocPath:   entry.DocPath,
				Reason:    "state is not applied",
			})
			continue
		}
		text, err := writer.Read(ctx, entry.DraftPath)
		if err != nil {
			result.Skipped = append(result.Skipped, DocsBridgeTidySkip{
				DraftPath: entry.DraftPath,
				DocPath:   entry.DocPath,
				Reason:    err.Error(),
			})
			continue
		}
		archived := setMarkdownFrontmatterValues(text, map[string]string{
			"status":      "applied",
			"trust":       "reviewed",
			"archived_at": time.Now().UTC().Format(time.RFC3339),
		})
		archivePath := filepath.ToSlash(filepath.Join(archiveFolder, filepath.Base(entry.DraftPath)))
		if err := writer.CreateNote(ctx, archivePath, archived, true); err != nil {
			return DocsBridgeTidyResult{}, fmt.Errorf("obsidian bridge: archive draft %s: %w", entry.DraftPath, err)
		}
		if err := os.Remove(filepath.Join(writer.VaultPath, filepath.FromSlash(entry.DraftPath))); err != nil && !os.IsNotExist(err) {
			return DocsBridgeTidyResult{}, fmt.Errorf("obsidian bridge: remove draft %s: %w", entry.DraftPath, err)
		}
		entry.DraftPath = archivePath
		entry.Status = "applied"
		entry.Trust = "reviewed"
		result.Archived = append(result.Archived, entry)
		if opts.MaxDrafts > 0 && len(result.Archived) >= opts.MaxDrafts {
			break
		}
	}
	return result, nil
}

func classifyBridgeDraftState(entry DocsBridgeDraftStatus) string {
	status := strings.ToLower(strings.TrimSpace(entry.Status))
	if status != "reviewed" {
		return "draft"
	}
	total := len(entry.SuggestedVaultRefs)
	if total == 0 {
		return "reviewed"
	}
	repoApplied := len(entry.RepoDocAppliedRefs)
	vaultApplied := len(entry.VaultNotesPatched)
	switch {
	case repoApplied == total && vaultApplied == total:
		return "applied"
	case repoApplied > 0 || vaultApplied > 0:
		return "partial"
	default:
		return "reviewed"
	}
}

func containsString(values []string, target string) bool {
	target = filepath.ToSlash(strings.TrimSpace(target))
	for _, value := range values {
		if filepath.ToSlash(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

func loadBridgeDocs(workspaceRoot, docsRoot string, includePrefixes, excludePrefixes []string) ([]bridgeDoc, error) {
	workspaceRoot = mustAbs(workspaceRoot)
	docsRoot = mustAbs(docsRoot)
	var docs []bridgeDoc
	err := filepath.WalkDir(docsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		doc := parseBridgeMarkdown(path, body)
		rel, err := filepath.Rel(workspaceRoot, mustAbs(path))
		if err != nil {
			return err
		}
		doc.RelPath = filepath.ToSlash(rel)
		if !bridgeDocAllowed(doc.RelPath, includePrefixes, excludePrefixes) {
			return nil
		}
		docs = append(docs, doc)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("obsidian bridge: walk docs: %w", err)
	}
	sort.SliceStable(docs, func(i, j int) bool {
		return docs[i].RelPath < docs[j].RelPath
	})
	return docs, nil
}

func bridgeDocAllowed(path string, includePrefixes, excludePrefixes []string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	if len(includePrefixes) > 0 {
		allowed := false
		for _, prefix := range includePrefixes {
			prefix = filepath.ToSlash(strings.TrimSpace(prefix))
			if prefix != "" && strings.HasPrefix(path, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	for _, prefix := range excludePrefixes {
		prefix = filepath.ToSlash(strings.TrimSpace(prefix))
		if prefix != "" && strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return true
}

func loadCanonicalBridgeVaultNotes(vaultRoot string, policy Policy) ([]bridgeVaultNote, error) {
	vaultRoot = mustAbs(vaultRoot)
	if strings.TrimSpace(vaultRoot) == "" {
		return nil, fmt.Errorf("obsidian bridge: vault path required")
	}
	var notes []bridgeVaultNote
	err := filepath.WalkDir(vaultRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".obsidian" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(vaultRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !isCanonicalVaultPath(rel, policy) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		note := parseBridgeMarkdown(path, body)
		notes = append(notes, bridgeVaultNote{
			Path:        rel,
			Title:       note.Title,
			FrontValues: note.FrontValues,
			FrontLists:  note.FrontLists,
			Body:        note.Body,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("obsidian bridge: walk canonical notes: %w", err)
	}
	sort.SliceStable(notes, func(i, j int) bool {
		return notes[i].Path < notes[j].Path
	})
	return notes, nil
}

func mustAbs(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(abs)
}

func isCanonicalVaultPath(path string, policy Policy) bool {
	path = normalizeVaultPath(path)
	for _, prefix := range policy.CanonicalPrefixes {
		if hasVaultPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func notesReferencingDoc(notes []bridgeVaultNote, docPath string) []string {
	var out []string
	for _, note := range notes {
		for _, candidate := range note.FrontLists["repo_docs"] {
			if filepath.ToSlash(strings.TrimSpace(candidate)) == filepath.ToSlash(strings.TrimSpace(docPath)) {
				out = append(out, note.Path)
				break
			}
		}
	}
	return uniqueStrings(out)
}

func suggestVaultNotes(ctx context.Context, doc bridgeDoc, notes []bridgeVaultNote, limit int, project string, provider DocsBridgeSearchProvider) []bridgeSuggestion {
	if limit <= 0 {
		limit = 5
	}
	queryTokens := bridgeQueryTokens(doc, project)
	if len(queryTokens) == 0 {
		return nil
	}
	docCategory := bridgeDocCategory(doc.RelPath)
	providerBoosts := map[string]int{}
	if provider != nil {
		if hits, err := provider.SearchBridgeCandidates(ctx, bridgeSearchQuery(doc), limit*3); err == nil {
			maxScore := 0
			for _, hit := range hits {
				if hit.Score > maxScore {
					maxScore = hit.Score
				}
			}
			if maxScore > 0 {
				for _, hit := range hits {
					path := filepath.ToSlash(strings.TrimSpace(hit.Path))
					if path == "" {
						continue
					}
					providerBoosts[path] = int((float64(hit.Score) / float64(maxScore)) * 30.0)
				}
			}
		}
	}
	suggestions := make([]bridgeSuggestion, 0, len(notes))
	for _, note := range notes {
		score := scoreBridgeNote(note, doc.RelPath, queryTokens, docCategory) + providerBoosts[filepath.ToSlash(note.Path)]
		if score <= 0 {
			continue
		}
		suggestions = append(suggestions, bridgeSuggestion{
			Path:  note.Path,
			Title: note.Title,
			Score: score,
		})
	}
	sort.SliceStable(suggestions, func(i, j int) bool {
		if suggestions[i].Score != suggestions[j].Score {
			return suggestions[i].Score > suggestions[j].Score
		}
		return suggestions[i].Path < suggestions[j].Path
	})
	if len(suggestions) > limit {
		suggestions = suggestions[:limit]
	}
	return suggestions
}

func bridgeSearchQuery(doc bridgeDoc) string {
	parts := []string{strings.TrimSpace(doc.Title)}
	headings := firstBridgeHeadings(doc.Body, 2)
	for _, heading := range headings {
		trimmed := strings.TrimSpace(heading)
		if trimmed != "" && !strings.EqualFold(trimmed, strings.TrimSpace(doc.Title)) {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}

func scoreBridgeNote(note bridgeVaultNote, docPath string, tokens []string, docCategory string) int {
	docPath = filepath.ToSlash(strings.TrimSpace(docPath))
	score := 0
	for _, repoDoc := range note.FrontLists["repo_docs"] {
		if filepath.ToSlash(strings.TrimSpace(repoDoc)) == docPath {
			score += 50
		}
	}
	titleLower := strings.ToLower(note.Title)
	pathLower := strings.ToLower(note.Path)
	bodyLower := strings.ToLower(note.Body)
	titleHits := 0
	pathHits := 0
	bodyHits := 0
	for _, token := range tokens {
		switch {
		case strings.Contains(titleLower, token):
			titleHits++
		case strings.Contains(pathLower, token):
			pathHits++
		case strings.Contains(bodyLower, token):
			bodyHits++
		}
	}
	score += titleHits * 9
	score += pathHits * 4
	score += bodyHits * 2

	noteKind := bridgeNoteKind(note.Path)
	score += bridgeCategoryWeight(docCategory, noteKind)
	if noteKind == "package" && titleHits == 0 && bodyHits < 2 {
		score -= 12
	}
	if noteKind == "topical" && (titleHits > 0 || bodyHits > 0) {
		score += 6
	}
	if noteKind == "root" && docCategory != "package" {
		score += 5
	}

	return score
}

func bridgeQueryTokens(doc bridgeDoc, project string) []string {
	var values []string
	values = append(values, tokenizeBridgeText(doc.Title, project)...)
	values = append(values, tokenizeBridgeText(strings.TrimSuffix(filepath.Base(doc.RelPath), filepath.Ext(doc.RelPath)), project)...)
	for _, heading := range firstBridgeHeadings(doc.Body, 2) {
		values = append(values, tokenizeBridgeText(heading, project)...)
	}
	return uniqueStrings(values)
}

func firstBridgeHeadings(body string, limit int) []string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, limit)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if title == "" {
			continue
		}
		out = append(out, title)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func tokenizeBridgeText(text string, project string) []string {
	stopwords := bridgeStopwords(project)
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 3 {
			continue
		}
		if _, ok := stopwords[field]; ok {
			continue
		}
		out = append(out, field)
	}
	return out
}

func bridgeStopwords(project string) map[string]struct{} {
	stopwords := map[string]struct{}{
		"docs":          {},
		"doc":           {},
		"documentation": {},
		"readme":        {},
		"guide":         {},
		"current":       {},
		"canonical":     {},
		"moved":         {},
		"overview":      {},
		"general":       {},
		"start":         {},
		"here":          {},
		"policy":        {},
		"index":         {},
	}
	for _, token := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(project)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if len(token) >= 3 {
			stopwords[token] = struct{}{}
		}
	}
	return stopwords
}

func bridgeDocCategory(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	switch {
	case strings.HasPrefix(path, "docs/architecture/"):
		return "architecture"
	case strings.HasPrefix(path, "docs/general/"):
		return "general"
	case strings.HasPrefix(path, "docs/start/"):
		return "start"
	case strings.HasSuffix(path, "/README.md") || path == "docs/README.md":
		return "overview"
	default:
		return "package"
	}
}

func bridgeNoteKind(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	switch {
	case strings.HasPrefix(path, "notes/repo/") && strings.HasSuffix(path, "/index.md"):
		return "root"
	case strings.HasPrefix(path, "notes/repo/") && strings.Contains(path, "/packages/"):
		return "package"
	case strings.HasPrefix(path, "notes/repo/"):
		return "topical"
	case path == "00-home/index.md":
		return "home"
	case strings.HasPrefix(path, "atlas/"):
		return "atlas"
	default:
		return "other"
	}
}

func bridgeCategoryWeight(docCategory, noteKind string) int {
	switch docCategory {
	case "architecture", "overview", "general", "start":
		switch noteKind {
		case "topical":
			return 14
		case "root":
			return 11
		case "home":
			return 8
		case "atlas":
			return 4
		case "package":
			return -4
		default:
			return 0
		}
	default:
		switch noteKind {
		case "package":
			return 4
		case "topical":
			return 6
		case "root":
			return 4
		case "home":
			return 2
		default:
			return 0
		}
	}
}

func parseBridgeMarkdown(path string, body []byte) bridgeDoc {
	text := string(body)
	frontLists, frontValues, remaining, _ := parseBridgeFrontmatter(text)
	title := ""
	if rawTitle := strings.TrimSpace(frontValues["title"]); rawTitle != "" {
		title = rawTitle
	}
	if title == "" {
		title = bridgeTitleFromContent(path, remaining)
	}
	return bridgeDoc{
		Title:       title,
		FrontValues: frontValues,
		FrontLists:  frontLists,
		Body:        remaining,
	}
}

func parseBridgeFrontmatter(text string) (map[string][]string, map[string]string, string, bool) {
	if !strings.HasPrefix(text, "---\n") {
		return map[string][]string{}, map[string]string{}, text, false
	}
	parts := strings.SplitN(text, "\n---\n", 2)
	if len(parts) != 2 {
		return map[string][]string{}, map[string]string{}, text, false
	}
	lists := map[string][]string{}
	values := map[string]string{}
	current := ""
	for _, line := range strings.Split(parts[0], "\n") {
		raw := strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || trimmed == "---" {
			continue
		}
		if current != "" && strings.HasPrefix(trimmed, "-") {
			item := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")), `"'`)
			lists[current] = append(lists[current], item)
			continue
		}
		current = ""
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if value != "" {
			values[key] = strings.Trim(value, `"'`)
		}
		switch key {
		case "repo_docs", "vault_refs", "suggested_vault_refs":
			if value == "" {
				current = key
				continue
			}
			lists[key] = append(lists[key], parseBridgeInlineList(value)...)
		}
	}
	for key, values := range lists {
		lists[key] = uniqueStrings(values)
	}
	return lists, values, parts[1], true
}

func mergeMarkdownFrontmatterList(markdown, key string, additions []string) (string, bool) {
	additions = uniqueStrings(additions)
	if len(additions) == 0 {
		return markdown, false
	}
	existing, _, body, hasFrontmatter := parseBridgeFrontmatter(markdown)
	current := uniqueStrings(existing[key])
	merged := uniqueStrings(append(append([]string{}, current...), additions...))
	if slicesEqualStrings(current, merged) {
		return markdown, false
	}
	if !hasFrontmatter {
		var b strings.Builder
		b.WriteString("---\n")
		writeFrontmatterList(&b, key, merged)
		b.WriteString("---\n\n")
		b.WriteString(strings.TrimLeft(markdown, "\n"))
		return strings.TrimRight(b.String(), "\n") + "\n", true
	}
	frontmatterText, _ := splitMarkdownFrontmatter(markdown)
	lines := strings.Split(frontmatterText, "\n")
	out := make([]string, 0, len(lines)+len(merged)+4)
	replaced := false
	skippingTarget := false
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "---" {
			if !skippingTarget {
				out = append(out, line)
			}
			continue
		}
		if skippingTarget {
			if strings.HasPrefix(strings.TrimLeft(line, " "), "-") {
				continue
			}
			if !strings.HasPrefix(line, " ") && strings.Contains(trimmed, ":") {
				skippingTarget = false
			} else {
				continue
			}
		}
		k, _, ok := strings.Cut(trimmed, ":")
		if ok && strings.ToLower(strings.TrimSpace(k)) == key {
			writeFrontmatterListLines(&out, key, merged)
			replaced = true
			skippingTarget = true
			continue
		}
		out = append(out, line)
	}
	if !replaced {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "---" && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		writeFrontmatterListLines(&out, key, merged)
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(strings.Join(out, "\n"), "\n"))
	b.WriteString("\n---\n")
	b.WriteString(strings.TrimLeft(body, "\n"))
	return strings.TrimRight(b.String(), "\n") + "\n", true
}

func setMarkdownFrontmatterValues(markdown string, values map[string]string) string {
	if len(values) == 0 {
		return markdown
	}
	pairs := orderedFrontmatterValues(values)
	_, _, body, hasFrontmatter := parseBridgeFrontmatter(markdown)
	if !hasFrontmatter {
		var b strings.Builder
		b.WriteString("---\n")
		for _, pair := range pairs {
			b.WriteString(pair.key + ": " + pair.value + "\n")
		}
		b.WriteString("---\n\n")
		b.WriteString(strings.TrimLeft(markdown, "\n"))
		return strings.TrimRight(b.String(), "\n") + "\n"
	}
	frontmatterText, _ := splitMarkdownFrontmatter(markdown)
	lines := strings.Split(frontmatterText, "\n")
	out := make([]string, 0, len(lines)+len(values))
	seen := map[string]struct{}{}
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "---" {
			out = append(out, line)
			continue
		}
		key, _, ok := strings.Cut(trimmed, ":")
		if !ok {
			out = append(out, line)
			continue
		}
		normalized := strings.ToLower(strings.TrimSpace(key))
		if value, ok := values[normalized]; ok {
			out = append(out, normalized+": "+value)
			seen[normalized] = struct{}{}
			continue
		}
		out = append(out, line)
	}
	for _, pair := range pairs {
		if _, ok := seen[pair.key]; ok {
			continue
		}
		out = append(out, pair.key+": "+pair.value)
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(strings.Join(out, "\n"), "\n"))
	b.WriteString("\n---\n")
	b.WriteString(strings.TrimLeft(body, "\n"))
	return strings.TrimRight(b.String(), "\n") + "\n"
}

type frontmatterValuePair struct {
	key   string
	value string
}

func orderedFrontmatterValues(values map[string]string) []frontmatterValuePair {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, strings.ToLower(strings.TrimSpace(key)))
	}
	sort.Strings(keys)
	out := make([]frontmatterValuePair, 0, len(keys))
	for _, key := range keys {
		out = append(out, frontmatterValuePair{
			key:   key,
			value: strings.TrimSpace(values[key]),
		})
	}
	return out
}

func splitMarkdownFrontmatter(markdown string) (string, string) {
	if !strings.HasPrefix(markdown, "---\n") {
		return "", markdown
	}
	parts := strings.SplitN(markdown, "\n---\n", 2)
	if len(parts) != 2 {
		return "", markdown
	}
	return parts[0], parts[1]
}

func writeFrontmatterList(b *strings.Builder, key string, values []string) {
	b.WriteString(key)
	b.WriteString(":\n")
	for _, value := range values {
		b.WriteString("  - ")
		b.WriteString(value)
		b.WriteString("\n")
	}
}

func writeFrontmatterListLines(out *[]string, key string, values []string) {
	*out = append(*out, key+":")
	for _, value := range values {
		*out = append(*out, "  - "+value)
	}
}

func slicesEqualStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func parseBridgeInlineList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || value == "[]" {
		return nil
	}
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.Trim(strings.TrimSpace(part), `"'`)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func bridgeTitleFromContent(path, body string) string {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			if title != "" {
				return title
			}
		}
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if base == "" {
		return "Doc Bridge"
	}
	return base
}

func renderDocsBridgeDraft(project string, doc bridgeDoc, existingVaultRefs, existingBackrefs []string, suggestions []bridgeSuggestion) string {
	title := doc.Title + " Bridge"
	suggestedPaths := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		suggestedPaths = append(suggestedPaths, suggestion.Path)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + title + "\n")
	b.WriteString("type: map\n")
	b.WriteString("project: " + strings.TrimSpace(project) + "\n")
	b.WriteString("status: draft\n")
	b.WriteString("trust: raw\n")
	b.WriteString("repo_docs:\n")
	b.WriteString("  - " + doc.RelPath + "\n")
	b.WriteString("vault_refs:\n")
	refs := uniqueStrings(append(append([]string{}, existingVaultRefs...), existingBackrefs...))
	for _, ref := range refs {
		b.WriteString("  - " + ref + "\n")
	}
	b.WriteString("suggested_vault_refs:\n")
	for _, ref := range suggestedPaths {
		b.WriteString("  - " + ref + "\n")
	}
	b.WriteString("updated: " + time.Now().UTC().Format("2006-01-02") + "\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + title + "\n\n")
	b.WriteString("## Repo Doc\n\n")
	b.WriteString("- Path: `" + doc.RelPath + "`\n")
	b.WriteString("- Title: `" + doc.Title + "`\n\n")

	b.WriteString("## Existing Links\n\n")
	if len(existingVaultRefs) == 0 {
		b.WriteString("- Repo doc frontmatter has no `vault_refs` entries.\n")
	} else {
		for _, ref := range existingVaultRefs {
			b.WriteString("- Repo doc `vault_refs`: `" + ref + "`\n")
		}
	}
	if len(existingBackrefs) == 0 {
		b.WriteString("- No canonical vault notes currently declare this doc in `repo_docs`.\n")
	} else {
		for _, ref := range existingBackrefs {
			b.WriteString("- Vault note `repo_docs` backlink: `" + ref + "`\n")
		}
	}

	b.WriteString("\n## Suggested Vault Links\n\n")
	if len(suggestions) == 0 {
		b.WriteString("- No strong canonical vault matches found.\n")
	} else {
		for _, suggestion := range suggestions {
			b.WriteString("- " + renderVaultWikiLink(suggestion.Path, suggestion.Title) + " (`" + suggestion.Path + "`, score " + fmt.Sprintf("%d", suggestion.Score) + ")\n")
		}
	}

	b.WriteString("\n## Suggested Repo Frontmatter Patch\n\n")
	b.WriteString("```yaml\n")
	b.WriteString("vault_refs:\n")
	for _, ref := range suggestedRefPaths(existingVaultRefs, suggestions) {
		b.WriteString("  - " + ref + "\n")
	}
	b.WriteString("```\n\n")

	b.WriteString("## Suggested Vault Frontmatter Patches\n\n")
	if len(suggestions) == 0 {
		b.WriteString("- No vault patch suggestions.\n")
	} else {
		for _, suggestion := range suggestions {
			b.WriteString("### `" + suggestion.Path + "`\n\n")
			b.WriteString("```yaml\n")
			b.WriteString("repo_docs:\n")
			b.WriteString("  - " + doc.RelPath + "\n")
			b.WriteString("```\n\n")
		}
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

func renderDocsBridgeRoot(project, docsRoot string, docs []DocsBridgeDocResult) string {
	var linked, missing int
	for _, doc := range docs {
		if len(doc.ExistingVaultRefs) > 0 || len(doc.ExistingRepoDocBackrefs) > 0 || len(doc.SuggestedVaultRefs) > 0 {
			linked++
		} else {
			missing++
		}
	}
	var b strings.Builder
	title := strings.TrimSpace(project) + " Docs Bridge"
	if strings.TrimSpace(project) == "" {
		title = "Docs Bridge"
	}
	b.WriteString("---\n")
	b.WriteString("title: " + title + "\n")
	b.WriteString("type: map\n")
	b.WriteString("project: " + strings.TrimSpace(project) + "\n")
	b.WriteString("status: draft\n")
	b.WriteString("trust: raw\n")
	b.WriteString("updated: " + time.Now().UTC().Format("2006-01-02") + "\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + title + "\n\n")
	b.WriteString("- Docs root: `" + filepath.ToSlash(docsRoot) + "`\n")
	b.WriteString("- Docs scanned: `" + fmt.Sprintf("%d", len(docs)) + "`\n")
	b.WriteString("- Docs with links or suggestions: `" + fmt.Sprintf("%d", linked) + "`\n")
	b.WriteString("- Docs missing bridge candidates: `" + fmt.Sprintf("%d", missing) + "`\n\n")
	b.WriteString("## Bridge Drafts\n\n")
	for _, doc := range docs {
		b.WriteString("- " + renderVaultWikiLink(doc.DraftPath, doc.Title+" Bridge") + " for `" + doc.DocPath + "`\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func renderVaultWikiLink(path, title string) string {
	target := strings.TrimSuffix(filepath.ToSlash(path), filepath.Ext(path))
	if strings.TrimSpace(title) == "" {
		return "[[" + target + "]]"
	}
	return "[[" + target + "|" + strings.TrimSpace(title) + "]]"
}

func safeVaultChildPath(vaultRoot, child string) (string, error) {
	vaultRoot = mustAbs(strings.TrimSpace(vaultRoot))
	child = strings.TrimSpace(filepath.ToSlash(child))
	if vaultRoot == "" {
		return "", fmt.Errorf("obsidian bridge: vault root required")
	}
	if child == "" || child == "." || child == "/" {
		return "", fmt.Errorf("obsidian bridge: unsafe draft folder %q", child)
	}
	target := mustAbs(filepath.Join(vaultRoot, filepath.FromSlash(child)))
	rel, err := filepath.Rel(vaultRoot, target)
	if err != nil {
		return "", fmt.Errorf("obsidian bridge: resolve draft folder: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("obsidian bridge: draft folder escapes vault root: %s", child)
	}
	return target, nil
}

func suggestedRefPaths(existing []string, suggestions []bridgeSuggestion) []string {
	out := append([]string{}, existing...)
	for _, suggestion := range suggestions {
		out = append(out, suggestion.Path)
	}
	return uniqueStrings(out)
}
