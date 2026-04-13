package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
)

func pathsFromCandidates(candidates []*codeSearchCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		out = append(out, candidate.Path)
	}
	return out
}

func TestCodeSearchExactProbes(t *testing.T) {
	t.Parallel()

	got := codeSearchExactProbes("Where does memory_ensemble_retrieve live?")
	if len(got) == 0 || got[0] != "memory_ensemble_retrieve" {
		t.Fatalf("probes=%v", got)
	}
}

func TestCodeSearchExactProbesIncludesPascalCaseIdentifiers(t *testing.T) {
	t.Parallel()

	got := codeSearchExactProbes("Which file defines Workflow?")
	found := false
	for _, probe := range got {
		if probe == "Workflow" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("probes=%v", got)
	}
}

func TestCodeSearchTaskExactProbesDerivePhraseIdentifiersForExecutionTrace(t *testing.T) {
	t.Parallel()

	got := codeSearchTaskExactProbes("Which files connect session restore to code/semantic_search execution?", codeSearchTaskExecutionTrace)
	found := false
	for _, probe := range got {
		if probe == "session_restore" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("probes=%v", got)
	}
}

func TestDeriveExecutionTraceAnchors(t *testing.T) {
	t.Parallel()

	anchors := deriveExecutionTraceAnchors("Which files connect session restore to code/semantic_search execution?", codeSearchTaskExecutionTrace)
	if anchors.SourceQuery != "session restore" {
		t.Fatalf("source=%q", anchors.SourceQuery)
	}
	if anchors.TargetQuery != "code/semantic_search" {
		t.Fatalf("target=%q", anchors.TargetQuery)
	}
	foundSource := false
	for _, probe := range anchors.SourceExactProbes {
		if probe == "session_restore" {
			foundSource = true
			break
		}
	}
	if !foundSource {
		t.Fatalf("source exact probes=%v", anchors.SourceExactProbes)
	}
}

func TestCodeSearchRepoProbes(t *testing.T) {
	t.Parallel()

	got := codeSearchRepoProbes("Where does memory_ensemble_retrieve live?")
	if len(got) == 0 {
		t.Fatal("expected non-empty probes")
	}
	foundIdentifier := false
	foundExpanded := false
	for _, probe := range got {
		if probe == "memory_ensemble_retrieve" {
			foundIdentifier = true
		}
		if probe == "memory ensemble retrieve" {
			foundExpanded = true
		}
	}
	if !foundIdentifier || !foundExpanded {
		t.Fatalf("probes=%v", got)
	}
}

func TestCodeSearchRepoProbesAvoidStrictPrefixOfExactProbe(t *testing.T) {
	t.Parallel()

	got := codeSearchRepoProbes("Where are the code/smart_search skill implementation and manifest declared?")
	for _, probe := range got {
		if probe == "code smart" {
			t.Fatalf("unexpected strict prefix probe in %v", got)
		}
	}
}

func TestCodeSearchPathTermScore(t *testing.T) {
	t.Parallel()

	terms := codeSearchPathTerms("Where does memory_ensemble_retrieve live?")
	gotMemoryEnsemble := codeSearchPathTermScore("internal/rlm/env/memory_ensemble.go", terms)
	gotAgentMemory := codeSearchPathTermScore("cmd/agentctl/cmd/agent_memory.go", terms)
	if gotMemoryEnsemble <= gotAgentMemory {
		t.Fatalf("memory_ensemble score=%v agent_memory score=%v", gotMemoryEnsemble, gotAgentMemory)
	}
}

func TestNormalizeRepoSearchProbeRewritesHyphens(t *testing.T) {
	t.Parallel()

	got := normalizeRepoSearchProbe("Where is the eval code-search-ensemble command implemented?")
	if strings.Contains(got, "-") {
		t.Fatalf("probe=%q", got)
	}
	if !strings.Contains(got, "code search ensemble") {
		t.Fatalf("probe=%q", got)
	}
}

func TestCodeSearchPathProbes(t *testing.T) {
	t.Parallel()

	got := codeSearchPathProbes("Where is the eval code-search-ensemble command implemented?")
	found := false
	for _, probe := range got {
		if probe == "code_search_ensemble" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("probes=%v", got)
	}
}

func TestCodeSearchPathProbesDeriveModulePathForms(t *testing.T) {
	t.Parallel()

	got := codeSearchPathProbes("Which files define Jido.AgentServer and Jido.Agent.Directive?")
	foundAgentServer := false
	foundAgentDirective := false
	for _, probe := range got {
		if probe == "agent_server" {
			foundAgentServer = true
		}
		if probe == "agent/directive" {
			foundAgentDirective = true
		}
	}
	if !foundAgentServer || !foundAgentDirective {
		t.Fatalf("probes=%v", got)
	}
}

func TestCodeSearchPathProbesAvoidBroadPrefixForNamespacedSkills(t *testing.T) {
	t.Parallel()

	got := codeSearchPathProbes("Where are the code/smart_search skill implementation and manifest declared?")
	for _, probe := range got {
		if probe == "code_smart" {
			t.Fatalf("unexpected broad prefix probe in %v", got)
		}
	}
}

func TestApplyACAGuidanceCandidatesAddsRepoPaths(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "infra", "k8s", "waitlist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "infra", "k8s", "waitlist", "ingress.yaml"), []byte("kind: Ingress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidates := map[string]*codeSearchCandidate{}
	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/praze/concepts/ingressprazewaitlist-api.md",
			RepoPaths: []string{"infra/k8s/waitlist/ingress.yaml"},
			Symbols:   []string{"IngressWaitlistAPI"},
		},
	}
	applied := applyACAGuidanceCandidates(workspace, "praze waitlist ingress", candidates, hits, codeSearchTaskFileLocate, nil)
	if applied != 1 {
		t.Fatalf("applied=%d", applied)
	}
	item := candidates["infra/k8s/waitlist/ingress.yaml"]
	if item == nil {
		t.Fatalf("missing ACA-seeded candidate: %#v", candidates)
	}
	if !candidateHasSource(item, "aca_guidance") {
		t.Fatalf("sources=%v", item.Sources)
	}
	if item.Support <= 0.8 {
		t.Fatalf("support=%v", item.Support)
	}
	if len(item.Symbols) == 0 || item.Symbols[0] != "IngressWaitlistAPI" {
		t.Fatalf("symbols=%v", item.Symbols)
	}
}

func TestApplyACAGuidanceCandidatesSkipsExcludedPaths(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "infra", "k8s", "waitlist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "infra", "k8s", "waitlist", "ingress.yaml"), []byte("kind: Ingress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidates := map[string]*codeSearchCandidate{}
	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/praze/concepts/ingressprazewaitlist-api.md",
			RepoPaths: []string{"infra/k8s/waitlist/ingress.yaml"},
		},
	}
	applied := applyACAGuidanceCandidates(workspace, "praze waitlist ingress", candidates, hits, codeSearchTaskFileLocate, []string{"infra/k8s/waitlist/ingress.yaml"})
	if applied != 0 {
		t.Fatalf("applied=%d", applied)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates=%v", candidates)
	}
}

func TestApplyACAGuidanceCandidatesSkipsRepoPathsOutsideWorkspace(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{}
	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/praze/concepts/ingressprazewaitlist-api.md",
			RepoPaths: []string{"infra/k8s/waitlist/ingress.yaml"},
		},
	}
	applied := applyACAGuidanceCandidates(t.TempDir(), "praze waitlist ingress", candidates, hits, codeSearchTaskFileLocate, nil)
	if applied != 0 {
		t.Fatalf("applied=%d", applied)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates=%v", candidates)
	}
}

func TestApplyACAGuidanceCandidatesSkipsLowOverlapHits(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "platform", "93-maple"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "platform", "93-maple", "maple-local.yaml"), []byte("kind: Ingress\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidates := map[string]*codeSearchCandidate{}
	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/praze/concepts/father-in-shapps-praze-api-scripts.md",
			RepoPaths: []string{"platform/93-maple/maple-local.yaml"},
			Symbols:   []string{"Father"},
			Title:     "Father in scripts",
		},
	}
	applied := applyACAGuidanceCandidates(workspace, "argocd application praze auth", candidates, hits, codeSearchTaskFileLocate, nil)
	if applied != 0 {
		t.Fatalf("applied=%d", applied)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates=%v", candidates)
	}
}

func TestApplyACAGuidanceSupportBoostsExactInfraCandidate(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"infra/k8s/waitlist/ingress.yaml": {
			Path:    "infra/k8s/waitlist/ingress.yaml",
			Sources: map[string]struct{}{"path_probe": {}},
			Support: 1.0,
		},
		"platform/10-argocd/apps/65-praze-auth.yaml": {
			Path:    "platform/10-argocd/apps/65-praze-auth.yaml",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.8,
		},
	}
	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/praze/concepts/ingressprazewaitlist-api.md",
			RepoPaths: []string{"infra/k8s/waitlist/ingress.yaml"},
			Symbols:   []string{"Ingress/praze/waitlist-api"},
			Title:     "Ingress waitlist api",
		},
	}

	applied := applyACAGuidanceSupport("praze waitlist ingress", candidates, hits, codeSearchRouteInfraResource, codeSearchTaskFileLocate, nil)
	if applied != 1 {
		t.Fatalf("applied=%d", applied)
	}
	candidate := candidates["infra/k8s/waitlist/ingress.yaml"]
	if candidate == nil {
		t.Fatal("missing candidate")
	}
	if !candidateHasSource(candidate, "aca_route_infra_exact") {
		t.Fatalf("sources=%v", candidate.Sources)
	}
	if candidate.Support <= 2.0 {
		t.Fatalf("support=%v", candidate.Support)
	}
	if len(candidates["platform/10-argocd/apps/65-praze-auth.yaml"].Sources) != 1 {
		t.Fatalf("unexpected support on unrelated candidate: %#v", candidates["platform/10-argocd/apps/65-praze-auth.yaml"])
	}
}

func TestApplyACAGuidanceSupportInfraPrefersResourceRolePath(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"platform/10-argocd/apps/65-praze-auth.yaml": {
			Path:    "platform/10-argocd/apps/65-praze-auth.yaml",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.8,
		},
		"platform/10-argocd/apps/10-shared.yaml": {
			Path:    "platform/10-argocd/apps/10-shared.yaml",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.8,
		},
	}
	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/praze/concepts/applicationargocdpraze-auth-in-k8splatform-10-argocd-apps.md",
			RepoPaths: []string{"platform/10-argocd/apps/10-shared.yaml", "platform/10-argocd/apps/65-praze-auth.yaml"},
			AnchorRoles: map[string][]string{
				"resource": {"platform/10-argocd/apps/65-praze-auth.yaml"},
			},
			Symbols: []string{"Application/argocd/praze-auth"},
			Title:   "praze auth argocd application",
		},
	}

	applied := applyACAGuidanceSupport("argocd application praze auth", candidates, hits, codeSearchRouteInfraResource, codeSearchTaskFileLocate, nil)
	if applied != 1 {
		t.Fatalf("applied=%d", applied)
	}
	if !candidateHasSource(candidates["platform/10-argocd/apps/65-praze-auth.yaml"], "aca_route_infra_exact") {
		t.Fatalf("resource candidate sources=%v", candidates["platform/10-argocd/apps/65-praze-auth.yaml"].Sources)
	}
	if candidateHasSource(candidates["platform/10-argocd/apps/10-shared.yaml"], "aca_route_infra_exact") {
		t.Fatalf("shared candidate sources=%v", candidates["platform/10-argocd/apps/10-shared.yaml"].Sources)
	}
}

func TestPreferredInfraAnchorPathsUsesResourceRoleFirst(t *testing.T) {
	t.Parallel()

	got := preferredInfraAnchorPaths(contextplane.RetrievalHit{
		PrimaryAnchorPath: "platform/10-argocd/apps/65-praze-auth.yaml",
		RepoPaths: []string{
			"platform/10-argocd/apps/10-shared.yaml",
			"platform/10-argocd/apps/65-praze-auth.yaml",
		},
		AnchorRoles: map[string][]string{
			"resource": {"platform/10-argocd/apps/65-praze-auth.yaml"},
		},
		AnchorPaths: []string{"platform/10-argocd/apps/10-shared.yaml"},
	})
	if len(got) == 0 || got[0] != "platform/10-argocd/apps/65-praze-auth.yaml" {
		t.Fatalf("got=%v", got)
	}
}

func TestApplyACAGuidanceSupportDoesNotIntroduceNewCandidates(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"internal/rlm/env/code_search_ensemble.go": {
			Path:    "internal/rlm/env/code_search_ensemble.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
		},
	}
	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/agentctl/packages/internal-rlm-env.md",
			RepoPaths: []string{"internal/contextplane/retrieval.go"},
			Symbols:   []string{"WorkspaceStore"},
			Title:     "internal rlm env",
		},
	}

	applied := applyACAGuidanceSupport("workspace retrieval", candidates, hits, codeSearchRoutePackageOwner, codeSearchTaskFileLocate, nil)
	if applied != 0 {
		t.Fatalf("applied=%d", applied)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates=%v", candidates)
	}
}

func TestApplyACAGuidanceSupportBoostsPackageSymbolOverlap(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"internal/contextplane/retrieval.go": {
			Path:    "internal/contextplane/retrieval.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
			Symbols: []string{"WorkspaceStore"},
		},
	}
	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/agentctl/packages/internal-contextplane.md",
			RepoPaths: []string{"internal/contextplane/types.go", "internal/contextplane/retrieval.go"},
			Symbols:   []string{"WorkspaceStore"},
			Title:     "internal contextplane",
		},
	}

	applied := applyACAGuidanceSupport("workspace store retrieval", candidates, hits, codeSearchRoutePackageOwner, codeSearchTaskFileLocate, nil)
	if applied < 2 {
		t.Fatalf("applied=%d", applied)
	}
	candidate := candidates["internal/contextplane/retrieval.go"]
	if !candidateHasSource(candidate, "aca_route_package_exact") {
		t.Fatalf("sources=%v", candidate.Sources)
	}
	if !candidateHasSource(candidate, "aca_route_package_symbol") {
		t.Fatalf("sources=%v", candidate.Sources)
	}
}

func TestApplyACAGuidanceSupportAddsPackageFamilyRepoPaths(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"internal/adapters/skillslib/skillmain/context.go": {
			Path:    "internal/adapters/skillslib/skillmain/context.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
		},
		"internal/adapters/skillslib/skillmain/stores.go": {
			Path:    "internal/adapters/skillslib/skillmain/stores.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.8,
		},
		"internal/adapters/skillslib/skillmain/providers.go": {
			Path:    "internal/adapters/skillslib/skillmain/providers.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.8,
		},
		"internal/adapters/skillslib/skillmain/main.go": {
			Path:    "internal/adapters/skillslib/skillmain/main.go",
			Sources: map[string]struct{}{"path_probe": {}},
			Support: 0.6,
		},
	}
	hits := []contextplane.RetrievalHit{
		{
			Path: "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md",
			RepoPaths: []string{
				"internal/adapters/skillslib/skillmain/context.go",
				"internal/adapters/skillslib/skillmain/main.go",
				"internal/adapters/skillslib/skillmain/stores.go",
			},
			Symbols: []string{"RunContext"},
			Title:   "internal adapters skillslib skillmain",
		},
	}

	applied := applyACAGuidanceSupport("skills main runtime wiring", candidates, hits, codeSearchRoutePackageOwner, codeSearchTaskFileLocate, nil)
	if applied < 2 {
		t.Fatalf("applied=%d", applied)
	}
	mainCandidate := candidates["internal/adapters/skillslib/skillmain/main.go"]
	if mainCandidate == nil || !candidateHasSource(mainCandidate, "aca_route_package_anchor") {
		t.Fatalf("sources=%v", mainCandidate.Sources)
	}
}

func TestApplyACAGuidanceSupportAddsSkillmainAnchorAgainstCompetingCandidates(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"skills/code_semantic_search/main.go": {
			Path:    "skills/code_semantic_search/main.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
			Symbols: []string{"CandidateBundle"},
		},
		"internal/intelligence/searchindex/model.go": {
			Path:    "internal/intelligence/searchindex/model.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.7,
		},
		"internal/domain/agent/agent.go": {
			Path:    "internal/domain/agent/agent.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.3,
		},
	}
	hits := []contextplane.RetrievalHit{
		{
			Path:              "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md",
			PrimaryAnchorPath: "internal/adapters/skillslib/skillmain/main.go",
			RepoPaths: []string{
				"internal/adapters/skillslib/skillmain/context.go",
				"internal/adapters/skillslib/skillmain/main.go",
				"internal/adapters/skillslib/skillmain/stores.go",
			},
			Symbols: []string{"RunContext"},
			Title:   "internal adapters skillslib skillmain",
			Score:   65,
		},
	}

	applied := applyACAGuidanceSupport("Which file anchors the skills main runtime wiring package?", candidates, hits, codeSearchRoutePackageOwner, codeSearchTaskFileLocate, nil)
	if applied < 1 {
		t.Fatalf("applied=%d", applied)
	}
	mainCandidate := candidates["internal/adapters/skillslib/skillmain/main.go"]
	if mainCandidate == nil || !candidateHasSource(mainCandidate, "aca_route_package_anchor") {
		t.Fatalf("main candidate=%#v", mainCandidate)
	}
}

func TestPackageGuidanceSpecificityScorePrefersPrimaryAnchorPath(t *testing.T) {
	t.Parallel()

	hit := contextplane.RetrievalHit{
		Path:              "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md",
		Title:             "internal adapters skillslib skillmain",
		PrimaryAnchorPath: "internal/adapters/skillslib/skillmain/main.go",
		AnchorPaths: []string{
			"internal/adapters/skillslib/skillmain/breakers.go",
			"internal/adapters/skillslib/skillmain/main.go",
		},
	}
	scoreWithPrimary := packageACAGuidanceSpecificityScore("Which file anchors the skills main runtime wiring package?", hit)
	hit.PrimaryAnchorPath = ""
	scoreWithoutPrimary := packageACAGuidanceSpecificityScore("Which file anchors the skills main runtime wiring package?", hit)
	if scoreWithPrimary <= scoreWithoutPrimary {
		t.Fatalf("scoreWithPrimary=%v scoreWithoutPrimary=%v", scoreWithPrimary, scoreWithoutPrimary)
	}
}

func TestApplyACAGuidanceSupportPrefersPrimaryPackageAnchor(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"internal/adapters/skillslib/skillmain/breakers.go": {
			Path:    "internal/adapters/skillslib/skillmain/breakers.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.5,
		},
		"internal/adapters/skillslib/skillmain/main.go": {
			Path:    "internal/adapters/skillslib/skillmain/main.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.5,
		},
	}
	hits := []contextplane.RetrievalHit{
		{
			Path:              "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md",
			Title:             "internal adapters skillslib skillmain",
			PrimaryAnchorPath: "internal/adapters/skillslib/skillmain/main.go",
			AnchorPaths: []string{
				"internal/adapters/skillslib/skillmain/breakers.go",
				"internal/adapters/skillslib/skillmain/main.go",
			},
			Symbols: []string{"RunContext"},
		},
	}

	applyACAGuidanceSupport("Which file anchors the skills main runtime wiring package?", candidates, hits, codeSearchRoutePackageOwner, codeSearchTaskFileLocate, nil)
	if candidates["internal/adapters/skillslib/skillmain/main.go"].Support <= candidates["internal/adapters/skillslib/skillmain/breakers.go"].Support {
		t.Fatalf("main=%v breakers=%v", candidates["internal/adapters/skillslib/skillmain/main.go"].Support, candidates["internal/adapters/skillslib/skillmain/breakers.go"].Support)
	}
}

func TestPreferredPackageAnchorPathsUsesImplRoleFirst(t *testing.T) {
	t.Parallel()

	got := preferredPackageAnchorPaths(contextplane.RetrievalHit{
		PrimaryAnchorPath: "internal/adapters/skillslib/skillmain/main.go",
		AnchorPaths: []string{
			"internal/adapters/skillslib/skillmain/breakers.go",
			"internal/adapters/skillslib/skillmain/main.go",
		},
		AnchorRoles: map[string][]string{
			"impl":    {"internal/adapters/skillslib/skillmain/main.go"},
			"support": {"internal/adapters/skillslib/skillmain/breakers.go"},
		},
	})
	if len(got) < 2 {
		t.Fatalf("got=%v", got)
	}
	if got[0] != "internal/adapters/skillslib/skillmain/main.go" || got[1] != "internal/adapters/skillslib/skillmain/breakers.go" {
		t.Fatalf("got=%v", got)
	}
}

func TestPrioritizedFileLocateGroundingCandidatesPrefersInfraExact(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{
			Path:    "platform/10-argocd/apps/10-shared.yaml",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
		},
		{
			Path:    "platform/10-argocd/apps/65-praze-auth.yaml",
			Sources: map[string]struct{}{"aca_route_infra_exact": {}, "search_repo": {}},
			Support: 1.0,
		},
	}
	got := prioritizedFileLocateGroundingCandidates(ranked, "argocd application praze auth")
	if len(got) == 0 || got[0].Path != "platform/10-argocd/apps/65-praze-auth.yaml" {
		t.Fatalf("got=%v", pathsFromCandidates(got))
	}
}

func TestPrioritizedFileLocateGroundingCandidatesDoesNotFrontloadSecondPackageAnchor(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{
			Path:    "internal/adapters/skillslib/skillmain/main.go",
			Sources: map[string]struct{}{"aca_route_package_anchor": {}, "search_repo": {}},
			Support: 1.4,
		},
		{
			Path:    "internal/adapters/skillslib/skillmain/breakers.go",
			Sources: map[string]struct{}{"aca_route_package_anchor": {}, "search_repo": {}},
			Support: 1.2,
		},
		{
			Path:    "internal/intelligence/searchindex/model.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
		},
	}
	got := prioritizedFileLocateGroundingCandidates(ranked, "Which file anchors the skills main runtime wiring package?")
	order := pathsFromCandidates(got)
	if len(order) < 3 {
		t.Fatalf("order=%v", order)
	}
	if order[0] != "internal/adapters/skillslib/skillmain/main.go" {
		t.Fatalf("order=%v", order)
	}
	if order[1] == "internal/adapters/skillslib/skillmain/breakers.go" {
		t.Fatalf("secondary package anchor was front-loaded: %v", order)
	}
}

func TestPrioritizedFileLocateGroundingCandidatesDoesNotFrontloadSecondInfraAnchor(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{
			Path:    "platform/10-argocd/apps/65-praze-auth.yaml",
			Sources: map[string]struct{}{"aca_route_infra_exact": {}, "search_repo": {}},
			Support: 1.5,
		},
		{
			Path:    "platform/10-argocd/apps-dev/65-praze-auth.yaml",
			Sources: map[string]struct{}{"aca_route_infra_exact": {}, "search_repo": {}},
			Support: 1.3,
		},
		{
			Path:    "apps/praze-auth/helm/praze-auth/.argocd-source-praze-auth.yaml",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
		},
	}
	got := prioritizedFileLocateGroundingCandidates(ranked, "Which file defines the argocd application for praze auth?")
	order := pathsFromCandidates(got)
	if len(order) < 3 {
		t.Fatalf("order=%v", order)
	}
	if order[0] != "platform/10-argocd/apps/65-praze-auth.yaml" {
		t.Fatalf("order=%v", order)
	}
	if order[1] == "platform/10-argocd/apps-dev/65-praze-auth.yaml" {
		t.Fatalf("secondary infra anchor was front-loaded: %v", order)
	}
}

func TestRankCodeSearchCandidatesFileLocateDemotesSecondaryPackageAnchor(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"internal/adapters/skillslib/skillmain/main.go": {
			Path:    "internal/adapters/skillslib/skillmain/main.go",
			Sources: map[string]struct{}{"aca_route_package_anchor": {}, "aca_route_package_primary_anchor": {}, "aca_route_package_exact": {}},
			Support: 1.0,
		},
		"internal/adapters/skillslib/skillmain/breakers.go": {
			Path:    "internal/adapters/skillslib/skillmain/breakers.go",
			Sources: map[string]struct{}{"aca_route_package_anchor": {}, "aca_route_package_secondary_anchor": {}, "aca_route_package_exact": {}},
			Support: 1.0,
		},
		"internal/intelligence/searchindex/model.go": {
			Path:    "internal/intelligence/searchindex/model.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.9,
		},
		"internal/domain/agent/agent.go": {
			Path:    "internal/domain/agent/agent.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.8,
		},
	}
	ranked := rankCodeSearchCandidatesWithPlan(candidates, "Which file anchors the skills main runtime wiring package?", codeSearchTaskFileLocate, 8, codeSearchPathProbes("Which file anchors the skills main runtime wiring package?"), codeSearchExactProbes("Which file anchors the skills main runtime wiring package?"), nil, nil, "")
	order := pathsFromCandidates(ranked)
	if len(order) < 4 {
		t.Fatalf("order=%v", order)
	}
	if order[0] != "internal/adapters/skillslib/skillmain/main.go" {
		t.Fatalf("order=%v", order)
	}
	if order[1] == "internal/adapters/skillslib/skillmain/breakers.go" {
		t.Fatalf("secondary package anchor ranked too high: %v", order)
	}
}

func TestRankCodeSearchCandidatesFileLocateDemotesSecondaryInfraAnchor(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"platform/10-argocd/apps/65-praze-auth.yaml": {
			Path:    "platform/10-argocd/apps/65-praze-auth.yaml",
			Sources: map[string]struct{}{"aca_route_infra_exact": {}, "aca_route_infra_primary_anchor": {}, "search_repo": {}},
			Support: 1.0,
		},
		"platform/10-argocd/apps-dev/65-praze-auth.yaml": {
			Path:    "platform/10-argocd/apps-dev/65-praze-auth.yaml",
			Sources: map[string]struct{}{"aca_route_infra_exact": {}, "aca_route_infra_secondary_anchor": {}, "search_repo": {}},
			Support: 1.0,
		},
		"platform/10-argocd/apps/60-praze.yaml": {
			Path:    "platform/10-argocd/apps/60-praze.yaml",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.9,
		},
	}
	ranked := rankCodeSearchCandidatesWithPlan(candidates, "Which file defines the argocd application for praze auth?", codeSearchTaskFileLocate, 8, codeSearchPathProbes("Which file defines the argocd application for praze auth?"), codeSearchExactProbes("Which file defines the argocd application for praze auth?"), nil, nil, "")
	order := pathsFromCandidates(ranked)
	if len(order) < 3 {
		t.Fatalf("order=%v", order)
	}
	if order[0] != "platform/10-argocd/apps/65-praze-auth.yaml" {
		t.Fatalf("order=%v", order)
	}
	if order[1] == "platform/10-argocd/apps-dev/65-praze-auth.yaml" {
		t.Fatalf("secondary infra anchor ranked too high: %v", order)
	}
}

func TestBuildFileLocateEvidenceBucketsGroupsSelectedFiles(t *testing.T) {
	t.Parallel()

	files := []codeSearchEvidenceFile{
		{Path: "internal/adapters/skillslib/skillmain/main.go"},
		{Path: "internal/domain/agent/agent.go"},
		{Path: "internal/intelligence/searchindex/model.go"},
		{Path: "internal/adapters/skillslib/skillmain/breakers.go"},
	}
	rankedByPath := map[string]*codeSearchCandidate{
		"internal/adapters/skillslib/skillmain/main.go": {
			Path:    "internal/adapters/skillslib/skillmain/main.go",
			Sources: map[string]struct{}{"aca_route_package_primary_anchor": {}},
		},
		"internal/domain/agent/agent.go": {
			Path:    "internal/domain/agent/agent.go",
			Sources: map[string]struct{}{"search_repo": {}},
		},
		"internal/intelligence/searchindex/model.go": {
			Path:    "internal/intelligence/searchindex/model.go",
			Sources: map[string]struct{}{"search_repo": {}},
		},
		"internal/adapters/skillslib/skillmain/breakers.go": {
			Path:    "internal/adapters/skillslib/skillmain/breakers.go",
			Sources: map[string]struct{}{"aca_route_package_secondary_anchor": {}},
		},
	}

	got := buildFileLocateEvidenceBuckets(files, rankedByPath)
	if len(got["primary_anchor"]) != 1 || got["primary_anchor"][0] != "internal/adapters/skillslib/skillmain/main.go" {
		t.Fatalf("buckets=%v", got)
	}
	if len(got["repo_evidence"]) != 2 {
		t.Fatalf("buckets=%v", got)
	}
	if len(got["secondary_anchor"]) != 1 || got["secondary_anchor"][0] != "internal/adapters/skillslib/skillmain/breakers.go" {
		t.Fatalf("buckets=%v", got)
	}
}

func TestBuildCodeSearchCandidateTraceIncludesFileLocateMetadata(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{
			Path:    "internal/adapters/skillslib/skillmain/main.go",
			Sources: map[string]struct{}{"aca_route_package_primary_anchor": {}, "aca_route_package_anchor": {}},
			Support: 1.0,
		},
	}
	selected := map[string]int{"internal/adapters/skillslib/skillmain/main.go": 1}
	got := buildCodeSearchCandidateTrace(ranked, selected, 4, codeSearchTaskFileLocate)
	if len(got) != 1 {
		t.Fatalf("trace=%v", got)
	}
	if got[0].EvidenceClass != "primary_anchor" || got[0].AnchorRole != "primary_anchor" {
		t.Fatalf("trace=%v", got)
	}
}

func TestPrioritizedFileLocateGroundingCandidatesPrefersDiverseComplements(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{
			Path:    "platform/10-argocd/apps/65-praze-auth.yaml",
			Sources: map[string]struct{}{"aca_route_infra_exact": {}, "aca_route_infra_primary_anchor": {}, "search_repo": {}},
			Support: 1.5,
		},
		{
			Path:    "platform/10-argocd/apps-dev/65-praze-auth.yaml",
			Sources: map[string]struct{}{"aca_route_infra_exact": {}, "aca_route_infra_secondary_anchor": {}, "search_repo": {}},
			Support: 1.3,
		},
		{
			Path:    "apps/praze-auth/helm/praze-auth/.argocd-source-praze-auth.yaml",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
		},
		{
			Path:    "platform/10-argocd/apps/60-praze.yaml",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.1,
		},
	}
	got := prioritizedFileLocateGroundingCandidates(ranked, "Which file defines the argocd application for praze auth?")
	order := pathsFromCandidates(got)
	if len(order) < 4 {
		t.Fatalf("order=%v", order)
	}
	if order[0] != "platform/10-argocd/apps/65-praze-auth.yaml" {
		t.Fatalf("order=%v", order)
	}
	if order[1] == "platform/10-argocd/apps-dev/65-praze-auth.yaml" || order[2] == "platform/10-argocd/apps-dev/65-praze-auth.yaml" {
		t.Fatalf("secondary infra anchor consumed early budget: %v", order)
	}
}

func TestBestACASymbolForPathPrefersMatchingSuffix(t *testing.T) {
	t.Parallel()

	got := bestACASymbolForPath("lib/jido/agent_server/directive_exec.ex", []string{
		"Jido.AgentServer.ChildInfo",
		"Jido.AgentServer.DirectiveExec",
	})
	if got != "Jido.AgentServer.DirectiveExec" {
		t.Fatalf("got=%q", got)
	}
}

func TestApplyACAGuidanceSupportAddsElixirStorageAnchor(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"lib/jido/storage/file.ex": {
			Path:    "lib/jido/storage/file.ex",
			Sources: map[string]struct{}{"search_repo": {}, "semantic_search_code": {}},
			Support: 1.0,
			Symbols: []string{"Jido.Storage.File"},
		},
	}
	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/jido/packages/storage.md",
			RepoPaths: []string{"lib/jido/storage/ets.ex", "lib/jido/storage/file.ex"},
			Symbols:   []string{"Jido.Storage.ETS", "append_thread"},
			Title:     "storage",
		},
	}

	applied := applyACAGuidanceSupport("storage package", candidates, hits, codeSearchRoutePackageOwner, codeSearchTaskFileLocate, nil)
	if applied < 1 {
		t.Fatalf("applied=%d", applied)
	}
	etsCandidate := candidates["lib/jido/storage/ets.ex"]
	if etsCandidate == nil || !candidateHasSource(etsCandidate, "aca_route_package_anchor") {
		t.Fatalf("ets candidate=%#v", etsCandidate)
	}
}

func TestApplyACAGuidanceSupportMarksDirectiveExecAsPackageAnchor(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"lib/jido/agent_server/directive_exec.ex": {
			Path:    "lib/jido/agent_server/directive_exec.ex",
			Sources: map[string]struct{}{"path_probe": {}},
			Support: 0.8,
		},
		"lib/jido/agent_server/directive_executors.ex": {
			Path:    "lib/jido/agent_server/directive_executors.ex",
			Sources: map[string]struct{}{"path_probe": {}},
			Support: 0.8,
		},
	}
	hits := []contextplane.RetrievalHit{
		{
			Path: "notes/repo/jido/packages/agentserver.md",
			RepoPaths: []string{
				"lib/jido/agent_server/child_info.ex",
				"lib/jido/agent_server/directive_exec.ex",
				"lib/jido/agent_server/directive_executors.ex",
			},
			Symbols: []string{"Jido.AgentServer.DirectiveExec", "Jido.AgentServer.ChildInfo"},
			Title:   "agent_server",
		},
	}

	applied := applyACAGuidanceSupport("agent_server package", candidates, hits, codeSearchRoutePackageOwner, codeSearchTaskFileLocate, nil)
	if applied < 1 {
		t.Fatalf("applied=%d", applied)
	}
	directiveExec := candidates["lib/jido/agent_server/directive_exec.ex"]
	if directiveExec == nil || !candidateHasSource(directiveExec, "aca_route_package_anchor") {
		t.Fatalf("directive_exec=%#v", directiveExec)
	}
}

func TestSelectACAPackageAnchorPathsPrefersSymbolAndQueryAlignedFile(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"lib/jido/agent_server.ex": {
			Path:    "lib/jido/agent_server.ex",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
		},
		"lib/jido/agent_server/directive_exec.ex": {
			Path:    "lib/jido/agent_server/directive_exec.ex",
			Sources: map[string]struct{}{"path_probe": {}},
			Support: 0.8,
		},
		"lib/jido/agent_server/directive_executors.ex": {
			Path:    "lib/jido/agent_server/directive_executors.ex",
			Sources: map[string]struct{}{"path_probe": {}},
			Support: 0.7,
		},
	}
	got := selectACAPackageAnchorPaths(
		[]string{
			"lib/jido/agent_server/child_info.ex",
			"lib/jido/agent_server/directive_exec.ex",
			"lib/jido/agent_server/directive_executors.ex",
		},
		candidates,
		[]string{"Jido.AgentServer.DirectiveExec"},
		codeSearchPathTerms("agent_server package"),
		codeSearchPathProbes("agent_server package"),
		codeSearchExactProbes("agent_server package"),
		1,
	)
	if len(got) != 1 || got[0] != "lib/jido/agent_server/directive_exec.ex" {
		t.Fatalf("got=%v", got)
	}
}

func TestSelectACAPackageAnchorPathsPrefersSkillmainMain(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"internal/adapters/skillslib/skillmain/context.go": {
			Path:    "internal/adapters/skillslib/skillmain/context.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
		},
		"skills/code_semantic_search/main.go": {
			Path:    "skills/code_semantic_search/main.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
		},
	}
	got := selectACAPackageAnchorPaths(
		[]string{
			"internal/adapters/skillslib/skillmain/context.go",
			"internal/adapters/skillslib/skillmain/main.go",
			"internal/adapters/skillslib/skillmain/stores.go",
		},
		candidates,
		[]string{"RunContext"},
		codeSearchPathTerms("skills main runtime wiring"),
		codeSearchPathProbes("skills main runtime wiring"),
		codeSearchExactProbes("skills main runtime wiring"),
		2,
	)
	if len(got) == 0 || got[0] != "internal/adapters/skillslib/skillmain/main.go" {
		t.Fatalf("got=%v", got)
	}
}

func TestPackageACAGuidanceHitsFromSearchHitsPrefersSpecificPackageNote(t *testing.T) {
	t.Parallel()

	hits := []obsidianindex.SearchHit{
		{
			Path:      "notes/repo/jido/packages/lib.md",
			Title:     "lib",
			Trust:     "canonical",
			Score:     103,
			RepoPaths: []string{"lib/jido.ex"},
		},
		{
			Path:      "notes/repo/jido/packages/storage.md",
			Title:     "storage",
			Trust:     "canonical",
			Score:     117,
			RepoPaths: []string{"lib/jido/storage/ets.ex", "lib/jido/storage/file.ex"},
			Symbols:   []string{"Jido.Storage.ETS"},
		},
	}

	got := packageACAGuidanceHitsFromSearchHits("/Users/joshka/repos/githubs/jido", "storage package", hits, 1)
	if len(got) != 1 || got[0].Path != "notes/repo/jido/packages/storage.md" {
		t.Fatalf("got=%v", got)
	}
}

func TestPackageACAGuidanceHitsFromSearchHitsPrefersSkillmainOverSemanticSearch(t *testing.T) {
	t.Parallel()

	hits := []obsidianindex.SearchHit{
		{
			Path:      "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md",
			Title:     "internal adapters skillslib skillmain",
			Trust:     "canonical",
			Score:     34,
			RepoPaths: []string{"internal/adapters/skillslib/skillmain/main.go"},
			Symbols:   []string{"RunContext"},
		},
		{
			Path:      "notes/repo/agentctl/packages/skills-codesemanticsearch.md",
			Title:     "skills code_semantic_search",
			Trust:     "canonical",
			Score:     31,
			RepoPaths: []string{"skills/code_semantic_search/main.go"},
			Symbols:   []string{"CandidateBundle"},
		},
	}

	got := packageACAGuidanceHitsFromSearchHits("/Users/joshka/repos/personal/agentctl", "skills main runtime wiring", hits, 1)
	if len(got) != 1 || got[0].Path != "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md" {
		t.Fatalf("got=%v", got)
	}
}

func TestTopACAGuidanceSupportHitsPrefersSkillmainOverSemanticSearch(t *testing.T) {
	t.Parallel()

	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md",
			Title:     "internal adapters skillslib skillmain",
			Trust:     "canonical",
			Score:     60,
			RepoPaths: []string{"internal/adapters/skillslib/skillmain/main.go"},
			Symbols:   []string{"RunContext"},
		},
		{
			Path:      "notes/repo/agentctl/packages/skills-codesemanticsearch.md",
			Title:     "skills code_semantic_search",
			Trust:     "canonical",
			Score:     40,
			RepoPaths: []string{"skills/code_semantic_search/main.go"},
			Symbols:   []string{"CandidateBundle"},
		},
	}

	got := topACAGuidanceSupportHits("skills main runtime wiring", hits, codeSearchRoutePackageOwner, codeSearchTaskFileLocate)
	if len(got) != 1 || got[0].Path != "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md" {
		t.Fatalf("got=%v", got)
	}
}

func TestPackageGuidancePipelinePrefersSkillmainOverSemanticSearch(t *testing.T) {
	t.Parallel()

	searchHits := []obsidianindex.SearchHit{
		{
			Path:      "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md",
			Title:     "internal adapters skillslib skillmain",
			Trust:     "canonical",
			Score:     34,
			RepoPaths: []string{"internal/adapters/skillslib/skillmain/main.go"},
			Symbols:   []string{"RunContext"},
		},
		{
			Path:      "notes/repo/agentctl/packages/skills-codesemanticsearch.md",
			Title:     "skills code_semantic_search",
			Trust:     "canonical",
			Score:     31,
			RepoPaths: []string{"skills/code_semantic_search/main.go"},
			Symbols:   []string{"CandidateBundle"},
		},
	}

	hits := packageACAGuidanceHitsFromSearchHits("/Users/joshka/repos/personal/agentctl", "skills main runtime wiring", searchHits, 6)
	got := topACAGuidanceSupportHits("skills main runtime wiring", hits, codeSearchRoutePackageOwner, codeSearchTaskFileLocate)
	if len(got) != 1 || got[0].Path != "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md" {
		t.Fatalf("got=%v", got)
	}
}

func TestPackageGuidancePipelinePrefersSkillmainOverSemanticSearchWithRealScores(t *testing.T) {
	t.Parallel()

	searchHits := []obsidianindex.SearchHit{
		{
			Path:      "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md",
			Title:     "internal adapters skillslib skillmain",
			Trust:     "canonical",
			Score:     46,
			RepoPaths: []string{"internal/adapters/skillslib/skillmain/main.go"},
			Symbols:   []string{"RunContext"},
		},
		{
			Path:      "notes/repo/agentctl/packages/skills-codesemanticsearch.md",
			Title:     "skills code_semantic_search",
			Trust:     "canonical",
			Score:     47,
			RepoPaths: []string{"skills/code_semantic_search/main.go"},
			Symbols:   []string{"CandidateBundle"},
		},
	}

	hits := packageACAGuidanceHitsFromSearchHits("/Users/joshka/repos/personal/agentctl", "Which file anchors the skills main runtime wiring package?", searchHits, 6)
	got := topACAGuidanceSupportHits("Which file anchors the skills main runtime wiring package?", hits, codeSearchRoutePackageOwner, codeSearchTaskFileLocate)
	if len(got) != 1 || got[0].Path != "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md" {
		t.Fatalf("got=%v", got)
	}
}

func TestShouldUsePackageACAGuidanceAcceptsSpecificSkillmainNote(t *testing.T) {
	t.Parallel()

	hits := []contextplane.RetrievalHit{
		{
			Path:        "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md",
			Title:       "internal adapters skillslib skillmain",
			Trust:       "canonical",
			AnchorPaths: []string{"internal/adapters/skillslib/skillmain/main.go"},
		},
	}

	if !shouldUsePackageACAGuidance(codeSearchRouteCode, "Which file anchors the skills main runtime wiring package?", hits) {
		t.Fatal("expected package guidance override for specific skillmain note")
	}
}

func TestShouldUsePackageACAGuidanceRejectsWeakGenericPackageMatch(t *testing.T) {
	t.Parallel()

	hits := []contextplane.RetrievalHit{
		{
			Path:        "notes/repo/agentctl/packages/internal-storage-memory.md",
			Title:       "internal storage memory",
			Trust:       "canonical",
			AnchorPaths: []string{"internal/storage/memory/search.go"},
		},
	}

	if shouldUsePackageACAGuidance(codeSearchRouteCode, "Where is the eval code-search-ensemble command implemented?", hits) {
		t.Fatal("expected weak package hit to be ignored for generic code query")
	}
}

func TestPromoteCodeSearchRouteFamilyRejectsWeakPackagePromotion(t *testing.T) {
	t.Parallel()

	hits := []contextplane.RetrievalHit{
		{
			Path:        "notes/repo/agentctl/packages/internal-storage-memory.md",
			Title:       "internal storage memory",
			Trust:       "canonical",
			AnchorPaths: []string{"internal/storage/memory/search.go"},
		},
	}

	got := promoteCodeSearchRouteFamily(codeSearchRouteCode, "Where is the eval code-search-ensemble command implemented?", codeSearchTaskFileLocate, hits, codeSearchRoutePackageOwner)
	if got != codeSearchRouteCode {
		t.Fatalf("route=%q", got)
	}
}

func TestPromoteCodeSearchRouteFamilyAcceptsSpecificPackagePromotion(t *testing.T) {
	t.Parallel()

	hits := []contextplane.RetrievalHit{
		{
			Path:        "notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md",
			Title:       "internal adapters skillslib skillmain",
			Trust:       "canonical",
			AnchorPaths: []string{"internal/adapters/skillslib/skillmain/main.go"},
		},
	}

	got := promoteCodeSearchRouteFamily(codeSearchRouteCode, "Which file anchors the skills main runtime wiring package?", codeSearchTaskFileLocate, hits, codeSearchRoutePackageOwner)
	if got != codeSearchRoutePackageOwner {
		t.Fatalf("route=%q", got)
	}
}

func TestTopACAGuidanceSupportHitsPrefersSpecificInfraConcepts(t *testing.T) {
	t.Parallel()

	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/praze/concepts/applicationargocdpraze-auth-in-k8splatform-10-argocd-apps.md",
			RepoPaths: []string{"platform/10-argocd/apps/65-praze-auth.yaml"},
			Title:     "praze auth argocd application",
			Score:     8,
		},
		{
			Path:      "notes/repo/praze/concepts/applicationargocdplatform-eu-in-k8splatform-10-argocd.md",
			RepoPaths: []string{"platform/10-argocd/app-of-apps.yaml"},
			Title:     "platform eu argocd application",
			Score:     7,
		},
		{
			Path:      "notes/repo/praze/concepts/applicationargocdimage-updater-in-k8splatform-10-argocd-apps.md",
			RepoPaths: []string{"platform/10-argocd/apps/15-argocd-image-updater.yaml"},
			Title:     "argocd image updater",
			Score:     6,
		},
	}

	top := topACAGuidanceSupportHits("argocd application praze auth", hits, codeSearchRouteInfraResource, codeSearchTaskFileLocate)
	if len(top) != 2 {
		t.Fatalf("top=%v", top)
	}
	if top[0].Path != "notes/repo/praze/concepts/applicationargocdpraze-auth-in-k8splatform-10-argocd-apps.md" {
		t.Fatalf("unexpected top[0]=%q", top[0].Path)
	}
}

func TestInferCodeSearchRouteFamilyInfra(t *testing.T) {
	t.Parallel()

	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/praze/concepts/ingressprazewaitlist-api.md",
			RepoPaths: []string{"infra/k8s/waitlist/ingress.yaml"},
		},
	}
	if got := inferCodeSearchRouteFamily(codeSearchTaskFileLocate, hits); got != codeSearchRouteInfraResource {
		t.Fatalf("route=%q", got)
	}
}

func TestInferCodeSearchRouteFamilyPackage(t *testing.T) {
	t.Parallel()

	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/agentctl/packages/internal-rlm-env.md",
			RepoPaths: []string{"internal/rlm/env/code_search_ensemble.go"},
		},
	}
	if got := inferCodeSearchRouteFamily(codeSearchTaskFileLocate, hits); got != codeSearchRoutePackageOwner {
		t.Fatalf("route=%q", got)
	}
}

func TestInferCodeSearchRouteFamilyCochange(t *testing.T) {
	t.Parallel()

	hits := []contextplane.RetrievalHit{
		{
			Path:      "notes/repo/agentctl/retrieval-ensemble-next-steps.md",
			RepoPaths: []string{"internal/rlm/env/code_search_ensemble.go", "internal/contextplane/retrieval.go"},
		},
	}
	if got := inferCodeSearchRouteFamily(codeSearchTaskChangeImpact, hits); got != codeSearchRouteCochangeHistory {
		t.Fatalf("route=%q", got)
	}
}

func TestInferCodeSearchRouteFamilyFromCandidatesInfra(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"platform/10-argocd/apps/65-praze-auth.yaml": {
			Path:    "platform/10-argocd/apps/65-praze-auth.yaml",
			Sources: map[string]struct{}{"search_repo": {}, "path_probe": {}},
			Support: 1.2,
			Symbols: []string{"application argocd/praze-auth"},
		},
		"platform/10-argocd/app-of-apps.yaml": {
			Path:    "platform/10-argocd/app-of-apps.yaml",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.9,
			Symbols: []string{"application argocd/platform-eu"},
		},
	}
	if got := inferCodeSearchRouteFamilyFromCandidates(codeSearchTaskFileLocate, "argocd application praze auth", candidates); got != codeSearchRouteInfraResource {
		t.Fatalf("route=%q", got)
	}
}

func TestInferCodeSearchRouteFamilyFromCandidatesPackage(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"internal/platform/config/config.go": {
			Path:    "internal/platform/config/config.go",
			Sources: map[string]struct{}{"search_repo": {}, "path_probe": {}},
			Support: 1.2,
		},
		"internal/platform/config/context.go": {
			Path:    "internal/platform/config/context.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.8,
		},
		"internal/platform/config/dotenv.go": {
			Path:    "internal/platform/config/dotenv.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.7,
		},
	}
	if got := inferCodeSearchRouteFamilyFromCandidates(codeSearchTaskFileLocate, "platform config package", candidates); got != codeSearchRoutePackageOwner {
		t.Fatalf("route=%q", got)
	}
}

func TestInferCodeSearchRouteFamilyFromCandidatesRejectsUnrelatedDensePackageFamily(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"internal/storage/memory/factory.go": {
			Path:    "internal/storage/memory/factory.go",
			Sources: map[string]struct{}{"search_repo": {}, "repo_motif_anchor_support": {}},
			Support: 1.2,
		},
		"internal/storage/memory/search.go": {
			Path:    "internal/storage/memory/search.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.9,
		},
		"internal/storage/memory/store.go": {
			Path:    "internal/storage/memory/store.go",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 0.8,
		},
	}
	if got := inferCodeSearchRouteFamilyFromCandidates(codeSearchTaskFileLocate, "Where is the eval code-search-ensemble command implemented?", candidates); got != codeSearchRouteCode {
		t.Fatalf("route=%q", got)
	}
}

func TestInferCodeSearchRouteFamilyFromCandidatesKeepsCode(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"internal/rlm/env/code_search_ensemble.go": {
			Path:    "internal/rlm/env/code_search_ensemble.go",
			Sources: map[string]struct{}{"path_probe": {}, "search_repo": {}},
			Support: 1.3,
			Symbols: []string{"ReadOnlyAdapter"},
		},
		"cmd/agentctl/cmd/eval_code_search_ensemble.go": {
			Path:    "cmd/agentctl/cmd/eval_code_search_ensemble.go",
			Sources: map[string]struct{}{"exact_probe": {}},
			Support: 1.1,
		},
	}
	if got := inferCodeSearchRouteFamilyFromCandidates(codeSearchTaskFileLocate, "Where is code_search_ensemble implemented?", candidates); got != codeSearchRouteCode {
		t.Fatalf("route=%q", got)
	}
}

func TestInferCodeSearchRouteFamilyFromCandidatesCochange(t *testing.T) {
	t.Parallel()

	if got := inferCodeSearchRouteFamilyFromCandidates(codeSearchTaskChangeImpact, "What changes when ACA retrieval moves?", map[string]*codeSearchCandidate{}); got != codeSearchRouteCochangeHistory {
		t.Fatalf("route=%q", got)
	}
}

func TestPrioritizedFileLocateGroundingCandidatesPreservesDeclarativeCompanion(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{Path: "skills/code_semantic_search/main.go", Support: 1.0, Sources: map[string]struct{}{"path_probe": {}, "search_repo": {}}},
		{Path: "internal/domain/skill/installer.go", Support: 1.0, Sources: map[string]struct{}{"search_repo": {}}},
		{Path: "skills/code_semantic_search/skill.yaml", Support: 1.0, Sources: map[string]struct{}{"path_probe": {}}},
	}
	got := prioritizedFileLocateGroundingCandidates(ranked, "skills code semantic search")
	if len(got) < 2 {
		t.Fatalf("got=%v", got)
	}
	if got[1].Path != "skills/code_semantic_search/skill.yaml" {
		t.Fatalf("order=%v", []string{got[0].Path, got[1].Path})
	}
}

func TestPrioritizedFileLocateGroundingCandidatesPrefersModuleRootOverSubtype(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{Path: "lib/jido/agent/directive.ex", Support: 1.0, Sources: map[string]struct{}{"exact_probe": {}, "path_probe": {}, "search_repo": {}, "semantic_search_code": {}}, Symbols: []string{"Jido.Agent.Directive"}},
		{Path: "lib/jido/agent/directive/cron.ex", Support: 1.0, Sources: map[string]struct{}{"exact_probe": {}, "path_probe": {}, "search_repo": {}, "semantic_search_code": {}}, Symbols: []string{"Jido.Agent.Directive.Cron"}},
		{Path: "lib/jido/agent_server.ex", Support: 1.0, Sources: map[string]struct{}{"exact_probe": {}, "path_probe": {}, "search_repo": {}}, Symbols: []string{"Jido.AgentServer"}},
	}
	got := prioritizedFileLocateGroundingCandidates(ranked, "Which file defines Jido.Agent.Directive?")
	if len(got) < 3 {
		t.Fatalf("got=%v", got)
	}
	order := []string{got[0].Path, got[1].Path, got[2].Path}
	if order[0] != "lib/jido/agent/directive.ex" || order[1] != "lib/jido/agent_server.ex" {
		t.Fatalf("order=%v", order)
	}
}

func TestCodeSearchAdjacentImplementationAugmentPrefersMatchingSibling(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	dir := filepath.Join(workspace, "lib", "jido", "agent_server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"agent_server.ex", "directive_exec.ex", "directive_executors.ex", "signal_router.ex"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("defmodule X do\nend\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	candidates := map[string]*codeSearchCandidate{
		"lib/jido/agent_server.ex": {
			Path:    "lib/jido/agent_server.ex",
			Sources: map[string]struct{}{"execution_graph": {}, "search_repo": {}, "trace_target_repo": {}},
			Support: 1.0,
			Symbols: []string{"Jido.AgentServer"},
		},
	}
	hits, _ := codeSearchAdjacentImplementationAugment(workspace, candidates, "Which files connect signal routing to directive execution in AgentServer?", []string{"signal_router", "directive_exec", "agent_server"}, []string{"Jido.AgentServer", "directive_exec"}, 4)
	if len(hits) == 0 {
		t.Fatal("expected adjacent hits")
	}
	found := false
	for _, hit := range hits {
		if hit.Path == "lib/jido/agent_server/directive_exec.ex" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("hits=%v", hits)
	}
}

func TestShouldRunCodeSearchLLMReplannerWhenSelectedMissingImplementation(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{Path: "lib/jido/agent_server.ex", Sources: map[string]struct{}{"trace_target_anchor": {}}, Support: 1},
		{Path: "lib/jido/agent.ex", Sources: map[string]struct{}{"trace_source_anchor": {}}, Support: 1},
		{Path: "lib/jido/agent_server/directive_exec.ex", Sources: map[string]struct{}{"adjacent_impl": {}}, Support: 1},
	}
	if !shouldRunCodeSearchLLMReplanner(ranked, 2, nil) {
		t.Fatal("expected replanner trigger when implementation exists outside selected window")
	}
}

func TestShouldRunCodeSearchLLMReplannerSkipsWhenSelectedAlreadyCoversRoles(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{Path: "lib/jido/agent.ex", Sources: map[string]struct{}{"trace_source_anchor": {}}, Support: 1},
		{Path: "lib/jido/agent_server.ex", Sources: map[string]struct{}{"trace_target_anchor": {}}, Support: 1},
		{Path: "lib/jido/agent_server/directive_exec.ex", Sources: map[string]struct{}{"adjacent_impl": {}}, Support: 1},
	}
	if shouldRunCodeSearchLLMReplanner(ranked, 3, nil) {
		t.Fatal("did not expect replanner trigger when selected window already covers source, target, and implementation")
	}
}

func TestShouldRunCodeSearchLLMReplannerWhenSameRoleAlternateExists(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{Path: "lib/jido/agent_server.ex", Sources: map[string]struct{}{"trace_target_anchor": {}, "exact_probe": {}}, Support: 3.0},
		{Path: "lib/jido/agent_server/child_info.ex", Sources: map[string]struct{}{"path_probe": {}, "trace_target_anchor": {}}, Support: 2.0},
		{Path: "lib/jido/agent_server/signal_router.ex", Sources: map[string]struct{}{"path_probe": {}, "trace_target_anchor": {}}, Support: 1.4},
	}
	if !shouldRunCodeSearchLLMReplanner(ranked, 2, nil) {
		t.Fatal("expected replanner trigger for same-role ambiguity just outside selected window")
	}
}

func TestApplyCodeSearchResolverHintsToQueue(t *testing.T) {
	t.Parallel()

	queue := []*codeSearchCandidate{
		{Path: "lib/jido/agent_server.ex"},
		{Path: "lib/jido/agent_server/child_info.ex"},
		{Path: "lib/jido/agent_server/directive_exec.ex"},
		{Path: "lib/jido/agent_server/directive_executors.ex"},
	}
	ambiguities := []map[string]any{
		{
			"class":          "direct_dispatch",
			"selected_path":  "lib/jido/agent_server/child_info.ex",
			"alternate_path": "lib/jido/agent_server/directive_exec.ex",
		},
	}
	got := applyCodeSearchResolverHintsToQueue(queue, []string{"lib/jido/agent_server/directive_exec.ex"}, []string{"lib/jido/agent_server/child_info.ex"}, ambiguities, 4)
	order := []string{got[0].Path, got[1].Path, got[2].Path, got[3].Path}
	want := []string{
		"lib/jido/agent_server/directive_exec.ex",
		"lib/jido/agent_server.ex",
		"lib/jido/agent_server/directive_executors.ex",
		"lib/jido/agent_server/child_info.ex",
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order=%v want=%v", order, want)
		}
	}
}

func TestCodeSearchProtocolImplementationPaths(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	dir := filepath.Join(workspace, "lib", "jido", "agent_server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "directive_exec.ex"), []byte("defprotocol Jido.AgentServer.DirectiveExec do\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "directive_executors.ex"), []byte("defimpl Jido.AgentServer.DirectiveExec, for: Any do\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := codeSearchProtocolImplementationPaths(workspace, "lib/jido/agent_server/directive_exec.ex", "defprotocol Jido.AgentServer.DirectiveExec do\nend\n", 2)
	if len(got) == 0 || got[0] != "lib/jido/agent_server/directive_executors.ex" {
		t.Fatalf("got=%v", got)
	}
}

func TestLooksLikeCommandFactory(t *testing.T) {
	t.Parallel()

	if !looksLikeCommandFactory("newEvalCodeSearchEnsembleCommand") {
		t.Fatal("expected command factory match")
	}
	if looksLikeCommandFactory("codeSearchEnsemble") {
		t.Fatal("did not expect plain helper function to match")
	}
}

func TestExecutionBridgeSearchWeighting(t *testing.T) {
	t.Parallel()

	if codeSearchExecutionBridgeWeight("internal/rlm/env/adapter.go") <= codeSearchExecutionBridgeWeight("internal/rlm/env/code_search_ensemble_test.go") {
		t.Fatal("expected adapter.go to outrank test bridge files")
	}
}

func TestCodeSearchPathProbeSearchSkipsBinaryArtifacts(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "skills", "code_semantic_search"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "skills", "code_semantic_search", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "skills", "code_semantic_search", "skill.yaml"), []byte("kind: Skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "skills", "code_semantic_search", "code_semantic_search"), []byte{0x7f, 'E', 'L', 'F'}, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "bin", "code_semantic_search"), []byte{0x7f, 'E', 'L', 'F'}, 0o755); err != nil {
		t.Fatal(err)
	}

	hits, err := codeSearchPathProbeSearch(workspace, "code_semantic_search", 8)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(hits))
	for _, hit := range hits {
		paths = append(paths, hit.Path)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "/code_semantic_search") || path == "bin/code_semantic_search" {
			t.Fatalf("unexpected binary artifact hit in %v", paths)
		}
	}
	if len(paths) == 0 {
		t.Fatal("expected text/code hits")
	}
}

func TestPrioritizedGroundingCandidatesExecutionTracePrefersAnchors(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{Path: "target-exposure.go", Sources: map[string]struct{}{"trace_target_anchor": {}, "execution_bridge": {}}},
		{Path: "other.go", Sources: map[string]struct{}{"execution_bridge": {}}},
		{Path: "source.yaml", Sources: map[string]struct{}{"trace_source_anchor": {}, "path_probe": {}}},
		{Path: "source.go", Sources: map[string]struct{}{"trace_source_anchor": {}, "path_probe": {}, "search_repo": {}}},
		{Path: "target.go", Sources: map[string]struct{}{"trace_target_anchor": {}, "path_probe": {}, "search_repo": {}}},
		{Path: "runtime.go", Sources: map[string]struct{}{"execution_bridge": {}}},
	}
	got := prioritizedGroundingCandidates(ranked, "Which files connect session restore to code/semantic_search execution?", codeSearchTaskExecutionTrace)
	if len(got) < 3 {
		t.Fatalf("got=%v", got)
	}
	if got[0].Path != "source.go" || got[1].Path != "target.go" || got[2].Path != "target-exposure.go" {
		t.Fatalf("order=%v", []string{got[0].Path, got[1].Path, got[2].Path})
	}
}

func TestPrioritizedGroundingCandidatesExecutionTracePreservesTargetCoverage(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{Path: "praze-app/lib/api/audio.web.ts", Sources: map[string]struct{}{"trace_source_anchor": {}, "exact_probe": {}}},
		{Path: "apps/praze-api/lib/praze_web/controllers/api/audio_upload_controller.ex", Sources: map[string]struct{}{"execution_graph": {}, "search_repo": {}}},
		{Path: "workers/audio/src/media-gateway.ts", Sources: map[string]struct{}{"search_repo": {}}},
		{Path: "workers/audio/src/upload-signer.ts", Sources: map[string]struct{}{"trace_source_repo": {}, "search_repo": {}}},
	}
	got := prioritizedGroundingCandidates(ranked, "Which files connect audio upload signing to the media gateway?", codeSearchTaskExecutionTrace)
	if len(got) < 3 {
		t.Fatalf("got=%v", got)
	}
	foundTarget := false
	for _, candidate := range got[:3] {
		if candidate.Path == "workers/audio/src/media-gateway.ts" {
			foundTarget = true
			break
		}
	}
	if !foundTarget {
		t.Fatalf("top candidates=%v", []string{got[0].Path, got[1].Path, got[2].Path})
	}
}

func TestPrioritizedGroundingCandidatesExecutionTracePrefersClusterCompanion(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{Path: "praze-app/lib/api/audio.native.ts", Sources: map[string]struct{}{"trace_source_repo": {}, "execution_graph": {}}},
		{Path: "workers/audio/src/media-gateway.ts", Sources: map[string]struct{}{"trace_target_repo": {}, "search_repo": {}}},
		{Path: "workers/audio/src/upload-signer.ts", Sources: map[string]struct{}{"trace_source_repo": {}, "search_repo": {}}},
		{Path: "workers/audio/alchemy.ts", Sources: map[string]struct{}{"trace_target_repo": {}, "search_repo": {}}},
	}
	got := prioritizedGroundingCandidates(ranked, "Which files connect audio upload signing to the media gateway?", codeSearchTaskExecutionTrace)
	if len(got) < 4 {
		t.Fatalf("got=%v", got)
	}
	foundAlchemy := false
	for _, candidate := range got[:4] {
		if candidate.Path == "workers/audio/alchemy.ts" {
			foundAlchemy = true
			break
		}
	}
	if !foundAlchemy {
		t.Fatalf("top candidates=%v", []string{got[0].Path, got[1].Path, got[2].Path, got[3].Path})
	}
}

func TestPrioritizedGroundingCandidatesExecutionTraceReservesAdjacentImplementation(t *testing.T) {
	t.Parallel()

	ranked := []*codeSearchCandidate{
		{Path: "lib/jido/agent_server/signal_router.ex", Sources: map[string]struct{}{"trace_source_repo": {}, "search_repo": {}}, Support: 1.0, Symbols: []string{"Jido.AgentServer.SignalRouter"}},
		{Path: "lib/jido/agent_server.ex", Sources: map[string]struct{}{"trace_target_repo": {}, "execution_graph": {}, "search_repo": {}}, Support: 1.0, Symbols: []string{"Jido.AgentServer"}},
		{Path: "lib/jido/agent_server/directive_exec.ex", Sources: map[string]struct{}{"adjacent_impl": {}, "trace_target_repo": {}, "search_repo": {}}, Support: 1.0, Symbols: []string{"exec"}},
		{Path: "lib/jido/agent_server/signal/child_exit.ex", Sources: map[string]struct{}{"trace_target_repo": {}, "search_repo": {}}, Support: 1.0, Symbols: []string{"Jido.AgentServer.Signal.ChildExit"}},
	}
	got := prioritizedGroundingCandidates(ranked, "Which files connect signal routing to directive execution in AgentServer?", codeSearchTaskExecutionTrace)
	if len(got) < 3 {
		t.Fatalf("got=%v", got)
	}
	found := false
	for _, candidate := range got[:3] {
		if candidate.Path == "lib/jido/agent_server/directive_exec.ex" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("top candidates=%v", []string{got[0].Path, got[1].Path, got[2].Path})
	}
}

func TestExecutionTraceHubScorePrefersCentralBridge(t *testing.T) {
	t.Parallel()

	sourceTerms := codeSearchPathTerms("signal routing")
	targetTerms := codeSearchPathTerms("directive execution AgentServer")
	hub := executionTraceHubScore(&codeSearchCandidate{
		Path:    "lib/jido/agent_server.ex",
		Sources: map[string]struct{}{"execution_graph": {}, "search_repo": {}, "trace_source_repo": {}, "trace_target_repo": {}},
	}, sourceTerms, targetTerms, []string{"signal_router"}, []string{"signal_routing"}, []string{"agent_server", "directive_exec"}, []string{"AgentServer"})
	leaf := executionTraceHubScore(&codeSearchCandidate{
		Path:    "lib/jido/agent_server/signal/child_exit.ex",
		Sources: map[string]struct{}{"search_repo": {}, "trace_target_repo": {}},
	}, sourceTerms, targetTerms, []string{"signal_router"}, []string{"signal_routing"}, []string{"agent_server", "directive_exec"}, []string{"AgentServer"})
	if hub <= leaf {
		t.Fatalf("hub=%v leaf=%v", hub, leaf)
	}
}

func TestRankCodeSearchCandidatesExecutionTracePrefersSourceAnchor(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"source.go": {
			Path:    "skills/session_restore/main.go",
			Sources: map[string]struct{}{"trace_source_anchor": {}, "path_probe": {}},
			Support: 1.0,
		},
		"target.go": {
			Path:    "cmd/agentctl/cmd/context_semantic_search_inspect.go",
			Sources: map[string]struct{}{"trace_target_anchor": {}, "execution_bridge": {}},
			Support: 1.0,
		},
	}
	ranked := rankCodeSearchCandidatesWithProbes(candidates, "Which files connect session restore to code/semantic_search execution?", codeSearchTaskExecutionTrace, 8, []string{"session_restore", "code_semantic_search"}, []string{"session_restore", "code/semantic_search"})
	if len(ranked) < 2 {
		t.Fatalf("ranked=%v", ranked)
	}
	if ranked[0].Path != "skills/session_restore/main.go" && ranked[1].Path != "skills/session_restore/main.go" {
		t.Fatalf("top two=%v", []string{ranked[0].Path, ranked[1].Path})
	}
}

func TestRankCodeSearchCandidatesExecutionTracePrefersTargetCoverage(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"controller": {
			Path:    "apps/praze-api/lib/praze_web/controllers/api/audio_upload_controller.ex",
			Sources: map[string]struct{}{"execution_graph": {}, "search_repo": {}},
			Support: 1.0,
		},
		"target": {
			Path:    "workers/audio/src/media-gateway.ts",
			Sources: map[string]struct{}{"trace_target_repo": {}, "search_repo": {}},
			Support: 1.0,
		},
	}
	ranked := rankCodeSearchCandidatesWithProbes(candidates, "Which files connect audio upload signing to the media gateway?", codeSearchTaskExecutionTrace, 8, []string{"audio_upload", "media_gateway"}, []string{"audio_upload_signing", "media_gateway"})
	if len(ranked) < 2 {
		t.Fatalf("ranked=%v", ranked)
	}
	if ranked[0].Path != "workers/audio/src/media-gateway.ts" {
		t.Fatalf("top=%s", ranked[0].Path)
	}
}

func TestCodeSearchExactSymbolScorePrefersExactModuleMatch(t *testing.T) {
	t.Parallel()

	exactProbes := []string{"Jido.AgentServer"}
	exact := codeSearchExactSymbolScore(&codeSearchCandidate{
		Symbols: []string{"Jido.AgentServer"},
	}, exactProbes)
	prefix := codeSearchExactSymbolScore(&codeSearchCandidate{
		Symbols: []string{"Jido.AgentServer.Signal.ChildExit"},
	}, exactProbes)
	if exact <= prefix {
		t.Fatalf("exact=%v prefix=%v", exact, prefix)
	}
}

func TestCodeSearchSubtypePenaltyPrefersParentModule(t *testing.T) {
	t.Parallel()

	penalty := codeSearchSubtypePenalty(&codeSearchCandidate{
		Symbols: []string{"Jido.AgentServer.Signal.ChildExit"},
	}, []string{"Jido.AgentServer"})
	if penalty <= 0 {
		t.Fatalf("penalty=%v", penalty)
	}
}

func TestRankCodeSearchCandidatesFileLocatePrefersExactSymbolRoot(t *testing.T) {
	t.Parallel()

	candidates := map[string]*codeSearchCandidate{
		"server": {
			Path:    "lib/jido/agent_server.ex",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
			Symbols: []string{"Jido.AgentServer"},
		},
		"child": {
			Path:    "lib/jido/agent_server/signal/child_exit.ex",
			Sources: map[string]struct{}{"search_repo": {}},
			Support: 1.0,
			Symbols: []string{"Jido.AgentServer.Signal.ChildExit"},
		},
	}
	ranked := rankCodeSearchCandidatesWithProbes(candidates, "Which files define Jido.AgentServer?", codeSearchTaskFileLocate, 8, []string{"jido_agentserver"}, []string{"Jido.AgentServer"})
	if len(ranked) < 2 {
		t.Fatalf("ranked=%v", ranked)
	}
	if ranked[0].Path != "lib/jido/agent_server.ex" {
		t.Fatalf("top=%s", ranked[0].Path)
	}
}

func TestIsLikelyDocumentationPath(t *testing.T) {
	t.Parallel()

	if !isLikelyDocumentationPath("guides/directives.md") {
		t.Fatal("expected guides/directives.md to be documentation")
	}
	if isLikelyDocumentationPath("lib/jido/agent_server.ex") {
		t.Fatal("did not expect lib/jido/agent_server.ex to be documentation")
	}
}

func TestCommonExecutionTraceClusterRoot(t *testing.T) {
	t.Parallel()

	got := commonExecutionTraceClusterRoot(
		&codeSearchCandidate{Path: "workers/audio/src/upload-signer.ts"},
		&codeSearchCandidate{Path: "workers/audio/src/media-gateway.ts"},
	)
	if got != "workers/audio" {
		t.Fatalf("cluster=%q", got)
	}
}

func TestCodeSearchBasenameProbeScore(t *testing.T) {
	t.Parallel()

	pathProbes := []string{"code_search_ensemble"}
	exactProbes := []string{"code_search_ensemble"}
	impl := codeSearchBasenameProbeScore("internal/rlm/env/code_search_ensemble.go", pathProbes, exactProbes)
	registry := codeSearchBasenameProbeScore("internal/rlm/env/tool_profiles.go", pathProbes, exactProbes)
	if impl <= registry {
		t.Fatalf("implementation score=%v registry score=%v", impl, registry)
	}
}

func TestCodeSearchPathTermsIncludesQueryWords(t *testing.T) {
	t.Parallel()

	terms := codeSearchPathTerms("If you change FilterTools, which files are directly impacted in the runtime and eval path?")
	foundEval := false
	foundRuntime := false
	for _, term := range terms {
		if term == "eval" {
			foundEval = true
		}
		if term == "runtime" {
			foundRuntime = true
		}
	}
	if !foundEval || !foundRuntime {
		t.Fatalf("terms=%v", terms)
	}
}

func TestCodeSearchSymbolProbes(t *testing.T) {
	t.Parallel()

	got := codeSearchSymbolProbes("Which file defines codeSearchEnsembleInput?")
	if len(got) == 0 || got[0] != "codeSearchEnsembleInput" {
		t.Fatalf("probes=%v", got)
	}
}

func TestExtractGroundedEvidenceSymbolsPrefersExactProbe(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		"type codeSearchEnsembleInput struct {",
		"\tQuery string",
		"}",
		"",
		"type codeSearchEvidenceFile struct {",
		"\tSupportScore float64",
		"}",
	}, "\n")
	got := extractGroundedEvidenceSymbols(
		"internal/rlm/env/code_search_ensemble.go",
		content,
		1,
		1,
		[]string{"codeSearchEnsembleInput"},
		nil,
		codeSearchTaskSymbolInspect,
	)
	if len(got) == 0 {
		t.Fatal("expected grounded symbols")
	}
	if got[0].Symbol != "codeSearchEnsembleInput" {
		t.Fatalf("symbols=%v", got)
	}
}

func TestExtractGroundedEvidenceSymbolsFiltersFileBasenameNoise(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		"type codeSearchEvidenceFile struct {",
		"\tSupportScore float64",
		"}",
	}, "\n")
	got := extractGroundedEvidenceSymbols(
		"internal/rlm/env/code_search_ensemble.go",
		content,
		1,
		1,
		nil,
		[]string{"file.go", "codeSearchEvidenceFile"},
		codeSearchTaskSymbolInspect,
	)
	if len(got) == 0 {
		t.Fatal("expected grounded symbols")
	}
	for _, item := range got {
		if item.Symbol == "file.go" {
			t.Fatalf("unexpected noisy symbol in %v", got)
		}
	}
}

func TestExtractCodeSearchBridgeTuples(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		"adapter := rlmenv.NewReadOnlyAdapter(cfg, workspace, vaultPath, companionDB, env)",
		`out, err := adapter.Execute(runCtx, "code_search_ensemble", raw)`,
	}, "\n")
	got := extractCodeSearchBridgeTuples(content)
	if len(got) != 1 {
		t.Fatalf("tuples=%v", got)
	}
	if got[0].Query != "ReadOnlyAdapter Execute code_search_ensemble" {
		t.Fatalf("tuple=%+v", got[0])
	}
}
