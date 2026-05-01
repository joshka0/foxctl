package contextengine

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// BundleReductionOptions configures deterministic reduction from evidence packs.
type BundleReductionOptions struct {
	MaxFacts             int
	MaxPaths             int
	MaxEvidencePerPath   int
	MaxContextChars      int
	MinConfidence        float64
	IncludeStale         bool
	TaskType             string
	SourceProfiles       []SourceProfile
	RequiredEvidence     []string
	CoverageRequirements []CoverageRequirement
	IDGen                IDGen
	Clock                ClockFunc
}

func (o BundleReductionOptions) defaults() BundleReductionOptions {
	if o.MaxFacts <= 0 {
		o.MaxFacts = 12
	}
	if o.MaxPaths <= 0 {
		o.MaxPaths = o.MaxFacts
	}
	if o.MaxEvidencePerPath <= 0 {
		o.MaxEvidencePerPath = 2
	}
	if o.IDGen == nil {
		o.IDGen = defaultContextIDGen("ctx")
	}
	if o.Clock == nil {
		o.Clock = func() time.Time { return time.Now().UTC() }
	}
	return o
}

// ReduceEvidenceToBundle reduces one EvidencePack into a ContextBundle.
func ReduceEvidenceToBundle(pack EvidencePack, opts BundleReductionOptions) (ContextBundle, error) {
	return ReduceEvidencePacksToBundle(pack.Query, "", []EvidencePack{pack}, opts)
}

// ReduceEvidencePacksToBundle reduces retrieval packs into a bounded context bundle.
func ReduceEvidencePacksToBundle(query, goal string, packs []EvidencePack, opts BundleReductionOptions) (ContextBundle, error) {
	opts = opts.defaults()
	query = strings.TrimSpace(query)
	if query == "" && len(packs) > 0 {
		query = packs[0].Query
	}
	if query == "" {
		return ContextBundle{}, EmptyQueryError{Lane: LaneMixed}
	}

	workspaceID := ""
	var telemetry EvidenceTelemetry
	sourcePackIDs := make([]string, 0, len(packs))
	sourceEpisodeIDs := make([]string, 0, len(packs))
	sourceCoverage := map[string]int{}
	evidenceByRef := map[string]EvidenceNode{}

	for i, pack := range packs {
		if err := pack.Validate(); err != nil {
			return ContextBundle{}, fmt.Errorf("reduce evidence: pack[%d]: %w", i, err)
		}
		if workspaceID == "" {
			workspaceID = pack.WorkspaceID
		}
		sourcePackIDs = append(sourcePackIDs, pack.ID)
		if episodeID := extractEpisodeID(pack.Metadata); episodeID != "" {
			sourceEpisodeIDs = append(sourceEpisodeIDs, episodeID)
		}
		telemetry.DurationMs += pack.Telemetry.DurationMs
		telemetry.TokensUsed += pack.Telemetry.TokensUsed
		if pack.Telemetry.LanesFused > telemetry.LanesFused {
			telemetry.LanesFused = pack.Telemetry.LanesFused
		}
		if len(pack.Nodes) == 0 {
			sourceCoverage[string(pack.Lane)] += 0
			continue
		}
		for _, node := range pack.Nodes {
			if node.Confidence > 0 && node.Confidence < opts.MinConfidence {
				continue
			}
			key := evidenceRefKey(node.Ref)
			current, ok := evidenceByRef[key]
			if !ok || strongerEvidence(node, current) {
				evidenceByRef[key] = node
			}
			lanes := extractLanes(node.Metadata)
			if len(lanes) == 0 {
				lanes = []string{string(nodeTypeToLane(node.NodeType))}
			}
			for _, lane := range lanes {
				sourceCoverage[lane]++
			}
		}
	}

	evidence := make([]EvidenceNode, 0, len(evidenceByRef))
	for _, node := range evidenceByRef {
		evidence = append(evidence, node)
	}
	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].Confidence != evidence[j].Confidence {
			return evidence[i].Confidence > evidence[j].Confidence
		}
		left := FormatEvidenceRef(evidence[i].Ref)
		right := FormatEvidenceRef(evidence[j].Ref)
		if left != right {
			return left < right
		}
		return evidence[i].ID < evidence[j].ID
	})

	rawContextChars := evidenceContextChars(evidence)
	requirements := normalizeCoverageRequirements(opts.CoverageRequirements, opts.RequiredEvidence, opts.SourceProfiles)
	evidence, coverageReport, omittedByPathSelection := selectEvidenceCoverageAware(evidence, opts, requirements)
	telemetry.OmittedContextItems += omittedByPathSelection
	evidence, omittedByBudget := applyEvidenceContextBudget(evidence, opts.MaxContextChars)
	coverageReport = filterCoverageReportForEvidence(coverageReport, evidence)
	telemetry.OmittedContextItems += omittedByBudget
	telemetry.RawContextChars = rawContextChars
	telemetry.EmittedContextChars = evidenceContextChars(evidence)

	facts := make([]ContextFact, 0, len(evidence))
	for _, node := range evidence {
		statement := strings.TrimSpace(node.Statement)
		if statement == "" {
			statement = refTitle(node.Ref)
		}
		if statement == "" {
			continue
		}
		fact := ContextFact{
			ID:            opts.IDGen(),
			WorkspaceID:   node.WorkspaceID,
			Kind:          node.NodeType,
			Fact:          statement,
			Refs:          []EvidenceRef{node.Ref},
			EvidenceIDs:   []string{node.ID},
			Confidence:    node.Confidence,
			Grounding:     node.Grounding,
			Status:        factStatusForNode(node),
			SourcePackIDs: append([]string(nil), sourcePackIDs...),
			Metadata:      copyMetadata(node.Metadata),
		}
		facts = append(facts, fact)
	}
	selectedPaths := buildSelectedPaths(evidence)
	categories := buildContextCategories(selectedPaths, opts)
	integrationEdges := buildContextIntegrationEdges(categories)
	answerCandidates := buildAnswerCandidates(facts, selectedPaths, opts.IDGen)

	status := ContextBundleStatusSufficient
	answerable := true
	var missing []ContextGap
	if len(facts) == 0 {
		status = ContextBundleStatusPartial
		answerable = false
		missing = append(missing, ContextGap{
			ID:       opts.IDGen(),
			Required: "evidence",
			Reason:   "no evidence nodes survived reduction",
		})
	}

	bundle := ContextBundle{
		ID:               opts.IDGen(),
		WorkspaceID:      workspaceID,
		Query:            query,
		Goal:             strings.TrimSpace(goal),
		Status:           status,
		Answerable:       answerable,
		Summary:          bundleSummary(facts),
		Categories:       categories,
		IntegrationEdges: integrationEdges,
		SelectedPaths:    selectedPaths,
		CoverageReport:   coverageReport,
		AnswerCandidates: answerCandidates,
		Facts:            facts,
		Evidence:         evidence,
		Missing:          missing,
		SourceCoverage:   sourceCoverage,
		SourcePackIDs:    sourcePackIDs,
		SourceEpisodeIDs: sourceEpisodeIDs,
		Telemetry:        telemetry,
		CreatedAt:        opts.Clock(),
		Metadata: map[string]any{
			"task_type":       strings.TrimSpace(opts.TaskType),
			"source_profiles": sourceProfilesToStrings(opts.SourceProfiles),
		},
	}
	if err := bundle.Validate(); err != nil {
		return ContextBundle{}, err
	}
	return bundle, nil
}

type reductionPathAcc struct {
	path        string
	nodes       []EvidenceNode
	score       float64
	bestRef     string
	bestNodeID  string
	firstRank   int
	support     int
	requiredHit int
	coverage    map[string]float64
}

func selectEvidenceCoverageAware(evidence []EvidenceNode, opts BundleReductionOptions, requirements []CoverageRequirement) ([]EvidenceNode, *CoverageReport, int) {
	if opts.MaxFacts <= 0 || len(evidence) <= opts.MaxFacts {
		report := buildCoverageReport(evidence, requirements)
		return evidence, report, 0
	}
	byPath := map[string]*reductionPathAcc{}
	var nonPath []EvidenceNode
	for idx, node := range evidence {
		path := evidenceNodePath(node)
		if path == "" {
			nonPath = append(nonPath, node)
			continue
		}
		item, ok := byPath[path]
		if !ok {
			item = &reductionPathAcc{path: path, firstRank: idx, bestRef: FormatEvidenceRef(node.Ref), bestNodeID: node.ID}
			byPath[path] = item
		}
		item.nodes = append(item.nodes, node)
		item.support++
		hits := requiredEvidenceHits(node, opts.RequiredEvidence)
		item.requiredHit += hits
		nodeScore := pathEvidenceScore(node, opts, hits)
		nodeCoverage := nodeCoverageScores(node, requirements)
		if len(nodeCoverage) > 0 && item.coverage == nil {
			item.coverage = map[string]float64{}
		}
		for id, score := range nodeCoverage {
			if score > item.coverage[id] {
				item.coverage[id] = score
			}
		}
		if nodeScore > item.score {
			item.score = nodeScore
			item.bestRef = FormatEvidenceRef(node.Ref)
			item.bestNodeID = node.ID
		}
	}
	if len(byPath) == 0 {
		if len(evidence) > opts.MaxFacts {
			selected := evidence[:opts.MaxFacts]
			return selected, buildCoverageReport(selected, requirements), len(evidence) - opts.MaxFacts
		}
		return evidence, buildCoverageReport(evidence, requirements), 0
	}
	paths := make([]*reductionPathAcc, 0, len(byPath))
	for _, item := range byPath {
		item.score += supportDensityScore(item.support)
		item.score += requiredCoverageScore(item.requiredHit, len(opts.RequiredEvidence))
		for reqID, coverageScore := range item.coverage {
			if coverageScore <= 0 {
				delete(item.coverage, reqID)
			}
		}
		if isLikelyTestPath(item.path) && !strings.Contains(strings.ToLower(strings.TrimSpace(opts.TaskType)), "test") {
			item.score -= 0.12
		}
		paths = append(paths, item)
	}
	sort.SliceStable(paths, func(i, j int) bool {
		if paths[i].score != paths[j].score {
			return paths[i].score > paths[j].score
		}
		if paths[i].support != paths[j].support {
			return paths[i].support > paths[j].support
		}
		if paths[i].firstRank != paths[j].firstRank {
			return paths[i].firstRank < paths[j].firstRank
		}
		return paths[i].path < paths[j].path
	})
	maxPaths := opts.MaxPaths
	if maxPaths <= 0 || maxPaths > opts.MaxFacts {
		maxPaths = opts.MaxFacts
	}
	selectedPaths := coverageSeedPathSet(paths, requirements, maxPaths, opts)
	for _, item := range paths {
		if len(selectedPaths) >= maxPaths {
			break
		}
		if _, ok := selectedPaths[item.path]; ok {
			continue
		}
		selectedPaths[item.path] = struct{}{}
	}
	selected := make([]EvidenceNode, 0, opts.MaxFacts)
	selectedIDs := map[string]struct{}{}
	for _, item := range paths {
		if len(selected) >= opts.MaxFacts || len(selectedIDs) >= opts.MaxFacts || maxPaths <= 0 {
			break
		}
		if _, ok := selectedPaths[item.path]; !ok {
			continue
		}
		sort.SliceStable(item.nodes, func(i, j int) bool {
			leftHits := requiredEvidenceHits(item.nodes[i], opts.RequiredEvidence)
			rightHits := requiredEvidenceHits(item.nodes[j], opts.RequiredEvidence)
			leftCoverage := len(nodeCoverageScores(item.nodes[i], requirements))
			rightCoverage := len(nodeCoverageScores(item.nodes[j], requirements))
			if leftCoverage != rightCoverage {
				return leftCoverage > rightCoverage
			}
			if leftHits != rightHits {
				return leftHits > rightHits
			}
			if item.nodes[i].Confidence != item.nodes[j].Confidence {
				return item.nodes[i].Confidence > item.nodes[j].Confidence
			}
			return FormatEvidenceRef(item.nodes[i].Ref) < FormatEvidenceRef(item.nodes[j].Ref)
		})
		keptForPath := 0
		for _, node := range item.nodes {
			if len(selected) >= opts.MaxFacts || keptForPath >= opts.MaxEvidencePerPath {
				break
			}
			if _, ok := selectedIDs[node.ID]; ok {
				continue
			}
			selectedIDs[node.ID] = struct{}{}
			selected = append(selected, node)
			keptForPath++
		}
		maxPaths--
	}
	for _, node := range nonPath {
		if len(selected) >= opts.MaxFacts {
			break
		}
		if _, ok := selectedIDs[node.ID]; ok {
			continue
		}
		selectedIDs[node.ID] = struct{}{}
		selected = append(selected, node)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		leftPath := evidenceNodePath(selected[i])
		rightPath := evidenceNodePath(selected[j])
		if leftPath != rightPath {
			return selectedPathRank(paths, leftPath) < selectedPathRank(paths, rightPath)
		}
		if selected[i].Confidence != selected[j].Confidence {
			return selected[i].Confidence > selected[j].Confidence
		}
		return selected[i].ID < selected[j].ID
	})
	if len(selected) >= len(evidence) {
		return selected, buildCoverageReport(selected, requirements), 0
	}
	return selected, buildCoverageReport(selected, requirements), len(evidence) - len(selected)
}

func coverageSeedPathSet(paths []*reductionPathAcc, requirements []CoverageRequirement, maxPaths int, opts BundleReductionOptions) map[string]struct{} {
	selected := map[string]struct{}{}
	if maxPaths <= 0 || len(requirements) == 0 {
		return selected
	}
	for _, req := range requirements {
		slots := req.MinPaths
		if slots <= 0 {
			slots = 1
		}
		for slots > 0 && len(selected) < maxPaths {
			best := bestPathForCoverage(paths, req.ID, selected, opts)
			if best == nil {
				break
			}
			selected[best.path] = struct{}{}
			slots--
		}
	}
	return selected
}

func bestPathForCoverage(paths []*reductionPathAcc, requirementID string, selected map[string]struct{}, opts BundleReductionOptions) *reductionPathAcc {
	best := bestPathForCoverageWithTestPolicy(paths, requirementID, selected, opts, false)
	if best != nil {
		return best
	}
	return bestPathForCoverageWithTestPolicy(paths, requirementID, selected, opts, true)
}

func bestPathForCoverageWithTestPolicy(paths []*reductionPathAcc, requirementID string, selected map[string]struct{}, opts BundleReductionOptions, allowTests bool) *reductionPathAcc {
	var best *reductionPathAcc
	bestScore := 0.0
	for _, item := range paths {
		if _, ok := selected[item.path]; ok {
			continue
		}
		if !allowTests && isLikelyTestPath(item.path) && !strings.Contains(strings.ToLower(strings.TrimSpace(opts.TaskType)), "test") {
			continue
		}
		coverageScore := item.coverage[requirementID]
		if coverageScore <= 0 {
			continue
		}
		score := coverageScore*10 + item.score
		if best == nil || score > bestScore || (score == bestScore && item.firstRank < best.firstRank) {
			best = item
			bestScore = score
		}
	}
	return best
}

func pathEvidenceScore(node EvidenceNode, opts BundleReductionOptions, requiredHits int) float64 {
	score := node.Confidence
	switch node.Grounding {
	case GroundingLoaded, GroundingValidated:
		score += 0.12
	case GroundingIndexed:
		score += 0.08
	case GroundingSemantic:
		score += 0.04
	}
	if node.Ref.Type == RefTypePath {
		score += 0.06
	}
	if role := metadataString(node.Metadata, "candidate_role"); role != "" {
		score += candidateRoleScore(role)
	}
	if sourceProfileMatch(node, opts.SourceProfiles) {
		score += 0.12
	}
	if sourceSignalMatch(node, SourceProfileCodemaps, opts.SourceProfiles) {
		score += 0.08
	}
	if sourceSignalMatch(node, SourceProfileCochangeHistory, opts.SourceProfiles) {
		score += 0.05
	}
	if requiredHits > 0 {
		score += 0.18 * float64(requiredHits)
	}
	return score
}

func supportDensityScore(count int) float64 {
	if count <= 1 {
		return 0
	}
	if count > 4 {
		count = 4
	}
	return 0.04 * float64(count-1)
}

func sourceProfileMatch(node EvidenceNode, requested []SourceProfile) bool {
	if len(requested) == 0 {
		return false
	}
	nodeProfile := SourceProfile(strings.TrimSpace(strings.ToLower(metadataString(node.Metadata, "source_profile"))))
	if !nodeProfile.IsValid() {
		return false
	}
	for _, profile := range requested {
		if profile == nodeProfile {
			return true
		}
	}
	return false
}

func sourceSignalMatch(node EvidenceNode, profile SourceProfile, requested []SourceProfile) bool {
	if len(requested) == 0 {
		return false
	}
	want := false
	for _, requestedProfile := range requested {
		if requestedProfile == profile {
			want = true
			break
		}
	}
	if !want {
		return false
	}
	signals := evidenceSourceSignals(node.Metadata)
	switch profile {
	case SourceProfileCodemaps:
		for _, signal := range signals {
			if signal == "codemaps" || signal == "codemap" || signal == "semantic_search_code" {
				return true
			}
		}
	case SourceProfileCochangeHistory:
		for _, signal := range signals {
			if strings.Contains(signal, "cochange") || strings.Contains(signal, "co_change") {
				return true
			}
		}
	}
	return false
}

func requiredCoverageScore(hits int, requiredCount int) float64 {
	if hits <= 0 || requiredCount <= 0 {
		return 0
	}
	if hits > requiredCount {
		hits = requiredCount
	}
	return 0.20 * (float64(hits) / float64(requiredCount))
}

func candidateRoleScore(role string) float64 {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "symbol_definition":
		return 0.30
	case "registration_file":
		return 0.28
	case "tool_declaration":
		return 0.25
	case "documentation_map":
		return 0.24
	case "documentation_anchor":
		return 0.22
	case "primary_anchor":
		return 0.20
	case "direct_dispatch_file":
		return 0.18
	case "definition_support":
		return 0.14
	case "structural_support":
		return 0.08
	case "test_support":
		return -0.08
	default:
		return 0
	}
}

func requiredEvidenceHits(node EvidenceNode, required []string) int {
	if len(required) == 0 {
		return 0
	}
	terms := evidenceNodeCoverageTerms(node)
	hits := 0
	for _, item := range required {
		req := CoverageRequirement{Terms: normalizeCoverageTerms([]string{item})}
		if coverageScoreForRequirement(terms, req) > 0 {
			hits++
		}
	}
	return hits
}

func splitEvidenceCoverageTerms(value string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for _, part := range strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= '0' && r <= '9':
			return false
		default:
			return true
		}
	}) {
		part = strings.TrimSpace(part)
		if len(part) < 4 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func nodeCoverageScores(node EvidenceNode, requirements []CoverageRequirement) map[string]float64 {
	if len(requirements) == 0 {
		return nil
	}
	terms := evidenceNodeCoverageTerms(node)
	out := map[string]float64{}
	for _, req := range requirements {
		score := coverageScoreForRequirement(terms, req)
		if score <= 0 {
			continue
		}
		if req.Weight > 0 {
			score *= req.Weight
		}
		out[req.ID] = score
	}
	return out
}

func evidenceNodeCoverageTerms(node EvidenceNode) map[string]struct{} {
	terms := map[string]struct{}{}
	add := func(values ...string) {
		for _, value := range values {
			for _, term := range normalizeCoverageTerms([]string{value}) {
				terms[term] = struct{}{}
			}
		}
	}
	add(node.Statement, node.Ref.Ref, node.Ref.Title, node.Ref.Excerpt, metadataString(node.Metadata, "path"))
	for _, term := range metadataStringSlice(node.Metadata, "matched_terms") {
		add(term)
	}
	for _, term := range metadataStringSlice(node.Metadata, "coverage_terms") {
		add(term)
	}
	for _, reqID := range metadataStringSlice(node.Metadata, "coverage_requirement_ids") {
		terms[strings.ToLower(strings.TrimSpace(reqID))] = struct{}{}
	}
	return terms
}

func coverageScoreForRequirement(terms map[string]struct{}, req CoverageRequirement) float64 {
	if len(terms) == 0 {
		return 0
	}
	for _, id := range []string{req.ID, strings.ToLower(strings.TrimSpace(req.ID))} {
		if id != "" {
			if _, ok := terms[id]; ok {
				return 1
			}
		}
	}
	reqTerms := req.Terms
	if len(reqTerms) == 0 {
		reqTerms = normalizeCoverageTerms([]string{req.Label})
	}
	if len(reqTerms) == 0 {
		return 0
	}
	covered := 0
	for _, term := range reqTerms {
		if _, ok := terms[term]; ok {
			covered++
		}
	}
	switch {
	case covered == len(reqTerms):
		return 1
	case len(reqTerms) >= 4 && covered >= len(reqTerms)-1:
		return float64(covered) / float64(len(reqTerms))
	default:
		return 0
	}
}

func buildCoverageReport(evidence []EvidenceNode, requirements []CoverageRequirement) *CoverageReport {
	if len(requirements) == 0 {
		return nil
	}
	report := &CoverageReport{
		Requirements: append([]CoverageRequirement(nil), requirements...),
	}
	coveredRequirements := map[string]struct{}{}
	for _, node := range evidence {
		path := evidenceNodePath(node)
		if path == "" {
			continue
		}
		for reqID, score := range nodeCoverageScores(node, requirements) {
			coveredRequirements[reqID] = struct{}{}
			report.Covered = append(report.Covered, PathCoverage{
				RequirementID: reqID,
				Path:          path,
				EvidenceIDs:   []string{node.ID},
				Score:         score,
			})
		}
	}
	sort.SliceStable(report.Covered, func(i, j int) bool {
		if report.Covered[i].RequirementID != report.Covered[j].RequirementID {
			return report.Covered[i].RequirementID < report.Covered[j].RequirementID
		}
		if report.Covered[i].Score != report.Covered[j].Score {
			return report.Covered[i].Score > report.Covered[j].Score
		}
		return report.Covered[i].Path < report.Covered[j].Path
	})
	for _, req := range requirements {
		if !req.Required {
			continue
		}
		if _, ok := coveredRequirements[req.ID]; !ok {
			report.Missing = append(report.Missing, req.ID)
		}
	}
	return report
}

func filterCoverageReportForEvidence(report *CoverageReport, evidence []EvidenceNode) *CoverageReport {
	if report == nil {
		return nil
	}
	evidenceIDs := map[string]struct{}{}
	for _, node := range evidence {
		evidenceIDs[node.ID] = struct{}{}
	}
	filtered := &CoverageReport{
		Requirements: append([]CoverageRequirement(nil), report.Requirements...),
		Missing:      append([]string(nil), report.Missing...),
	}
	coveredRequirements := map[string]struct{}{}
	for _, item := range report.Covered {
		ids := make([]string, 0, len(item.EvidenceIDs))
		for _, id := range item.EvidenceIDs {
			if _, ok := evidenceIDs[id]; ok {
				ids = append(ids, id)
			}
		}
		if len(item.EvidenceIDs) > 0 && len(ids) == 0 {
			continue
		}
		item.EvidenceIDs = ids
		filtered.Covered = append(filtered.Covered, item)
		coveredRequirements[item.RequirementID] = struct{}{}
	}
	filtered.Missing = filtered.Missing[:0]
	for _, req := range filtered.Requirements {
		if !req.Required {
			continue
		}
		if _, ok := coveredRequirements[req.ID]; !ok {
			filtered.Missing = append(filtered.Missing, req.ID)
		}
	}
	return filtered
}

func selectedPathRank(paths []*reductionPathAcc, path string) int {
	if path == "" {
		return len(paths) + 1
	}
	for i, item := range paths {
		if item.path == path {
			return i
		}
	}
	return len(paths) + 1
}

func isLikelyTestPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	return strings.Contains(path, "_test.") || strings.Contains(path, "/test/") || strings.Contains(path, "/tests/")
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func selectedPathMetadataString(metadata map[string]any, key string) string {
	return metadataString(metadata, key)
}

func metadataStringSlice(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = appendUniqueString(out, item)
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = appendUniqueString(out, fmt.Sprint(item))
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	default:
		return nil
	}
}

func evidenceSourceSignals(metadata map[string]any) []string {
	out := make([]string, 0, 4)
	for _, key := range []string{"source", "source_profile", "candidate_role", "evidence_class"} {
		if value := metadataString(metadata, key); value != "" {
			out = appendUniqueString(out, value)
		}
	}
	for _, value := range metadataStringSlice(metadata, "sources") {
		out = appendUniqueString(out, value)
	}
	return out
}

func buildSelectedPaths(evidence []EvidenceNode) []ContextSelectedPath {
	type acc struct {
		path        string
		evidenceIDs []string
		refs        []EvidenceRef
		coverageIDs []string
		confidence  float64
		reason      string
		metadata    map[string]any
	}
	byPath := map[string]*acc{}
	order := make([]string, 0)
	for _, node := range evidence {
		path := evidenceNodePath(node)
		if path == "" {
			continue
		}
		item, ok := byPath[path]
		if !ok {
			item = &acc{
				path:       path,
				confidence: node.Confidence,
				reason:     selectedPathReason(node),
				metadata:   selectedPathMetadata(node),
			}
			byPath[path] = item
			order = append(order, path)
		}
		item.evidenceIDs = appendUniqueString(item.evidenceIDs, node.ID)
		item.refs = appendEvidenceRefUnique(item.refs, node.Ref)
		for _, id := range metadataStringSlice(node.Metadata, "coverage_requirement_ids") {
			item.coverageIDs = appendUniqueString(item.coverageIDs, id)
		}
		if node.Confidence > item.confidence {
			item.confidence = node.Confidence
			item.reason = selectedPathReason(node)
			item.metadata = selectedPathMetadata(node)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		left := byPath[order[i]]
		right := byPath[order[j]]
		if left.confidence != right.confidence {
			return left.confidence > right.confidence
		}
		return left.path < right.path
	})
	out := make([]ContextSelectedPath, 0, len(order))
	for i, path := range order {
		item := byPath[path]
		out = append(out, ContextSelectedPath{
			Path:        item.path,
			EvidenceIDs: append([]string(nil), item.evidenceIDs...),
			Refs:        append([]EvidenceRef(nil), item.refs...),
			CoverageIDs: append([]string(nil), item.coverageIDs...),
			Confidence:  item.confidence,
			Rank:        i + 1,
			Reason:      item.reason,
			Metadata:    item.metadata,
		})
	}
	return out
}

func buildAnswerCandidates(facts []ContextFact, selectedPaths []ContextSelectedPath, idGen IDGen) []ContextAnswerCandidate {
	out := make([]ContextAnswerCandidate, 0, len(selectedPaths)+len(facts))
	for _, selected := range selectedPaths {
		out = append(out, ContextAnswerCandidate{
			ID:          idGen(),
			Kind:        "path",
			Value:       selected.Path,
			EvidenceIDs: append([]string(nil), selected.EvidenceIDs...),
			Refs:        append([]EvidenceRef(nil), selected.Refs...),
			Confidence:  selected.Confidence,
			Rank:        len(out) + 1,
			Reason:      selected.Reason,
			Metadata:    copyMetadata(selected.Metadata),
		})
	}
	for _, fact := range facts {
		out = append(out, ContextAnswerCandidate{
			ID:          idGen(),
			Kind:        answerCandidateKind(fact),
			Value:       fact.Fact,
			EvidenceIDs: append([]string(nil), fact.EvidenceIDs...),
			Refs:        append([]EvidenceRef(nil), fact.Refs...),
			Confidence:  fact.Confidence,
			Rank:        len(out) + 1,
			Reason:      "reduced context fact",
			Metadata:    copyMetadata(fact.Metadata),
		})
	}
	return out
}

func evidenceNodePath(node EvidenceNode) string {
	if node.Ref.Type == RefTypePath {
		return normalizeSelectedPath(node.Ref.Ref)
	}
	if path, ok := node.Metadata["path"].(string); ok {
		return normalizeSelectedPath(path)
	}
	return ""
}

func normalizeSelectedPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	return path
}

func selectedPathReason(node EvidenceNode) string {
	switch node.Ref.Type {
	case RefTypePath:
		return "direct path evidence"
	case RefTypeSymbol:
		return "symbol evidence with source path"
	default:
		return "evidence metadata path"
	}
}

func selectedPathMetadata(node EvidenceNode) map[string]any {
	out := map[string]any{
		"ref_type":  string(node.Ref.Type),
		"node_type": string(node.NodeType),
	}
	for _, key := range []string{"candidate_role", "source_profile", "file_kind", "evidence_class", "source", "supporting_path"} {
		if value := metadataString(node.Metadata, key); value != "" {
			out[key] = value
		}
	}
	if node.Grounding != "" {
		out["grounding"] = string(node.Grounding)
	}
	if signals := evidenceSourceSignals(node.Metadata); len(signals) > 0 {
		out["signals"] = signals
	}
	if coverageIDs := metadataStringSlice(node.Metadata, "coverage_requirement_ids"); len(coverageIDs) > 0 {
		out["coverage_ids"] = coverageIDs
	}
	return out
}

type contextCategoryAcc struct {
	name        string
	role        string
	paths       []string
	evidenceIDs []string
	signals     []string
	rank        int
	metadata    map[string]any
}

func buildContextCategories(selectedPaths []ContextSelectedPath, opts BundleReductionOptions) []ContextCategory {
	if len(selectedPaths) == 0 || !shouldBuildContextMap(opts) {
		return nil
	}
	byName := map[string]*contextCategoryAcc{}
	order := make([]string, 0)
	for _, selected := range selectedPaths {
		name := contextCategoryName(selected)
		if name == "" {
			continue
		}
		item, ok := byName[name]
		if !ok {
			item = &contextCategoryAcc{
				name:     name,
				role:     contextCategoryRole(selected),
				rank:     len(order) + 1,
				metadata: map[string]any{"grouping": "path_family"},
			}
			byName[name] = item
			order = append(order, name)
		}
		item.paths = appendUniqueString(item.paths, selected.Path)
		for _, id := range selected.EvidenceIDs {
			item.evidenceIDs = appendUniqueString(item.evidenceIDs, id)
		}
		for _, signal := range selectedPathSignals(selected) {
			item.signals = appendUniqueString(item.signals, signal)
		}
	}
	out := make([]ContextCategory, 0, len(order))
	for _, name := range order {
		item := byName[name]
		out = append(out, ContextCategory{
			Name:        item.name,
			Role:        item.role,
			Paths:       append([]string(nil), item.paths...),
			EvidenceIDs: append([]string(nil), item.evidenceIDs...),
			Signals:     append([]string(nil), item.signals...),
			Rank:        item.rank,
			Metadata:    copyMetadata(item.metadata),
		})
	}
	return out
}

func buildContextIntegrationEdges(categories []ContextCategory) []ContextIntegrationEdge {
	if len(categories) < 2 {
		return nil
	}
	out := make([]ContextIntegrationEdge, 0, len(categories)-1)
	for i := 0; i+1 < len(categories); i++ {
		left := categories[i]
		right := categories[i+1]
		paths := make([]string, 0, 2)
		if len(left.Paths) > 0 {
			paths = append(paths, left.Paths[0])
		}
		if len(right.Paths) > 0 {
			paths = append(paths, right.Paths[0])
		}
		signals := append([]string(nil), left.Signals...)
		for _, signal := range right.Signals {
			signals = appendUniqueString(signals, signal)
		}
		evidenceIDs := append([]string(nil), left.EvidenceIDs...)
		for _, id := range right.EvidenceIDs {
			evidenceIDs = appendUniqueString(evidenceIDs, id)
		}
		out = append(out, ContextIntegrationEdge{
			From:        left.Name,
			To:          right.Name,
			Paths:       paths,
			EvidenceIDs: evidenceIDs,
			Signals:     signals,
			Metadata:    map[string]any{"edge_kind": "selected_path_order"},
		})
	}
	return out
}

func shouldBuildContextMap(opts BundleReductionOptions) bool {
	switch strings.TrimSpace(strings.ToLower(opts.TaskType)) {
	case "architecture_map", "subsystem_map", "integration_surface", "change_impact":
		return true
	}
	for _, profile := range opts.SourceProfiles {
		switch profile {
		case SourceProfileCodemaps, SourceProfileCochangeHistory:
			return true
		}
	}
	return false
}

func contextCategoryName(selected ContextSelectedPath) string {
	path := normalizeSelectedPath(selected.Path)
	if path == "" {
		return ""
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "." || dir == "" {
		return "repo root"
	}
	return dir
}

func contextCategoryRole(selected ContextSelectedPath) string {
	role := selectedPathMetadataString(selected.Metadata, "candidate_role")
	profile := selectedPathMetadataString(selected.Metadata, "source_profile")
	switch {
	case role != "" && profile != "":
		return profile + " / " + role
	case role != "":
		return role
	case profile != "":
		return profile
	default:
		return "selected path family"
	}
}

func selectedPathSignals(selected ContextSelectedPath) []string {
	out := make([]string, 0, 4)
	for _, key := range []string{"candidate_role", "source_profile", "evidence_class", "source", "grounding"} {
		if value := selectedPathMetadataString(selected.Metadata, key); value != "" {
			out = appendUniqueString(out, value)
		}
	}
	for _, signal := range metadataStringSlice(selected.Metadata, "signals") {
		out = appendUniqueString(out, signal)
	}
	return out
}

func answerCandidateKind(fact ContextFact) string {
	if len(fact.Refs) > 0 {
		switch fact.Refs[0].Type {
		case RefTypePath:
			return "path_fact"
		case RefTypeSymbol:
			return "symbol"
		case RefTypeMemoryClaim:
			return "memory"
		case RefTypeSession:
			return "session"
		case RefTypeTask:
			return "task"
		case RefTypeNote:
			return "note"
		}
	}
	return string(fact.Kind)
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendEvidenceRefUnique(values []EvidenceRef, ref EvidenceRef) []EvidenceRef {
	key := evidenceRefKey(ref)
	for _, existing := range values {
		if evidenceRefKey(existing) == key {
			return values
		}
	}
	return append(values, ref)
}

func evidenceRefKey(ref EvidenceRef) string {
	return string(ref.Type) + "\x00" + ref.Ref
}

func strongerEvidence(candidate, current EvidenceNode) bool {
	if candidate.Confidence != current.Confidence {
		return candidate.Confidence > current.Confidence
	}
	if groundingRank(candidate.Grounding) != groundingRank(current.Grounding) {
		return groundingRank(candidate.Grounding) > groundingRank(current.Grounding)
	}
	return candidate.ID < current.ID
}

func groundingRank(g Grounding) int {
	switch g {
	case GroundingValidated:
		return 5
	case GroundingLoaded:
		return 4
	case GroundingIndexed:
		return 3
	case GroundingSemantic:
		return 2
	case GroundingInferred:
		return 1
	default:
		return 0
	}
}

func factStatusForNode(node EvidenceNode) ContextFactStatus {
	if status, ok := node.Metadata["status"].(string); ok {
		switch ClaimStatus(status) {
		case ClaimStatusCandidate, ClaimStatusNeedsRevalidation:
			return ContextFactStatusCandidate
		case ClaimStatusStale, ClaimStatusSuperseded:
			return ContextFactStatusStale
		case ClaimStatusRejected:
			return ContextFactStatusUnsupported
		case ClaimStatusCurrent:
			return ContextFactStatusSupported
		}
	}
	return ContextFactStatusSupported
}

func copyMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func bundleSummary(facts []ContextFact) string {
	if len(facts) == 0 {
		return "No supported context facts were found."
	}
	if len(facts) == 1 {
		return facts[0].Fact
	}
	return fmt.Sprintf("Reduced %d context facts from retrieved evidence.", len(facts))
}

func applyEvidenceContextBudget(evidence []EvidenceNode, maxChars int) ([]EvidenceNode, int) {
	if maxChars <= 0 || len(evidence) == 0 {
		return evidence, 0
	}
	out := make([]EvidenceNode, 0, len(evidence))
	remaining := maxChars
	omitted := 0
	for _, node := range evidence {
		cost := evidenceNodeContextChars(node)
		if cost == 0 {
			out = append(out, node)
			continue
		}
		if cost <= remaining {
			out = append(out, node)
			remaining -= cost
			continue
		}
		if remaining > 0 && len(out) == 0 {
			trimmed := trimEvidenceNodeContext(node, remaining)
			if evidenceNodeContextChars(trimmed) > 0 {
				out = append(out, trimmed)
				remaining = 0
			} else {
				omitted++
			}
			continue
		}
		omitted++
	}
	return out, omitted
}

func evidenceContextChars(evidence []EvidenceNode) int {
	total := 0
	for _, node := range evidence {
		total += evidenceNodeContextChars(node)
	}
	return total
}

func evidenceNodeContextChars(node EvidenceNode) int {
	return len(strings.TrimSpace(node.Statement)) + len(strings.TrimSpace(node.Ref.Excerpt))
}

func trimEvidenceNodeContext(node EvidenceNode, maxChars int) EvidenceNode {
	if maxChars <= 0 {
		node.Statement = ""
		node.Ref.Excerpt = ""
		return node
	}
	statement := strings.TrimSpace(node.Statement)
	excerpt := strings.TrimSpace(node.Ref.Excerpt)
	statementBudget := maxChars
	if excerpt != "" && statement != "" {
		statementBudget = maxChars / 2
	}
	node.Statement = truncateContextText(statement, statementBudget)
	remaining := maxChars - len(strings.TrimSpace(node.Statement))
	node.Ref.Excerpt = truncateContextText(excerpt, remaining)
	if node.Metadata == nil {
		node.Metadata = map[string]any{}
	} else {
		node.Metadata = copyMetadata(node.Metadata)
	}
	node.Metadata["context_truncated"] = true
	return node
}

func truncateContextText(value string, maxChars int) string {
	value = strings.TrimSpace(value)
	if maxChars <= 0 || value == "" {
		return ""
	}
	if len(value) <= maxChars {
		return value
	}
	if maxChars <= 3 {
		return value[:maxChars]
	}
	return strings.TrimSpace(value[:maxChars-3]) + "..."
}

func defaultContextIDGen(prefix string) IDGen {
	var n int
	var mu sync.Mutex
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("%s_%d", prefix, n)
	}
}
