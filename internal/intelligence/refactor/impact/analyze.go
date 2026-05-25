package impact

import (
	"context"
	"fmt"
	"strings"
)

func Analyze(ctx context.Context, in Input, providers Providers) (Packet, error) {
	normalized, err := normalizeInput(in)
	if err != nil {
		return Packet{}, err
	}

	targets := append([]Target(nil), normalized.Targets...)
	lanes := make([]Lane, 0, 4)
	if len(targets) > 0 {
		lanes = append(lanes, Lane{Name: SourceExplicitTargets, Status: LaneAvailable})
	}

	if normalized.diffRequested {
		if providers.Diff == nil {
			return Packet{}, fmt.Errorf("diff target requires diff provider")
		}
		diffInput := DiffInput{BaseRef: DefaultBaseRef}
		if normalized.Diff != nil {
			diffInput = *normalized.Diff
		}
		changes, err := providers.Diff.ChangedFiles(ctx, diffInput)
		if err != nil {
			return Packet{}, err
		}
		diffTargets := targetsFromChanges(changes)
		targets = append(targets, diffTargets...)
		lanes = append(lanes, Lane{Name: SourceGitDiff, Status: LaneAvailable})
	}
	targets = dedupeTargets(targets)
	if len(targets) > normalized.MaxTargets {
		targets = targets[:normalized.MaxTargets]
	}

	agg := newAggregator()
	for _, target := range targets {
		addDirectTarget(agg, target)
	}

	lanes = append(lanes, collectStructuralLane(ctx, providers.Structural, targets, normalized.Input, agg))
	lanes = append(lanes, collectSemanticLane(ctx, providers.Semantic, targets, normalized.Input, agg))

	groups := agg.groups(normalized.Limit)
	packet := Packet{
		Workspace:        normalized.Workspace,
		Intent:           string(normalized.Intent),
		Targets:          targets,
		Lanes:            lanes,
		MustUpdate:       groups[BucketMustUpdate],
		ShouldInspect:    groups[BucketShouldInspect],
		LikelyDuplicate:  groups[BucketLikelyDuplicate],
		ContractBoundary: groups[BucketContractBoundary],
		TestsToRun:       groups[BucketTestsToRun],
		DocsToUpdate:     groups[BucketDocsToUpdate],
		ContextOnly:      groups[BucketContextOnly],
	}
	packet.Summary = summarize(packet)
	return packet, nil
}

func addDirectTarget(agg *aggregator, target Target) {
	path := target.Path
	if path == "" {
		return
	}
	label := targetLabel(target)
	for _, src := range target.Sources {
		agg.add(candidatePatch{
			path:     path,
			symbol:   target.Symbol,
			bucket:   BucketMustUpdate,
			score:    100,
			source:   src,
			reason:   directTargetReason(target, src),
			lineHint: 0,
			relationship: TargetRelationship{
				TargetKey: targetKey(target),
				Target:    label,
				Section:   SectionDirectTarget,
			},
		})
	}
	if len(target.Sources) == 0 {
		agg.add(candidatePatch{
			path:   path,
			symbol: target.Symbol,
			bucket: BucketMustUpdate,
			score:  100,
			source: SourceExplicitTargets,
			reason: directTargetReason(target, SourceExplicitTargets),
			relationship: TargetRelationship{
				TargetKey: targetKey(target),
				Target:    label,
				Section:   SectionDirectTarget,
			},
		})
	}
}

func directTargetReason(target Target, source Source) string {
	if source == SourceGitDiff {
		if target.IsDeleted {
			return "deleted branch diff target"
		}
		return "branch diff target"
	}
	return "explicit refactor target"
}

func collectStructuralLane(ctx context.Context, provider StructuralProvider, targets []Target, in Input, agg *aggregator) Lane {
	if provider == nil {
		return Lane{Name: SourceRepoindexGraph, Status: LaneUnavailable, Reason: "repoindex graph provider not configured"}
	}
	result, err := provider.Candidates(ctx, targets, StructuralOptions{
		Depth:        in.Depth,
		Limit:        in.Limit,
		PerTargetCap: in.PerTargetCap,
		Intent:       in.Intent,
		IncludeTests: in.IncludeTests,
		IncludeDocs:  in.IncludeDocs,
	})
	if err != nil {
		return Lane{Name: SourceRepoindexGraph, Status: LaneUnavailable, Reason: err.Error()}
	}
	status := LaneUnavailable
	if result.Available {
		status = LaneAvailable
	}
	lane := Lane{Name: SourceRepoindexGraph, Status: status, Reason: result.Reason}
	if !result.Available {
		return lane
	}
	for _, candidate := range result.Candidates {
		path := cleanPath(candidate.Path)
		if path == "" {
			continue
		}
		bucket := structuralBucket(candidate, in)
		agg.add(candidatePatch{
			path:     path,
			symbol:   candidate.Symbol,
			lineHint: candidate.LineHint,
			bucket:   bucket,
			score:    structuralScore(candidate, in.Intent),
			source:   SourceRepoindexGraph,
			reason:   structuralReason(candidate),
			relationship: TargetRelationship{
				TargetKey: candidate.TargetKey,
				Target:    candidate.TargetLabel,
				Section:   candidate.Section,
				Depth:     candidate.Depth,
				EdgeTypes: candidate.EdgeTypes,
			},
		})
	}
	return lane
}

func collectSemanticLane(ctx context.Context, provider SemanticProvider, targets []Target, in Input, agg *aggregator) Lane {
	if provider == nil {
		return Lane{Name: SourceSemanticNeighbor, Status: LaneUnavailable, Reason: "semantic provider not configured"}
	}
	result, err := provider.Neighbors(ctx, SemanticNeighborRequest{
		WorkspaceRoot: in.Workspace,
		Targets:       targets,
		ExcludePaths:  targetPaths(targets),
		Limit:         in.Limit,
		PerTargetCap:  in.PerTargetCap,
	})
	if err != nil {
		return Lane{Name: SourceSemanticNeighbor, Status: LaneUnavailable, Reason: err.Error()}
	}
	status := LaneUnavailable
	if result.Available {
		status = LaneAvailable
	}
	lane := Lane{Name: SourceSemanticNeighbor, Status: status, Reason: result.Reason}
	if !result.Available {
		return lane
	}
	source := result.Source
	if source == "" {
		source = SourceSemanticNeighbor
	}
	for _, candidate := range result.Candidates {
		path := cleanPath(candidate.Path)
		if path == "" {
			continue
		}
		bucket := semanticBucket(candidate, in.Intent)
		agg.add(candidatePatch{
			path:     path,
			symbol:   candidate.Symbol,
			lineHint: candidate.LineHint,
			bucket:   bucket,
			score:    semanticScore(candidate.Similarity),
			source:   source,
			reason:   semanticReason(candidate, source),
			summary:  candidate.Summary,
			relationship: TargetRelationship{
				TargetKey: candidate.TargetKey,
				Target:    candidate.TargetLabel,
			},
		})
	}
	return lane
}

func structuralBucket(candidate StructuralCandidate, in Input) Bucket {
	if hasEdge(candidate.EdgeTypes, "TESTS") || candidate.Section == SectionTest {
		if in.IncludeTests {
			return BucketTestsToRun
		}
		return BucketContextOnly
	}
	if hasDocEdge(candidate.EdgeTypes) || candidate.Section == SectionDoc {
		if in.IncludeDocs {
			return BucketDocsToUpdate
		}
		return BucketContextOnly
	}
	if hasContractBoundaryEdge(candidate.EdgeTypes) {
		return BucketContractBoundary
	}
	switch candidate.Section {
	case SectionDirectTarget:
		return BucketMustUpdate
	case SectionCaller, SectionImportRef:
		if contractChangingIntent(in.Intent) {
			return BucketMustUpdate
		}
		return BucketShouldInspect
	case SectionCallee, SectionChild:
		return BucketShouldInspect
	case SectionContainer, SectionCochange, SectionGraphNeighbor:
		if candidate.Depth > 1 {
			return BucketContextOnly
		}
		return BucketShouldInspect
	default:
		return BucketContextOnly
	}
}

func structuralScore(candidate StructuralCandidate, intent RefactorIntent) int {
	score := 25
	switch candidate.Section {
	case SectionDirectTarget:
		score = 90
	case SectionCaller:
		score = 60
		if contractChangingIntent(intent) {
			score = 75
		}
	case SectionImportRef:
		score = 55
		if contractChangingIntent(intent) {
			score = 70
		}
	case SectionCallee, SectionChild:
		score = 45
	case SectionTest:
		score = 70
	case SectionDoc:
		score = 55
	case SectionContract:
		score = 65
	case SectionCochange:
		score = 35
	case SectionGraphNeighbor:
		score = 35
	}
	if hasContractBoundaryEdge(candidate.EdgeTypes) && score < 65 {
		score = 65
	}
	if candidate.Depth > 0 {
		score -= candidate.Depth * 5
	}
	if score < 10 {
		return 10
	}
	return score
}

func semanticBucket(candidate SemanticCandidate, intent RefactorIntent) Bucket {
	if intent == IntentConsolidate && candidate.Similarity >= 0.85 {
		return BucketLikelyDuplicate
	}
	if candidate.Similarity >= 0.82 {
		return BucketShouldInspect
	}
	return BucketContextOnly
}

func semanticScore(similarity float64) int {
	switch {
	case similarity >= 0.90:
		return 45
	case similarity >= 0.82:
		return 35
	case similarity >= 0.70:
		return 25
	default:
		return 15
	}
}

func structuralReason(candidate StructuralCandidate) string {
	parts := []string{"repoindex structural neighbor"}
	if candidate.Section != "" {
		parts = append(parts, string(candidate.Section))
	}
	if candidate.Depth > 0 {
		parts = append(parts, fmt.Sprintf("depth %d", candidate.Depth))
	}
	if len(candidate.EdgeTypes) > 0 {
		parts = append(parts, "via "+strings.Join(uniqueSorted(candidate.EdgeTypes), ","))
	}
	if strings.TrimSpace(candidate.Description) != "" {
		parts = append(parts, strings.TrimSpace(candidate.Description))
	}
	return strings.Join(parts, "; ")
}

func semanticReason(candidate SemanticCandidate, source Source) string {
	parts := []string{"semantic neighbor of refactor target"}
	if candidate.Similarity > 0 {
		parts = append(parts, fmt.Sprintf("similarity %.3f", candidate.Similarity))
	}
	if source != "" {
		parts = append(parts, "via "+string(source))
	}
	return strings.Join(parts, "; ")
}

func targetPaths(targets []Target) []string {
	paths := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.Path != "" {
			paths = append(paths, target.Path)
		}
		if target.OldPath != "" {
			paths = append(paths, target.OldPath)
		}
	}
	return uniqueSorted(paths)
}

func summarize(packet Packet) Summary {
	return Summary{
		TargetCount:           len(packet.Targets),
		MustUpdateCount:       len(packet.MustUpdate),
		ShouldInspectCount:    len(packet.ShouldInspect),
		LikelyDuplicateCount:  len(packet.LikelyDuplicate),
		ContractBoundaryCount: len(packet.ContractBoundary),
		TestsToRunCount:       len(packet.TestsToRun),
		DocsToUpdateCount:     len(packet.DocsToUpdate),
		ContextOnlyCount:      len(packet.ContextOnly),
	}
}

func contractChangingIntent(intent RefactorIntent) bool {
	switch intent {
	case IntentRename, IntentDelete, IntentTypeTighten, IntentAPIContractChange:
		return true
	default:
		return false
	}
}

func hasEdge(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasDocEdge(values []string) bool {
	for _, value := range values {
		switch value {
		case "DESCRIBED_BY", "DOC_RELATED", "DOC_FLOW", "DECIDED_BY":
			return true
		}
	}
	return false
}

func hasContractBoundaryEdge(values []string) bool {
	for _, value := range values {
		switch value {
		case "IMPLEMENTS", "EMBEDS", "ENFORCES", "IMPLEMENTS_PROTOCOL":
			return true
		}
	}
	return false
}
