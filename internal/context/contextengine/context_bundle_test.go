package contextengine

import (
	"context"
	"encoding/json"
	"reflect"
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

func TestReduceEvidenceToBundleKeepsSameSymbolInDifferentFiles(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-symbol-collision",
		WorkspaceID: "ws-1",
		Query:       "same symbol in multiple files",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			{
				ID:          "node-a",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypeSymbol, Ref: "Handler"},
				Statement:   "Handler in API package.",
				Confidence:  0.9,
				Grounding:   GroundingIndexed,
				Metadata:    map[string]any{"path": "apps/api/handler.go"},
			},
			{
				ID:          "node-b",
				WorkspaceID: "ws-1",
				NodeType:    EvidenceNodeTypeCode,
				Ref:         EvidenceRef{Type: RefTypeSymbol, Ref: "Handler"},
				Statement:   "Handler in web package.",
				Confidence:  0.8,
				Grounding:   GroundingIndexed,
				Metadata:    map[string]any{"path": "apps/web/handler.go"},
			},
		},
	}

	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		IDGen: defaultContextIDGen("symbol"),
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	got := selectedPathStrings(bundle.SelectedPaths)
	if !containsString(got, "apps/api/handler.go") || !containsString(got, "apps/web/handler.go") {
		t.Fatalf("selected paths=%v want both same-named symbol files", got)
	}
	if len(bundle.Evidence) != 2 {
		t.Fatalf("evidence=%d want 2", len(bundle.Evidence))
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

func TestReduceEvidenceToBundleAddsCopyableCoverageFacts(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-coverage-fact",
		WorkspaceID: "ws-1",
		Query:       "find eval baseline files",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-eval", "cmd/foxctl/cmd/eval_gather_context.go", "gather_context eval reporting", 0.94),
			coverageNode("node-agent", "cmd/foxctl/cmd/eval_agents.go", "external subagent baseline comparison", 0.93),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		TaskType:         "subsystem_map",
		RequiredEvidence: []string{"eval_gather_context", "eval_agents", "baseline"},
		IDGen:            defaultContextIDGen("coverage-fact"),
		Clock:            fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	found := false
	for _, fact := range bundle.Facts {
		if fact.Metadata["fact_kind"] == "coverage_requirement" && fact.Metadata["coverage_label"] == "baseline" {
			found = true
			if len(fact.EvidenceIDs) == 0 {
				t.Fatalf("coverage fact missing evidence ids: %#v", fact)
			}
			break
		}
	}
	if !found {
		t.Fatalf("facts=%#v missing baseline coverage fact", bundle.Facts)
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
	if cert.Trust == nil || cert.Trust.Status != "blocked" || cert.Trust.InternalEvidenceOK != true || cert.Trust.RequiredEvidenceOK != false {
		t.Fatalf("trust=%#v want internally valid but blocked by coverage", cert.Trust)
	}
	if gate := trustGateByName(cert.Trust, "coverage"); gate == nil || gate.Status != "fail" || gate.Score != 0.5 {
		t.Fatalf("coverage gate=%#v want fail at 0.5", gate)
	}
}

func TestCertifyContextBundleMarksGraphSensitiveWithoutGraphConfidencePartial(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-graph-needed",
		WorkspaceID: "ws-1",
		Query:       "map subsystem",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-gather", "internal/context/contextengine/context_gather.go", "GatherContext orchestrates context gathering.", 0.94),
			coverageNode("node-reduce", "internal/context/contextengine/context_reduce.go", "ReduceEvidencePacksToBundle builds answer paths.", 0.91),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		TaskType: "subsystem_map",
		IDGen:    defaultContextIDGen("graph-needed"),
		Clock:    fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	cert, err := CertifyContextBundle(bundle, nil, CertificationOptions{IDGen: defaultContextIDGen("cert-graph-needed"), Clock: fixedClock})
	if err != nil {
		t.Fatalf("CertifyContextBundle: %v", err)
	}
	if cert.Status != ContextCertificateStatusPartial {
		t.Fatalf("cert status=%q want partial; cert=%#v", cert.Status, cert)
	}
	if cert.Trust == nil || !cert.Trust.InternalEvidenceOK || !cert.Trust.RequiredEvidenceOK || cert.Trust.AnswerContextOK || !cert.Trust.GraphRecommended {
		t.Fatalf("trust=%#v want internally valid, graph recommended partial", cert.Trust)
	}
	if gate := trustGateByName(cert.Trust, "graph_confidence"); gate == nil || gate.Status != "warn" || !containsString(gate.Missing, "graph_confidence") {
		t.Fatalf("graph gate=%#v want warn missing graph_confidence", gate)
	}

	bundle.Certificate = &cert
	applyCertificateToBundle(&bundle)
	if bundle.Status != ContextBundleStatusPartial || !bundle.Answerable {
		t.Fatalf("bundle status/answerable=%q/%v want partial answerable", bundle.Status, bundle.Answerable)
	}
	if bundle.Trust == nil || !bundle.Trust.GraphRecommended || bundle.Metadata["graph_recommended"] != true {
		t.Fatalf("bundle trust/metadata=%#v/%#v", bundle.Trust, bundle.Metadata)
	}
	if len(bundle.Missing) != 1 || bundle.Missing[0].ID != "graph-confidence" {
		t.Fatalf("missing gaps=%#v want graph-confidence gap", bundle.Missing)
	}
}

func TestCertifyContextBundleAcceptsGraphSensitiveWithGraphConfidence(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-graph-confident",
		WorkspaceID: "ws-1",
		Query:       "trace execution",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-entry", "cmd/foxctl/cmd/rlm.go", "RLM command creates the runtime path.", 0.94),
			coverageNode("node-exec", "internal/rlm/env/tool_exec.go", "tool_exec dispatches gather_context.", 0.91),
		},
		Metadata: map[string]any{"graph_confidence": 0.86},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		TaskType: "execution_trace",
		IDGen:    defaultContextIDGen("graph-confident"),
		Clock:    fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if bundle.Metadata["graph_confidence"] != 0.86 {
		t.Fatalf("bundle metadata graph_confidence=%v", bundle.Metadata["graph_confidence"])
	}
	cert, err := CertifyContextBundle(bundle, nil, CertificationOptions{IDGen: defaultContextIDGen("cert-graph-confident"), Clock: fixedClock})
	if err != nil {
		t.Fatalf("CertifyContextBundle: %v", err)
	}
	if cert.Status != ContextCertificateStatusCertified {
		t.Fatalf("cert status=%q want certified; cert=%#v", cert.Status, cert)
	}
	if cert.Trust == nil || cert.Trust.Status != "trusted" || cert.Trust.GraphRecommended || cert.Trust.GraphConfidence != 0.86 {
		t.Fatalf("trust=%#v want trusted graph confidence", cert.Trust)
	}
	if gate := trustGateByName(cert.Trust, "graph_confidence"); gate == nil || gate.Status != "pass" || gate.Score != 0.86 {
		t.Fatalf("graph gate=%#v want pass 0.86", gate)
	}
}

func TestCertifyContextBundleLeavesFileLocateStableWithoutGraphConfidence(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-file-locate",
		WorkspaceID: "ws-1",
		Query:       "find context bundle file",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-bundle", "internal/context/contextengine/context_bundle.go", "ContextBundle is defined here.", 0.94),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		TaskType: "file_locate",
		IDGen:    defaultContextIDGen("file-locate"),
		Clock:    fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	cert, err := CertifyContextBundle(bundle, nil, CertificationOptions{IDGen: defaultContextIDGen("cert-file-locate"), Clock: fixedClock})
	if err != nil {
		t.Fatalf("CertifyContextBundle: %v", err)
	}
	if cert.Status != ContextCertificateStatusCertified {
		t.Fatalf("cert status=%q want certified; cert=%#v", cert.Status, cert)
	}
	if cert.Trust == nil || !cert.Trust.AnswerContextOK || cert.Trust.GraphRecommended {
		t.Fatalf("trust=%#v want answerable with no graph recommendation", cert.Trust)
	}
	if gate := trustGateByName(cert.Trust, "graph_confidence"); gate != nil {
		t.Fatalf("file locate should not have graph gate: %#v", gate)
	}
}

func TestReduceEvidenceToBundleCoverageRequiresSourceProfileMatch(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-coverage-profile",
		WorkspaceID: "ws-1",
		Query:       "architecture docs",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-code", "internal/context/contextengine/context_gather.go", "architecture docs gather context", 0.92),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		TaskType: "architecture_map",
		CoverageRequirements: []CoverageRequirement{
			{ID: "docs", Label: "architecture docs", Required: true, SourceProfiles: []SourceProfile{SourceProfileRepoDocs}},
		},
		IDGen: defaultContextIDGen("coverage-profile"),
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if bundle.CoverageReport == nil || !containsString(bundle.CoverageReport.Missing, "docs") {
		t.Fatalf("coverage report=%#v want docs missing because source profile is repo_code", bundle.CoverageReport)
	}
}

func TestReduceEvidenceToBundleCoverageRequiresMinPaths(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-coverage-minpaths",
		WorkspaceID: "ws-1",
		Query:       "architecture docs",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNodeWithProfile("node-doc-1", "docs/architecture/one.md", "architecture docs overview", 0.92, SourceProfileRepoDocs),
			coverageNodeWithProfile("node-doc-2", "docs/architecture/two.md", "architecture docs details", 0.88, SourceProfileRepoDocs),
		},
	}
	opts := BundleReductionOptions{
		TaskType: "architecture_map",
		CoverageRequirements: []CoverageRequirement{
			{ID: "docs", Label: "architecture docs", Required: true, MinPaths: 2, SourceProfiles: []SourceProfile{SourceProfileRepoDocs}},
		},
		IDGen: defaultContextIDGen("coverage-minpaths"),
		Clock: fixedClock,
	}
	onePathPack := pack
	onePathPack.Nodes = onePathPack.Nodes[:1]
	onePathBundle, err := ReduceEvidenceToBundle(onePathPack, opts)
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle one path: %v", err)
	}
	if onePathBundle.CoverageReport == nil || !containsString(onePathBundle.CoverageReport.Missing, "docs") {
		t.Fatalf("one-path coverage report=%#v want docs missing", onePathBundle.CoverageReport)
	}
	twoPathBundle, err := ReduceEvidenceToBundle(pack, opts)
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle two paths: %v", err)
	}
	if twoPathBundle.CoverageReport == nil || containsString(twoPathBundle.CoverageReport.Missing, "docs") {
		t.Fatalf("two-path coverage report=%#v want docs covered", twoPathBundle.CoverageReport)
	}
}

func TestReduceEvidenceToBundleCoverageRequiresRepoDocsMinPathsByRole(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-generic-docs-minpaths",
		WorkspaceID: "ws-1",
		Query:       "operator guide",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-code-mentions-guide", "src/runtime/operator.go", "operator guide deployment notes", 0.99),
			coverageNodeWithProfile("node-doc-1", "guides/operator.md", "operator guide deployment notes", 0.90, SourceProfileRepoDocs),
			coverageNodeWithProfile("node-doc-2", "reference/operator.md", "operator guide rollback notes", 0.88, SourceProfileRepoDocs),
		},
	}
	opts := BundleReductionOptions{
		CoverageRequirements: []CoverageRequirement{
			{ID: "operator-guide", Kind: "repo_docs", Label: "operator guide", Required: true, MinPaths: 2, SourceProfiles: []SourceProfile{SourceProfileRepoDocs}},
		},
		IDGen: defaultContextIDGen("generic-docs-minpaths"),
		Clock: fixedClock,
	}
	oneDocPack := pack
	oneDocPack.Nodes = oneDocPack.Nodes[:2]
	oneDocBundle, err := ReduceEvidenceToBundle(oneDocPack, opts)
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle one doc: %v", err)
	}
	if oneDocBundle.CoverageReport == nil || !containsString(oneDocBundle.CoverageReport.Missing, "operator-guide") {
		t.Fatalf("one-doc coverage report=%#v want operator-guide missing despite code mention", oneDocBundle.CoverageReport)
	}
	twoDocBundle, err := ReduceEvidenceToBundle(pack, opts)
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle two docs: %v", err)
	}
	if twoDocBundle.CoverageReport == nil || containsString(twoDocBundle.CoverageReport.Missing, "operator-guide") {
		t.Fatalf("two-doc coverage report=%#v want operator-guide covered", twoDocBundle.CoverageReport)
	}
}

func TestReduceEvidenceToBundleCoverageRequiresDeployConfigAndDataRoles(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-generic-config-data",
		WorkspaceID: "ws-1",
		Query:       "release inputs",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-code-config", "src/release/config.go", "rollout target deployment config", 0.99),
			coverageNode("node-deploy-config", "deploy/production/service.yaml", "rollout target deployment config", 0.86),
			coverageNode("node-code-data", "src/release/fixtures.go", "seed customer fixture data", 0.98),
			coverageNode("node-test-data", "testdata/customers.json", "seed customer fixture data", 0.84),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		CoverageRequirements: []CoverageRequirement{
			{ID: "deploy-config", Kind: "deploy_config", Label: "rollout target deployment config", Required: true},
			{ID: "seed-data", Kind: "test_data", Label: "seed customer fixture data", Required: true},
		},
		IDGen: defaultContextIDGen("generic-config-data"),
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if bundle.CoverageReport == nil || len(bundle.CoverageReport.Missing) != 0 {
		t.Fatalf("coverage report=%#v want deploy_config and test_data covered", bundle.CoverageReport)
	}
	covered := map[string]string{}
	for _, item := range bundle.CoverageReport.Covered {
		covered[item.RequirementID] = item.Path
	}
	if covered["deploy-config"] != "deploy/production/service.yaml" {
		t.Fatalf("deploy-config covered by %q want deploy yaml; report=%#v", covered["deploy-config"], bundle.CoverageReport)
	}
	if covered["seed-data"] != "testdata/customers.json" {
		t.Fatalf("seed-data covered by %q want testdata json; report=%#v", covered["seed-data"], bundle.CoverageReport)
	}
}

func TestReduceEvidenceToBundleAdmitsDataButKeepsDocsAndTestsSupportingOnly(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-answer-admission",
		WorkspaceID: "ws-1",
		Query:       "physics protocol scenarios",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-code", "server/src/physics.rs", "server physics protocol", 0.95),
			coverageNodeWithProfile("node-doc-protocol", "docs/physics_protocol.md", "physics protocol documentation", 0.91, SourceProfileRepoDocs),
			coverageNodeWithProfile("node-doc-notes", "docs/notes/design_decisions.md", "physics notes and design decisions", 0.90, SourceProfileRepoDocs),
			coverageNodeWithPathMetadata("node-data", "tests_data/physics_scenarios.json", "physics scenarios fixture data", 0.89, map[string]any{
				"candidate_role": "test_data_support",
				"file_role":      "test_data",
				"is_test":        true,
			}),
			coverageNodeWithPathMetadata("node-test", "server/src/tests.rs", "physics tests", 0.88, map[string]any{
				"candidate_role": "test_support",
				"file_role":      "test",
				"is_test":        true,
			}),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MaxFacts:         8,
		MaxPaths:         8,
		TaskType:         "subsystem_map",
		SourceProfiles:   []SourceProfile{SourceProfileRepoCode, SourceProfileRepoDocs},
		RequiredEvidence: []string{"physics protocol", "physics scenarios"},
		IDGen:            defaultContextIDGen("answer-admission"),
		Clock:            fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	got := selectedPathStrings(bundle.SelectedPaths)
	for _, want := range []string{"server/src/physics.rs", "docs/physics_protocol.md", "tests_data/physics_scenarios.json"} {
		if !containsString(got, want) {
			t.Fatalf("selected paths=%v missing %s", got, want)
		}
	}
	for _, unwanted := range []string{"docs/notes/design_decisions.md", "server/src/tests.rs"} {
		if containsString(got, unwanted) {
			t.Fatalf("selected paths=%v should keep %s supporting-only", got, unwanted)
		}
	}
}

func TestCertifyContextBundleFailsCoverageMinPaths(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-coverage-cert-minpaths",
		WorkspaceID: "ws-1",
		Query:       "architecture docs",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNodeWithProfile("node-doc-1", "docs/architecture/one.md", "architecture docs overview", 0.92, SourceProfileRepoDocs),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		TaskType: "architecture_map",
		CoverageRequirements: []CoverageRequirement{
			{ID: "docs", Label: "architecture docs", Required: true, MinPaths: 2, SourceProfiles: []SourceProfile{SourceProfileRepoDocs}},
		},
		IDGen: defaultContextIDGen("coverage-cert-minpaths"),
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	cert, err := CertifyContextBundle(bundle, nil, CertificationOptions{
		IDGen: defaultContextIDGen("cert-minpaths"),
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("CertifyContextBundle: %v", err)
	}
	if cert.Status != ContextCertificateStatusFailed {
		t.Fatalf("cert status=%q want failed; cert=%#v", cert.Status, cert)
	}
	if !containsString(cert.MissingEvidence, "coverage:docs") {
		t.Fatalf("missing evidence=%v want coverage:docs", cert.MissingEvidence)
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

func TestReduceEvidenceToBundlePrefersProductionCounterpartOverTest(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-counterpart",
		WorkspaceID: "ws-1",
		Query:       "map context bundle subsystem",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-test", "internal/context/contextengine/context_bundle_test.go", "context bundle tests", 0.99),
			coverageNode("node-prod", "internal/context/contextengine/context_bundle.go", "context bundle implementation", 0.75),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MaxFacts:         1,
		MaxPaths:         1,
		TaskType:         "subsystem_map",
		RequiredEvidence: []string{"context_bundle"},
		IDGen:            defaultContextIDGen("counterpart"),
		Clock:            fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	if got := bundle.SelectedPaths[0].Path; got != "internal/context/contextengine/context_bundle.go" {
		t.Fatalf("selected=%q want production counterpart", got)
	}
}

func TestReduceEvidenceToBundlePrefersCoherentComponentRootForSubsystemTasks(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-component-coherence",
		WorkspaceID: "ws-1",
		Query:       "service routing config domain",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNodeWithPathMetadata("node-service-b-config", "apps/service-b/config/settings.ts", "service config settings", 0.99, map[string]any{
				"component_root": "apps/service-b",
				"path_family":    "apps/service-b/config",
				"file_role":      "config",
			}),
			coverageNodeWithPathMetadata("node-service-a-router", "apps/service-a/router/routes.ts", "service router routes", 0.96, map[string]any{
				"component_root": "apps/service-a",
				"path_family":    "apps/service-a/router",
				"file_role":      "router",
			}),
			coverageNodeWithPathMetadata("node-service-a-domain", "apps/service-a/domain/model.ts", "service domain model", 0.95, map[string]any{
				"component_root": "apps/service-a",
				"path_family":    "apps/service-a/domain",
				"file_role":      "domain",
			}),
			coverageNodeWithPathMetadata("node-service-a-config", "apps/service-a/config/settings.ts", "service config settings", 0.86, map[string]any{
				"component_root": "apps/service-a",
				"path_family":    "apps/service-a/config",
				"file_role":      "config",
			}),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MaxFacts: 3,
		MaxPaths: 3,
		TaskType: "subsystem_map",
		CoverageRequirements: []CoverageRequirement{
			{ID: "config", Kind: "config", Label: "config", Terms: []string{"config"}, Required: true},
			{ID: "router", Kind: "router", Label: "router", Terms: []string{"router"}, Required: true},
			{ID: "domain", Kind: "domain", Label: "domain", Terms: []string{"domain"}, Required: true},
		},
		IDGen: defaultContextIDGen("component-coherence"),
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	got := selectedPathStrings(bundle.SelectedPaths)
	want := []string{
		"apps/service-a/router/routes.ts",
		"apps/service-a/domain/model.ts",
		"apps/service-a/config/settings.ts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected=%v want coherent service-a paths", got)
	}
	if selectedPathByPath(bundle.SelectedPaths, "apps/service-b/config/settings.ts") != nil {
		t.Fatalf("selected=%v should demote sibling service config", got)
	}
}

func TestReduceEvidenceToBundleKeepsCrossComponentPathForUniqueCoverage(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-component-unique-coverage",
		WorkspaceID: "ws-1",
		Query:       "service routing config domain",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNodeWithPathMetadata("node-service-a-router", "apps/service-a/router/routes.ts", "service router routes", 0.96, map[string]any{
				"component_root": "apps/service-a",
				"path_family":    "apps/service-a/router",
				"file_role":      "router",
			}),
			coverageNodeWithPathMetadata("node-service-a-config", "apps/service-a/config/settings.ts", "service config settings", 0.94, map[string]any{
				"component_root": "apps/service-a",
				"path_family":    "apps/service-a/config",
				"file_role":      "config",
			}),
			coverageNodeWithPathMetadata("node-service-b-domain", "apps/service-b/domain/model.ts", "service domain model", 0.90, map[string]any{
				"component_root": "apps/service-b",
				"path_family":    "apps/service-b/domain",
				"file_role":      "domain",
			}),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MaxFacts: 3,
		MaxPaths: 3,
		TaskType: "subsystem_map",
		CoverageRequirements: []CoverageRequirement{
			{ID: "config", Kind: "config", Label: "config", Terms: []string{"config"}, Required: true},
			{ID: "router", Kind: "router", Label: "router", Terms: []string{"router"}, Required: true},
			{ID: "domain", Kind: "domain", Label: "domain", Terms: []string{"domain"}, Required: true},
		},
		IDGen: defaultContextIDGen("component-unique"),
		Clock: fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	got := selectedPathStrings(bundle.SelectedPaths)
	if !containsString(got, "apps/service-b/domain/model.ts") {
		t.Fatalf("selected=%v want cross-component domain because it is the only domain coverage candidate", got)
	}
	if bundle.CoverageReport == nil || len(bundle.CoverageReport.Missing) != 0 {
		t.Fatalf("coverage report=%#v want all required roles covered", bundle.CoverageReport)
	}
}

func TestReduceEvidenceToBundleDemotesPeripheralFileRolesForSubsystemTasks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "hidden", path: ".github/workflows/ci.yml"},
		{name: "tooling", path: "scripts/evals/gather_context.sh"},
		{name: "template", path: "internal/context/contextengine/templates/context.tmpl"},
		{name: "generated", path: "internal/context/contextengine/context.pb.go"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pack := EvidencePack{
				ID:          "pack-" + tt.name,
				WorkspaceID: "ws-1",
				Query:       "contextengine reduction implementation",
				Lane:        LaneCode,
				Nodes: []EvidenceNode{
					coverageNode("node-peripheral", tt.path, "contextengine reduction mentioned by peripheral file", 0.99),
					coverageNode("node-prod", "internal/context/contextengine/context_reduce.go", "contextengine reduction implementation", 0.82),
				},
			}
			bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
				MaxFacts:         1,
				MaxPaths:         1,
				TaskType:         "subsystem_map",
				RequiredEvidence: []string{"contextengine reduction"},
				IDGen:            defaultContextIDGen("role-" + tt.name),
				Clock:            fixedClock,
			})
			if err != nil {
				t.Fatalf("ReduceEvidenceToBundle: %v", err)
			}
			if got := bundle.SelectedPaths[0].Path; got != "internal/context/contextengine/context_reduce.go" {
				t.Fatalf("selected=%q want implementation file over %s", got, tt.path)
			}
		})
	}
}

func TestReduceEvidenceToBundleAttachesGenericPathMetadata(t *testing.T) {
	t.Parallel()

	pack := EvidencePack{
		ID:          "pack-path-metadata",
		WorkspaceID: "ws-1",
		Query:       "context reduction metadata",
		Lane:        LaneCode,
		Nodes: []EvidenceNode{
			coverageNode("node-prod", "internal/context/contextengine/context_reduce.go", "contextengine reduction implementation", 0.92),
			coverageNode("node-tool", "scripts/evals/gather_context.sh", "gather context tooling wrapper", 0.91),
		},
	}
	bundle, err := ReduceEvidenceToBundle(pack, BundleReductionOptions{
		MaxFacts: 2,
		MaxPaths: 2,
		TaskType: "subsystem_map",
		IDGen:    defaultContextIDGen("path-metadata"),
		Clock:    fixedClock,
	})
	if err != nil {
		t.Fatalf("ReduceEvidenceToBundle: %v", err)
	}
	prod := selectedPathByPath(bundle.SelectedPaths, "internal/context/contextengine/context_reduce.go")
	if prod == nil {
		t.Fatalf("selected paths=%v missing context_reduce.go", selectedPathStrings(bundle.SelectedPaths))
	}
	if prod.Metadata["file_kind"] != "go" || prod.Metadata["file_role"] != "implementation" {
		t.Fatalf("prod metadata=%#v want go implementation", prod.Metadata)
	}
	if prod.Metadata["component_root"] != "internal/context/contextengine" || prod.Metadata["path_family"] != "internal/context/contextengine" {
		t.Fatalf("prod metadata=%#v want component/path family", prod.Metadata)
	}
	if tool := selectedPathByPath(bundle.SelectedPaths, "scripts/evals/gather_context.sh"); tool != nil {
		t.Fatalf("selected paths=%v should keep tooling out of subsystem answer paths", selectedPathStrings(bundle.SelectedPaths))
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
	return coverageNodeWithProfile(id, path, statement, confidence, SourceProfileRepoCode)
}

func coverageNodeWithProfile(id, path, statement string, confidence float64, profile SourceProfile) EvidenceNode {
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
			"source_profile": string(profile),
			"coverage_terms": normalizeCoverageTerms([]string{path, statement}),
		},
	}
}

func coverageNodeWithPathMetadata(id, path, statement string, confidence float64, metadata map[string]any) EvidenceNode {
	node := coverageNode(id, path, statement, confidence)
	for key, value := range metadata {
		node.Metadata[key] = value
	}
	return node
}

func selectedPathStrings(paths []ContextSelectedPath) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, path.Path)
	}
	return out
}

func selectedPathByPath(paths []ContextSelectedPath, want string) *ContextSelectedPath {
	for i := range paths {
		if paths[i].Path == want {
			return &paths[i]
		}
	}
	return nil
}

func trustGateByName(report *ContextTrustReport, name string) *ContextTrustGate {
	if report == nil {
		return nil
	}
	for i := range report.Gates {
		if report.Gates[i].Name == name {
			return &report.Gates[i]
		}
	}
	return nil
}
