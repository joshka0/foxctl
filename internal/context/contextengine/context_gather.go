package contextengine

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ContextBudget constrains context gathering and reduction.
type ContextBudget struct {
	MaxSources      int `json:"max_sources,omitempty"`
	MaxContextChars int `json:"max_context_chars,omitempty"`
}

// GatherContextRequest describes one context gathering operation.
type GatherContextRequest struct {
	Query                string                 `json:"query"`
	Goal                 string                 `json:"goal,omitempty"`
	TaskID               string                 `json:"task_id,omitempty"`
	TaskType             string                 `json:"task_type,omitempty"`
	SourceProfiles       []SourceProfile        `json:"source_profiles,omitempty"`
	Lanes                []EvidenceLane         `json:"lanes,omitempty"`
	Limit                int                    `json:"limit,omitempty"`
	RequiredEvidence     []string               `json:"required_evidence,omitempty"`
	CoverageRequirements []CoverageRequirement  `json:"coverage_requirements,omitempty"`
	Budget               ContextBudget          `json:"budget,omitempty"`
	Reduction            BundleReductionOptions `json:"-"`
	Certification        CertificationOptions   `json:"-"`
}

// GatherContextDeps holds retrieval dependencies for GatherContext.
type GatherContextDeps struct {
	CodeSearch    CodeSearchFunc
	MemoryQuery   MemoryQueryFunc
	ContextQuery  ContextQueryFunc
	ContextPacks  ContextPackFunc
	TaskQuery     TaskQueryFunc
	TaskList      TaskListFunc
	SessionRecall SessionRecallFunc
	Staleness     StalenessLookupFunc
}

// GatherContext retrieves, reduces, and certifies context using existing lanes.
func GatherContext(ctx context.Context, cfg LaneConfig, deps GatherContextDeps, req GatherContextRequest) (ContextBundle, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return ContextBundle{}, EmptyQueryError{Lane: LaneMixed}
	}
	if cfg.IDGen == nil {
		cfg.IDGen = defaultContextIDGen("ce")
	}
	if cfg.Clock == nil {
		cfg.Clock = req.Reduction.defaults().Clock
	}

	lanes := normalizeGatherLanes(req.Lanes)
	packs, err := retrieveGatherPacks(ctx, cfg, deps, req, lanes)
	if err != nil {
		return ContextBundle{}, err
	}

	reduction := req.Reduction
	if reduction.IDGen == nil {
		reduction.IDGen = cfg.IDGen
	}
	if reduction.Clock == nil {
		reduction.Clock = cfg.Clock
	}
	if reduction.MaxFacts <= 0 {
		if req.Budget.MaxSources > 0 {
			reduction.MaxFacts = req.Budget.MaxSources
		} else if req.Limit > 0 {
			reduction.MaxFacts = req.Limit
		}
	}
	if reduction.MaxContextChars <= 0 && req.Budget.MaxContextChars > 0 {
		reduction.MaxContextChars = req.Budget.MaxContextChars
	}
	if strings.TrimSpace(reduction.TaskType) == "" {
		reduction.TaskType = strings.TrimSpace(req.TaskType)
	}
	if len(reduction.SourceProfiles) == 0 && len(req.SourceProfiles) > 0 {
		reduction.SourceProfiles = append([]SourceProfile(nil), req.SourceProfiles...)
	}
	if len(reduction.RequiredEvidence) == 0 && len(req.RequiredEvidence) > 0 {
		reduction.RequiredEvidence = cleanRequiredEvidence(req.RequiredEvidence)
	}
	if len(reduction.CoverageRequirements) == 0 && len(req.CoverageRequirements) > 0 {
		reduction.CoverageRequirements = append([]CoverageRequirement(nil), req.CoverageRequirements...)
	}
	bundle, err := ReduceEvidencePacksToBundle(query, req.Goal, packs, reduction)
	if err != nil {
		return ContextBundle{}, err
	}

	var markers []StalenessMarker
	if deps.Staleness != nil && len(bundle.RelevantRefs()) > 0 {
		markers, err = deps.Staleness(ctx, cfg.WorkspaceID, bundle.RelevantRefs())
		if err != nil {
			return ContextBundle{}, fmt.Errorf("gather context: staleness: %w", err)
		}
	}

	certification := req.Certification
	if certification.IDGen == nil {
		certification.IDGen = cfg.IDGen
	}
	if certification.Clock == nil {
		certification.Clock = cfg.Clock
	}
	cert, err := CertifyContextBundle(bundle, markers, certification)
	if err != nil {
		return ContextBundle{}, err
	}
	bundle.Certificate = &cert
	applyCertificateToBundle(&bundle)
	if err := bundle.Validate(); err != nil {
		return ContextBundle{}, err
	}
	persistContextBundleProjection(ctx, cfg, bundle)
	return bundle, nil
}

type contextBundleProjectionStore interface {
	PutProjection(ctx context.Context, id, workspaceID, projectionType string, version int, taskID string, generatedFromEvents []string, payload any, generatedAt, expiresAt time.Time) error
}

func persistContextBundleProjection(ctx context.Context, cfg LaneConfig, bundle ContextBundle) {
	if cfg.Store == nil {
		return
	}
	store, ok := cfg.Store.(contextBundleProjectionStore)
	if !ok {
		return
	}
	generatedAt := bundle.CreatedAt
	if generatedAt.IsZero() {
		generatedAt = cfg.Clock()
	}
	_ = store.PutProjection(ctx, bundle.ID, bundle.WorkspaceID, "context_bundle", 1, bundle.MetadataString("task_id"), bundle.SourceEpisodeIDs, bundle, generatedAt, time.Time{})
}

// RelevantRefs returns bundle refs in deterministic bundle order.
func (b ContextBundle) RelevantRefs() []EvidenceRef {
	refs := make([]EvidenceRef, 0, len(b.Evidence))
	seen := map[string]struct{}{}
	for _, node := range b.Evidence {
		key := evidenceRefKey(node.Ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, node.Ref)
	}
	return refs
}

// MetadataString returns a string metadata value when present.
func (b ContextBundle) MetadataString(key string) string {
	if len(b.Metadata) == 0 {
		return ""
	}
	value, _ := b.Metadata[key].(string)
	return value
}

func normalizeGatherLanes(input []EvidenceLane) []EvidenceLane {
	if len(input) == 0 {
		return []EvidenceLane{LaneMixed}
	}
	seen := map[EvidenceLane]struct{}{}
	out := make([]EvidenceLane, 0, len(input))
	for _, lane := range input {
		if !lane.IsValid() {
			continue
		}
		if lane == LaneMixed {
			return []EvidenceLane{LaneMixed}
		}
		if _, ok := seen[lane]; ok {
			continue
		}
		seen[lane] = struct{}{}
		out = append(out, lane)
	}
	if len(out) == 0 {
		return []EvidenceLane{LaneMixed}
	}
	return out
}

func retrieveGatherPacks(ctx context.Context, cfg LaneConfig, deps GatherContextDeps, req GatherContextRequest, lanes []EvidenceLane) ([]EvidencePack, error) {
	query := gatherRetrievalQuery(req.Query, req.RequiredEvidence)
	if len(lanes) == 1 && lanes[0] == LaneMixed {
		pack, err := RetrieveMixed(ctx, cfg, deps.CodeSearch, deps.MemoryQuery, deps.ContextQuery, deps.TaskQuery, deps.TaskList, req.TaskID, query)
		if err != nil {
			if _, ok := err.(LaneError); !ok {
				return nil, err
			}
		}
		packs := []EvidencePack{pack}
		if deps.SessionRecall != nil {
			sessionPack, sessionErr := RetrieveSessionRecall(ctx, cfg, deps.SessionRecall, query, req.Limit)
			if sessionErr != nil {
				if _, ok := sessionErr.(LaneError); !ok {
					return nil, sessionErr
				}
			}
			if len(sessionPack.Nodes) > 0 {
				packs = append(packs, sessionPack)
			}
		}
		if deps.ContextPacks != nil {
			extraPacks, extraErr := deps.ContextPacks(ctx, cfg.WorkspaceID, query, req.Limit)
			if extraErr != nil {
				if _, ok := extraErr.(LaneError); !ok {
					return nil, extraErr
				}
			}
			packs = appendNonEmptyEvidencePacks(packs, extraPacks...)
		}
		return packs, nil
	}

	packs := make([]EvidencePack, 0, len(lanes))
	for _, lane := range lanes {
		var (
			pack EvidencePack
			err  error
		)
		switch lane {
		case LaneCode:
			pack, err = RetrieveCode(ctx, cfg, deps.CodeSearch, query)
		case LaneMemory:
			pack, err = RetrieveMemory(ctx, cfg, deps.MemoryQuery, query)
		case LaneContext:
			pack, err = RetrieveContext(ctx, cfg, deps.ContextQuery, query)
			if err == nil && deps.SessionRecall != nil {
				sessionPack, sessionErr := RetrieveSessionRecall(ctx, cfg, deps.SessionRecall, query, req.Limit)
				if sessionErr != nil {
					if _, ok := sessionErr.(LaneError); !ok {
						return nil, sessionErr
					}
				}
				if len(sessionPack.Nodes) > 0 {
					packs = append(packs, sessionPack)
				}
			}
			if err == nil && deps.ContextPacks != nil {
				extraPacks, extraErr := deps.ContextPacks(ctx, cfg.WorkspaceID, query, req.Limit)
				if extraErr != nil {
					if _, ok := extraErr.(LaneError); !ok {
						return nil, extraErr
					}
				}
				packs = appendNonEmptyEvidencePacks(packs, extraPacks...)
			}
		case LaneTask:
			pack, err = RetrieveTask(ctx, cfg, deps.TaskQuery, deps.TaskList, req.TaskID, query)
		default:
			continue
		}
		if err != nil {
			if _, ok := err.(LaneError); !ok {
				return nil, err
			}
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

func gatherRetrievalQuery(query string, requiredEvidence []string) string {
	query = strings.TrimSpace(query)
	required := cleanRequiredEvidence(requiredEvidence)
	if len(required) == 0 {
		return query
	}
	if query == "" {
		return strings.Join(required, " ")
	}
	return "Required evidence: " + strings.Join(required, "; ") + "\n" + query
}

func cleanRequiredEvidence(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func appendNonEmptyEvidencePacks(base []EvidencePack, extra ...EvidencePack) []EvidencePack {
	for _, pack := range extra {
		if len(pack.Nodes) == 0 {
			continue
		}
		base = append(base, pack)
	}
	return base
}

// RetrieveSessionRecall wraps session/chat recall results as context evidence.
func RetrieveSessionRecall(ctx context.Context, cfg LaneConfig, recallFn SessionRecallFunc, query string, limit int) (EvidencePack, error) {
	if err := validateQuery(query, LaneContext); err != nil {
		return EvidencePack{}, err
	}
	start := cfg.Clock()
	hits, err := recallFn(ctx, cfg.WorkspaceID, query, limit)
	elapsed := cfg.Clock().Sub(start)
	packID := cfg.IDGen()
	if err != nil {
		pack := EvidencePack{
			ID:          packID,
			WorkspaceID: cfg.WorkspaceID,
			Query:       query,
			Lane:        LaneContext,
			Telemetry:   EvidenceTelemetry{DurationMs: elapsed.Milliseconds()},
			Metadata:    map[string]any{"error": err.Error(), "source_profile": "session_recall"},
		}
		_ = recordPack(ctx, cfg, pack)
		episodeID, _ := recordEpisode(ctx, cfg, query, LaneContext, packID, elapsed.Milliseconds(), 0, nil)
		pack.Metadata["episode_id"] = episodeID
		return pack, LaneError{Lane: LaneContext, Err: err}
	}
	nodes := make([]EvidenceNode, 0, len(hits))
	for _, hit := range hits {
		if hit.SessionID == "" {
			continue
		}
		statement := hit.Summary
		if statement == "" && len(hit.Decisions) > 0 {
			statement = "session decisions: " + strings.Join(hit.Decisions, "; ")
		}
		if statement == "" {
			statement = "session recall " + hit.SessionID
		}
		metadata := copyMetadata(hit.Metadata)
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["source_profile"] = "session_recall"
		metadata["source"] = hit.Source
		metadata["can_verify"] = hit.CanVerify
		if hit.SpanLocator != "" {
			metadata["span_locator"] = hit.SpanLocator
		}
		if len(hit.Decisions) > 0 {
			metadata["decisions"] = hit.Decisions
		}
		if len(hit.Gotchas) > 0 {
			metadata["gotchas"] = hit.Gotchas
		}
		if len(hit.KeyFiles) > 0 {
			metadata["key_files"] = hit.KeyFiles
		}
		if !hit.StartedAt.IsZero() {
			metadata["started_at"] = hit.StartedAt.UTC().Format(time.RFC3339Nano)
		}
		nodes = append(nodes, EvidenceNode{
			ID:          cfg.IDGen(),
			WorkspaceID: cfg.WorkspaceID,
			NodeType:    EvidenceNodeTypeContext,
			Ref:         EvidenceRef{Type: RefTypeSession, Ref: hit.SessionID, WorkspaceID: cfg.WorkspaceID},
			Statement:   statement,
			Confidence:  hit.Score,
			Grounding:   GroundingIndexed,
			Metadata:    metadata,
		})
	}
	pack := EvidencePack{
		ID:          packID,
		WorkspaceID: cfg.WorkspaceID,
		Query:       query,
		Lane:        LaneContext,
		Nodes:       nodes,
		Telemetry:   EvidenceTelemetry{DurationMs: elapsed.Milliseconds()},
		Metadata:    map[string]any{"source_profile": "session_recall"},
	}
	_ = recordPack(ctx, cfg, pack)
	episodeID, _ := recordEpisode(ctx, cfg, query, LaneContext, packID, elapsed.Milliseconds(), len(nodes), nil)
	pack.Metadata["episode_id"] = episodeID
	return pack, nil
}

func applyCertificateToBundle(bundle *ContextBundle) {
	if bundle == nil || bundle.Certificate == nil {
		return
	}
	if bundle.Certificate.Trust != nil {
		bundle.Trust = bundle.Certificate.Trust
		if bundle.Metadata == nil {
			bundle.Metadata = map[string]any{}
		}
		bundle.Metadata["trust_status"] = bundle.Certificate.Trust.Status
		bundle.Metadata["internal_evidence_ok"] = bundle.Certificate.Trust.InternalEvidenceOK
		bundle.Metadata["answer_context_ok"] = bundle.Certificate.Trust.AnswerContextOK
		bundle.Metadata["graph_recommended"] = bundle.Certificate.Trust.GraphRecommended
		bundle.Metadata["freshness_score"] = bundle.Certificate.Trust.FreshnessScore
		if bundle.Certificate.Trust.CoverageScore > 0 || bundle.CoverageReport != nil {
			bundle.Metadata["coverage_score"] = bundle.Certificate.Trust.CoverageScore
		}
		if bundle.Certificate.Trust.GraphConfidence > 0 {
			bundle.Metadata["graph_confidence"] = bundle.Certificate.Trust.GraphConfidence
		}
		if bundle.Certificate.Trust.GraphRecommended {
			bundle.Missing = appendContextGapUnique(bundle.Missing, ContextGap{
				ID:       "graph-confidence",
				Required: "context graph",
				Reason:   "graph expansion recommended for this task type",
			})
		}
	}
	switch bundle.Certificate.Status {
	case ContextCertificateStatusCertified:
		bundle.Status = ContextBundleStatusSufficient
		bundle.Answerable = true
	case ContextCertificateStatusPartial:
		bundle.Status = ContextBundleStatusPartial
		bundle.Answerable = true
	case ContextCertificateStatusFailed:
		bundle.Status = ContextBundleStatusBlocked
		bundle.Answerable = false
	}
}

func appendContextGapUnique(values []ContextGap, gap ContextGap) []ContextGap {
	if gap.ID == "" && gap.Required == "" {
		return values
	}
	for _, existing := range values {
		if existing.ID == gap.ID && existing.Required == gap.Required {
			return values
		}
	}
	return append(values, gap)
}
