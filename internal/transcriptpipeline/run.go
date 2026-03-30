package transcriptpipeline

import (
	"context"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	memstore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/v2/adapters/sourceimport"
)

// SingleRunOptions configures one single-transcript derivation run.
type SingleRunOptions struct {
	StorageRoot   string
	CASPath       string
	Provider      string
	SourceFile    string
	SessionID     string
	Workspace     string
	ActorID       string
	FrameLimit    int
	Runtime       LocalModelRuntime
	Classifier    DerivationClassifier
	PersistMemory bool
	AlignDoctrine bool
}

// SingleRunResult is the stage output for one single-transcript run.
type SingleRunResult struct {
	Parsed                  sourceimport.ParsedSession           `json:"-"`
	ConversationID          string                               `json:"conversation_id"`
	WorkspaceFamilyPath     string                               `json:"workspace_family_path,omitempty"`
	Frames                  []companion.AnchoredInteractionFrame `json:"frames"`
	Derivations             []companion.AnchoredMemoryDerivation `json:"derivations"`
	Insights                []DecisionInsight                    `json:"insights,omitempty"`
	InsightBrief            *InsightBrief                        `json:"insight_brief,omitempty"`
	InsightTimeline         []InsightTimelineEntry               `json:"insight_timeline,omitempty"`
	NotableInsights         []NotableInsight                     `json:"notable_insights,omitempty"`
	HistoryProfile          *HistoryProfile                      `json:"history_profile,omitempty"`
	HistoryAnswers          []HistoryAnswer                      `json:"history_answers,omitempty"`
	HistoryPack             *HistoryPack                         `json:"history_pack,omitempty"`
	HistoryRecords          []HistoryRecord                      `json:"history_records,omitempty"`
	PersistedHistory        []PersistedHistoryRecord             `json:"persisted_history,omitempty"`
	RemovedHistory          []string                             `json:"removed_history,omitempty"`
	Objective               *SessionObjective                    `json:"objective,omitempty"`
	Synopses                []FrameSynopsis                      `json:"synopses,omitempty"`
	ClassifiedClaims        []ClassifiedClaim                    `json:"classified_claims,omitempty"`
	ConsolidatedClaims      []ClassifiedClaim                    `json:"consolidated_claims,omitempty"`
	ReviewedClaims          []ClassifiedClaim                    `json:"reviewed_claims,omitempty"`
	DoctrineSeedClaims      []ClassifiedClaim                    `json:"doctrine_seed_claims,omitempty"`
	DoctrineClaims          []ClassifiedClaim                    `json:"doctrine_claims,omitempty"`
	AlignedClaims           []ClassifiedClaim                    `json:"aligned_claims,omitempty"`
	ClassificationArtifact  *ArtifactCacheReport                 `json:"classification_artifact,omitempty"`
	ClassificationArtifacts []ArtifactCacheReport                `json:"classification_artifacts,omitempty"`
	ReviewArtifact          *ArtifactCacheReport                 `json:"review_artifact,omitempty"`
	DoctrineSeedArtifact    *ArtifactCacheReport                 `json:"doctrine_seed_artifact,omitempty"`
	DoctrineArtifact        *ArtifactCacheReport                 `json:"doctrine_artifact,omitempty"`
	AlignmentArtifact       *ArtifactCacheReport                 `json:"alignment_artifact,omitempty"`
	PrederivedArtifacts     []ArtifactCacheReport                `json:"prederived_artifacts,omitempty"`
	PersistedMemory         []PersistedMemory                    `json:"persisted_memory,omitempty"`
	TranscriptCacheRoot     string                               `json:"transcript_cache_root"`
	TranscriptCachePath     string                               `json:"transcript_cache_path"`
}

// GroupRunOptions configures one grouped multi-transcript derivation run.
type GroupRunOptions struct {
	StorageRoot   string
	CASPath       string
	SourceFiles   []string
	ActorID       string
	Workspace     string
	FrameLimit    int
	Runtime       LocalModelRuntime
	Classifier    DerivationClassifier
	PersistMemory bool
	AlignDoctrine bool
}

// GroupRunItem is one grouped transcript family result.
type GroupRunItem struct {
	GroupID                 string                               `json:"group_id"`
	WorkspacePath           string                               `json:"workspace_path,omitempty"`
	WorkspaceFamilyPath     string                               `json:"workspace_family_path,omitempty"`
	SessionIDs              []string                             `json:"session_ids"`
	MainlineSessionIDs      []string                             `json:"mainline_session_ids,omitempty"`
	SidecarSessionIDs       []string                             `json:"sidecar_session_ids,omitempty"`
	SourceFiles             []string                             `json:"source_files"`
	Topline                 string                               `json:"topline,omitempty"`
	ToplineArtifact         *ArtifactCacheReport                 `json:"topline_artifact,omitempty"`
	ToplineClaimsArtifact   *ArtifactCacheReport                 `json:"topline_claims_artifact,omitempty"`
	ToplineClaims           []ConsensusClaim                     `json:"topline_claims,omitempty"`
	ConsensusClaims         []ConsensusClaim                     `json:"consensus_claims,omitempty"`
	Insights                []DecisionInsight                    `json:"insights,omitempty"`
	InsightBrief            *InsightBrief                        `json:"insight_brief,omitempty"`
	InsightTimeline         []InsightTimelineEntry               `json:"insight_timeline,omitempty"`
	NotableInsights         []NotableInsight                     `json:"notable_insights,omitempty"`
	HistoryAnswers          []HistoryAnswer                      `json:"history_answers,omitempty"`
	HistoryPack             *HistoryPack                         `json:"history_pack,omitempty"`
	HistoryRecords          []HistoryRecord                      `json:"history_records,omitempty"`
	PersistedHistory        []PersistedHistoryRecord             `json:"persisted_history,omitempty"`
	RemovedHistory          []string                             `json:"removed_history,omitempty"`
	Objective               *SessionObjective                    `json:"objective,omitempty"`
	Synopses                []FrameSynopsis                      `json:"synopses,omitempty"`
	ClassifiedClaims        []ClassifiedClaim                    `json:"classified_claims,omitempty"`
	ConsolidatedClaims      []ClassifiedClaim                    `json:"consolidated_claims,omitempty"`
	ReviewedClaims          []ClassifiedClaim                    `json:"reviewed_claims,omitempty"`
	DoctrineSeedClaims      []ClassifiedClaim                    `json:"doctrine_seed_claims,omitempty"`
	DoctrineClaims          []ClassifiedClaim                    `json:"doctrine_claims,omitempty"`
	AlignedClaims           []ClassifiedClaim                    `json:"aligned_claims,omitempty"`
	ClassificationArtifact  *ArtifactCacheReport                 `json:"classification_artifact,omitempty"`
	ClassificationArtifacts []ArtifactCacheReport                `json:"classification_artifacts,omitempty"`
	ReviewArtifact          *ArtifactCacheReport                 `json:"review_artifact,omitempty"`
	DoctrineSeedArtifact    *ArtifactCacheReport                 `json:"doctrine_seed_artifact,omitempty"`
	DoctrineArtifact        *ArtifactCacheReport                 `json:"doctrine_artifact,omitempty"`
	AlignmentArtifact       *ArtifactCacheReport                 `json:"alignment_artifact,omitempty"`
	Prederived              []ArtifactCacheReport                `json:"prederived_artifacts,omitempty"`
	Frames                  []companion.AnchoredInteractionFrame `json:"frames,omitempty"`
	Derivations             []companion.AnchoredMemoryDerivation `json:"derivations,omitempty"`
	PersistedMemory         []PersistedMemory                    `json:"persisted_memory,omitempty"`
	RemovedMemory           []string                             `json:"removed_memory,omitempty"`
	PacketCounts            map[string]int                       `json:"packet_counts,omitempty"`
}

// GroupRunResult is the stage output for one grouped transcript run.
type GroupRunResult struct {
	Groups              []GroupRunItem  `json:"groups"`
	TranscriptCacheRoot string          `json:"transcript_cache_root"`
	TranscriptCachePath string          `json:"transcript_cache_path"`
	HistoryProfile      *HistoryProfile `json:"history_profile,omitempty"`
}

// RunSingleInsight orchestrates a lightweight decision-support pass over one transcript.
func RunSingleInsight(ctx context.Context, opts SingleRunOptions) (SingleRunResult, error) {
	parsed, err := ResolveAndParseTranscript(opts.Provider, opts.SourceFile, opts.SessionID, opts.Workspace, opts.ActorID)
	if err != nil {
		return SingleRunResult{}, err
	}

	cacheStore, cacheRoot, err := OpenTranscriptCacheStore(ctx, opts.StorageRoot)
	if err != nil {
		return SingleRunResult{}, err
	}
	defer cacheStore.Close()

	prederived, err := PreprocessParsedSession(ctx, parsed, cacheStore, opts.Runtime.PreprocessOptions())
	if err != nil {
		return SingleRunResult{}, err
	}

	anchored, err := BuildAnchoredDerivations(ctx, prederived.Parsed, opts.FrameLimit)
	if err != nil {
		return SingleRunResult{}, err
	}
	objective, err := BuildSessionObjective(ctx, cacheStore, opts.Runtime, prederived.Parsed)
	if err != nil {
		return SingleRunResult{}, err
	}
	objectiveInsights := InsightFromObjective(&objective)
	rawInsights := append(BuildDecisionInsights(anchored.Derivations, 16), objectiveInsights...)
	insights := FinalizeDecisionInsights(rawInsights, 8)
	brief := BuildInsightBrief(insights)
	notables := BuildNotableInsights(anchored.Derivations, rawInsights, 6)
	profile := DefaultHistoryProfile()
	historyAnswers := BuildHistoryAnswers(profile, &objective, brief, notables, insights)
	return SingleRunResult{
		Parsed:              prederived.Parsed,
		ConversationID:      anchored.ConversationID,
		WorkspaceFamilyPath: workspace.FamilyPath(prederived.Parsed.WorkspacePath),
		Frames:              anchored.Frames,
		Derivations:         anchored.Derivations,
		Insights:            insights,
		InsightBrief:        brief,
		InsightTimeline:     BuildInsightTimeline(anchored.Derivations, insights, 6),
		NotableInsights:     notables,
		HistoryProfile:      profile,
		HistoryAnswers:      historyAnswers,
		HistoryPack:         BuildHistoryPack(historyAnswers),
		HistoryRecords: BuildHistoryRecords(profile, HistoryRecordContext{
			ConversationID:  anchored.ConversationID,
			SessionIDs:      []string{prederived.Parsed.SessionID},
			SourceStartedAt: parsedSessionStartedAt(prederived.Parsed),
		}, insights, notables, historyAnswers),
		Objective:           &objective,
		PrederivedArtifacts: prederived.Artifacts,
		TranscriptCacheRoot: cacheRoot,
		TranscriptCachePath: cacheStore.Path(),
	}, nil
}

// RunGroupedInsight orchestrates a lightweight grouped decision-support pass.
func RunGroupedInsight(ctx context.Context, opts GroupRunOptions) (GroupRunResult, error) {
	cacheStore, cacheRoot, err := OpenTranscriptCacheStore(ctx, opts.StorageRoot)
	if err != nil {
		return GroupRunResult{}, err
	}
	defer cacheStore.Close()

	bundles, err := LoadSourceBundles(opts.SourceFiles, opts.ActorID, opts.Workspace)
	if err != nil {
		return GroupRunResult{}, err
	}
	groups := GroupSourceBundles(bundles)

	result := GroupRunResult{
		Groups:              make([]GroupRunItem, 0, len(groups)),
		TranscriptCacheRoot: cacheRoot,
		TranscriptCachePath: cacheStore.Path(),
		HistoryProfile:      DefaultHistoryProfile(),
	}

	for _, group := range groups {
		combined := CombineMainline(group)
		prederived, err := PreprocessParsedSession(ctx, combined, cacheStore, opts.Runtime.PreprocessOptions())
		if err != nil {
			return GroupRunResult{}, err
		}
		anchored, err := BuildAnchoredDerivations(ctx, prederived.Parsed, opts.FrameLimit)
		if err != nil {
			return GroupRunResult{}, err
		}
		objective, err := BuildSessionObjective(ctx, cacheStore, opts.Runtime, prederived.Parsed)
		if err != nil {
			return GroupRunResult{}, err
		}

		packetSet := BuildPacketSet(group, anchored.Frames)
		workerCfg := opts.Runtime.WorkerConfig()
		topline, toplineArtifact, err := BuildGroupTopline(ctx, packetSet.Sidecars, cacheStore, workerCfg, opts.Runtime.Mode, opts.Runtime.GroupToplinePromptVersion, opts.Runtime.TimeoutSeconds())
		if err != nil {
			return GroupRunResult{}, err
		}
		toplineClaims, toplineClaimsArtifact, err := BuildStructuredToplineClaims(ctx, packetSet.Sidecars, cacheStore, workerCfg, opts.Runtime.Mode, opts.Runtime.GroupClaimsPromptVersion, opts.Runtime.TimeoutSeconds())
		if err != nil {
			return GroupRunResult{}, err
		}
		consensusClaims := DeriveConsensusClaims(packetSet.Sidecars, prederived.Parsed, anchored.Frames, anchored.Derivations, toplineClaims)
		consensusInsights := InsightsFromConsensusClaims(consensusClaims, 6)
		insightInputs := append([]DecisionInsight{}, BuildDecisionInsights(anchored.Derivations, 16)...)
		insightInputs = append(insightInputs, InsightFromObjective(&objective)...)
		insightInputs = append(insightInputs, consensusInsights...)
		insights := FinalizeDecisionInsights(insightInputs, 8)
		brief := BuildInsightBrief(insights)
		notables := BuildNotableInsights(anchored.Derivations, insightInputs, 6)
		historyAnswers := BuildHistoryAnswers(result.HistoryProfile, &objective, brief, notables, insights)

		item := GroupRunItem{
			GroupID:               group.GroupID,
			WorkspacePath:         group.WorkspacePath,
			WorkspaceFamilyPath:   group.WorkspaceFamilyPath,
			SessionIDs:            group.SessionIDs(),
			MainlineSessionIDs:    SessionIDsForBundles(group.MainlineBundles()),
			SidecarSessionIDs:     SessionIDsForBundles(group.SidecarBundles()),
			SourceFiles:           sourceFilesForGroup(group),
			Topline:               topline,
			ToplineArtifact:       toplineArtifact,
			ToplineClaimsArtifact: toplineClaimsArtifact,
			ToplineClaims:         toplineClaims,
			ConsensusClaims:       consensusClaims,
			Insights:              insights,
			InsightBrief:          brief,
			InsightTimeline:       BuildInsightTimeline(anchored.Derivations, insights, 6),
			NotableInsights:       notables,
			HistoryAnswers:        historyAnswers,
			HistoryPack:           BuildHistoryPack(historyAnswers),
			HistoryRecords: BuildHistoryRecords(result.HistoryProfile, HistoryRecordContext{
				ConversationID:  group.GroupID,
				GroupID:         group.GroupID,
				SessionIDs:      group.SessionIDs(),
				SourceStartedAt: parsedSessionStartedAt(prederived.Parsed),
			}, insights, notables, historyAnswers),
			Objective:   &objective,
			Frames:      anchored.Frames,
			Derivations: anchored.Derivations,
			Prederived:  prederived.Artifacts,
			PacketCounts: map[string]int{
				"mainline_interaction": len(packetSet.Mainline),
				"sidecar_result":       len(packetSet.Sidecars),
			},
		}
		result.Groups = append(result.Groups, item)
	}
	return result, nil
}

func parsedSessionStartedAt(parsed sourceimport.ParsedSession) time.Time {
	if len(parsed.Turns) == 0 {
		return time.Time{}
	}
	return parsed.Turns[0].CreatedAt.UTC()
}

// RunSingleDoctrine orchestrates the lighter doctrine-only path:
// transcript -> anchored derivations -> bridge seeds -> objective alignment -> persist.
func RunSingleDoctrine(ctx context.Context, opts SingleRunOptions) (SingleRunResult, error) {
	parsed, err := ResolveAndParseTranscript(opts.Provider, opts.SourceFile, opts.SessionID, opts.Workspace, opts.ActorID)
	if err != nil {
		return SingleRunResult{}, err
	}

	cacheStore, cacheRoot, err := OpenTranscriptCacheStore(ctx, opts.StorageRoot)
	if err != nil {
		return SingleRunResult{}, err
	}
	defer cacheStore.Close()

	prederived, err := PreprocessParsedSession(ctx, parsed, cacheStore, opts.Runtime.PreprocessOptions())
	if err != nil {
		return SingleRunResult{}, err
	}

	anchored, err := BuildAnchoredDerivations(ctx, prederived.Parsed, opts.FrameLimit)
	if err != nil {
		return SingleRunResult{}, err
	}

	classifier := NewCachedClaimClassifier(opts.Runtime)
	objective, err := BuildSessionObjective(ctx, cacheStore, opts.Runtime, prederived.Parsed)
	if err != nil {
		return SingleRunResult{}, err
	}
	doctrineSeeds, doctrineSeedArtifact, err := classifier.bridgeDoctrineSeeds(ctx, cacheStore, prederived.Parsed, anchored.Derivations, len(anchored.Frames))
	if err != nil {
		return SingleRunResult{}, err
	}
	doctrineClaims := finalizeDoctrineClaims(ConsolidateClassifiedClaims(doctrineSeeds), 2)
	doctrineArtifact := doctrineSeedArtifact
	if len(doctrineSeeds) > 0 {
		doctrineClaims, doctrineArtifact, err = classifier.distillSegmentedDoctrineClaims(ctx, cacheStore, len(anchored.Frames), doctrineSeeds)
		if err != nil {
			return SingleRunResult{}, err
		}
		doctrineClaims = finalizeDoctrineClaims(doctrineClaims, 2)
	}
	var alignedClaims []ClassifiedClaim
	var alignmentArtifact *ArtifactCacheReport
	if opts.AlignDoctrine && len(doctrineClaims) > 0 && strings.TrimSpace(objective.Objective) != "" {
		alignedClaims, alignmentArtifact, err = classifier.alignClaimsToObjective(ctx, cacheStore, objective, doctrineClaims)
		if err != nil {
			return SingleRunResult{}, err
		}
	} else {
		doctrineClaims = stripObjectiveAnnotations(doctrineClaims)
		alignedClaims = doctrineClaims
	}

	result := SingleRunResult{
		Parsed:               prederived.Parsed,
		ConversationID:       anchored.ConversationID,
		WorkspaceFamilyPath:  workspace.FamilyPath(prederived.Parsed.WorkspacePath),
		Frames:               anchored.Frames,
		Derivations:          anchored.Derivations,
		Objective:            &objective,
		DoctrineSeedClaims:   doctrineSeeds,
		DoctrineClaims:       doctrineClaims,
		AlignedClaims:        alignedClaims,
		DoctrineSeedArtifact: doctrineSeedArtifact,
		DoctrineArtifact:     doctrineArtifact,
		AlignmentArtifact:    alignmentArtifact,
		PrederivedArtifacts:  prederived.Artifacts,
		TranscriptCacheRoot:  cacheRoot,
		TranscriptCachePath:  cacheStore.Path(),
	}
	if !opts.PersistMemory {
		return result, nil
	}

	memStore, err := memstore.Open(ctx, opts.StorageRoot, opts.CASPath)
	if err != nil {
		return SingleRunResult{}, err
	}
	defer memStore.Close()
	workspaceID := resolveWorkspaceID(prederived.Parsed.WorkspacePath)
	persisted, err := PersistClassifiedClaims(ctx, memStore, prederived.Parsed, anchored.ConversationID, workspaceID, &objective, alignedClaims)
	if err != nil {
		return SingleRunResult{}, err
	}
	result.PersistedMemory = persisted
	return result, nil
}

// RunGroupedDoctrine orchestrates the lighter grouped doctrine-only path.
func RunGroupedDoctrine(ctx context.Context, opts GroupRunOptions) (GroupRunResult, error) {
	cacheStore, cacheRoot, err := OpenTranscriptCacheStore(ctx, opts.StorageRoot)
	if err != nil {
		return GroupRunResult{}, err
	}
	defer cacheStore.Close()

	bundles, err := LoadSourceBundles(opts.SourceFiles, opts.ActorID, opts.Workspace)
	if err != nil {
		return GroupRunResult{}, err
	}
	groups := GroupSourceBundles(bundles)

	result := GroupRunResult{
		Groups:              make([]GroupRunItem, 0, len(groups)),
		TranscriptCacheRoot: cacheRoot,
		TranscriptCachePath: cacheStore.Path(),
	}

	var memStore *memstore.Store
	if opts.PersistMemory {
		memStore, err = memstore.Open(ctx, opts.StorageRoot, opts.CASPath)
		if err != nil {
			return GroupRunResult{}, err
		}
		defer memStore.Close()
	}

	classifier := NewCachedClaimClassifier(opts.Runtime)
	for _, group := range groups {
		combined := CombineMainline(group)
		prederived, err := PreprocessParsedSession(ctx, combined, cacheStore, opts.Runtime.PreprocessOptions())
		if err != nil {
			return GroupRunResult{}, err
		}
		anchored, err := BuildAnchoredDerivations(ctx, prederived.Parsed, opts.FrameLimit)
		if err != nil {
			return GroupRunResult{}, err
		}
		objective, err := BuildSessionObjective(ctx, cacheStore, opts.Runtime, prederived.Parsed)
		if err != nil {
			return GroupRunResult{}, err
		}
		doctrineSeeds, doctrineSeedArtifact, err := classifier.bridgeDoctrineSeeds(ctx, cacheStore, prederived.Parsed, anchored.Derivations, len(anchored.Frames))
		if err != nil {
			return GroupRunResult{}, err
		}

		packetSet := BuildPacketSet(group, anchored.Frames)
		workerCfg := opts.Runtime.WorkerConfig()
		topline, toplineArtifact, err := BuildGroupTopline(ctx, packetSet.Sidecars, cacheStore, workerCfg, opts.Runtime.Mode, opts.Runtime.GroupToplinePromptVersion, opts.Runtime.TimeoutSeconds())
		if err != nil {
			return GroupRunResult{}, err
		}
		toplineClaims, toplineClaimsArtifact, err := BuildStructuredToplineClaims(ctx, packetSet.Sidecars, cacheStore, workerCfg, opts.Runtime.Mode, opts.Runtime.GroupClaimsPromptVersion, opts.Runtime.TimeoutSeconds())
		if err != nil {
			return GroupRunResult{}, err
		}
		consensusClaims := DeriveConsensusClaims(packetSet.Sidecars, prederived.Parsed, anchored.Frames, anchored.Derivations, toplineClaims)

		doctrineClaims := finalizeGroupedDoctrineClaims(ConsolidateClassifiedClaims(mergeClassifiedClaims(doctrineSeeds, ClassifiedClaimsFromConsensus(consensusClaims))), 2)
		doctrineArtifact := doctrineSeedArtifact
		if len(doctrineSeeds) > 0 {
			doctrineClaims, doctrineArtifact, err = classifier.distillSegmentedDoctrineClaims(ctx, cacheStore, len(anchored.Frames), doctrineSeeds)
			if err != nil {
				return GroupRunResult{}, err
			}
			doctrineClaims = finalizeGroupedDoctrineClaims(ConsolidateClassifiedClaims(mergeClassifiedClaims(doctrineClaims, ClassifiedClaimsFromConsensus(consensusClaims))), 2)
		}
		var alignedClaims []ClassifiedClaim
		var alignmentArtifact *ArtifactCacheReport
		if opts.AlignDoctrine && len(doctrineClaims) > 0 && strings.TrimSpace(objective.Objective) != "" {
			alignedClaims, alignmentArtifact, err = classifier.alignClaimsToObjective(ctx, cacheStore, objective, doctrineClaims)
			if err != nil {
				return GroupRunResult{}, err
			}
		} else {
			doctrineClaims = stripObjectiveAnnotations(doctrineClaims)
			alignedClaims = doctrineClaims
		}

		item := GroupRunItem{
			GroupID:               group.GroupID,
			WorkspacePath:         group.WorkspacePath,
			WorkspaceFamilyPath:   group.WorkspaceFamilyPath,
			SessionIDs:            group.SessionIDs(),
			MainlineSessionIDs:    SessionIDsForBundles(group.MainlineBundles()),
			SidecarSessionIDs:     SessionIDsForBundles(group.SidecarBundles()),
			SourceFiles:           sourceFilesForGroup(group),
			Topline:               topline,
			ToplineArtifact:       toplineArtifact,
			ToplineClaimsArtifact: toplineClaimsArtifact,
			ToplineClaims:         toplineClaims,
			ConsensusClaims:       consensusClaims,
			Objective:             &objective,
			Frames:                anchored.Frames,
			Derivations:           anchored.Derivations,
			DoctrineSeedClaims:    doctrineSeeds,
			DoctrineClaims:        doctrineClaims,
			AlignedClaims:         alignedClaims,
			DoctrineSeedArtifact:  doctrineSeedArtifact,
			DoctrineArtifact:      doctrineArtifact,
			AlignmentArtifact:     alignmentArtifact,
			Prederived:            prederived.Artifacts,
			PacketCounts: map[string]int{
				"mainline_interaction": len(packetSet.Mainline),
				"sidecar_result":       len(packetSet.Sidecars),
			},
		}
		if memStore != nil {
			workspaceID := resolveWorkspaceID(group.WorkspacePath)
			persisted, err := PersistClassifiedClaims(ctx, memStore, prederived.Parsed, anchored.ConversationID, workspaceID, &objective, alignedClaims)
			if err != nil {
				return GroupRunResult{}, err
			}
			item.PersistedMemory = persisted
		}
		result.Groups = append(result.Groups, item)
	}
	return result, nil
}

// RunSingle orchestrates one full single-transcript derivation pipeline run.
func RunSingle(ctx context.Context, opts SingleRunOptions) (SingleRunResult, error) {
	parsed, err := ResolveAndParseTranscript(opts.Provider, opts.SourceFile, opts.SessionID, opts.Workspace, opts.ActorID)
	if err != nil {
		return SingleRunResult{}, err
	}

	cacheStore, cacheRoot, err := OpenTranscriptCacheStore(ctx, opts.StorageRoot)
	if err != nil {
		return SingleRunResult{}, err
	}
	defer cacheStore.Close()

	prederived, err := PreprocessParsedSession(ctx, parsed, cacheStore, opts.Runtime.PreprocessOptions())
	if err != nil {
		return SingleRunResult{}, err
	}

	anchored, err := BuildAnchoredDerivations(ctx, prederived.Parsed, opts.FrameLimit)
	if err != nil {
		return SingleRunResult{}, err
	}
	var classification ClassificationResult
	if opts.Classifier != nil {
		classification, err = opts.Classifier.Classify(ctx, cacheStore, prederived.Parsed, anchored.Frames, anchored.Derivations)
		if err != nil {
			return SingleRunResult{}, err
		}
	}

	result := SingleRunResult{
		Parsed:                  prederived.Parsed,
		ConversationID:          anchored.ConversationID,
		WorkspaceFamilyPath:     workspace.FamilyPath(prederived.Parsed.WorkspacePath),
		Frames:                  anchored.Frames,
		Derivations:             anchored.Derivations,
		Objective:               classification.Objective,
		Synopses:                classification.Synopses,
		ClassifiedClaims:        classification.Claims,
		ConsolidatedClaims:      classification.ConsolidatedClaims,
		ReviewedClaims:          classification.ReviewedClaims,
		DoctrineSeedClaims:      classification.DoctrineSeedClaims,
		DoctrineClaims:          classification.DoctrineClaims,
		AlignedClaims:           classification.AlignedClaims,
		ClassificationArtifact:  classification.Artifact,
		ClassificationArtifacts: classification.Artifacts,
		ReviewArtifact:          classification.ReviewArtifact,
		DoctrineSeedArtifact:    classification.DoctrineSeedArtifact,
		DoctrineArtifact:        classification.DoctrineArtifact,
		AlignmentArtifact:       classification.AlignmentArtifact,
		PrederivedArtifacts:     prederived.Artifacts,
		TranscriptCacheRoot:     cacheRoot,
		TranscriptCachePath:     cacheStore.Path(),
	}

	if !opts.PersistMemory {
		return result, nil
	}

	memStore, err := memstore.Open(ctx, opts.StorageRoot, opts.CASPath)
	if err != nil {
		return SingleRunResult{}, err
	}
	defer memStore.Close()

	workspaceID := resolveWorkspaceID(prederived.Parsed.WorkspacePath)
	claimsForPersistence := classification.AlignedClaims
	if len(claimsForPersistence) == 0 {
		claimsForPersistence = classification.ReviewedClaims
	}
	if len(claimsForPersistence) == 0 {
		claimsForPersistence = classification.ConsolidatedClaims
	}
	persisted, err := PersistClassifiedClaims(ctx, memStore, prederived.Parsed, anchored.ConversationID, workspaceID, classification.Objective, claimsForPersistence)
	if err != nil {
		return SingleRunResult{}, err
	}
	allowRawFallback := len(classification.Claims) == 0 && len(classification.ReviewedClaims) == 0 && len(classification.AlignedClaims) == 0
	if len(persisted) == 0 && allowRawFallback {
		persisted, err = PersistDurableTranscriptMemories(ctx, memStore, prederived.Parsed, anchored.ConversationID, workspaceID, anchored.Derivations)
	}
	if err != nil {
		return SingleRunResult{}, err
	}
	result.PersistedMemory = persisted
	return result, nil
}

// RunGrouped orchestrates one full grouped transcript derivation pipeline run.
func RunGrouped(ctx context.Context, opts GroupRunOptions) (GroupRunResult, error) {
	cacheStore, cacheRoot, err := OpenTranscriptCacheStore(ctx, opts.StorageRoot)
	if err != nil {
		return GroupRunResult{}, err
	}
	defer cacheStore.Close()

	bundles, err := LoadSourceBundles(opts.SourceFiles, opts.ActorID, opts.Workspace)
	if err != nil {
		return GroupRunResult{}, err
	}
	groups := GroupSourceBundles(bundles)

	result := GroupRunResult{
		Groups:              make([]GroupRunItem, 0, len(groups)),
		TranscriptCacheRoot: cacheRoot,
		TranscriptCachePath: cacheStore.Path(),
	}

	var memStore *memstore.Store
	if opts.PersistMemory {
		memStore, err = memstore.Open(ctx, opts.StorageRoot, opts.CASPath)
		if err != nil {
			return GroupRunResult{}, err
		}
		defer memStore.Close()
	}

	for _, group := range groups {
		combined := CombineMainline(group)
		prederived, err := PreprocessParsedSession(ctx, combined, cacheStore, opts.Runtime.PreprocessOptions())
		if err != nil {
			return GroupRunResult{}, err
		}
		anchored, err := BuildAnchoredDerivations(ctx, prederived.Parsed, opts.FrameLimit)
		if err != nil {
			return GroupRunResult{}, err
		}
		var classification ClassificationResult
		if opts.Classifier != nil {
			classification, err = opts.Classifier.Classify(ctx, cacheStore, prederived.Parsed, anchored.Frames, anchored.Derivations)
			if err != nil {
				return GroupRunResult{}, err
			}
		}

		packetSet := BuildPacketSet(group, anchored.Frames)
		workerCfg := opts.Runtime.WorkerConfig()
		topline, toplineArtifact, err := BuildGroupTopline(ctx, packetSet.Sidecars, cacheStore, workerCfg, opts.Runtime.Mode, opts.Runtime.GroupToplinePromptVersion, opts.Runtime.TimeoutSeconds())
		if err != nil {
			return GroupRunResult{}, err
		}
		toplineClaims, toplineClaimsArtifact, err := BuildStructuredToplineClaims(ctx, packetSet.Sidecars, cacheStore, workerCfg, opts.Runtime.Mode, opts.Runtime.GroupClaimsPromptVersion, opts.Runtime.TimeoutSeconds())
		if err != nil {
			return GroupRunResult{}, err
		}
		consensusClaims := DeriveConsensusClaims(packetSet.Sidecars, prederived.Parsed, anchored.Frames, anchored.Derivations, toplineClaims)
		if len(consensusClaims) > 0 {
			classification.Claims = mergeClassifiedClaims(classification.Claims, ClassifiedClaimsFromConsensus(consensusClaims))
			classification.ConsolidatedClaims = ConsolidateClassifiedClaims(classification.Claims)
			if len(classification.ReviewedClaims) > 0 {
				classification.ReviewedClaims = ConsolidateClassifiedClaims(mergeClassifiedClaims(classification.ReviewedClaims, ClassifiedClaimsFromConsensus(consensusClaims)))
			}
			doctrineSeeds := mergeClassifiedClaims(classification.DoctrineSeedClaims, ClassifiedClaimsFromConsensus(consensusClaims))
			classification.DoctrineSeedClaims = doctrineSeeds
			classification.DoctrineClaims = ConsolidateClassifiedClaims(doctrineSeeds)
			classification.DoctrineArtifact = classification.DoctrineSeedArtifact
			classification.AlignedClaims = classification.DoctrineClaims
			classification.AlignmentArtifact = nil
			if len(classification.DoctrineClaims) > 0 && classification.Objective != nil && strings.TrimSpace(classification.Objective.Objective) != "" {
				if cached, ok := opts.Classifier.(*CachedClaimClassifier); ok {
					alignedClaims, alignmentArtifact, err := cached.alignClaimsToObjective(ctx, cacheStore, *classification.Objective, classification.DoctrineClaims)
					if err != nil {
						return GroupRunResult{}, err
					}
					classification.AlignedClaims = alignedClaims
					classification.AlignmentArtifact = alignmentArtifact
				}
			}
		}

		item := GroupRunItem{
			GroupID:                 group.GroupID,
			WorkspacePath:           group.WorkspacePath,
			WorkspaceFamilyPath:     group.WorkspaceFamilyPath,
			SessionIDs:              group.SessionIDs(),
			MainlineSessionIDs:      SessionIDsForBundles(group.MainlineBundles()),
			SidecarSessionIDs:       SessionIDsForBundles(group.SidecarBundles()),
			SourceFiles:             sourceFilesForGroup(group),
			Topline:                 topline,
			ToplineArtifact:         toplineArtifact,
			ToplineClaimsArtifact:   toplineClaimsArtifact,
			ToplineClaims:           toplineClaims,
			ConsensusClaims:         consensusClaims,
			Objective:               classification.Objective,
			Synopses:                classification.Synopses,
			ClassifiedClaims:        classification.Claims,
			ConsolidatedClaims:      classification.ConsolidatedClaims,
			ReviewedClaims:          classification.ReviewedClaims,
			DoctrineSeedClaims:      classification.DoctrineSeedClaims,
			DoctrineClaims:          classification.DoctrineClaims,
			AlignedClaims:           classification.AlignedClaims,
			ClassificationArtifact:  classification.Artifact,
			ClassificationArtifacts: classification.Artifacts,
			ReviewArtifact:          classification.ReviewArtifact,
			DoctrineSeedArtifact:    classification.DoctrineSeedArtifact,
			DoctrineArtifact:        classification.DoctrineArtifact,
			AlignmentArtifact:       classification.AlignmentArtifact,
			Prederived:              prederived.Artifacts,
			Frames:                  anchored.Frames,
			Derivations:             anchored.Derivations,
			PacketCounts: map[string]int{
				"mainline_interaction": len(packetSet.Mainline),
				"sidecar_result":       len(packetSet.Sidecars),
			},
		}

		if memStore != nil {
			workspaceID := resolveWorkspaceID(group.WorkspacePath)
			claimsForPersistence := classification.AlignedClaims
			if len(claimsForPersistence) == 0 {
				claimsForPersistence = classification.ReviewedClaims
			}
			if len(claimsForPersistence) == 0 {
				claimsForPersistence = classification.ConsolidatedClaims
			}
			persisted, err := PersistClassifiedClaims(ctx, memStore, prederived.Parsed, anchored.ConversationID, workspaceID, classification.Objective, claimsForPersistence)
			if err != nil {
				return GroupRunResult{}, err
			}
			consensusPersisted, err := PersistConsensusClaims(ctx, memStore, prederived.Parsed, anchored.ConversationID, workspaceID, consensusClaims)
			if err != nil {
				return GroupRunResult{}, err
			}
			allowRawFallback := len(classification.Claims) == 0 && len(classification.ReviewedClaims) == 0 && len(classification.AlignedClaims) == 0
			if len(persisted) == 0 && len(consensusPersisted) == 0 && allowRawFallback {
				persisted, err = PersistDurableTranscriptMemories(ctx, memStore, prederived.Parsed, anchored.ConversationID, workspaceID, anchored.Derivations)
				if err != nil {
					return GroupRunResult{}, err
				}
			}
			persisted = append(persisted, consensusPersisted...)
			removed, err := ReconcileMemoryPrefix(ctx, memStore, workspaceID, TranscriptMemoryPrefix(prederived.Parsed.SessionID), persisted)
			if err != nil {
				return GroupRunResult{}, err
			}
			item.PersistedMemory = persisted
			item.RemovedMemory = removed
		}

		result.Groups = append(result.Groups, item)
	}
	return result, nil
}

func resolveWorkspaceID(workspacePath string) string {
	target := strings.TrimSpace(workspacePath)
	if target == "" {
		target = workspace.Detect("")
	} else {
		target = workspace.Detect(target)
	}
	return workspace.Normalize(target)
}

func sourceFilesForGroup(group SourceGroup) []string {
	out := make([]string, 0, len(group.Bundles))
	for _, bundle := range group.Bundles {
		out = append(out, bundle.Meta.SourcePath)
	}
	return out
}
