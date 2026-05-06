package cmd

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semanticanchors"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newIndexAnchorsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "anchors",
		Short: "Lint and explain semantic code anchors",
	}
	cmd.AddCommand(newIndexAnchorsLintCommand(), newIndexAnchorsExplainCommand())
	return cmd
}

func newIndexAnchorsLintCommand() *cobra.Command {
	var workspace string
	var summaryOnly bool
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Lint semantic anchors in source-code comments",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexAnchors(cmd, workspace, "", anchorReportOptions{SummaryOnly: summaryOnly})
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().BoolVar(&summaryOnly, "summary", false, "Emit counts and findings without per-file occurrence details")
	return cmd
}

func newIndexAnchorsExplainCommand() *cobra.Command {
	var workspace string
	var path string
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Explain semantic anchors for one source file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runIndexAnchors(cmd, workspace, path, anchorReportOptions{})
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root directory")
	cmd.Flags().StringVar(&path, "path", "", "Repo-relative source path to explain")
	_ = cmd.MarkFlagRequired("path")
	return cmd
}

type anchorReportOptions struct {
	SummaryOnly bool
}

func runIndexAnchors(cmd *cobra.Command, workspace, onlyPath string, opts anchorReportOptions) error {
	ctx := cmd.Context()
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}
	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	store, err := repoindex.Open(ctx, cfg.Storage.Root, absWorkspace)
	if err != nil {
		return fmt.Errorf("open repoindex store: %w", err)
	}
	defer store.Close()

	resolver, err := newCLIAnchorOwnerResolver(ctx, store)
	if err != nil {
		return fmt.Errorf("load repoindex owners: %w", err)
	}
	policy := semanticanchors.DefaultAnchorPolicy(store.RepoKey(), nil)
	targets := cliAnchorTargetResolver{workspace: absWorkspace}
	paths, err := anchorSourcePaths(absWorkspace, onlyPath)
	if err != nil {
		return err
	}

	files := make([]anchorFileReport, 0, len(paths))
	findingSummary := newAnchorFindingSummary()
	bindingSummary := anchorOwnerBindingSummary{}
	for _, relPath := range paths {
		src, err := os.ReadFile(filepath.Join(absWorkspace, filepath.FromSlash(relPath)))
		if err != nil {
			return fmt.Errorf("read %s: %w", relPath, err)
		}
		extracted, err := semanticanchors.ExtractAnchorsFromSource(ctx, policy, resolver, relPath, src)
		if err != nil {
			return fmt.Errorf("extract anchors from %s: %w", relPath, err)
		}
		var report anchorFileReport
		if !opts.SummaryOnly {
			report = anchorFileReport{
				Path:     relPath,
				Language: extracted.Language,
				Support:  extracted.Support,
				Findings: extracted.Findings,
			}
		}
		findingSummary.AddAll(extracted.Findings)
		for _, occ := range extracted.Occurrences {
			item := explainAnchorOccurrence(ctx, policy, targets, occ)
			if !opts.SummaryOnly {
				report.Occurrences = append(report.Occurrences, item)
			}
			findingSummary.AddAll(item.Findings)
			bindingSummary.Add(item)
		}
		if !opts.SummaryOnly && (len(report.Occurrences) > 0 || len(report.Findings) > 0 || onlyPath != "") {
			files = append(files, report)
		}
	}

	command := "index.anchors.lint"
	if onlyPath != "" {
		command = "index.anchors.explain"
	}
	data := map[string]any{
		"workspace":             absWorkspace,
		"files":                 files,
		"file_count":            len(files),
		"scanned_file_count":    len(paths),
		"finding_count":         findingSummary.Total,
		"finding_summary":       findingSummary,
		"owner_index":           resolver.Summary(store),
		"owner_binding_summary": bindingSummary,
		"evidence_authority":    "evidence_only",
		"permitted_use":         []string{"retrieval_ranking", "review_signal"},
		"instruction_eligible":  false,
		"summary_only":          opts.SummaryOnly,
	}
	if onlyPath != "" {
		data["path"] = paths[0]
	}
	env := protocol.OK(command, data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	return protocol.Write(cmd.OutOrStdout(), env)
}

type anchorFileReport struct {
	Path        string                                `json:"path"`
	Language    string                                `json:"language,omitempty"`
	Support     semanticanchors.LanguageAnchorSupport `json:"support,omitempty"`
	Occurrences []anchorOccurrenceReport              `json:"occurrences,omitempty"`
	Findings    []semanticanchors.Finding             `json:"findings,omitempty"`
}

type anchorOccurrenceReport struct {
	Type                   semanticanchors.AnchorType             `json:"type"`
	Scope                  semanticanchors.AnchorScope            `json:"scope"`
	DisplaySyntax          string                                 `json:"display_syntax,omitempty"`
	TargetDisplay          string                                 `json:"target_display,omitempty"`
	TargetID               semanticanchors.AnchorTargetID         `json:"target_id,omitempty"`
	RepoScopedKey          semanticanchors.RepoScopedAnchorKey    `json:"repo_scoped_key"`
	WouldBeRepoindexNodeID string                                 `json:"would_be_repoindex_node_id,omitempty"`
	LineStart              int                                    `json:"line_start,omitempty"`
	LineEnd                int                                    `json:"line_end,omitempty"`
	OwnerNodeID            string                                 `json:"owner_node_id,omitempty"`
	OwnerKind              string                                 `json:"owner_kind,omitempty"`
	OwnerStableKey         string                                 `json:"owner_stable_key,omitempty"`
	ValidationStatus       semanticanchors.AnchorValidationStatus `json:"validation_status"`
	EdgeAction             semanticanchors.AnchorEdgeAction       `json:"edge_action"`
	Relation               semanticanchors.SemanticAnchorRelation `json:"relation,omitempty"`
	IntendedRelation       semanticanchors.SemanticAnchorRelation `json:"intended_relation,omitempty"`
	SourceHash             string                                 `json:"source_hash,omitempty"`
	OccurrenceID           string                                 `json:"occurrence_id,omitempty"`
	EvidenceAuthority      string                                 `json:"evidence_authority"`
	PermittedUse           []string                               `json:"permitted_use"`
	InstructionEligible    bool                                   `json:"instruction_eligible"`
	IndexingNote           string                                 `json:"indexing_note,omitempty"`
	Findings               []semanticanchors.Finding              `json:"findings,omitempty"`
}

type anchorFindingSummary struct {
	Total      int            `json:"total"`
	ByReason   map[string]int `json:"by_reason,omitempty"`
	BySeverity map[string]int `json:"by_severity,omitempty"`
}

func newAnchorFindingSummary() anchorFindingSummary {
	return anchorFindingSummary{
		ByReason:   map[string]int{},
		BySeverity: map[string]int{},
	}
}

func (s *anchorFindingSummary) AddAll(findings []semanticanchors.Finding) {
	for _, finding := range findings {
		s.Total++
		if finding.Reason != "" {
			s.ByReason[string(finding.Reason)]++
		}
		if finding.Severity != "" {
			s.BySeverity[string(finding.Severity)]++
		}
	}
}

type anchorOwnerBindingSummary struct {
	OccurrenceCount        int `json:"occurrence_count"`
	BoundOccurrenceCount   int `json:"bound_occurrence_count"`
	UnboundOccurrenceCount int `json:"unbound_occurrence_count"`
	UnsupportedOwnerCount  int `json:"unsupported_owner_count"`
}

func (s *anchorOwnerBindingSummary) Add(item anchorOccurrenceReport) {
	s.OccurrenceCount++
	if item.OwnerNodeID != "" {
		s.BoundOccurrenceCount++
		return
	}
	s.UnboundOccurrenceCount++
	for _, finding := range item.Findings {
		if finding.Reason == semanticanchors.AnchorFindingUnsupportedOwner {
			s.UnsupportedOwnerCount++
			return
		}
	}
}

func explainAnchorOccurrence(ctx context.Context, policy semanticanchors.AnchorPolicy, targets semanticanchors.TargetResolver, occ semanticanchors.AnchorOccurrence) anchorOccurrenceReport {
	res := semanticanchors.AnchorResolution{
		Occurrence:       occ,
		Relation:         semanticanchors.SemanticAnchorRelationDeclaresTarget,
		IntendedRelation: semanticanchors.SemanticAnchorRelationDeclaresTarget,
		EdgeAction:       semanticanchors.AnchorEdgeNone,
	}
	if occ.OwnerBinding.OwnerNodeID != "" {
		if resolved, err := semanticanchors.ResolveAnchorOccurrence(ctx, policy, occ, targets); err == nil {
			res = resolved
			occ = resolved.Occurrence
		}
	}
	displaySyntax := occ.DisplaySyntax
	targetDisplay := occ.TargetDisplay
	if anchorNeedsRedaction(occ.Findings) {
		reason := firstAnchorRedactionReason(occ.Findings)
		displaySyntax = "[[redacted:" + string(reason) + "]]"
		targetDisplay = "[redacted:" + string(reason) + "]"
	}
	var nodeID string
	if occ.TargetID != "" {
		if id, err := semanticanchors.AnchorTargetNodeID(policy.RepoKey, occ.TargetID); err == nil {
			nodeID = string(id)
		}
	}
	return anchorOccurrenceReport{
		Type:                   occ.Type,
		Scope:                  occ.Scope,
		DisplaySyntax:          displaySyntax,
		TargetDisplay:          targetDisplay,
		TargetID:               occ.TargetID,
		RepoScopedKey:          semanticanchors.RepoScopedAnchorKeyFor(policy.RepoKey, occ.TargetID),
		WouldBeRepoindexNodeID: nodeID,
		LineStart:              occ.Span.LineStart,
		LineEnd:                occ.Span.LineEnd,
		OwnerNodeID:            occ.OwnerBinding.OwnerNodeID,
		OwnerKind:              occ.OwnerBinding.OwnerKind,
		OwnerStableKey:         occ.OwnerBinding.OwnerStableKey,
		ValidationStatus:       occ.ValidationStatus,
		EdgeAction:             res.EdgeAction,
		Relation:               res.Relation,
		IntendedRelation:       res.IntendedRelation,
		SourceHash:             occ.SourceHash,
		OccurrenceID:           occ.OccurrenceID,
		EvidenceAuthority:      "evidence_only",
		PermittedUse:           []string{"retrieval_ranking", "review_signal"},
		InstructionEligible:    false,
		IndexingNote:           anchorIndexingNote(occ, res),
		Findings:               occ.Findings,
	}
}

func anchorIndexingNote(occ semanticanchors.AnchorOccurrence, res semanticanchors.AnchorResolution) string {
	if occ.Type == semanticanchors.AnchorTypeBeacon {
		return "beacon anchors are advisory recall hints and are not indexed as semantic graph edges"
	}
	if res.EdgeAction == semanticanchors.AnchorEdgeNone && occ.OwnerBinding.OwnerNodeID == "" {
		return "semantic graph edge not emitted because the anchor owner is unbound"
	}
	return ""
}

func anchorSourcePaths(workspace, onlyPath string) ([]string, error) {
	if strings.TrimSpace(onlyPath) != "" {
		rel, err := normalizeAnchorSourcePath(onlyPath)
		if err != nil {
			return nil, err
		}
		return []string{rel}, nil
	}
	var paths []string
	err := filepath.WalkDir(workspace, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			switch name {
			case ".git", "node_modules", ".direnv", ".cache":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !isAnchorSourcePath(path) {
			return nil
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	return paths, err
}

func normalizeAnchorSourcePath(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", fmt.Errorf("anchor source path is required")
	}
	if strings.Contains(raw, `\`) {
		return "", fmt.Errorf("anchor source path must use repo-relative slash paths: %q", value)
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("anchor source path must be repo-relative: %q", value)
	}
	clean := path.Clean(filepath.ToSlash(raw))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("anchor source path escapes workspace: %q", value)
	}
	if !isAnchorSourcePath(clean) {
		return "", fmt.Errorf("anchor source path must be a supported source file: %q", value)
	}
	return clean, nil
}

func isAnchorSourcePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs":
		return true
	default:
		return false
	}
}

type cliAnchorOwnerResolver struct {
	files   map[string]repoindex.Node
	symbols map[string][]repoindex.Node
}

type anchorOwnerIndexSummary struct {
	RepoKey         string `json:"repo_key"`
	StorePath       string `json:"store_path,omitempty"`
	FileNodeCount   int    `json:"file_node_count"`
	SymbolNodeCount int    `json:"symbol_node_count"`
	Status          string `json:"status"`
	Hint            string `json:"hint,omitempty"`
}

func newCLIAnchorOwnerResolver(ctx context.Context, store *repoindex.Store) (*cliAnchorOwnerResolver, error) {
	files, err := store.ListNodesByKind(ctx, repoindex.NodeFile, 100000)
	if err != nil {
		return nil, err
	}
	symbols, err := store.ListNodesByKind(ctx, repoindex.NodeSymbol, 100000)
	if err != nil {
		return nil, err
	}
	resolver := &cliAnchorOwnerResolver{files: map[string]repoindex.Node{}, symbols: map[string][]repoindex.Node{}}
	for _, file := range files {
		resolver.files[filepath.ToSlash(file.File)] = file
	}
	for _, symbol := range symbols {
		path := filepath.ToSlash(symbol.File)
		resolver.symbols[path] = append(resolver.symbols[path], symbol)
	}
	return resolver, nil
}

func (r *cliAnchorOwnerResolver) Summary(store *repoindex.Store) anchorOwnerIndexSummary {
	summary := anchorOwnerIndexSummary{}
	if store != nil {
		summary.RepoKey = store.RepoKey()
		summary.StorePath = store.Path()
	}
	if r != nil {
		summary.FileNodeCount = len(r.files)
		for _, nodes := range r.symbols {
			summary.SymbolNodeCount += len(nodes)
		}
	}
	switch {
	case summary.FileNodeCount == 0 && summary.SymbolNodeCount == 0:
		summary.Status = "empty"
		summary.Hint = "run foxctl index repo build --workspace . before graph-binding semantic anchors"
	case summary.SymbolNodeCount == 0:
		summary.Status = "files_only"
		summary.Hint = "repoindex has file nodes but no symbol nodes; rebuild with language indexing enabled"
	default:
		summary.Status = "ready"
	}
	return summary
}

func (r *cliAnchorOwnerResolver) ResolveFileOwner(path string) semanticanchors.AnchorOwner {
	if r == nil {
		return semanticanchors.AnchorOwner{}
	}
	node := r.files[filepath.ToSlash(path)]
	return cliAnchorOwnerForNode(node, "file:"+filepath.ToSlash(path))
}

func (r *cliAnchorOwnerResolver) ResolveSymbolOwner(path, lang string, span semanticanchors.Span, qualifiedName string) (semanticanchors.AnchorOwner, bool) {
	if r == nil {
		return semanticanchors.AnchorOwner{}, false
	}
	candidates := r.symbols[filepath.ToSlash(path)]
	for _, node := range candidates {
		if node.Name == qualifiedName {
			return cliAnchorOwnerForNode(node, cliAnchorSymbolStableKey(node)), true
		}
	}
	for _, node := range candidates {
		if node.SpanStart == span.LineStart || (node.SpanStart <= span.LineStart && node.SpanEnd >= span.LineStart) {
			return cliAnchorOwnerForNode(node, cliAnchorSymbolStableKey(node)), true
		}
	}
	return semanticanchors.AnchorOwner{}, false
}

func cliAnchorOwnerForNode(node repoindex.Node, stableKey string) semanticanchors.AnchorOwner {
	return semanticanchors.AnchorOwner{
		NodeID:    node.ID,
		Kind:      string(node.Kind),
		StableKey: stableKey,
		Path:      node.File,
		Name:      node.Name,
		StartLine: node.SpanStart,
		EndLine:   node.SpanEnd,
	}
}

func cliAnchorSymbolStableKey(node repoindex.Node) string {
	return strings.Join([]string{"symbol", node.Pkg, filepath.ToSlash(node.File), node.Name}, ":")
}

type cliAnchorTargetResolver struct {
	workspace string
}

func (r cliAnchorTargetResolver) ResolveAnchorTarget(_ context.Context, occ semanticanchors.AnchorOccurrence) (semanticanchors.TargetResolution, error) {
	if occ.Type != semanticanchors.AnchorTypeDoc && occ.Type != semanticanchors.AnchorTypeTest {
		return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationEvidenceOnly}, nil
	}
	targetPath, fragment, _ := strings.Cut(occ.Target, "#")
	fullPath := filepath.Join(r.workspace, filepath.FromSlash(targetPath))
	rel, err := filepath.Rel(r.workspace, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationLintError}, nil
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return cliMissingAnchorTarget(semanticanchors.AnchorFindingMissingTarget), nil
	}
	if strings.TrimSpace(fragment) == "" {
		return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationValidReference}, nil
	}
	switch occ.Type {
	case semanticanchors.AnchorTypeDoc:
		if anchorMarkdownHeadingExists(string(content), fragment) {
			return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationValidReference}, nil
		}
	case semanticanchors.AnchorTypeTest:
		if anchorTestSymbolExists(string(content), fragment) {
			return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationValidReference}, nil
		}
	}
	return cliMissingAnchorTarget(semanticanchors.AnchorFindingUnresolvedFragment), nil
}

func cliMissingAnchorTarget(reason semanticanchors.AnchorFindingReason) semanticanchors.TargetResolution {
	finding := semanticanchors.Finding{ID: "anchor-finding:" + string(reason), Reason: reason, Severity: semanticanchors.AnchorFindingWarning, Message: "semantic anchor " + string(reason)}
	return semanticanchors.TargetResolution{Status: semanticanchors.AnchorValidationMissingTarget, Finding: &finding}
}

func anchorMarkdownHeadingExists(content, fragment string) bool {
	want := anchorMarkdownSlug(fragment)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if anchorMarkdownSlug(heading) == want {
			return true
		}
	}
	return false
}

func anchorMarkdownSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || unicode.IsSpace(r):
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func anchorTestSymbolExists(content, fragment string) bool {
	fragment = regexp.QuoteMeta(strings.TrimSpace(fragment))
	if fragment == "" {
		return true
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\bfunc\s+` + fragment + `\s*\(`),
		regexp.MustCompile(`\bdef\s+` + fragment + `\s*\(`),
		regexp.MustCompile(`\b` + fragment + `\s*[:=]\s*(?:async\s*)?\(?`),
	}
	for _, pattern := range patterns {
		if pattern.MatchString(content) {
			return true
		}
	}
	return false
}

func anchorNeedsRedaction(findings []semanticanchors.Finding) bool {
	return firstAnchorRedactionReason(findings) != ""
}

func firstAnchorRedactionReason(findings []semanticanchors.Finding) semanticanchors.AnchorFindingReason {
	for _, finding := range findings {
		switch finding.Reason {
		case semanticanchors.AnchorFindingUnsafeURL,
			semanticanchors.AnchorFindingAbsolutePath,
			semanticanchors.AnchorFindingPathTraversal,
			semanticanchors.AnchorFindingBackslashPath,
			semanticanchors.AnchorFindingControlChar,
			semanticanchors.AnchorFindingEnvVarExpansion,
			semanticanchors.AnchorFindingSecretLike,
			semanticanchors.AnchorFindingSessionLike,
			semanticanchors.AnchorFindingPIILike:
			return finding.Reason
		}
	}
	return ""
}
