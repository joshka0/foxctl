package contextplane

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/indexing/rerank"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"gopkg.in/yaml.v3"
)

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
		SemanticMatch:  2,
	}
}

type retrievalPolicyFile struct {
	RankingWeights map[string]int `yaml:"ranking_weights"`
}

type scoredVaultHit struct {
	hit           obsidianindex.SearchHit
	lexicalScore  int
	semanticScore int
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
	override("semantic_match", &weights.SemanticMatch)
	return weights
}

// Retrieve blends ACA state with ranked vault hits.
func (s *WorkspaceStore) Retrieve(ctx context.Context, index obsidianindex.Store, repo *repoindex.Store, semanticProvider semantic.EmbeddingProvider, query string, limit int) (RetrievalResult, error) {
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
	if index == nil || strings.TrimSpace(query) == "" {
		return result, nil
	}
	codeHints := deriveCodeHints(ctx, repo, query, result.TopOfMind, report.LatestHandoff)
	hits, err := index.SearchNotes(ctx, query, limit*3)
	if err != nil {
		return RetrievalResult{}, err
	}
	byPath := map[string]*scoredVaultHit{}
	for _, hit := range hits {
		byPath[hit.Path] = &scoredVaultHit{hit: hit, lexicalScore: hit.Score}
	}
	maxSemantic := 0
	if semanticProvider != nil {
		if semanticHits, err := index.SearchNotesSemantic(ctx, query, semanticProvider, limit*3); err == nil {
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
	vaultHits := make([]RetrievalHit, 0, len(byPath))
	codeCentric := queryLooksCodeCentric(query)
	for _, hit := range byPath {
		vaultHits = append(vaultHits, scoreVaultHit(*hit, maxSemantic, codeCentric, result, report, codeHints))
	}
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

func scoreVaultHit(entry scoredVaultHit, maxSemantic int, codeCentric bool, result RetrievalResult, report Report, codeHints retrievalCodeHints) RetrievalHit {
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
	if result.TopOfMind != nil && matchesRefs(hit.Path, result.TopOfMind.RelevantRefs) {
		score += result.Weights.RelevantRef
	}
	if report.LatestHandoff != nil && matchesRefs(hit.Path, append(report.LatestHandoff.Handoff.EvidenceRefs, report.LatestHandoff.Handoff.FilesTouched...)) {
		score += result.Weights.HandoffRef
	}
	if codeCentric && matchesCodePaths(hit.RepoPaths, codeHints.Paths) {
		score += result.Weights.CodePath
	}
	if codeCentric && matchesCodeSymbols(hit.Symbols, codeHints.Symbols) {
		score += result.Weights.CodeSymbol
	}
	return RetrievalHit{
		Path:    hit.Path,
		Title:   hit.Title,
		Type:    hit.Type,
		Trust:   hit.Trust,
		Score:   score,
		Snippet: hit.Snippet,
	}
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
