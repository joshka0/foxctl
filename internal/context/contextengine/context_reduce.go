package contextengine

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
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
	codeSearchProviderTelemetry := make([]any, 0, len(packs))
	graphConfidence := 0.0
	graphConfidenceObserved := false

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
		if telemetry := pack.Metadata["code_search_provider_telemetry"]; telemetry != nil {
			codeSearchProviderTelemetry = append(codeSearchProviderTelemetry, telemetry)
		}
		if value, ok := packGraphConfidence(pack); ok {
			if !graphConfidenceObserved || value > graphConfidence {
				graphConfidence = value
				graphConfidenceObserved = true
			}
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
			key := evidenceNodeIdentityKey(node)
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
	facts = prependCoverageFacts(facts, coverageReport, evidence, opts, sourcePackIDs)
	selectedPaths := buildSelectedPaths(evidence, opts, requirements)
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
	if len(codeSearchProviderTelemetry) > 0 {
		bundle.Metadata["code_search_provider_telemetry"] = codeSearchProviderTelemetry
	}
	if graphConfidenceObserved {
		bundle.Metadata["graph_confidence"] = graphConfidence
	}
	if err := bundle.Validate(); err != nil {
		return ContextBundle{}, err
	}
	return bundle, nil
}

type reductionPathAcc struct {
	path          string
	nodes         []EvidenceNode
	score         float64
	bestRef       string
	bestNodeID    string
	bestSymbol    string
	componentRoot string
	pathFamily    string
	fileRole      string
	firstRank     int
	support       int
	requiredHit   int
	coverage      map[string]float64
}

func (item *reductionPathAcc) coverageIDs() []string {
	if item == nil || len(item.coverage) == 0 {
		return nil
	}
	out := make([]string, 0, len(item.coverage))
	for id, score := range item.coverage {
		if score > 0 {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func (item *reductionPathAcc) bestMetadata() map[string]any {
	if item == nil {
		return nil
	}
	for _, node := range item.nodes {
		if node.ID == item.bestNodeID {
			return node.Metadata
		}
	}
	if len(item.nodes) > 0 {
		return item.nodes[0].Metadata
	}
	return nil
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
			item.bestSymbol = reductionNodeSymbol(node)
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
	allowTests := reductionAllowsTests(opts, requirements)
	for _, item := range byPath {
		classification := classifyReductionPathAcc(item)
		item.componentRoot = classification.ComponentRoot
		item.pathFamily = classification.PathFamily
		item.fileRole = classification.FileRole
		item.score += supportDensityScore(item.support)
		item.score += requiredCoverageScore(item.requiredHit, len(opts.RequiredEvidence))
		for reqID, coverageScore := range item.coverage {
			if coverageScore <= 0 {
				delete(item.coverage, reqID)
			}
		}
		if isLikelyTestPath(item.path) && !allowTests {
			item.score -= 0.12
			if counterpart := productionCounterpartReductionPath(item.path); counterpart != "" {
				if _, ok := byPath[counterpart]; ok {
					item.score -= 0.35
				}
			}
		}
		item.score += pathRoleReductionScore(classification, opts, allowTests)
		paths = append(paths, item)
	}
	applyComponentCoherenceScores(paths, opts)
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
		if reductionPathSkippedByTestPolicy(item.path, item.coverageIDs(), item.bestMetadata(), opts, requirements, allowTests) {
			continue
		}
		if shouldSkipLowRoleReductionPath(item.path, opts, allowTests) {
			continue
		}
		if !reductionPathAdmittedToAnswerPaths(item.path, item.coverageIDs(), item.bestMetadata(), opts, requirements, allowTests) {
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
	allowTests := reductionAllowsTests(opts, requirements)
	for _, req := range requirements {
		slots := req.MinPaths
		if slots <= 0 {
			slots = 1
		}
		for slots > 0 && len(selected) < maxPaths {
			best := bestPathForCoverage(paths, req, selected, opts, allowTests)
			if best == nil {
				break
			}
			selected[best.path] = struct{}{}
			slots--
		}
	}
	return selected
}

func applyComponentCoherenceScores(paths []*reductionPathAcc, opts BundleReductionOptions) {
	if len(paths) == 0 || !reductionTaskWantsComponentCoherence(opts) {
		return
	}
	root := preferredReductionComponentRoot(paths)
	if root == "" {
		return
	}
	rootParent := reductionComponentRootParent(root)
	for _, item := range paths {
		if item == nil || item.componentRoot == "" {
			continue
		}
		switch {
		case item.componentRoot == root:
			item.score += 0.22
		case rootParent != "" && reductionComponentRootParent(item.componentRoot) == rootParent:
			item.score -= 0.28
		default:
			item.score -= 0.08
		}
	}
}

func reductionTaskWantsComponentCoherence(opts BundleReductionOptions) bool {
	switch strings.TrimSpace(strings.ToLower(opts.TaskType)) {
	case "subsystem_map", "integration_surface", "change_impact":
		return true
	default:
		return false
	}
}

func preferredReductionComponentRoot(paths []*reductionPathAcc) string {
	type rootScore struct {
		score     float64
		bestRank  int
		bestPath  string
		qualified bool
	}
	roots := map[string]rootScore{}
	for _, item := range paths {
		if item == nil || item.componentRoot == "" {
			continue
		}
		if !reductionComponentAnchorRole(item.fileRole) {
			continue
		}
		anchorScore := item.score
		if item.bestHighConfidence() {
			anchorScore += 0.35
		}
		current := roots[item.componentRoot]
		if current.bestRank == 0 || item.firstRank+1 < current.bestRank {
			current.bestRank = item.firstRank + 1
			current.bestPath = item.path
		}
		current.score += anchorScore
		if item.bestHighConfidence() {
			current.qualified = true
		}
		roots[item.componentRoot] = current
	}
	bestRoot := ""
	best := rootScore{}
	for root, score := range roots {
		if !score.qualified {
			continue
		}
		if bestRoot == "" || score.score > best.score || (score.score == best.score && score.bestRank < best.bestRank) || (score.score == best.score && score.bestRank == best.bestRank && score.bestPath < best.bestPath) {
			bestRoot = root
			best = score
		}
	}
	return bestRoot
}

func (item *reductionPathAcc) bestHighConfidence() bool {
	if item == nil {
		return false
	}
	for _, node := range item.nodes {
		if node.Confidence >= 0.85 {
			return true
		}
	}
	return false
}

func reductionComponentAnchorRole(role string) bool {
	switch normalizeCoverageRoleName(role) {
	case "implementation", "config", "router", "domain":
		return true
	default:
		return false
	}
}

func reductionComponentRootParent(root string) string {
	root = normalizeSelectedPath(root)
	if root == "" {
		return ""
	}
	parent := filepath.ToSlash(filepath.Dir(root))
	if parent == "." || parent == "" {
		return ""
	}
	return parent
}

func classifyReductionPathAcc(item *reductionPathAcc) reductionPathClassification {
	if item == nil {
		return reductionPathClassification{}
	}
	classification := classifyReductionPath(item.path)
	var metadata map[string]any
	for _, node := range item.nodes {
		if node.ID == item.bestNodeID {
			metadata = node.Metadata
			break
		}
	}
	if metadata == nil && len(item.nodes) > 0 {
		metadata = item.nodes[0].Metadata
	}
	if value := metadataString(metadata, "component_root"); value != "" {
		classification.ComponentRoot = normalizeSelectedPath(value)
	}
	if value := metadataString(metadata, "path_family"); value != "" {
		classification.PathFamily = normalizeSelectedPath(value)
	}
	if value := metadataString(metadata, "file_role"); value != "" {
		classification.FileRole = normalizeCoverageRoleName(value)
	}
	return classification
}

func sourceProfileRequested(profiles []SourceProfile, want SourceProfile) bool {
	for _, profile := range profiles {
		if profile == want {
			return true
		}
	}
	return false
}

func isLikelyDocumentationPathReduction(path string) bool {
	path = strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	return strings.HasPrefix(path, "docs/") || strings.Contains(path, "/docs/") || strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".mdx") || strings.HasSuffix(path, ".rst")
}

func reductionNodeAdmittedToAnswerPaths(node EvidenceNode, opts BundleReductionOptions, requirements []CoverageRequirement, coverageIDs []string) bool {
	return reductionPathAdmittedToAnswerPaths(evidenceNodePath(node), coverageIDs, node.Metadata, opts, requirements, reductionAllowsTests(opts, requirements))
}

func reductionPathAdmittedToAnswerPaths(path string, coverageIDs []string, metadata map[string]any, opts BundleReductionOptions, requirements []CoverageRequirement, allowTests bool) bool {
	path = normalizeSelectedPath(path)
	if path == "" {
		return false
	}
	classification := classifyReductionPath(path)
	if value := metadataString(metadata, "file_role"); value != "" {
		classification.FileRole = normalizeCoverageRoleName(value)
	}
	if value := metadataString(metadata, "file_kind"); value != "" {
		classification.FileKind = strings.TrimSpace(strings.ToLower(value))
	}
	if value := metadataBool(metadata, "is_tooling"); value {
		classification.IsTooling = true
	}
	if value := metadataBool(metadata, "is_generated"); value {
		classification.IsGenerated = true
	}
	if value := metadataBool(metadata, "is_test"); value {
		classification.IsTest = true
	}
	if !allowTests && classification.IsTest && classification.FileRole != "data" && classification.FileRole != "test_data" {
		return false
	}
	if classification.IsHidden || classification.IsGenerated || classification.FileRole == "template" {
		return reductionTaskWantsPeripheralFiles(opts)
	}
	if classification.IsTooling && strings.TrimSpace(opts.TaskType) != "" && !reductionTaskWantsPeripheralFiles(opts) {
		return false
	}
	switch classification.FileRole {
	case "documentation":
		return reductionDocsAdmittedToAnswerPaths(path, coverageIDs, opts, requirements)
	case "config":
		return reductionCoverageIDsIncludeRole(coverageIDs, requirements, "config") || reductionTaskWantsPeripheralFiles(opts)
	case "data", "test_data":
		return true
	default:
		return true
	}
}

func reductionPathSkippedByTestPolicy(path string, coverageIDs []string, metadata map[string]any, opts BundleReductionOptions, requirements []CoverageRequirement, allowTests bool) bool {
	if allowTests || !isLikelyTestPath(path) {
		return false
	}
	classification := classifyReductionPath(path)
	if value := metadataString(metadata, "file_role"); value != "" {
		classification.FileRole = normalizeCoverageRoleName(value)
	}
	switch classification.FileRole {
	case "data", "test_data":
		return !reductionPathAdmittedToAnswerPaths(path, coverageIDs, metadata, opts, requirements, allowTests)
	default:
		return true
	}
}

func reductionDocsAdmittedToAnswerPaths(path string, coverageIDs []string, opts BundleReductionOptions, requirements []CoverageRequirement) bool {
	taskType := strings.ToLower(strings.TrimSpace(opts.TaskType))
	if strings.Contains(taskType, "documentation") {
		return true
	}
	if !sourceProfileRequested(opts.SourceProfiles, SourceProfileRepoDocs) && !strings.Contains(taskType, "architecture") {
		return false
	}
	if reductionCoverageIDsIncludeDocs(coverageIDs, requirements) {
		return true
	}
	return reductionDocPathMatchesRequiredAnchor(path, opts.RequiredEvidence)
}

func reductionCoverageIDsIncludeDocs(coverageIDs []string, requirements []CoverageRequirement) bool {
	return reductionCoverageIDsIncludeRole(coverageIDs, requirements, "documentation")
}

func reductionCoverageIDsIncludeRole(coverageIDs []string, requirements []CoverageRequirement, role string) bool {
	if len(coverageIDs) == 0 || len(requirements) == 0 {
		return false
	}
	ids := map[string]struct{}{}
	for _, id := range coverageIDs {
		ids[strings.TrimSpace(id)] = struct{}{}
	}
	for _, req := range requirements {
		if _, ok := ids[req.ID]; !ok {
			continue
		}
		if coverageRequirementFileRoleConstraint(req) == role {
			return true
		}
		if role == "documentation" {
			for _, profile := range req.SourceProfiles {
				if profile == SourceProfileRepoDocs || profile == SourceProfileVaultDocs {
					return true
				}
			}
		}
	}
	return false
}

func reductionDocPathMatchesRequiredAnchor(path string, required []string) bool {
	if len(required) == 0 {
		return false
	}
	baseTerms := splitEvidenceCoverageTerms(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if len(baseTerms) == 0 {
		return false
	}
	baseSet := map[string]struct{}{}
	for _, term := range baseTerms {
		baseSet[term] = struct{}{}
	}
	for _, req := range required {
		matched := 0
		for _, term := range splitEvidenceCoverageTerms(req) {
			if _, ok := baseSet[term]; ok {
				matched++
			}
		}
		if matched >= minInt(2, len(baseTerms)) {
			return true
		}
	}
	return false
}

func bestPathForCoverage(paths []*reductionPathAcc, req CoverageRequirement, selected map[string]struct{}, opts BundleReductionOptions, allowTests bool) *reductionPathAcc {
	best := bestPathForCoverageWithTestPolicy(paths, req.ID, selected, opts, false)
	if best != nil {
		return best
	}
	if !allowTests && !coverageRequirementAllowsTests(req) {
		return nil
	}
	return bestPathForCoverageWithTestPolicy(paths, req.ID, selected, opts, true)
}

func coverageRequirementAllowsTests(req CoverageRequirement) bool {
	if coverageTextSuggestsTest(req.ID) || coverageTextSuggestsTest(req.Kind) || coverageTextSuggestsTest(req.Label) {
		return true
	}
	for _, term := range req.Terms {
		if coverageTextSuggestsTest(term) {
			return true
		}
	}
	return false
}

func bestPathForCoverageWithTestPolicy(paths []*reductionPathAcc, requirementID string, selected map[string]struct{}, opts BundleReductionOptions, allowTests bool) *reductionPathAcc {
	var best *reductionPathAcc
	bestScore := 0.0
	for _, item := range paths {
		if _, ok := selected[item.path]; ok {
			continue
		}
		if reductionPathSkippedByTestPolicy(item.path, item.coverageIDs(), item.bestMetadata(), opts, opts.CoverageRequirements, allowTests) {
			continue
		}
		if shouldSkipLowRoleReductionPath(item.path, opts, allowTests) {
			continue
		}
		if !reductionPathAdmittedToAnswerPaths(item.path, item.coverageIDs(), item.bestMetadata(), opts, opts.CoverageRequirements, allowTests) {
			continue
		}
		coverageScore := item.coverage[requirementID]
		if coverageScore <= 0 {
			continue
		}
		score := coverageScore*10 + directCoverageAnchorScore(item, requirementID, opts) + item.score
		if best == nil || score > bestScore || (score == bestScore && item.firstRank < best.firstRank) {
			best = item
			bestScore = score
		}
	}
	return best
}

func directCoverageAnchorScore(item *reductionPathAcc, requirementID string, opts BundleReductionOptions) float64 {
	if item == nil {
		return 0
	}
	req, ok := coverageRequirementByID(opts, requirementID)
	if !ok {
		return 0
	}
	anchors := []string{
		strings.TrimSuffix(filepath.Base(item.path), filepath.Ext(item.path)),
		item.bestSymbol,
	}
	needles := req.Terms
	if len(needles) == 0 {
		needles = normalizeCoverageTerms([]string{req.Label, req.ID})
	}
	needles = append(append([]string(nil), needles...), req.Label, req.ID)
	for _, anchor := range anchors {
		anchor = compactCoverageAnchor(anchor)
		if anchor == "" {
			continue
		}
		for _, needle := range needles {
			if anchor == compactCoverageAnchor(needle) {
				return 3.0
			}
		}
	}
	return 0
}

func coverageRequirementByID(opts BundleReductionOptions, id string) (CoverageRequirement, bool) {
	requirements := normalizeCoverageRequirements(opts.CoverageRequirements, opts.RequiredEvidence, opts.SourceProfiles)
	for _, req := range requirements {
		if req.ID == id {
			return req, true
		}
	}
	return CoverageRequirement{}, false
}

func compactCoverageAnchor(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func reductionNodeSymbol(node EvidenceNode) string {
	if node.Ref.Type == RefTypeSymbol {
		return node.Ref.Ref
	}
	return metadataString(node.Metadata, "symbol")
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

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
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

func reductionAllowsTests(opts BundleReductionOptions, requirements []CoverageRequirement) bool {
	if coverageTextSuggestsTest(opts.TaskType) {
		return true
	}
	for _, req := range requirements {
		if coverageTextSuggestsTest(req.ID) || coverageTextSuggestsTest(req.Kind) || coverageTextSuggestsTest(req.Label) {
			return true
		}
		for _, term := range req.Terms {
			if coverageTextSuggestsTest(term) {
				return true
			}
		}
	}
	return false
}

func coverageTextSuggestsTest(value string) bool {
	for _, term := range splitEvidenceCoverageTerms(value) {
		switch term {
		case "test", "tests", "testing", "spec", "specs", "fixture", "fixtures":
			return true
		}
	}
	return false
}

func candidateRoleScore(role string) float64 {
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "symbol_definition":
		return 0.30
	case "required_path_support":
		return 0.30
	case "registration_file":
		return 0.28
	case "tool_declaration":
		return 0.25
	case "documentation_map":
		return 0.24
	case "test_data_support":
		return 0.24
	case "documentation_anchor":
		return 0.22
	case "primary_anchor":
		return 0.20
	case "import_reference":
		return 0.20
	case "data_support":
		return 0.18
	case "direct_dispatch_file":
		return 0.18
	case "config_support":
		return 0.16
	case "production_companion":
		return 0.16
	case "module_entrypoint":
		return 0.16
	case "definition_support":
		return 0.14
	case "test_companion":
		return 0.10
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
		if !coverageRequirementSourceProfileMatches(node, req) {
			continue
		}
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

func coverageRequirementSourceProfileMatches(node EvidenceNode, req CoverageRequirement) bool {
	roleConstraint := coverageRequirementFileRoleConstraint(req)
	if roleConstraint != "" && !evidenceNodeMatchesFileRole(node, roleConstraint) {
		return false
	}
	if len(req.SourceProfiles) == 0 {
		return true
	}
	for _, profile := range req.SourceProfiles {
		if evidenceNodeMatchesSourceProfile(node, profile) {
			return true
		}
	}
	return false
}

func evidenceNodeMatchesSourceProfile(node EvidenceNode, profile SourceProfile) bool {
	if !profile.IsValid() {
		return false
	}
	if role := sourceProfileFileRoleConstraint(profile); role != "" && !evidenceNodeMatchesFileRole(node, role) {
		return false
	}
	nodeProfile := SourceProfile(strings.TrimSpace(strings.ToLower(metadataString(node.Metadata, "source_profile"))))
	if nodeProfile == profile {
		return true
	}
	for _, source := range metadataStringSlice(node.Metadata, "sources") {
		if SourceProfile(strings.TrimSpace(strings.ToLower(source))) == profile {
			return true
		}
	}
	return false
}

func coverageRequirementFileRoleConstraint(req CoverageRequirement) string {
	for _, value := range []string{req.Kind, req.ID, req.Label} {
		normalized := normalizeCoverageRoleName(value)
		switch normalized {
		case "repo_docs", "docs", "documentation":
			return "documentation"
		case "deploy_config", "config":
			return "config"
		case "test_data":
			return "test_data"
		case "data":
			return "data"
		case "router", "domain":
			return normalized
		}
	}
	return ""
}

func sourceProfileFileRoleConstraint(profile SourceProfile) string {
	switch profile {
	case SourceProfileRepoDocs, SourceProfileVaultDocs:
		return "documentation"
	case SourceProfileRepoCode:
		return "implementation"
	default:
		return ""
	}
}

func evidenceNodeMatchesFileRole(node EvidenceNode, role string) bool {
	role = normalizeCoverageRoleName(role)
	if role == "" {
		return true
	}
	pathRole := normalizeCoverageRoleName(classifyReductionPath(evidenceNodePath(node)).FileRole)
	metadataRole := normalizeCoverageRoleName(metadataString(node.Metadata, "file_role"))
	switch role {
	case "data":
		return pathRole == "data" || pathRole == "test_data" || metadataRole == "data" || metadataRole == "test_data"
	default:
		return pathRole == role || metadataRole == role
	}
}

func normalizeCoverageRoleName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
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
	coveredPaths := map[string]map[string]struct{}{}
	for _, node := range evidence {
		path := evidenceNodePath(node)
		if path == "" {
			continue
		}
		for reqID, score := range nodeCoverageScores(node, requirements) {
			if coveredPaths[reqID] == nil {
				coveredPaths[reqID] = map[string]struct{}{}
			}
			coveredPaths[reqID][path] = struct{}{}
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
		if len(coveredPaths[req.ID]) < coverageRequirementMinPaths(req) {
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
	coveredPaths := map[string]map[string]struct{}{}
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
		if filteredPath := normalizeCoveragePath(item.Path); filteredPath != "" {
			if coveredPaths[item.RequirementID] == nil {
				coveredPaths[item.RequirementID] = map[string]struct{}{}
			}
			coveredPaths[item.RequirementID][filteredPath] = struct{}{}
		}
	}
	filtered.Missing = filtered.Missing[:0]
	for _, req := range filtered.Requirements {
		if !req.Required {
			continue
		}
		if len(coveredPaths[req.ID]) < coverageRequirementMinPaths(req) {
			filtered.Missing = append(filtered.Missing, req.ID)
		}
	}
	return filtered
}

func coverageRequirementMinPaths(req CoverageRequirement) int {
	if req.MinPaths > 0 {
		return req.MinPaths
	}
	return 1
}

func normalizeCoveragePath(path string) string {
	return filepath.ToSlash(strings.TrimSpace(path))
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
	base := filepath.Base(path)
	switch {
	case strings.Contains(path, "_test."),
		strings.Contains(path, "/test/"),
		strings.Contains(path, "/tests/"),
		strings.Contains(path, "/__tests__/"),
		base == "test.go",
		base == "tests.go",
		base == "test.rs",
		base == "tests.rs",
		base == "test.py",
		base == "tests.py",
		base == "test.ts",
		base == "tests.ts",
		base == "test.tsx",
		base == "tests.tsx",
		strings.HasPrefix(base, "test_"),
		strings.HasPrefix(base, "test-"),
		strings.Contains(base, ".test."),
		strings.Contains(base, ".spec."),
		strings.Contains(base, "-test-"):
		return true
	default:
		return false
	}
}

func productionCounterpartReductionPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, "_test.go"):
		return path[:len(path)-len("_test.go")] + ".go"
	case strings.HasSuffix(lower, ".test.ts"):
		return path[:len(path)-len(".test.ts")] + ".ts"
	case strings.HasSuffix(lower, ".spec.ts"):
		return path[:len(path)-len(".spec.ts")] + ".ts"
	case strings.HasSuffix(lower, ".test.tsx"):
		return path[:len(path)-len(".test.tsx")] + ".tsx"
	case strings.HasSuffix(lower, ".spec.tsx"):
		return path[:len(path)-len(".spec.tsx")] + ".tsx"
	case strings.HasSuffix(lower, ".test.js"):
		return path[:len(path)-len(".test.js")] + ".js"
	case strings.HasSuffix(lower, ".spec.js"):
		return path[:len(path)-len(".spec.js")] + ".js"
	case strings.HasSuffix(lower, ".test.jsx"):
		return path[:len(path)-len(".test.jsx")] + ".jsx"
	case strings.HasSuffix(lower, ".spec.jsx"):
		return path[:len(path)-len(".spec.jsx")] + ".jsx"
	}
	return ""
}

type reductionPathClassification struct {
	FileKind      string
	FileRole      string
	ComponentRoot string
	PathFamily    string
	IsTooling     bool
	IsGenerated   bool
	IsTest        bool
	IsHidden      bool
}

func classifyReductionPath(path string) reductionPathClassification {
	path = normalizeSelectedPath(path)
	lower := strings.ToLower(path)
	parts := splitReductionPath(path)
	out := reductionPathClassification{
		FileKind:      reductionFileKind(path),
		FileRole:      "implementation",
		ComponentRoot: reductionComponentRoot(parts),
		PathFamily:    reductionPathFamily(parts),
		IsTest:        isLikelyTestPath(path),
		IsHidden:      reductionPathHasHiddenPart(parts),
	}
	if out.IsTest {
		out.FileRole = "test"
	}
	if strings.Contains(lower, ".generated.") || strings.Contains(lower, "_generated.") || strings.HasSuffix(lower, ".pb.go") || strings.HasSuffix(lower, ".gen.go") || strings.HasSuffix(lower, ".min.js") || strings.Contains(lower, "/generated/") || strings.Contains(lower, "/gen/") {
		out.IsGenerated = true
		out.FileRole = "generated"
	}
	if strings.Contains(lower, "/template/") || strings.Contains(lower, "/templates/") || strings.Contains(lower, ".tmpl.") || strings.HasSuffix(lower, ".tmpl") || strings.HasSuffix(lower, ".tpl") || strings.HasSuffix(lower, ".hbs") {
		out.FileRole = "template"
	}
	if isLikelyDocumentationPathReduction(path) {
		out.FileRole = "documentation"
	}
	if reductionPathIsConfig(parts, lower, out.FileKind) {
		out.FileRole = "config"
	}
	if reductionPathIsData(parts, lower, out.FileKind) {
		out.FileRole = "data"
	}
	if reductionPathIsTestData(parts, lower) {
		out.FileRole = "test_data"
	}
	if reductionPathIsTooling(parts, lower) {
		out.IsTooling = true
		if out.FileRole == "implementation" {
			out.FileRole = "tooling"
		}
	}
	return out
}

func splitReductionPath(path string) []string {
	path = normalizeSelectedPath(path)
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func reductionFileKind(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		return "unknown"
	}
	switch ext {
	case "go":
		return "go"
	case "ts", "tsx", "js", "jsx":
		return "typescript"
	case "md", "mdx", "rst":
		return "docs"
	case "json", "yaml", "yml", "toml":
		return "config"
	case "sh", "bash", "zsh":
		return "script"
	default:
		return ext
	}
}

func reductionComponentRoot(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) >= 2 {
		switch parts[0] {
		case "app", "apps", "service", "services", "module", "modules", "package", "packages":
			return strings.Join(parts[:2], "/")
		}
	}
	if len(parts) >= 3 && parts[0] == "internal" {
		return strings.Join(parts[:3], "/")
	}
	if len(parts) >= 2 {
		switch parts[0] {
		case "cmd", "docs", "deploy", "scripts", "testdata", "configs", "pkg", "internal":
			return strings.Join(parts[:2], "/")
		}
	}
	return parts[0]
}

func reductionPathFamily(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return "repo root"
	}
	dir := filepath.ToSlash(filepath.Dir(strings.Join(parts, "/")))
	if dir == "." || dir == "" {
		return "repo root"
	}
	return dir
}

func reductionPathHasHiddenPart(parts []string) bool {
	for _, part := range parts {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return true
		}
	}
	return false
}

func reductionPathIsTooling(parts []string, lower string) bool {
	if len(parts) == 0 {
		return false
	}
	switch filepath.Base(lower) {
	case "makefile", "dockerfile", "justfile", "taskfile.yml", "taskfile.yaml":
		return true
	}
	switch parts[0] {
	case "ci", "scripts", "tools", "tooling":
		return true
	}
	for _, part := range parts {
		switch part {
		case ".github", ".devcontainer":
			return true
		}
	}
	return false
}

func reductionPathIsConfig(parts []string, lower string, fileKind string) bool {
	if len(parts) == 0 {
		return false
	}
	if fileKind == "config" {
		return true
	}
	base := filepath.Base(lower)
	switch base {
	case "config.py", "config.ex", "config.exs", "settings.py", "settings.ex", "settings.exs":
		return true
	}
	switch parts[0] {
	case "config", "configs", "deploy":
		return true
	}
	for _, part := range parts {
		switch part {
		case ".github", ".devcontainer", "config", "configs", "deploy":
			return true
		}
	}
	return strings.Contains(lower, "/config/") || strings.Contains(lower, "/configs/")
}

func reductionPathIsData(parts []string, lower string, fileKind string) bool {
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case "data", "datasets", "testdata", "tests_data":
		return true
	}
	for _, part := range parts {
		switch part {
		case "data", "datasets", "testdata", "tests_data", "fixtures":
			return true
		}
	}
	switch fileKind {
	case "csv", "tsv", "jsonl", "parquet", "sqlite", "db":
		return true
	default:
		return strings.Contains(lower, "/fixtures/")
	}
}

func reductionPathIsTestData(parts []string, lower string) bool {
	for _, part := range parts {
		switch part {
		case "testdata", "tests_data", "fixtures":
			return true
		}
	}
	return strings.Contains(lower, "/testdata/") || strings.Contains(lower, "/tests_data/") || strings.Contains(lower, "/fixtures/")
}

func pathRoleReductionScore(classification reductionPathClassification, opts BundleReductionOptions, allowTests bool) float64 {
	if reductionTaskWantsPeripheralFiles(opts) {
		return 0
	}
	score := 0.0
	if classification.IsHidden {
		score -= 0.26
	}
	if classification.IsTooling {
		score -= 0.18
	}
	if classification.IsGenerated {
		score -= 0.42
	}
	if classification.FileRole == "template" {
		score -= 0.22
	}
	if classification.IsTest && !allowTests {
		score -= 0.08
	}
	return score
}

func shouldSkipLowRoleReductionPath(path string, opts BundleReductionOptions, allowTests bool) bool {
	if reductionTaskWantsPeripheralFiles(opts) {
		return false
	}
	classification := classifyReductionPath(path)
	if classification.IsTest && !allowTests && classification.FileRole != "data" && classification.FileRole != "test_data" {
		return true
	}
	return classification.IsHidden || classification.IsGenerated || classification.FileRole == "template"
}

func reductionTaskWantsPeripheralFiles(opts BundleReductionOptions) bool {
	taskType := strings.ToLower(strings.TrimSpace(opts.TaskType))
	return strings.Contains(taskType, "test") || strings.Contains(taskType, "tool") || strings.Contains(taskType, "template") || strings.Contains(taskType, "generated") || strings.Contains(taskType, "documentation")
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

func metadataBool(metadata map[string]any, key string) bool {
	if metadata == nil {
		return false
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes":
			return true
		default:
			return false
		}
	default:
		return fmt.Sprint(value) == "true"
	}
}

func metadataFloat(metadata map[string]any, key string) (float64, bool) {
	if metadata == nil {
		return 0, false
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return 0, false
	}
	return metadataFloatValue(value)
}

func metadataFloatValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return clampUnitFloat(typed), true
	case float32:
		return clampUnitFloat(float64(typed)), true
	case int:
		return clampUnitFloat(float64(typed)), true
	case int64:
		return clampUnitFloat(float64(typed)), true
	case int32:
		return clampUnitFloat(float64(typed)), true
	case uint:
		return clampUnitFloat(float64(typed)), true
	case uint64:
		return clampUnitFloat(float64(typed)), true
	case uint32:
		return clampUnitFloat(float64(typed)), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return clampUnitFloat(parsed), true
	default:
		return 0, false
	}
}

func clampUnitFloat(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
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

func packGraphConfidence(pack EvidencePack) (float64, bool) {
	for _, key := range []string{"graph_confidence", "context_graph_confidence", "repo_graph_confidence"} {
		if value, ok := metadataFloat(pack.Metadata, key); ok {
			return value, true
		}
	}
	for _, key := range []string{"graph_confidence", "context_graph_confidence", "graph_report"} {
		if value, ok := nestedGraphConfidence(pack.Metadata[key]); ok {
			return value, true
		}
	}
	return 0, false
}

func buildSelectedPaths(evidence []EvidenceNode, opts BundleReductionOptions, requirements []CoverageRequirement) []ContextSelectedPath {
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
		coverageIDs := metadataStringSlice(node.Metadata, "coverage_requirement_ids")
		for id := range nodeCoverageScores(node, requirements) {
			coverageIDs = appendUniqueString(coverageIDs, id)
		}
		if !reductionNodeAdmittedToAnswerPaths(node, opts, requirements, coverageIDs) {
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
		for _, id := range coverageIDs {
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

func prependCoverageFacts(facts []ContextFact, report *CoverageReport, evidence []EvidenceNode, opts BundleReductionOptions, sourcePackIDs []string) []ContextFact {
	if report == nil || len(report.Covered) == 0 {
		return facts
	}
	requirements := map[string]CoverageRequirement{}
	for _, req := range report.Requirements {
		requirements[req.ID] = req
	}
	evidenceByID := map[string]EvidenceNode{}
	for _, node := range evidence {
		evidenceByID[node.ID] = node
	}
	seen := map[string]struct{}{}
	keptByRequirement := map[string]int{}
	coverageFacts := make([]ContextFact, 0, len(report.Covered))
	for _, covered := range report.Covered {
		req, ok := requirements[covered.RequirementID]
		if !ok || strings.TrimSpace(covered.Path) == "" || len(covered.EvidenceIDs) == 0 {
			continue
		}
		maxFactsForRequirement := req.MinPaths
		if maxFactsForRequirement <= 0 {
			maxFactsForRequirement = 1
		}
		if keptByRequirement[covered.RequirementID] >= maxFactsForRequirement {
			continue
		}
		key := covered.RequirementID + "\x00" + covered.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		node, ok := evidenceByID[covered.EvidenceIDs[0]]
		if !ok {
			continue
		}
		label := coverageRequirementDisplayLabel(req)
		if label == "" {
			continue
		}
		metadata := copyMetadata(node.Metadata)
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["fact_kind"] = "coverage_requirement"
		metadata["coverage_requirement_id"] = covered.RequirementID
		metadata["coverage_label"] = label
		metadata["path"] = covered.Path
		coverageFacts = append(coverageFacts, ContextFact{
			ID:            opts.IDGen(),
			WorkspaceID:   node.WorkspaceID,
			Kind:          node.NodeType,
			Fact:          fmt.Sprintf("%s covers required evidence: %s.", covered.Path, label),
			Refs:          []EvidenceRef{node.Ref},
			EvidenceIDs:   append([]string(nil), covered.EvidenceIDs...),
			Confidence:    maxFloat(node.Confidence, covered.Score),
			Grounding:     node.Grounding,
			Status:        factStatusForNode(node),
			SourcePackIDs: append([]string(nil), sourcePackIDs...),
			Metadata:      metadata,
		})
		keptByRequirement[covered.RequirementID]++
	}
	if len(coverageFacts) == 0 {
		return facts
	}
	out := make([]ContextFact, 0, len(coverageFacts)+len(facts))
	out = append(out, coverageFacts...)
	out = append(out, facts...)
	return out
}

func coverageRequirementDisplayLabel(req CoverageRequirement) string {
	if label := strings.TrimSpace(req.Label); label != "" {
		return label
	}
	if len(req.Terms) == 0 {
		return strings.TrimSpace(req.ID)
	}
	return strings.Join(req.Terms, " ")
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
	path := evidenceNodePath(node)
	classification := classifyReductionPath(path)
	if classification.FileKind != "" {
		out["file_kind"] = classification.FileKind
	}
	if classification.FileRole != "" {
		out["file_role"] = classification.FileRole
	}
	if classification.ComponentRoot != "" {
		out["component_root"] = classification.ComponentRoot
	}
	if classification.PathFamily != "" {
		out["path_family"] = classification.PathFamily
	}
	if classification.IsTooling {
		out["is_tooling"] = true
	}
	if classification.IsGenerated {
		out["is_generated"] = true
	}
	if classification.IsTest {
		out["is_test"] = true
	}
	if classification.IsHidden {
		out["is_hidden"] = true
	}
	for _, key := range []string{"candidate_role", "source_profile", "evidence_class", "source", "supporting_path", "symbol", "symbol_ref"} {
		if value := metadataString(node.Metadata, key); value != "" {
			out[key] = value
		}
	}
	for _, key := range []string{"file_kind", "file_role", "component_root", "path_family"} {
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

func evidenceNodeIdentityKey(node EvidenceNode) string {
	key := evidenceRefKey(node.Ref)
	if node.Ref.Type != RefTypeSymbol {
		return key
	}
	if path := evidenceNodePath(node); path != "" {
		return key + "\x00path\x00" + path
	}
	return key
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
