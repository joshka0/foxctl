package contextengine

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestReduceEvidenceToBundleRequiresFactsToReferenceEvidence(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-1",
		WorkspaceID: "ws-1",
		Query:       "auth decision",
		Lane:        LaneMixed,
		Nodes: []EvidenceNode{
			{
				ID:          "node-low",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeMemory,
				Ref:         EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-low"},
				Statement:   "low confidence",
				Confidence:  0.1,
				Grounding:   GroundingValidated,
			},
			{
				ID:          "node-strong",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeMemory,
				Ref:         EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-strong"},
				Statement:   "Use runtime certification for final context.",
				Confidence:  0.9,
				Grounding:   GroundingValidated,
				Metadata:    map[string]any{"status": string(ClaimStatusCurrent)},
			},
		},
	}

	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MinConfidence: 0.5,
		IDGen:         defaultContextIDGen("test"),
		Clock:         fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if len(bundle.Facts) != 1 {
		t.Fatalf("facts=%d want 1", len(bundle.Facts))
	}
	if got := bundle.Facts[0].EvidenceIDs; len(got) != 1 || got[0] != "node-strong" {
		t.Fatalf("evidence ids=%v want node-strong", got)
	}
	if len(bundle.AnswerCandidates) == 0 {
		t.Fatalf("answer candidates missing")
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle Validate: %v", err)
	}

	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	var roundTrip ContextBundle
	if err := json.Unmarshal(body, &roundTrip); err != nil {
		t.Fatalf("unmarshal bundle: %v", err)
	}
	if err := roundTrip.Validate(); err != nil {
		t.Fatalf("round trip Validate: %v", err)
	}
}

func TestReduceEvidenceToBundleBuildsSelectedPathsAndAnswerCandidates(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-1",
		WorkspaceID: "ws-1",
		Query:       "selected paths",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			{
				ID:          "node-path",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypePath, Ref: "./internal/context/contextengine/context_bundle.go"},
				Statement:   "ContextBundle defines selected paths.",
				Confidence:  0.8,
				Grounding:   GroundingLoaded,
			},
			{
				ID:          "node-symbol",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypeSymbol, Ref: "ContextBundle"},
				Statement:   "ContextBundle has answer candidates.",
				Confidence:  0.9,
				Grounding:   GroundingLoaded,
				Metadata:    map[string]any{"path": "internal/context/contextengine/context_bundle.go"},
			},
		},
	}

	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		IDGen: defaultContextIDGen("selected"),
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if len(bundle.SelectedPaths) != 1 {
		t.Fatalf("selected paths=%d want 1: %#v", len(bundle.SelectedPaths), bundle.SelectedPaths)
	}
	selected := bundle.SelectedPaths[0]
	if selected.Path != "internal/context/contextengine/context_bundle.go" {
		t.Fatalf("selected path=%q", selected.Path)
	}
	if selected.Rank != 1 || selected.Confidence != 0.9 {
		t.Fatalf("selected rank/confidence=%d/%f", selected.Rank, selected.Confidence)
	}
	if len(selected.EvidenceIDs) != 2 {
		t.Fatalf("selected evidence ids=%v want two", selected.EvidenceIDs)
	}
	if len(bundle.AnswerCandidates) < 2 {
		t.Fatalf("answer candidates=%d want path plus facts", len(bundle.AnswerCandidates))
	}
	if bundle.AnswerCandidates[0].Kind != "path" || bundle.AnswerCandidates[0].Value != selected.Path {
		t.Fatalf("first answer candidate=%#v want selected path", bundle.AnswerCandidates[0])
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle Validate: %v", err)
	}
}

func TestReduceEvidenceToBundlePathFirstSelectionKeepsRequiredCoverage(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-1",
		WorkspaceID: "ws-1",
		Query:       "load evidence ref contract",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			{
				ID:          "node-noisy-test",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypePath, Ref: "internal/rlm/env/adapter_test.go"},
				Statement:   "Adapter tests mention bounded loading.",
				Confidence:  0.98,
				Grounding:   GroundingLoaded,
			},
			{
				ID:          "node-required-contract",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypePath, Ref: "internal/context/contextengine/refs.go"},
				Statement:   "EvidenceRef and RefType define the load_evidence_ref contract.",
				Confidence:  0.70,
				Grounding:   GroundingIndexed,
			},
		},
	}

	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MaxFacts:         1,
		MaxPaths:         1,
		RequiredEvidence: []string{"load_evidence_ref", "EvidenceRef", "RefType"},
		IDGen:            defaultContextIDGen("pathfirst"),
		Clock:            fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if len(bundle.SelectedPaths) != 1 {
		t.Fatalf("selected paths=%d want 1", len(bundle.SelectedPaths))
	}
	if got := bundle.SelectedPaths[0].Path; got != "internal/context/contextengine/refs.go" {
		t.Fatalf("selected path=%q want refs.go; bundle=%#v", got, bundle.SelectedPaths)
	}
	if len(bundle.Evidence) != 1 || bundle.Evidence[0].ID != "node-required-contract" {
		t.Fatalf("evidence=%#v want required contract node", bundle.Evidence)
	}
}

func TestReduceEvidenceToBundleCoverageSlotsPreserveLowerConfidenceRole(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-coverage",
		WorkspaceID: "ws-1",
		Query:       "map subsystem",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-tool", "internal/rlm/env/tools.go", "RLM tool declaration exposes gather_context.", 0.96),
			coverageNode("node-dispatch", "internal/rlm/env/tool_exec.go", "adapter dispatch executes gather_context.", 0.95),
			coverageNode("node-gather", "internal/context/contextengine/context_gather.go", "contextengine GatherContext retrieves packs.", 0.94),
			coverageNode("node-reduce", "internal/context/contextengine/context_reduce.go", "contextengine reduction builds bundle.", 0.93),
			coverageNode("node-certify", "internal/context/contextengine/context_certify.go", "contextengine certification validates bundle.", 0.62),
			coverageNode("node-lambda", "internal/rlm/lambda_runner.go", "lambda answer_surface uses gather_context.", 0.92),
			coverageNode("node-eval", "cmd/foxctl/cmd/eval_gather_context.go", "eval coverage checks gather_context.", 0.91),
			coverageNode("node-noisy", "internal/rlm/env/adapter_test.go", "tests mention adapter dispatch and gather_context.", 0.99),
		},
	}

	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MaxFacts:         7,
		MaxPaths:         7,
		TaskType:         "subsystem_map",
		RequiredEvidence: []string{"RLM tool declaration", "adapter dispatch", "contextengine GatherContext", "contextengine reduction", "contextengine certification", "lambda answer_surface", "eval coverage"},
		IDGen:            defaultContextIDGen("coverage"),
		Clock:            fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if got := selectedPathStrings(bundle.SelectedPaths); !containsString(got, "internal/context/contextengine/context_certify.go") {
		t.Fatalf("selected paths=%v want certifier despite lower confidence", got)
	}
	if got := selectedPathStrings(bundle.SelectedPaths); containsString(got, "internal/rlm/env/adapter_test.go") {
		t.Fatalf("selected paths=%v should not keep noisy redundant test path", got)
	}
	if bundle.CoverageReport == nil || len(bundle.CoverageReport.Missing) != 0 {
		t.Fatalf("coverage report=%#v", bundle.CoverageReport)
	}
}

func TestCertifyContextBundleFailsMissingRequiredCoverage(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-coverage-cert",
		WorkspaceID: "ws-1",
		Query:       "map subsystem",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-gather", "internal/context/contextengine/context_gather.go", "contextengine GatherContext retrieves packs.", 0.94),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		TaskType: "subsystem_map",
		CoverageRequirements: []CoverageRequirement{
			{ID: "gather", Label: "contextengine GatherContext", Required: true},
			{ID: "certifier", Label: "contextengine certification", Required: true},
		},
		IDGen: defaultContextIDGen("coverage-cert"),
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if bundle.CoverageReport == nil || !containsString(bundle.CoverageReport.Missing, "certifier") {
		t.Fatalf("coverage report=%#v", bundle.CoverageReport)
	}
	cert, err := CertifyContextBundle(bundle, nil, CertificationOptions{
		IDGen: defaultContextIDGen("cert"),
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("CertifyContextBundle: %v", err)
	}
	if cert.Status != ContextCertificateStatusFailed {
		t.Fatalf("cert status=%q want failed; cert=%#v", cert.Status, cert)
	}
	if cert.RequiredEvidenceOK {
		t.Fatalf("RequiredEvidenceOK=true want false")
	}
	if !containsString(cert.MissingEvidence, "coverage:certifier") {
		t.Fatalf("missing evidence=%v want coverage:certifier", cert.MissingEvidence)
	}
}

func TestReduceEvidenceToBundleCoverageAwareFallsBackToScoreWithoutRequirements(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-score",
		WorkspaceID: "ws-1",
		Query:       "simple file locate",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-low", "internal/low.go", "low score implementation", 0.4),
			coverageNode("node-high", "internal/high.go", "high score implementation", 0.95),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MaxFacts: 1,
		MaxPaths: 1,
		IDGen:    defaultContextIDGen("score"),
		Clock:    fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if got := bundle.SelectedPaths[0].Path; got != "internal/high.go" {
		t.Fatalf("selected=%q want high score fallback", got)
	}
}

func TestReduceEvidenceToBundleCoverageAwareDemotesTestsForNonTestTask(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-test-demote",
		WorkspaceID: "ws-1",
		Query:       "certification implementation",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-test", "internal/context/contextengine/context_certify_test.go", "contextengine certification tests", 0.99),
			coverageNode("node-prod", "internal/context/contextengine/context_certify.go", "contextengine certification implementation", 0.75),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MaxFacts:         1,
		MaxPaths:         1,
		TaskType:         "subsystem_map",
		RequiredEvidence: []string{"contextengine certification"},
		IDGen:            defaultContextIDGen("demote"),
		Clock:            fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if got := bundle.SelectedPaths[0].Path; got != "internal/context/contextengine/context_certify.go" {
		t.Fatalf("selected=%q want production file", got)
	}
}

func TestReduceEvidenceToBundleBuildsSubsystemCategories(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-1",
		WorkspaceID: "ws-1",
		Query:       "map gather context subsystem",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			{
				ID:          "node-rlm",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypePath, Ref: "internal/rlm/env/tool_exec.go"},
				Statement:   "RLM adapter calls gather_context.",
				Confidence:  0.9,
				Grounding:   GroundingLoaded,
				Metadata:    map[string]any{"candidate_role": "direct_dispatch_file", "source_profile": "repo_code", "sources": []string{"cochange_history"}},
			},
			{
				ID:          "node-ce",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypePath, Ref: "internal/context/contextengine/context_gather.go"},
				Statement:   "Context engine gathers, reduces, and certifies bundles.",
				Confidence:  0.85,
				Grounding:   GroundingIndexed,
				Metadata:    map[string]any{"candidate_role": "primary_anchor", "source_profile": "repo_code", "sources": []string{"codemaps"}},
			},
		},
	}

	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		TaskType:       "subsystem_map",
		SourceProfiles: []SourceProfile{SourceProfileRepoCode, SourceProfileCodemaps, SourceProfileCochangeHistory},
		IDGen:          defaultContextIDGen("subsystem"),
		Clock:          fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if len(bundle.Categories) != 2 {
		t.Fatalf("categories=%#v want two package groups", bundle.Categories)
	}
	if bundle.Categories[0].Name != "internal/rlm/env" {
		t.Fatalf("first category=%#v", bundle.Categories[0])
	}
	if len(bundle.IntegrationEdges) != 1 {
		t.Fatalf("integration edges=%#v want one", bundle.IntegrationEdges)
	}
	if !containsString(bundle.Categories[0].Signals, "cochange_history") {
		t.Fatalf("signals=%v want cochange_history", bundle.Categories[0].Signals)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle Validate: %v", err)
	}
}

func TestCertifyContextBundleMarksStaleEvidencePartial(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-1",
		WorkspaceID: "ws-1",
		Query:       "auth decision",
		Lane:        LaneMemory,
		Nodes: []EvidenceNode{{
			ID:          "node-1",
			WorkspaceID: "ws-1",
			NodeType:    EvidenceNodeTypeMemory,
			Ref:         EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-1"},
			Statement:   "Use runtime certification.",
			Confidence:  0.9,
			Grounding:   GroundingValidated,
		}},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{IDGen: defaultContextIDGen("test"), Clock: fixedClock})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}

	cert, err := CertifyContextBundle(bundle, []StalenessMarker{{
		ID:          "marker-1",
		WorkspaceID: "ws-1",
		TargetRef:   EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-1"},
		Status:      StalenessStatusStale,
		CreatedAt:   fixedClock(),
		UpdatedAt:   fixedClock(),
	}}, CertificationOptions{IDGen: defaultContextIDGen("cert"), Clock: fixedClock})
	if err != nil {
		t.Fatalf("CertifyContextBundle: %v", err)
	}
	if cert.Status != ContextCertificateStatusPartial {
		t.Fatalf("cert status=%s want partial", cert.Status)
	}
	if len(cert.StaleEvidenceIDs) != 1 || cert.StaleEvidenceIDs[0] != "node-1" {
		t.Fatalf("stale evidence=%v", cert.StaleEvidenceIDs)
	}
}

func TestCertifyContextBundleRejectsInvalidSourceContract(t *testing.T) {
	t.Parallel()

	bundle := ContextBundle{
		ID:          "bundle-1",
		WorkspaceID: "ws-1",
		Query:       "bad code evidence",
		Status:      ContextBundleStatusSufficient,
		Answerable:  true,
		Evidence: []EvidenceNode{{
			ID:          "node-1",
			WorkspaceID: "ws-1",
			NodeType:    EvidenceNodeTypeCode,
			Ref:         EvidenceRef{Type: RefTypeTask, Ref: "task-1"},
			Statement:   "task ref pretending to be code",
			Grounding:   GroundingLoaded,
		}},
		Facts: []ContextFact{{
			ID:          "fact-1",
			WorkspaceID: "ws-1",
			Kind:        EvidenceNodeTypeCode,
			Fact:        "task ref pretending to be code",
			EvidenceIDs: []string{"node-1"},
			Status:      ContextFactStatusSupported,
		}},
	}

	cert, err := CertifyContextBundle(bundle, nil, CertificationOptions{IDGen: defaultContextIDGen("cert"), Clock: fixedClock})
	if err != nil {
		t.Fatalf("CertifyContextBundle: %v", err)
	}
	if cert.Status != ContextCertificateStatusFailed {
		t.Fatalf("cert status=%s want failed", cert.Status)
	}
	if len(cert.UnloadableRefs) != 1 || cert.UnloadableRefs[0].Type != RefTypeTask {
		t.Fatalf("unloadable refs=%v", cert.UnloadableRefs)
	}
	if len(cert.UnsupportedFacts) != 1 || cert.UnsupportedFacts[0] != "fact-1" {
		t.Fatalf("unsupported facts=%v", cert.UnsupportedFacts)
	}
}

func TestCertifyContextBundleWarnsOnNeedsRevalidationMemory(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-1",
		WorkspaceID: "ws-1",
		Query:       "memory",
		Lane:        LaneMemory,
		Nodes: []EvidenceNode{{
			ID:          "node-1",
			WorkspaceID: "ws-1",
			NodeType:    EvidenceNodeTypeMemory,
			Ref:         EvidenceRef{Type: RefTypeMemoryClaim, Ref: "claim-1"},
			Statement:   "Needs revalidation.",
			Confidence:  0.8,
			Grounding:   GroundingValidated,
			Metadata:    map[string]any{"status": string(ClaimStatusNeedsRevalidation)},
		}},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{IDGen: defaultContextIDGen("test"), Clock: fixedClock})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	cert, err := CertifyContextBundle(bundle, nil, CertificationOptions{IDGen: defaultContextIDGen("cert"), Clock: fixedClock})
	if err != nil {
		t.Fatalf("CertifyContextBundle: %v", err)
	}
	if cert.Status != ContextCertificateStatusPartial {
		t.Fatalf("cert status=%s want partial", cert.Status)
	}
	if len(cert.StaleEvidenceIDs) != 1 || cert.StaleEvidenceIDs[0] != "node-1" {
		t.Fatalf("stale evidence=%v", cert.StaleEvidenceIDs)
	}
}

func TestReduceEvidenceToBundleAppliesContextCharBudget(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-1",
		WorkspaceID: "ws-1",
		Query:       "budgeted context",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			{
				ID:          "node-large",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypePath, Ref: "internal/large.go", Excerpt: "0123456789012345678901234567890123456789"},
				Statement:   "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
				Confidence:  0.99,
				Grounding:   GroundingLoaded,
			},
			{
				ID:          "node-small",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypePath, Ref: "internal/small.go"},
				Statement:   "small context that should be omitted by the budget",
				Confidence:  0.8,
				Grounding:   GroundingLoaded,
			},
		},
	}

	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MaxContextChars: 30,
		IDGen:           defaultContextIDGen("budget"),
		Clock:           fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if len(bundle.Evidence) != 1 {
		t.Fatalf("evidence=%d want 1", len(bundle.Evidence))
	}
	if got := bundle.Telemetry.EmittedContextChars; got > 30 {
		t.Fatalf("emitted chars=%d want <= 30", got)
	}
	if bundle.Telemetry.RawContextChars <= bundle.Telemetry.EmittedContextChars {
		t.Fatalf("raw=%d emitted=%d, want compression", bundle.Telemetry.RawContextChars, bundle.Telemetry.EmittedContextChars)
	}
	if bundle.Telemetry.OmittedContextItems != 1 {
		t.Fatalf("omitted=%d want 1", bundle.Telemetry.OmittedContextItems)
	}
	if len(bundle.Facts) != 1 || len(bundle.Facts[0].EvidenceIDs) != 1 || bundle.Facts[0].EvidenceIDs[0] != "node-large" {
		t.Fatalf("facts not tied to retained evidence: %#v", bundle.Facts)
	}
	if truncated, _ := bundle.Evidence[0].Metadata["context_truncated"].(bool); !truncated {
		t.Fatalf("expected retained large evidence to be marked truncated: %#v", bundle.Evidence[0].Metadata)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle Validate: %v", err)
	}
}

func TestGatherContextUsesExistingMixedRetrievalAndCertifiesBundle(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	cfg := LaneConfig{
		Store:       store,
		IDGen:       defaultContextIDGen("gather"),
		Clock:       fixedClock,
		WorkspaceID: "ws-1",
	}
	bundle, err := GatherContext(context.Background(), cfg, GatherContextDeps{
		CodeSearch: func(context.Context, string) ([]CodeSearchHit, error) {
			return []CodeSearchHit{{Path: "internal/auth.go", Snippet: "auth handler", Score: 0.8, Language: "go"}}, nil
		},
		MemoryQuery: func(context.Context, string, string) ([]MemoryClaim, error) {
			return []MemoryClaim{{ID: "claim-1", WorkspaceID: "ws-1", ClaimType: "decision", Status: ClaimStatusCurrent, Summary: "Use certified bundles", Confidence: 0.95}}, nil
		},
		ContextQuery: func(context.Context, string) (*ContextPacket, error) {
			return &ContextPacket{WorkspaceID: "ws-1", Objective: "certify context", Phase: "implementation"}, nil
		},
		TaskQuery: func(context.Context, string, string) (*TaskContext, error) {
			return nil, nil
		},
		TaskList: func(context.Context, string) ([]string, error) {
			return nil, nil
		},
	}, GatherContextRequest{Query: "certified context", Limit: 5})
	if err != nil {
		t.Fatalf("GatherContext: %v", err)
	}
	if !bundle.Answerable {
		t.Fatalf("bundle answerable=false: %#v", bundle.Certificate)
	}
	if bundle.Certificate == nil || bundle.Certificate.Status != ContextCertificateStatusCertified {
		t.Fatalf("certificate=%#v want certified", bundle.Certificate)
	}
	if len(bundle.SourcePackIDs) != 1 {
		t.Fatalf("source packs=%v", bundle.SourcePackIDs)
	}
	if _, err := store.GetEvidencePack(context.Background(), bundle.SourcePackIDs[0]); err != nil {
		t.Fatalf("stored pack: %v", err)
	}
	projectionType, _, _, _, _, _, _, err := store.GetProjection(context.Background(), bundle.WorkspaceID, bundle.ID)
	if err != nil {
		t.Fatalf("stored bundle projection: %v", err)
	}
	if projectionType != "context_bundle" {
		t.Fatalf("projection type=%q want context_bundle", projectionType)
	}
}

func TestGatherContextIncludesSessionRecallEvidence(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	cfg := LaneConfig{
		Store:       store,
		IDGen:       defaultContextIDGen("gather"),
		Clock:       fixedClock,
		WorkspaceID: "ws-1",
	}
	bundle, err := GatherContext(context.Background(), cfg, GatherContextDeps{
		ContextQuery: func(context.Context, string) (*ContextPacket, error) {
			return nil, nil
		},
		SessionRecall: func(_ context.Context, workspaceID, query string, limit int) ([]SessionRecallHit, error) {
			if workspaceID != "ws-1" || query != "prior decision" || limit != 3 {
				t.Fatalf("session recall args workspace=%q query=%q limit=%d", workspaceID, query, limit)
			}
			return []SessionRecallHit{{
				SessionID:   "sess-1",
				Summary:     "We decided gather_context returns certified bundles.",
				Score:       0.8,
				Decisions:   []string{"Use ContextBundle as the answerer surface."},
				KeyFiles:    []string{"internal/context/contextengine/context_gather.go"},
				Source:      "test_session_recall",
				CanVerify:   true,
				SpanLocator: "session:sess-1#chunk=0",
			}}, nil
		},
	}, GatherContextRequest{Query: "prior decision", Limit: 3, Lanes: []EvidenceLane{LaneContext}})
	if err != nil {
		t.Fatalf("GatherContext: %v", err)
	}
	if len(bundle.Evidence) != 1 {
		t.Fatalf("evidence=%d want 1", len(bundle.Evidence))
	}
	node := bundle.Evidence[0]
	if node.Ref.Type != RefTypeSession || node.Ref.Ref != "sess-1" {
		t.Fatalf("ref=%v want session:sess-1", node.Ref)
	}
	if node.Metadata["span_locator"] != "session:sess-1#chunk=0" {
		t.Fatalf("metadata=%v", node.Metadata)
	}
	if bundle.Certificate == nil || bundle.Certificate.Status != ContextCertificateStatusCertified {
		t.Fatalf("certificate=%#v want certified", bundle.Certificate)
	}
}

func fixedClock() time.Time {
	return time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func coverageNode(id, path, statement string, confidence float64) EvidenceNode {
	return EvidenceNode{
		ID:          id,
		WorkspaceID: "ws-1",
		NodeType:    EvidenceNodeTypeCode,
		Ref:         EvidenceRef{Type: RefTypePath, Ref: path},
		Statement:   statement,
		Confidence:  confidence,
		Grounding:   GroundingIndexed,
		Metadata: map[string]any{
			"path":           path,
			"source_profile": "repo_code",
			"coverage_terms": normalizeCoverageTerms([]string{path, statement}),
		},
	}
}

func selectedPathStrings(paths []ContextSelectedPath) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, path.Path)
	}
	return out
}
