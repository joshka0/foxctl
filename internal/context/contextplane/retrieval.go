package contextplane

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/rerank"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"gopkg.in/yaml.v3"
)

func retrievalWorkspaceRepoName(workspacePath string) string {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return ""
	}
	if !filepath.IsAbs(workspacePath) {
		if absPath, err := filepath.Abs(workspacePath); err == nil {
			workspacePath = absPath
		}
	}
	workspacePath = filepath.Clean(workspacePath)
	base := strings.TrimSpace(filepath.Base(workspacePath))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return strings.ToLower(base)
}

func defaultRetrievalWeights() RetrievalWeights {
	return RetrievalWeights{
		BaseIndexScore: 1,
		ADR:            5,
		Pattern:        4,
		Incident:       4,
		Investigation:  3,
		Map:            3,
		Canonical:      3,
		Reviewed:       2,
		Raw:            1,
		RelevantRef:    3,
		HandoffRef:     2,
		CodePath:       4,
		CodeSymbol:     4,
		RepoMotif:      3,
		CoChange:       3,
		SemanticMatch:  2,
	}
}

type retrievalPolicyFile struct {
	RankingWeights map[string]int `yaml:"ranking_weights"`
	ACA            struct {
		PackageNoteFallback       bool `yaml:"package_note_fallback"`
		CoChangePrior             bool `yaml:"co_change_prior"`
		CoChangeCommitLimit       int  `yaml:"co_change_commit_limit"`
		CoChangeMaxFilesPerCommit int  `yaml:"co_change_max_files_per_commit"`
		CoChangeHalfLifeDays      int  `yaml:"co_change_half_life_days"`
		ContinuityBundles         bool `yaml:"continuity_bundles"`
	} `yaml:"aca"`
}

func DefaultRetrievalOptions() RetrievalOptions {
	return RetrievalOptions{
		IncludeTopOfMindResult:    true,
		IncludeLatestHandoff:      true,
		IncludeVaultHits:          true,
		UseRelevantRefBoost:       true,
		UseHandoffRefBoost:        true,
		UseCodeHints:              true,
		UseSemanticVaultSearch:    true,
		UsePackageNoteFallback:    false,
		UseRepoMotifPrior:         true,
		UseCoChangePrior:          false,
		CoChangeCommitLimit:       40,
		CoChangeMaxFilesPerCommit: 20,
		CoChangeHalfLifeDays:      90,
		UseContinuityBundles:      true,
		UseQueryTypeBias:          false,
		IncludeControlPlaneRefs:   true,
	}
}

type scoredVaultHit struct {
	hit           obsidianindex.SearchHit
	lexicalScore  int
	semanticScore int
	packageBoost  int
}

type repoMotifPrior struct {
	pathScores   map[string]float64
	symbolScores map[string]float64
	maxPathScore float64
	maxSymScore  float64
}

func emptyRepoMotifPrior() repoMotifPrior {
	return repoMotifPrior{
		pathScores:   map[string]float64{},
		symbolScores: map[string]float64{},
	}
}

func (s *WorkspaceStore) loadRetrievalWeights() RetrievalWeights {
	weights := defaultRetrievalWeights()
	body, err := os.ReadFile(s.layout.RetrievalPolicyPath)
	if err != nil {
		return weights
	}
	var policy retrievalPolicyFile
	if err := yaml.Unmarshal(body, &policy); err != nil {
		return weights
	}
	override := func(key string, target *int) {
		if v, ok := policy.RankingWeights[key]; ok {
			*target = v
		}
	}
	override("project_match", &weights.RelevantRef)
	override("trust_level", &weights.Reviewed)
	override("note_type_weight", &weights.Pattern)
	override("link_proximity", &weights.HandoffRef)
	override("recency", &weights.Raw)
	override("code_path", &weights.CodePath)
	override("code_symbol", &weights.CodeSymbol)
	override("co_change", &weights.CoChange)
	override("semantic_match", &weights.SemanticMatch)
	return weights
}

func (s *WorkspaceStore) loadRetrievalOptions() RetrievalOptions {
	opts := DefaultRetrievalOptions()
	body, err := os.ReadFile(s.layout.RetrievalPolicyPath)
	if err != nil {
		return opts
	}
	var policy retrievalPolicyFile
	if err := yaml.Unmarshal(body, &policy); err != nil {
		return opts
	}
	opts.UsePackageNoteFallback = policy.ACA.PackageNoteFallback
	opts.UseRepoMotifPrior = true
	opts.UseCoChangePrior = policy.ACA.CoChangePrior
	if policy.ACA.CoChangeCommitLimit > 0 {
		opts.CoChangeCommitLimit = policy.ACA.CoChangeCommitLimit
	}
	if policy.ACA.CoChangeMaxFilesPerCommit > 0 {
		opts.CoChangeMaxFilesPerCommit = policy.ACA.CoChangeMaxFilesPerCommit
	}
	if policy.ACA.CoChangeHalfLifeDays > 0 {
		opts.CoChangeHalfLifeDays = policy.ACA.CoChangeHalfLifeDays
	}
	opts.UseContinuityBundles = policy.ACA.ContinuityBundles
	return opts
}

// Retrieve blends ACA state with ranked vault hits.
func (s *WorkspaceStore) Retrieve(ctx context.Context, index obsidianindex.Store, repo *repoindex.Store, semanticProvider semantic.EmbeddingProvider, query string, limit int) (RetrievalResult, error) {
	return s.RetrieveWithOptionsAndMemory(ctx, index, repo, semanticProvider, nil, query, limit, s.loadRetrievalOptions())
}

// RetrieveWithOptions blends ACA state with ranked vault hits under explicit retrieval options.
func (s *WorkspaceStore) RetrieveWithOptions(ctx context.Context, index obsidianindex.Store, repo *repoindex.Store, semanticProvider semantic.EmbeddingProvider, query string, limit int, opts RetrievalOptions) (RetrievalResult, error) {
	return s.RetrieveWithOptionsAndMemory(ctx, index, repo, semanticProvider, nil, query, limit, opts)
}

// RetrieveWithOptionsAndMemory blends ACA state with ranked vault hits and optional memory-backed structural priors.
func (s *WorkspaceStore) RetrieveWithOptionsAndMemory(ctx context.Context, index obsidianindex.Store, repo *repoindex.Store, semanticProvider semantic.EmbeddingProvider, memStore storage.MemoryStore, query string, limit int, opts RetrievalOptions) (RetrievalResult, error) {
	if limit <= 0 {
		limit = 5
	}
	report, err := s.BuildReport()
	if err != nil {
		return RetrievalResult{}, err
	}
	top, err := s.LoadTopOfMind()
	if err != nil {
		return RetrievalResult{}, err
	}
	result := RetrievalResult{
		WorkspaceID:   report.WorkspaceID,
		Query:         strings.TrimSpace(query),
		TopOfMind:     &top,
		LatestHandoff: report.LatestHandoff,
		Observations:  append([]Observation(nil), report.TopObservations...),
		Tensions:      append([]Tension(nil), report.OpenTensions...),
		Weights:       s.loadRetrievalWeights(),
		GeneratedAt:   report.GeneratedAt,
	}
	if !opts.IncludeVaultHits || index == nil || strings.TrimSpace(query) == "" {
		return result, nil
	}
	codeCentric := queryLooksCodeCentric(query)
	codeHints := retrievalCodeHints{}
	if opts.UseCodeHints {
		codeHints = deriveCodeHints(ctx, repo, query, result.TopOfMind, report.LatestHandoff)
	}
	cochange := emptyCoChangePrior()
	if opts.UseCoChangePrior {
		seedPaths := append([]string(nil), codeHints.Paths...)
		seedPaths = append(seedPaths, continuityBundlePaths(result.TopOfMind, report.LatestHandoff, opts)...)
		cochange, _ = buildCoChangePrior(ctx, s.layout.WorkspacePath, seedPaths, coChangeConfigFromOptions(opts))
	}
	motifPrior := emptyRepoMotifPrior()
	motifSearchLimit := limit * 3
	if codeCentric {
		motifSearchLimit = maxInt(motifSearchLimit, limit*120)
		motifSearchLimit = maxInt(motifSearchLimit, 1000)
	}
	if opts.UseRepoMotifPrior && memStore != nil {
		if motifHits, err := SearchRepoMotifArtifacts(ctx, s.layout.WorkspacePath, query, motifSearchLimit, memStore, nil); err == nil {
			result.RepoMotifHits = motifHits
			motifPrior = buildRepoMotifPrior(motifHits)
		}
	}
	searchLimit := limit * 3
	if codeCentric {
		searchLimit = maxInt(searchLimit, limit*120)
		searchLimit = maxInt(searchLimit, 1000)
	}
	hits, err := index.SearchNotes(ctx, query, searchLimit)
	if err != nil {
		return RetrievalResult{}, err
	}
	byPath := map[string]*scoredVaultHit{}
	for _, hit := range hits {
		byPath[hit.Path] = &scoredVaultHit{hit: hit, lexicalScore: hit.Score}
	}
	maxSemantic := 0
	if semanticProvider != nil && opts.UseSemanticVaultSearch {
		if semanticHits, err := index.SearchNotesSemantic(ctx, query, semanticProvider, searchLimit); err == nil {
			result.SemanticUsed = true
			result.SemanticModel = semanticProvider.Model()
			for _, hit := range semanticHits {
				if hit.Score > maxSemantic {
					maxSemantic = hit.Score
				}
				existing, ok := byPath[hit.Path]
				if !ok {
					byPath[hit.Path] = &scoredVaultHit{hit: hit, semanticScore: hit.Score}
					continue
				}
				existing.semanticScore = hit.Score
				if existing.hit.Snippet == "" && hit.Snippet != "" {
					existing.hit.Snippet = hit.Snippet
				}
				if existing.hit.Title == "" && hit.Title != "" {
					existing.hit.Title = hit.Title
				}
				if existing.hit.Type == "" && hit.Type != "" {
					existing.hit.Type = hit.Type
				}
				if existing.hit.Trust == "" && hit.Trust != "" {
					existing.hit.Trust = hit.Trust
				}
			}
		}
	}
	if index != nil && len(codeHints.Paths) > 0 {
		boostBase := 0
		if opts.UsePackageNoteFallback {
			boostBase = 30
		} else if codeCentric {
			boostBase = 12
		}
		if boostBase > 0 {
			mergePackageFallbackHits(ctx, index, s.layout.WorkspacePath, query, codeHints, byPath, boostBase)
		}
	}
	vaultHits := make([]RetrievalHit, 0, len(byPath))
	for _, hit := range byPath {
		vaultHits = append(vaultHits, scoreVaultHit(*hit, maxSemantic, codeCentric, s.layout.WorkspacePath, result, report, codeHints, cochange, motifPrior, query, opts))
	}
	vaultHits = filterRetrievalHitsByTrust(vaultHits, opts.AllowedTrusts)
	sort.SliceStable(vaultHits, func(i, j int) bool {
		if vaultHits[i].Score != vaultHits[j].Score {
			return vaultHits[i].Score > vaultHits[j].Score
		}
		return vaultHits[i].Path < vaultHits[j].Path
	})
	rerankCfg := rerank.FromEnv()
	if rerankCfg.Enabled {
		if provider, err := rerank.NewVoyageProvider(rerankCfg.ToVoyageConfig()); err == nil {
			if reranked, used, model, err := rerankVaultHits(ctx, query, vaultHits, result.Weights, provider, rerankCfg); err == nil {
				vaultHits = reranked
				result.SemanticUsed = used
				result.SemanticModel = model
			}
		}
	}
	result.VaultHits = vaultHits
	return result, nil
}

func noteTypeWeight(noteType string, weights RetrievalWeights) int {
	switch strings.ToLower(strings.TrimSpace(noteType)) {
	case "adr":
		return weights.ADR
	case "pattern":
		return weights.Pattern
	case "incident":
		return weights.Incident
	case "investigation":
		return weights.Investigation
	case "map":
		return weights.Map
	default:
		return 0
	}
}

func trustWeight(trust string, weights RetrievalWeights) int {
	switch strings.ToLower(strings.TrimSpace(trust)) {
	case "canonical":
		return weights.Canonical
	case "reviewed":
		return weights.Reviewed
	case "raw":
		return weights.Raw
	default:
		return 0
	}
}

func matchesRefs(path string, refs []string) bool {
	path = filepathBaseAware(path)
	for _, ref := range refs {
		ref = strings.TrimSpace(strings.TrimPrefix(ref, "path:"))
		if ref == "" {
			continue
		}
		if filepathBaseAware(ref) == path {
			return true
		}
	}
	return false
}

func filepathBaseAware(path string) string {
	path = strings.TrimSpace(strings.TrimSuffix(path, ".md"))
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

// DetectContradictions links open tensions to relevant durable notes in the vault index.
func (s *WorkspaceStore) DetectContradictions(ctx context.Context, index obsidianindex.Store, repo *repoindex.Store, semanticProvider semantic.EmbeddingProvider, limit int) ([]ContradictionFinding, error) {
	if limit <= 0 {
		limit = 10
	}
	tensions, err := s.ListTensions(limit)
	if err != nil {
		return nil, err
	}
	out := make([]ContradictionFinding, 0, len(tensions))
	for _, tension := range tensions {
		if strings.EqualFold(strings.TrimSpace(tension.Status), "closed") {
			continue
		}
		query := contradictionQuery(tension.Statement)
		finding := ContradictionFinding{
			Tension:          tension,
			Query:            query,
			BlockedPromotion: impactRank(tension.Impact) >= impactRank("high"),
		}
		if index != nil && strings.TrimSpace(query) != "" {
			hits, err := s.Retrieve(ctx, index, repo, semanticProvider, query, 3)
			if err != nil {
				return nil, err
			}
			finding.SupportingNotes = hits.VaultHits
			for _, hit := range finding.SupportingNotes {
				if (strings.EqualFold(hit.Trust, "canonical") || strings.EqualFold(hit.Trust, "reviewed")) &&
					(strings.EqualFold(hit.Type, "adr") || strings.EqualFold(hit.Type, "pattern") || strings.EqualFold(hit.Type, "incident")) {
					finding.BlockedPromotion = true
					break
				}
			}
		}
		out = append(out, finding)
	}
	return out, nil
}

func scoreVaultHit(entry scoredVaultHit, maxSemantic int, codeCentric bool, workspacePath string, result RetrievalResult, report Report, codeHints retrievalCodeHints, cochange coChangePrior, motifs repoMotifPrior, query string, opts RetrievalOptions) RetrievalHit {
	hit := entry.hit
	score := entry.lexicalScore * result.Weights.BaseIndexScore
	if entry.semanticScore > 0 && maxSemantic > 0 {
		score += int((float64(entry.semanticScore) / float64(maxSemantic)) * float64(result.Weights.SemanticMatch*10))
		if entry.lexicalScore > 0 {
			score += result.Weights.SemanticMatch
		}
	}
	score += noteTypeWeight(hit.Type, result.Weights)
	score += trustWeight(hit.Trust, result.Weights)
	if opts.UseRelevantRefBoost && result.TopOfMind != nil && matchesRefs(hit.Path, result.TopOfMind.RelevantRefs) {
		score += result.Weights.RelevantRef
	}
	if opts.UseHandoffRefBoost && report.LatestHandoff != nil && matchesRefs(hit.Path, append(report.LatestHandoff.Handoff.EvidenceRefs, report.LatestHandoff.Handoff.FilesTouched...)) {
		score += result.Weights.HandoffRef
	}
	if opts.UseCodeHints && codeCentric && matchesCodePaths(hit.RepoPaths, codeHints.Paths) {
		score += result.Weights.CodePath
	}
	if opts.UseCodeHints && codeCentric && matchesCodeSymbols(hit.Symbols, codeHints.Symbols) {
		score += result.Weights.CodeSymbol
	}
	score += packageNoteHintBoost(workspacePath, hit, codeHints)
	if opts.UseRepoMotifPrior {
		score += repoMotifBoostForHit(hit.RepoPaths, hit.Symbols, motifs, result.Weights)
	}
	if opts.UseCoChangePrior {
		score += coChangeBoostForHit(hit.RepoPaths, cochange, result.Weights)
	}
	if opts.UseQueryTypeBias {
		score += queryTypeBias(query, hit, result.Weights)
	}
	score += workspaceProjectNoteBias(workspacePath, query, hit)
	score += noteQueryCoverageBias(workspacePath, query, hit)
	score += packageNoteSpecificityBias(workspacePath, query, hit)
	score += conceptNoteSpecificityBias(workspacePath, query, hit)
	score += repoPathClassBias(query, hit.RepoPaths)
	score += entry.packageBoost
	return RetrievalHit{
		Path:              hit.Path,
		Title:             hit.Title,
		Type:              hit.Type,
		Trust:             hit.Trust,
		Score:             score,
		Snippet:           hit.Snippet,
		PrimaryAnchorPath: strings.TrimSpace(hit.PrimaryAnchorPath),
		RepoPaths:         append([]string(nil), hit.RepoPaths...),
		AnchorPaths:       append([]string(nil), hit.AnchorPaths...),
		AnchorRoles:       cloneAnchorRoles(hit.AnchorRoles),
		Symbols:           append([]string(nil), hit.Symbols...),
	}
}

func cloneAnchorRoles(raw map[string][]string) map[string][]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string][]string, len(raw))
	for role, paths := range raw {
		if len(paths) == 0 {
			continue
		}
		out[role] = append([]string(nil), paths...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func workspaceProjectNoteBias(workspacePath, query string, hit obsidianindex.SearchHit) int {
	repoName := retrievalWorkspaceRepoName(workspacePath)
	pathValue := strings.ToLower(filepath.ToSlash(strings.TrimSpace(hit.Path)))
	if repoName == "" || pathValue == "" {
		return 0
	}
	comparative := queryLooksComparative(query)
	bias := 0
	canonicalPrefix := "notes/repo/" + repoName + "/"
	inboxPrefix := "inbox/drafted-from-foxctl/repo-graph/" + repoName + "/"
	switch {
	case strings.HasPrefix(pathValue, canonicalPrefix):
		bias += 24
	case strings.HasPrefix(pathValue, inboxPrefix):
		bias += 6
	}
	if strings.Contains(pathValue, "notes/repo/") || strings.Contains(pathValue, "inbox/drafted-from-foxctl/repo-graph/") {
		if !comparative && !strings.Contains(pathValue, "/"+repoName+"/") {
			bias -= 18
		}
		if strings.Contains(pathValue, "inbox/drafted-from-foxctl/") {
			bias -= 14
		}
	}
	return bias
}

func noteQueryCoverageBias(workspacePath, query string, hit obsidianindex.SearchHit) int {
	repoName := retrievalWorkspaceRepoName(workspacePath)
	pathValue := strings.ToLower(filepath.ToSlash(strings.TrimSpace(hit.Path)))
	if repoName == "" || pathValue == "" {
		return 0
	}
	queryTerms := retrievalQueryTerms(query)
	if len(queryTerms) == 0 {
		return 0
	}
	titleTerms := retrievalExpandedTerms(hit.Title)
	pathTerms := retrievalExpandedTerms(strings.Join(hit.RepoPaths, " "))
	symbolTerms := retrievalExpandedTerms(strings.Join(hit.Symbols, " "))
	titleMatches := overlapTermCount(queryTerms, titleTerms)
	pathMatches := overlapTermCount(queryTerms, pathTerms)
	symbolMatches := overlapTermCount(queryTerms, symbolTerms)
	if titleMatches == 0 && pathMatches == 0 && symbolMatches == 0 {
		return 0
	}
	bias := titleMatches*8 + pathMatches*4 + symbolMatches*3
	canonicalPrefix := "notes/repo/" + repoName + "/"
	canonicalPackagePrefix := canonicalPrefix + "packages/"
	canonicalRoot := canonicalPrefix + "index.md"
	switch {
	case pathValue == canonicalRoot:
		bias += titleMatches * 10
	case strings.HasPrefix(pathValue, canonicalPackagePrefix):
		bias += titleMatches * 4
	}
	return bias
}

func queryLooksComparative(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return false
	}
	return strings.Contains(query, " compare ") ||
		strings.Contains(query, " versus ") ||
		strings.Contains(query, " vs ") ||
		strings.Contains(query, " difference ") ||
		strings.Contains(query, " differences ") ||
		strings.Contains(query, " between ")
}

func packageNoteSpecificityBias(workspacePath, query string, hit obsidianindex.SearchHit) int {
	repoName := retrievalWorkspaceRepoName(workspacePath)
	pathValue := filepath.ToSlash(strings.TrimSpace(hit.Path))
	if repoName == "" || pathValue == "" {
		return 0
	}
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	lowerPath := strings.ToLower(pathValue)
	bias := 0
	canonicalRoot := "notes/repo/" + strings.ToLower(repoName) + "/index.md"
	canonicalPackagePrefix := "notes/repo/" + strings.ToLower(repoName) + "/packages/"
	inboxRoot := "inbox/drafted-from-foxctl/repo-graph/" + strings.ToLower(repoName) + "/index.md"
	inboxPackagePrefix := "inbox/drafted-from-foxctl/repo-graph/" + strings.ToLower(repoName) + "/packages/"
	if strings.Contains(lowerQuery, "repo graph") && lowerPath == canonicalRoot {
		bias += 40
	}
	if queryLooksRepoRootIntent(lowerQuery) && lowerPath == canonicalRoot {
		bias += 28
	}
	if queryLooksRepoRootIntent(lowerQuery) && lowerPath == inboxRoot {
		bias += 8
	}
	if strings.Contains(lowerQuery, "package") && strings.HasPrefix(lowerPath, canonicalPackagePrefix) {
		bias += 16
	}
	if strings.Contains(lowerQuery, "package") && strings.HasPrefix(lowerPath, inboxPackagePrefix) {
		bias += 6
	}
	if strings.Contains(lowerQuery, "package") && (lowerPath == canonicalRoot || lowerPath == inboxRoot) {
		bias -= 16
	}
	slug := strings.TrimSuffix(filepath.Base(lowerPath), filepath.Ext(lowerPath))
	if strings.HasPrefix(lowerPath, canonicalPackagePrefix) || strings.HasPrefix(lowerPath, inboxPackagePrefix) {
		slugScore := packageSlugQueryMatchScore(lowerQuery, slug, hit)
		bias += slugScore
	}
	return bias
}

func conceptNoteSpecificityBias(workspacePath, query string, hit obsidianindex.SearchHit) int {
	repoName := retrievalWorkspaceRepoName(workspacePath)
	pathValue := filepath.ToSlash(strings.TrimSpace(hit.Path))
	if repoName == "" || pathValue == "" {
		return 0
	}
	lowerPath := strings.ToLower(pathValue)
	canonicalConceptPrefix := "notes/repo/" + strings.ToLower(repoName) + "/concepts/"
	inboxConceptPrefix := "inbox/drafted-from-foxctl/repo-graph/" + strings.ToLower(repoName) + "/concepts/"
	if !strings.HasPrefix(lowerPath, canonicalConceptPrefix) && !strings.HasPrefix(lowerPath, inboxConceptPrefix) {
		return 0
	}
	bias := 0
	if strings.HasPrefix(lowerPath, canonicalConceptPrefix) {
		bias += 4
	} else {
		bias += 1
	}
	queryTerms := retrievalQueryTerms(query)
	if len(queryTerms) == 0 {
		return bias
	}
	slug := strings.TrimSuffix(filepath.Base(lowerPath), filepath.Ext(lowerPath))
	title := strings.ToLower(strings.TrimSpace(hit.Title))
	matches := 0
	for _, qterm := range queryTerms {
		matched := false
		if strings.Contains(slug, qterm) {
			bias += 6
			matched = true
		}
		if strings.Contains(title, qterm) {
			bias += 4
			matched = true
		}
		for _, symbol := range hit.Symbols {
			if strings.Contains(strings.ToLower(strings.TrimSpace(symbol)), qterm) {
				bias += 5
				matched = true
				break
			}
		}
		for _, repoPath := range hit.RepoPaths {
			if strings.Contains(strings.ToLower(strings.TrimSpace(repoPath)), qterm) {
				bias += 3
				matched = true
				break
			}
		}
		if matched {
			matches++
		}
	}
	if matches >= 2 {
		bias += 10 + (matches-2)*3
	}
	return bias
}

func packageSlugQueryMatchScore(query, slug string, hit obsidianindex.SearchHit) int {
	query = strings.ToLower(strings.TrimSpace(query))
	slug = strings.ToLower(strings.TrimSpace(slug))
	if query == "" || slug == "" {
		return 0
	}
	queryTerms := retrievalQueryTerms(query)
	terms := strings.Split(strings.ReplaceAll(slug, "-", " "), " ")
	score := 0
	matches := 0
	for _, qterm := range queryTerms {
		if len(qterm) < 3 {
			continue
		}
		if slug == qterm {
			matches++
			score += 10
			continue
		}
		if strings.Contains(slug, qterm) {
			matches++
			score += 6
		}
	}
	title := strings.ToLower(strings.TrimSpace(hit.Title))
	for _, qterm := range queryTerms {
		if len(qterm) < 3 {
			continue
		}
		if strings.Contains(title, qterm) {
			score += 2
		}
	}
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if len(term) < 3 {
			continue
		}
		if strings.Contains(query, term) {
			score += 2
		}
	}
	if matches >= 2 {
		score += 8
	}
	return score
}

func retrievalQueryTerms(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(value string) {
		for _, normalized := range normalizeRetrievalTerms(value) {
			if _, ok := seen[normalized]; ok {
				continue
			}
			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}
	for _, field := range strings.FieldsFunc(query, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsNumber(r))
	}) {
		add(field)
	}
	return out
}

func retrievalExpandedTerms(text string) map[string]struct{} {
	terms := make(map[string]struct{})
	for _, token := range splitRetrievalTokens(text) {
		for _, normalized := range normalizeRetrievalTerms(token) {
			terms[normalized] = struct{}{}
		}
	}
	return terms
}

func overlapTermCount(queryTerms []string, noteTerms map[string]struct{}) int {
	count := 0
	for _, term := range queryTerms {
		for noteTerm := range noteTerms {
			if retrievalTermsOverlap(term, noteTerm) {
				count++
				break
			}
		}
	}
	return count
}

func retrievalTermsOverlap(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if len(a) >= 4 && strings.Contains(b, a) {
		return true
	}
	if len(b) >= 4 && strings.Contains(a, b) {
		return true
	}
	return false
}

func splitRetrievalTokens(text string) []string {
	var tokens []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		tokens = append(tokens, string(current))
		current = current[:0]
	}
	var prev rune
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			if len(current) > 0 && unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsNumber(prev)) {
				flush()
			}
			current = append(current, unicode.ToLower(r))
		default:
			flush()
		}
		prev = r
	}
	flush()
	return tokens
}

func normalizeRetrievalTerms(value string) []string {
	value = strings.TrimSpace(strings.ToLower(value))
	if len(value) < 3 || !utf8.ValidString(value) {
		return nil
	}
	out := []string{value}
	if strings.HasSuffix(value, "s") && len(value) > 4 {
		singular := strings.TrimSuffix(value, "s")
		if len(singular) >= 3 {
			out = append(out, singular)
		}
	}
	return out
}

func queryLooksRepoRootIntent(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return false
	}
	return strings.Contains(query, "repo graph") ||
		strings.Contains(query, "repo map") ||
		strings.Contains(query, "repo index") ||
		strings.Contains(query, "overview") ||
		(strings.Contains(query, "repo") && strings.Contains(query, "graph"))
}

func repoPathClassBias(query string, repoPaths []string) int {
	if len(repoPaths) == 0 {
		return 0
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 0
	}
	hasPrefix := func(prefix string) bool {
		prefix = strings.ToLower(strings.TrimSpace(prefix))
		for _, pathValue := range normalizeRepoPaths(repoPaths) {
			if strings.HasPrefix(strings.ToLower(pathValue), prefix) {
				return true
			}
		}
		return false
	}
	internalIntent := containsAny(query, "runtime", "wiring", "adapter", "adapters", "platform", "indexing", "storage", "daemon", "engine", "contextplane")
	skillIntent := containsAny(query, "skill", "skills", "hook", "hooks", "tool", "tools")
	cmdIntent := containsAny(query, "cmd", "command", "commands", "cli")
	bias := 0
	if internalIntent && hasPrefix("internal/") {
		bias += 12
	}
	if internalIntent && hasPrefix("skills/") {
		bias -= 8
	}
	if skillIntent && hasPrefix("skills/") {
		bias += 6
	}
	if skillIntent && hasPrefix("internal/") {
		bias += 2
	}
	if cmdIntent && hasPrefix("cmd/") {
		bias += 10
	}
	if cmdIntent && hasPrefix("skills/") && !skillIntent {
		bias -= 4
	}
	return bias
}

func filterRetrievalHitsByTrust(hits []RetrievalHit, allowed []string) []RetrievalHit {
	if len(allowed) == 0 {
		return hits
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, trust := range allowed {
		trust = strings.ToLower(strings.TrimSpace(trust))
		if trust == "" {
			continue
		}
		allow[trust] = struct{}{}
	}
	if len(allow) == 0 {
		return hits
	}
	out := make([]RetrievalHit, 0, len(hits))
	for _, hit := range hits {
		if _, ok := allow[strings.ToLower(strings.TrimSpace(hit.Trust))]; ok {
			out = append(out, hit)
		}
	}
	return out
}

func buildRepoMotifPrior(hits []RepoMotifSearchHit) repoMotifPrior {
	prior := emptyRepoMotifPrior()
	for _, hit := range hits {
		score := hit.Score
		if score <= 0 {
			score = 0.1
		}
		if pathValue := strings.TrimSpace(hit.AnchorPath); pathValue != "" {
			prior.pathScores[pathValue] += score
			if prior.pathScores[pathValue] > prior.maxPathScore {
				prior.maxPathScore = prior.pathScores[pathValue]
			}
		}
		for _, pathValue := range normalizeRepoPaths(append([]string(nil), hit.Paths...)) {
			prior.pathScores[pathValue] += score * 0.75
			if prior.pathScores[pathValue] > prior.maxPathScore {
				prior.maxPathScore = prior.pathScores[pathValue]
			}
		}
		for _, symbol := range uniqueStrings(hit.Symbols) {
			symbol = strings.TrimSpace(symbol)
			if symbol == "" {
				continue
			}
			prior.symbolScores[symbol] += score
			if prior.symbolScores[symbol] > prior.maxSymScore {
				prior.maxSymScore = prior.symbolScores[symbol]
			}
		}
	}
	return prior
}

func repoMotifBoostForHit(repoPaths, symbols []string, prior repoMotifPrior, weights RetrievalWeights) int {
	if weights.RepoMotif <= 0 {
		return 0
	}
	best := 0.0
	for _, pathValue := range normalizeRepoPaths(repoPaths) {
		if score := prior.pathScores[pathValue]; score > best {
			best = score
		}
	}
	for _, symbol := range uniqueStrings(symbols) {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			continue
		}
		if score := prior.symbolScores[symbol]; score > best {
			best = score
		}
	}
	maxScore := prior.maxPathScore
	if prior.maxSymScore > maxScore {
		maxScore = prior.maxSymScore
	}
	if best <= 0 || maxScore <= 0 {
		return 0
	}
	normalized := best / maxScore
	return int(normalized * float64(weights.RepoMotif*10))
}

func mergePackageFallbackHits(ctx context.Context, index obsidianindex.Store, workspacePath, query string, codeHints retrievalCodeHints, byPath map[string]*scoredVaultHit, boostBase int) {
	repoName := retrievalWorkspaceRepoName(workspacePath)
	if repoName == "" {
		return
	}
	candidates := packageNotePathCandidates(repoName, codeHints.Paths)
	for i, candidate := range candidates {
		hit, ok := findExactCandidateNote(ctx, index, candidate)
		if !ok {
			continue
		}
		boost := maxInt(0, boostBase-(i*2))
		boost = scaledPackageFallbackBoost(boost, noteQueryCoverageBias(workspacePath, query, hit))
		existing, ok := byPath[hit.Path]
		if !ok {
			byPath[hit.Path] = &scoredVaultHit{
				hit:          hit,
				lexicalScore: 0,
				packageBoost: boost,
			}
			continue
		}
		if existing.packageBoost < boost {
			existing.packageBoost = boost
		}
		if existing.hit.Snippet == "" {
			existing.hit.Snippet = hit.Snippet
		}
		if existing.hit.Title == "" {
			existing.hit.Title = hit.Title
		}
		if existing.hit.Trust == "" {
			existing.hit.Trust = hit.Trust
		}
		if existing.hit.Type == "" {
			existing.hit.Type = hit.Type
		}
	}
}

func scaledPackageFallbackBoost(base, coverage int) int {
	if base <= 0 {
		return 0
	}
	switch {
	case coverage <= 0:
		return 0
	case coverage < 12:
		if base > 6 {
			return 6
		}
		return base
	case coverage < 24:
		if base > 14 {
			return 14
		}
		return base
	default:
		return base
	}
}

func packageNoteHintBoost(workspacePath string, hit obsidianindex.SearchHit, codeHints retrievalCodeHints) int {
	repoName := retrievalWorkspaceRepoName(workspacePath)
	pathValue := strings.ToLower(filepath.ToSlash(strings.TrimSpace(hit.Path)))
	if repoName == "" || pathValue == "" {
		return 0
	}
	canonicalPackagePrefix := "notes/repo/" + repoName + "/packages/"
	inboxPackagePrefix := "inbox/drafted-from-foxctl/repo-graph/" + repoName + "/packages/"
	if !strings.HasPrefix(pathValue, canonicalPackagePrefix) && !strings.HasPrefix(pathValue, inboxPackagePrefix) {
		return 0
	}
	boost := 0
	if matchesCodePaths(hit.RepoPaths, codeHints.Paths) {
		boost += 12
	}
	if matchesCodeSymbols(hit.Symbols, codeHints.Symbols) {
		boost += 8
	}
	if strings.HasPrefix(pathValue, canonicalPackagePrefix) {
		boost += 4
	}
	return boost
}

func packageNotePathCandidates(repoName string, paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" {
			continue
		}
		dir := path
		if ext := filepath.Ext(dir); ext != "" {
			dir = filepath.Dir(dir)
		}
		dir = filepath.ToSlash(strings.Trim(strings.TrimSpace(dir), "/"))
		if dir == "." || dir == "" {
			continue
		}
		slug := packageNoteSlug(dir)
		if slug == "" {
			continue
		}
		candidate := filepath.ToSlash(filepath.Join("notes", "repo", repoName, "packages", slug+".md"))
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func packageNoteSlug(path string) string {
	path = strings.Trim(filepath.ToSlash(strings.TrimSpace(path)), "/")
	if path == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range path {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r):
			b.WriteRune(unicode.ToLower(r))
			lastDash = false
		case r == '/':
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		case r == '-', r == '.':
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		case r == '_':
			// Drop underscores so praze_web -> prazeweb, matching current package-note naming.
		}
	}
	return strings.Trim(b.String(), "-")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func queryTypeBias(query string, hit obsidianindex.SearchHit, weights RetrievalWeights) int {
	q := strings.ToLower(strings.TrimSpace(query))
	bonus := 0
	if containsAll(q, "package") || containsAny(q, "runtime", "transport", "platform", "config", "api", "web", "semantic", "memory") {
		if strings.EqualFold(hit.Trust, "canonical") {
			bonus += weights.Canonical
		}
		if strings.EqualFold(hit.Type, "map") {
			bonus += weights.Map
		}
	}
	if containsAny(q, "decision", "policy", "contract", "why") {
		if strings.EqualFold(hit.Type, "adr") {
			bonus += weights.ADR
		}
		if strings.EqualFold(hit.Type, "pattern") {
			bonus += weights.Pattern
		}
	}
	if containsAny(q, "incident", "bug", "failure", "outage", "contradiction", "gotcha") {
		if strings.EqualFold(hit.Type, "incident") {
			bonus += weights.Incident
		}
		if strings.EqualFold(hit.Type, "investigation") {
			bonus += weights.Investigation
		}
	}
	return bonus
}

func containsAny(parts string, options ...string) bool {
	for _, option := range options {
		if strings.Contains(parts, option) {
			return true
		}
	}
	return false
}

func containsAll(query string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(query, part) {
			return false
		}
	}
	return true
}

func queryLooksCodeCentric(query string) bool {
	query = strings.TrimSpace(query)
	lower := strings.ToLower(query)
	if strings.ContainsAny(query, "/._:") {
		return true
	}
	for _, r := range query {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	for _, token := range []string{"package", "symbol", "function", "method", "class", "module", "controller", "api", "config"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

type retrievalCodeHints struct {
	Paths   []string
	Symbols []string
}

func deriveCodeHints(ctx context.Context, repo *repoindex.Store, query string, top *TopOfMind, latest *HandoffRecord) retrievalCodeHints {
	hints := retrievalCodeHints{}
	if top != nil {
		for _, ref := range top.RelevantRefs {
			if trimmed, ok := trimPathRef(ref); ok {
				hints.Paths = append(hints.Paths, trimmed)
			}
		}
	}
	if latest != nil {
		hints.Paths = append(hints.Paths, latest.Handoff.FilesTouched...)
		for _, ref := range latest.Handoff.EvidenceRefs {
			if trimmed, ok := trimPathRef(ref); ok {
				hints.Paths = append(hints.Paths, trimmed)
			}
		}
	}
	if repo != nil && strings.TrimSpace(query) != "" {
		engine := repoindex.NewQueryEngine(repo)
		if nodes, err := engine.SearchScored(ctx, query, 8); err == nil {
			for _, item := range nodes {
				if strings.TrimSpace(item.Node.File) != "" {
					hints.Paths = append(hints.Paths, item.Node.File)
				}
				if item.Node.Kind == repoindex.NodeSymbol && strings.TrimSpace(item.Node.Name) != "" {
					hints.Symbols = append(hints.Symbols, item.Node.Name)
				}
			}
		}
	}
	hints.Paths = uniqueStrings(hints.Paths)
	hints.Symbols = uniqueStrings(hints.Symbols)
	return hints
}

func continuityBundlePaths(top *TopOfMind, latest *HandoffRecord, opts RetrievalOptions) []string {
	if !opts.UseContinuityBundles {
		return nil
	}
	var paths []string
	if top != nil {
		for _, ref := range top.RelevantRefs {
			if trimmed, ok := trimPathRef(ref); ok {
				paths = append(paths, trimmed)
			}
		}
	}
	if latest != nil {
		paths = append(paths, latest.Handoff.FilesTouched...)
		for _, ref := range latest.Handoff.EvidenceRefs {
			if trimmed, ok := trimPathRef(ref); ok {
				paths = append(paths, trimmed)
			}
		}
	}
	return uniqueStrings(paths)
}

func trimPathRef(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "path:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(ref, "path:")), true
}

func matchesCodePaths(notePaths, codePaths []string) bool {
	for _, notePath := range notePaths {
		noteFull := strings.Trim(strings.ToLower(strings.TrimSpace(notePath)), "/")
		for _, codePath := range codePaths {
			codeFull := strings.Trim(strings.ToLower(strings.TrimSpace(codePath)), "/")
			if codeFull == "" || noteFull == "" {
				continue
			}
			if codeFull == noteFull || strings.HasPrefix(codeFull, noteFull+"/") || strings.HasPrefix(noteFull, codeFull+"/") {
				return true
			}
		}
	}
	return false
}

func matchesCodeSymbols(noteSymbols, codeSymbols []string) bool {
	for _, noteSymbol := range noteSymbols {
		normNote := strings.ToLower(strings.TrimSpace(noteSymbol))
		for _, codeSymbol := range codeSymbols {
			if normNote != "" && normNote == strings.ToLower(strings.TrimSpace(codeSymbol)) {
				return true
			}
		}
	}
	return false
}

func rerankVaultHits(ctx context.Context, query string, hits []RetrievalHit, weights RetrievalWeights, provider rerank.Provider, cfg rerank.Config) ([]RetrievalHit, bool, string, error) {
	if provider == nil || !cfg.Enabled || len(hits) == 0 {
		return hits, false, "", nil
	}
	topK := cfg.TopK
	if topK <= 0 || topK > len(hits) {
		topK = len(hits)
	}
	candidates := make([]rerank.Candidate, 0, topK)
	for _, hit := range hits[:topK] {
		candidates = append(candidates, rerank.Candidate{
			ID:            hit.Path,
			Content:       rerankContent(hit),
			OriginalScore: float64(hit.Score),
			Metadata: map[string]any{
				"path": hit.Path,
			},
		})
	}
	results, err := provider.Rerank(ctx, query, candidates, 0)
	if err != nil {
		return hits, false, provider.Model(), err
	}
	updated := make(map[string]RetrievalHit, len(hits))
	for _, hit := range hits {
		updated[hit.Path] = hit
	}
	bonusScale := weights.SemanticMatch * 10
	if bonusScale <= 0 {
		bonusScale = 1
	}
	for _, ranked := range results {
		hit := updated[ranked.ID]
		hit.Score += int(ranked.FinalScore * float64(bonusScale))
		updated[ranked.ID] = hit
	}
	out := make([]RetrievalHit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, updated[hit.Path])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Path < out[j].Path
	})
	if cfg.FinalK > 0 && len(out) > cfg.FinalK {
		out = out[:cfg.FinalK]
	}
	return out, true, provider.Model(), nil
}

func rerankContent(hit RetrievalHit) string {
	parts := []string{hit.Title, hit.Type, hit.Snippet}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func contradictionQuery(statement string) string {
	statement = strings.ToLower(strings.TrimSpace(statement))
	if statement == "" {
		return ""
	}
	stopwords := map[string]struct{}{
		"the": {}, "and": {}, "but": {}, "are": {}, "for": {}, "with": {}, "that": {}, "this": {}, "into": {}, "from": {}, "when": {}, "they": {}, "says": {}, "saying": {}, "note": {}, "runtime": {},
	}
	parts := strings.Fields(statement)
	keywords := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, ".,:;!?()[]{}\"'")
		if len(part) < 4 {
			continue
		}
		if _, ok := stopwords[part]; ok {
			continue
		}
		keywords = append(keywords, part)
		if len(keywords) >= 4 {
			break
		}
	}
	return strings.Join(keywords, " ")
}
