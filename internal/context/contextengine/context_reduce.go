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
	MaxFacts           int
	MaxPaths           int
	MaxEvidencePerPath int
	MaxContextChars    int
	MinConfidence      float64
	IncludeStale       bool
	TaskType           string
	RequiredEvidence   []string
	IDGen              IDGen
	Clock              ClockFunc
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
	evidence, omittedByPathSelection := selectEvidencePathFirst(evidence, opts)
	telemetry.OmittedContextItems += omittedByPathSelection
	evidence, omittedByBudget := applyEvidenceContextBudget(evidence, opts.MaxContextChars)
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
		SelectedPaths:    selectedPaths,
		AnswerCandidates: answerCandidates,
		Facts:            facts,
		Evidence:         evidence,
		Missing:          missing,
		SourceCoverage:   sourceCoverage,
		SourcePackIDs:    sourcePackIDs,
		SourceEpisodeIDs: sourceEpisodeIDs,
		Telemetry:        telemetry,
		CreatedAt:        opts.Clock(),
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
}

func selectEvidencePathFirst(evidence []EvidenceNode, opts BundleReductionOptions) ([]EvidenceNode, int) {
	if opts.MaxFacts <= 0 || len(evidence) <= opts.MaxFacts {
		return evidence, 0
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
		if nodeScore > item.score {
			item.score = nodeScore
			item.bestRef = FormatEvidenceRef(node.Ref)
			item.bestNodeID = node.ID
		}
	}
	if len(byPath) == 0 {
		if len(evidence) > opts.MaxFacts {
			return evidence[:opts.MaxFacts], len(evidence) - opts.MaxFacts
		}
		return evidence, 0
	}
	paths := make([]*reductionPathAcc, 0, len(byPath))
	for _, item := range byPath {
		item.score += supportDensityScore(item.support)
		item.score += requiredCoverageScore(item.requiredHit, len(opts.RequiredEvidence))
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
	selected := make([]EvidenceNode, 0, opts.MaxFacts)
	selectedIDs := map[string]struct{}{}
	for _, item := range paths {
		if len(selected) >= opts.MaxFacts || len(selectedIDs) >= opts.MaxFacts || maxPaths <= 0 {
			break
		}
		sort.SliceStable(item.nodes, func(i, j int) bool {
			leftHits := requiredEvidenceHits(item.nodes[i], opts.RequiredEvidence)
			rightHits := requiredEvidenceHits(item.nodes[j], opts.RequiredEvidence)
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
		return selected, 0
	}
	return selected, len(evidence) - len(selected)
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
	case "primary_anchor":
		return 0.20
	case "registration_file", "tool_declaration":
		return 0.19
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
	haystack := strings.ToLower(strings.Join([]string{
		node.Statement,
		node.Ref.Ref,
		node.Ref.Title,
		node.Ref.Excerpt,
		metadataString(node.Metadata, "path"),
	}, "\n"))
	hits := 0
	for _, item := range required {
		item = strings.TrimSpace(strings.ToLower(item))
		if item == "" {
			continue
		}
		if strings.Contains(haystack, item) {
			hits++
		}
	}
	return hits
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

func buildSelectedPaths(evidence []EvidenceNode) []ContextSelectedPath {
	type acc struct {
		path        string
		evidenceIDs []string
		refs        []EvidenceRef
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
