package contextplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"gopkg.in/yaml.v3"
)

type RetrievalInspection struct {
	Query               string                        `json:"query"`
	ExpectedPaths       []string                      `json:"expected_paths,omitempty"`
	RetrievedPaths      []string                      `json:"retrieved_paths,omitempty"`
	Matched             bool                          `json:"matched"`
	Classification      string                        `json:"classification"`
	CandidateNote       string                        `json:"candidate_note,omitempty"`
	CandidateExists     bool                          `json:"candidate_exists"`
	Observation         Observation                   `json:"observation"`
	Proposal            RetrievalCorrectionAction     `json:"proposal"`
	SemanticAnchorHints *SemanticAnchorRetrievalHints `json:"semantic_anchor_hints,omitempty"`
	GeneratedAt         time.Time                     `json:"generated_at"`
}

type RetrievalCorrectionAction struct {
	Kind              string   `json:"kind"`
	Summary           string   `json:"summary"`
	PolicyPath        string   `json:"policy_path,omitempty"`
	PolicyPatch       string   `json:"policy_patch,omitempty"`
	TargetNotePath    string   `json:"target_note_path,omitempty"`
	NoteType          string   `json:"note_type,omitempty"`
	NoteTitle         string   `json:"note_title,omitempty"`
	MetadataPatch     string   `json:"metadata_patch,omitempty"`
	SupportingNote    string   `json:"supporting_note,omitempty"`
	ExpectedRepoPaths []string `json:"expected_repo_paths,omitempty"`
}

type RetrievalInspectionBatchSummary struct {
	Queries               int            `json:"queries"`
	Matched               int            `json:"matched"`
	Misses                int            `json:"misses"`
	Classifications       map[string]int `json:"classifications,omitempty"`
	RecommendedActions    []string       `json:"recommended_actions,omitempty"`
	PolicyPatchCandidate  bool           `json:"policy_patch_candidate"`
	PackageNoteCandidates []string       `json:"package_note_candidates,omitempty"`
}

func (s *WorkspaceStore) CurrentRetrievalOptions() RetrievalOptions {
	return s.loadRetrievalOptions()
}

func (s *WorkspaceStore) SetRetrievalPackageNoteFallback(enabled bool) (string, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return "", err
	}
	body, err := s.ReadRetrievalPolicy()
	if err != nil {
		return "", err
	}
	doc := map[string]any{}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return "", fmt.Errorf("decode retrieval policy: %w", err)
		}
	}
	aca, _ := doc["aca"].(map[string]any)
	if aca == nil {
		aca = map[string]any{}
	}
	aca["package_note_fallback"] = enabled
	doc["aca"] = aca
	updated, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("encode retrieval policy: %w", err)
	}
	if err := atomicWriteFile(layout.RetrievalPolicyPath, updated, 0o644); err != nil {
		return "", err
	}
	return layout.RetrievalPolicyPath, nil
}

func (s *WorkspaceStore) ReadRetrievalPolicy() ([]byte, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(layout.RetrievalPolicyPath)
}

func (s *WorkspaceStore) WriteRetrievalPolicy(body []byte) (string, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return "", err
	}
	if err := atomicWriteFile(layout.RetrievalPolicyPath, body, 0o644); err != nil {
		return "", err
	}
	return layout.RetrievalPolicyPath, nil
}

func (s *WorkspaceStore) InspectRetrieval(
	ctx context.Context,
	index obsidianindex.Store,
	vaultRoot string,
	query string,
	expectedPaths []string,
	result RetrievalResult,
	opts RetrievalOptions,
	limit int,
) (RetrievalInspection, error) {
	query = strings.TrimSpace(query)
	expectedPaths = normalizeExpectedPaths(expectedPaths)
	retrievedPaths := extractRetrievalPaths(result.VaultHits)
	retrievedMatches := retrievedPathsMatchExpected(result.VaultHits, expectedPaths)
	if retrievedMatches {
		return RetrievalInspection{
			Query:          query,
			ExpectedPaths:  expectedPaths,
			RetrievedPaths: retrievedPaths,
			Matched:        true,
			Classification: "matched",
			Observation:    buildRetrievalObservation(filepath.Base(s.layout.WorkspacePath), "matched", query, expectedPaths, "", result.VaultHits),
			Proposal: RetrievalCorrectionAction{
				Kind:    "none",
				Summary: "ACA retrieval already matched the expected path set.",
			},
			SemanticAnchorHints: result.SemanticAnchorHints,
			GeneratedAt:         time.Now().UTC(),
		}, nil
	}

	expectedRepoPaths := expectedCodePaths(expectedPaths)
	candidateNote := ""
	candidateExists := false
	var candidateHit obsidianindex.SearchHit
	if len(expectedRepoPaths) > 0 && index != nil {
		candidateNote = firstPackageNoteCandidate(filepath.Base(s.layout.WorkspacePath), expectedRepoPaths)
		if candidateNote != "" {
			if vaultNoteExists(vaultRoot, candidateNote) {
				candidateExists = true
			}
			hit, ok := findExactCandidateNote(ctx, index, candidateNote)
			if ok {
				candidateExists = true
				candidateHit = hit
			}
		}
	}

	var classification string
	var proposal RetrievalCorrectionAction

	switch {
	case opts.UseSemanticAnchors && len(expectedRepoPaths) > 0 && semanticAnchorHintsMissingExpected(result.SemanticAnchorHints, expectedRepoPaths):
		classification = "missing_semantic_anchor"
		proposal = RetrievalCorrectionAction{
			Kind:              "semantic_anchor_patch",
			Summary:           "Add a reviewed semantic anchor linking the expected code path to this query intent.",
			ExpectedRepoPaths: expectedRepoPaths,
		}
	case opts.UseSemanticAnchors && len(expectedRepoPaths) > 0 && !semanticAnchorHintsMatchExpected(result.SemanticAnchorHints, expectedRepoPaths):
		classification = "stale_semantic_anchor"
		proposal = RetrievalCorrectionAction{
			Kind:              "semantic_anchor_patch",
			Summary:           "Review existing semantic anchor hints; none point at the expected code path.",
			ExpectedRepoPaths: expectedRepoPaths,
		}
	case candidateExists && !opts.UsePackageNoteFallback && queryLooksCodeCentric(query):
		classification = "package_note_fallback_disabled"
		proposal = RetrievalCorrectionAction{
			Kind:        "policy_patch",
			Summary:     "Enable deterministic ACA package-note fallback for this workspace.",
			PolicyPath:  s.layout.RetrievalPolicyPath,
			PolicyPatch: "aca:\n  package_note_fallback: true\n",
		}
	case candidateExists && len(expectedRepoPaths) > 0 && !searchHitMatchesExpected(candidateHit, expectedPaths):
		classification = "bridge_metadata_gap"
		targetPath := candidateHit.Path
		if strings.TrimSpace(targetPath) == "" {
			targetPath = candidateNote
		}
		proposal = RetrievalCorrectionAction{
			Kind:              "metadata_patch",
			Summary:           fmt.Sprintf("Add repo path metadata to %s so ACA can connect it to the expected files.", targetPath),
			TargetNotePath:    targetPath,
			SupportingNote:    targetPath,
			ExpectedRepoPaths: expectedRepoPaths,
			MetadataPatch:     renderRepoPathMetadataPatch(expectedRepoPaths),
		}
	case !candidateExists && len(expectedRepoPaths) > 0:
		classification = "missing_package_note"
		proposal = RetrievalCorrectionAction{
			Kind:              "draft_package_note",
			Summary:           fmt.Sprintf("Create a canonical package note for %s.", candidateNote),
			TargetNotePath:    candidateNote,
			NoteType:          "map",
			NoteTitle:         suggestedPackageNoteTitle(candidateNote),
			ExpectedRepoPaths: expectedRepoPaths,
		}
	case len(result.VaultHits) > 0:
		classification = "ranking_mismatch"
		proposal = RetrievalCorrectionAction{
			Kind:           "manual_review",
			Summary:        "ACA retrieved notes, but ranking did not surface the expected path set.",
			SupportingNote: firstRetrievedPath(result.VaultHits),
		}
	default:
		classification = "missing_vault_coverage"
		proposal = RetrievalCorrectionAction{
			Kind:    "manual_review",
			Summary: "ACA found no useful durable note coverage for this query.",
		}
	}

	return RetrievalInspection{
		Query:               query,
		ExpectedPaths:       expectedPaths,
		RetrievedPaths:      retrievedPaths,
		Matched:             false,
		Classification:      classification,
		CandidateNote:       candidateNote,
		CandidateExists:     candidateExists,
		Observation:         buildRetrievalObservation(filepath.Base(s.layout.WorkspacePath), classification, query, expectedPaths, candidateNote, result.VaultHits),
		Proposal:            proposal,
		SemanticAnchorHints: result.SemanticAnchorHints,
		GeneratedAt:         time.Now().UTC(),
	}, nil
}

func SummarizeRetrievalInspections(items []RetrievalInspection) RetrievalInspectionBatchSummary {
	summary := RetrievalInspectionBatchSummary{
		Queries:         len(items),
		Classifications: map[string]int{},
	}
	packageCandidates := []string{}
	packageSeen := map[string]struct{}{}
	for _, item := range items {
		summary.Classifications[item.Classification]++
		if item.Matched {
			summary.Matched++
		} else {
			summary.Misses++
		}
		if item.Proposal.Kind == "policy_patch" {
			summary.PolicyPatchCandidate = true
		}
		if item.Proposal.Kind == "draft_package_note" && strings.TrimSpace(item.Proposal.TargetNotePath) != "" {
			if _, ok := packageSeen[item.Proposal.TargetNotePath]; !ok {
				packageSeen[item.Proposal.TargetNotePath] = struct{}{}
				packageCandidates = append(packageCandidates, item.Proposal.TargetNotePath)
			}
		}
	}
	sort.Strings(packageCandidates)
	summary.PackageNoteCandidates = packageCandidates
	if summary.PolicyPatchCandidate {
		summary.RecommendedActions = append(summary.RecommendedActions, "Enable aca.package_note_fallback and rerun ACA retrieval.")
	}
	if len(summary.PackageNoteCandidates) > 0 {
		summary.RecommendedActions = append(summary.RecommendedActions, "Draft canonical package notes for the missing package-note targets.")
	}
	if summary.Classifications["bridge_metadata_gap"] > 0 {
		summary.RecommendedActions = append(summary.RecommendedActions, "Patch note frontmatter paths for canonical notes that exist but do not map to repo files.")
	}
	if len(summary.RecommendedActions) == 0 {
		summary.RecommendedActions = append(summary.RecommendedActions, "No deterministic corrective action identified; inspect ranking and note coverage manually.")
	}
	return summary
}

func PersistRetrievalInspectionReport(ctx context.Context, casRoot string, report any) (string, error) {
	casRoot = strings.TrimSpace(casRoot)
	if casRoot == "" {
		return "", nil
	}
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	obj, err := store.Put(ctx, bytes.NewReader(append(body, '\n')), "application/json", []string{"aca-retrieval-inspection"})
	if err != nil {
		return "", err
	}
	return obj.Digest, nil
}

func ReadInspectionArtifact(ctx context.Context, casRoot, digest string) ([]byte, error) {
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	rc, _, err := store.Get(ctx, digest)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func normalizeExpectedPaths(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		normalized := filepath.ToSlash(strings.TrimSpace(item))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func extractRetrievalPaths(hits []RetrievalHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		path := filepath.ToSlash(strings.TrimSpace(hit.Path))
		if path == "" {
			continue
		}
		out = append(out, path)
	}
	return uniqueStrings(out)
}

func retrievedPathsMatchExpected(hits []RetrievalHit, expected []string) bool {
	for _, hit := range hits {
		if retrievalHitMatchesExpected(hit, expected) {
			return true
		}
	}
	return false
}

func retrievalHitMatchesExpected(hit RetrievalHit, expected []string) bool {
	path := filepath.ToSlash(strings.TrimSpace(hit.Path))
	for _, item := range expected {
		if item == path {
			return true
		}
	}
	for _, repoPath := range hit.RepoPaths {
		repoPath = filepath.ToSlash(strings.TrimSpace(repoPath))
		for _, item := range expected {
			if item == repoPath {
				return true
			}
		}
	}
	return false
}

func searchHitMatchesExpected(hit obsidianindex.SearchHit, expected []string) bool {
	if containsString(expected, filepath.ToSlash(strings.TrimSpace(hit.Path))) {
		return true
	}
	for _, repoPath := range hit.RepoPaths {
		if containsString(expected, filepath.ToSlash(strings.TrimSpace(repoPath))) {
			return true
		}
	}
	return false
}

func expectedCodePaths(expected []string) []string {
	out := make([]string, 0, len(expected))
	for _, item := range expected {
		item = filepath.ToSlash(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if strings.HasSuffix(strings.ToLower(item), ".md") {
			continue
		}
		out = append(out, item)
	}
	return uniqueStrings(out)
}

func semanticAnchorHintsMissingExpected(hints *SemanticAnchorRetrievalHints, expectedRepoPaths []string) bool {
	if len(expectedRepoPaths) == 0 {
		return false
	}
	return hints == nil || (len(hints.Paths) == 0 && len(hints.Evidence) == 0)
}

func semanticAnchorHintsMatchExpected(hints *SemanticAnchorRetrievalHints, expectedRepoPaths []string) bool {
	if hints == nil {
		return false
	}
	return matchesCodePaths(expectedRepoPaths, hints.Paths)
}

func firstPackageNoteCandidate(repoName string, expectedRepoPaths []string) string {
	candidates := packageNotePathCandidates(repoName, expectedRepoPaths)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func findExactCandidateNote(ctx context.Context, index obsidianindex.Store, candidate string) (obsidianindex.SearchHit, bool) {
	for _, query := range []string{candidate, filepath.Base(candidate), strings.TrimSuffix(filepath.Base(candidate), filepath.Ext(candidate))} {
		if strings.TrimSpace(query) == "" {
			continue
		}
		hits, err := index.SearchNotes(ctx, query, 10)
		if err != nil {
			continue
		}
		for _, hit := range hits {
			if filepath.ToSlash(strings.TrimSpace(hit.Path)) == filepath.ToSlash(strings.TrimSpace(candidate)) {
				return hit, true
			}
		}
	}
	return obsidianindex.SearchHit{}, false
}

func vaultNoteExists(vaultRoot, notePath string) bool {
	vaultRoot = strings.TrimSpace(vaultRoot)
	notePath = filepath.ToSlash(strings.TrimSpace(notePath))
	if vaultRoot == "" || notePath == "" {
		return false
	}
	fullPath := filepath.Join(vaultRoot, filepath.FromSlash(notePath))
	info, err := os.Stat(fullPath)
	return err == nil && !info.IsDir()
}

func suggestedPackageNoteTitle(path string) string {
	base := strings.TrimSuffix(filepath.Base(strings.TrimSpace(path)), filepath.Ext(path))
	if base == "" {
		return "Package Note"
	}
	parts := strings.Fields(strings.ReplaceAll(base, "-", " "))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func renderRepoPathMetadataPatch(paths []string) string {
	var b strings.Builder
	b.WriteString("paths:\n")
	for _, path := range uniqueStrings(paths) {
		b.WriteString("  - ")
		b.WriteString(filepath.ToSlash(strings.TrimSpace(path)))
		b.WriteString("\n")
	}
	return b.String()
}

func buildRetrievalObservation(project, classification, query string, expectedPaths []string, candidateNote string, hits []RetrievalHit) Observation {
	statement := retrievalObservationStatement(classification, query, candidateNote, expectedPaths)
	evidence := []string{"query:" + strings.TrimSpace(query)}
	for _, path := range expectedPaths {
		if strings.HasSuffix(strings.ToLower(path), ".md") {
			evidence = append(evidence, "note:"+path)
		} else {
			evidence = append(evidence, "path:"+path)
		}
	}
	if candidate := strings.TrimSpace(candidateNote); candidate != "" {
		evidence = append(evidence, "note:"+candidate)
	}
	limit := len(hits)
	if limit > 3 {
		limit = 3
	}
	for _, hit := range hits[:limit] {
		if path := strings.TrimSpace(hit.Path); path != "" {
			evidence = append(evidence, "note:"+path)
		}
	}
	now := time.Now().UTC()
	return Observation{
		Statement:    statement,
		Confidence:   retrievalObservationConfidence(classification),
		Count:        1,
		Project:      strings.TrimSpace(project),
		Area:         "aca-retrieval",
		EvidenceRefs: stringsToEvidenceRefs(uniqueStrings(evidence)),
		FirstSeen:    now,
		LastSeen:     now,
	}
}

func retrievalObservationStatement(classification, query, candidateNote string, expectedPaths []string) string {
	target := candidateNote
	if target == "" && len(expectedPaths) > 0 {
		target = expectedPaths[0]
	}
	switch classification {
	case "package_note_fallback_disabled":
		return fmt.Sprintf("ACA retrieval missed deterministic package-note fallback for %s on query %q.", target, strings.TrimSpace(query))
	case "missing_package_note":
		return fmt.Sprintf("ACA retrieval lacks a canonical package note for %s.", target)
	case "bridge_metadata_gap":
		return fmt.Sprintf("ACA retrieval note metadata is missing repo path coverage for %s.", target)
	case "ranking_mismatch":
		return fmt.Sprintf("ACA retrieval ranked the wrong notes for query %q against %s.", strings.TrimSpace(query), target)
	case "matched":
		return fmt.Sprintf("ACA retrieval matched the expected path set for query %q.", strings.TrimSpace(query))
	default:
		return fmt.Sprintf("ACA retrieval found no durable note coverage for query %q.", strings.TrimSpace(query))
	}
}

func retrievalObservationConfidence(classification string) float64 {
	switch classification {
	case "package_note_fallback_disabled", "missing_package_note", "bridge_metadata_gap":
		return 0.82
	case "ranking_mismatch":
		return 0.68
	default:
		return 0.61
	}
}

func firstRetrievedPath(hits []RetrievalHit) string {
	if len(hits) == 0 {
		return ""
	}
	return strings.TrimSpace(hits[0].Path)
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
