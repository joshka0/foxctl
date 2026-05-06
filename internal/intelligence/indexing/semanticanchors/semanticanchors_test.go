package semanticanchors

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/evidence"
)

func TestParseInlineAnchorValidForms(t *testing.T) {
	policy := DefaultAnchorPolicy("github.com/joshka0/foxctl", nil)
	tests := []struct {
		name     string
		syntax   string
		wantID   AnchorTargetID
		wantType AnchorType
		wantPath string
	}{
		{
			name:     "scoped concept",
			syntax:   "[[foxctl:invariant/no-send-without-read]]",
			wantID:   "anchor:foxctl:invariant:no-send-without-read",
			wantType: AnchorTypeInvariant,
		},
		{
			name:     "unscoped concept",
			syntax:   "[[invariant:no-send-without-read]]",
			wantID:   "anchor:repo:invariant:no-send-without-read",
			wantType: AnchorTypeInvariant,
		},
		{
			name:     "doc preserves case",
			syntax:   "[[doc:docs/General/Tmux-Collaboration.md#Room-Access]]",
			wantID:   "anchor:repo:doc:docs/General/Tmux-Collaboration.md#Room-Access",
			wantType: AnchorTypeDoc,
		},
		{
			name:     "test preserves case",
			syntax:   "[[test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite]]",
			wantID:   "anchor:repo:test:internal/runtime/terminal/tmuxbridge/client_test.go#TestReadBeforeWrite",
			wantType: AnchorTypeTest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			occ, findings := ParseInlineAnchor(policy, tc.syntax)
			if len(findings) != 0 {
				t.Fatalf("unexpected findings: %+v", findings)
			}
			if occ.TargetID != tc.wantID || occ.Type != tc.wantType {
				t.Fatalf("parsed anchor = (%q, %q), want (%q, %q)", occ.TargetID, occ.Type, tc.wantID, tc.wantType)
			}
		})
	}
}

func TestParseInlineAnchorRejectsInvalidForms(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	tests := []struct {
		name       string
		syntax     string
		wantReason AnchorFindingReason
	}{
		{"scoped doc", "[[foxctl:doc/docs/foo.md]]", AnchorFindingScopedPathAnchor},
		{"scoped test", "[[foxctl:test/internal/foo_test.go#TestX]]", AnchorFindingScopedPathAnchor},
		{"unknown scope", "[[randomscope:invariant/no-send-without-read]]", AnchorFindingUnknownScope},
		{"unknown type", "[[foxctl:unknown/no-send-without-read]]", AnchorFindingUnknownType},
		{"unsafe url", "[[doc:https://example.com/a.md]]", AnchorFindingUnsafeURL},
		{"absolute path", "[[doc:/tmp/a.md]]", AnchorFindingAbsolutePath},
		{"traversal", "[[doc:../../secret.md]]", AnchorFindingPathTraversal},
		{"env var", "[[doc:$HOME/secret.md]]", AnchorFindingEnvVarExpansion},
		{"backslash", `[[doc:docs\foo.md]]`, AnchorFindingBackslashPath},
		{"control", "[[invariant:bad\nvalue]]", AnchorFindingControlChar},
		{"secret like", "[[invariant:api-key-leak]]", AnchorFindingSecretLike},
		{"token like", "[[invariant:ghp_abcdef123456]]", AnchorFindingSecretLike},
		{"session like", "[[invariant:session_abcdef123456]]", AnchorFindingSessionLike},
		{"namespace collision", "[[invariant:foo::bar]]", AnchorFindingNamespaceCollision},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, findings := ParseInlineAnchor(policy, tc.syntax)
			if len(findings) != 1 || findings[0].Reason != tc.wantReason {
				t.Fatalf("findings = %+v, want reason %s", findings, tc.wantReason)
			}
		})
	}
}

func TestResolveAnchorOccurrenceUsesTargetResolverForPathAnchors(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	occ, findings := ParseInlineAnchor(policy, "[[doc:docs/missing.md#Nope]]")
	if len(findings) != 0 {
		t.Fatalf("unexpected parse findings: %+v", findings)
	}
	occ.OwnerBinding = AnchorOwnerBinding{OwnerNodeID: "foxctl::file:internal/foo.go", OwnerKind: "file", OwnerStableKey: "internal/foo.go"}
	res, err := ResolveAnchorOccurrence(context.Background(), policy, occ, stubResolver{status: AnchorValidationMissingTarget})
	if err != nil {
		t.Fatal(err)
	}
	if res.Relation != SemanticAnchorRelationDeclaresTarget || res.IntendedRelation != SemanticAnchorRelationDescribedBy || res.EdgeAction != AnchorEdgeMissingTarget {
		t.Fatalf("resolution = %+v", res)
	}
}

func TestResolveAnchorOccurrenceDoesNotIndexBeacon(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	occ, findings := ParseInlineAnchor(policy, "[[foxctl:beacon/agent-terminal-safety]]")
	if len(findings) != 0 {
		t.Fatalf("unexpected parse findings: %+v", findings)
	}
	occ.OwnerBinding = AnchorOwnerBinding{OwnerNodeID: "foxctl::symbol:internal/foo.go:Guard", OwnerKind: "symbol", OwnerStableKey: "Guard"}
	res, err := ResolveAnchorOccurrence(context.Background(), policy, occ, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.EdgeAction != AnchorEdgeNone {
		t.Fatalf("beacon EdgeAction=%q want none", res.EdgeAction)
	}
}

func TestResolveAnchorOccurrenceErrorsPreventEdges(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	occ, findings := ParseInlineAnchor(policy, "[[foxctl:invariant/no-send-without-read]]")
	if len(findings) != 0 {
		t.Fatalf("unexpected parse findings: %+v", findings)
	}
	occ.OwnerBinding = AnchorOwnerBinding{OwnerNodeID: "foxctl::symbol:internal/foo.go:Guard", OwnerKind: "symbol", OwnerStableKey: "Guard"}
	occ.Findings = append(occ.Findings, Finding{ID: "unbound", Reason: AnchorFindingUnboundOwner, Severity: AnchorFindingError})
	res, err := ResolveAnchorOccurrence(context.Background(), policy, occ, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.EdgeAction != AnchorEdgeNone || res.Occurrence.ValidationStatus != AnchorValidationLintError {
		t.Fatalf("resolution = %+v, want lint/no edge", res)
	}
}

func TestResolveAnchorOccurrenceIDIgnoresLineMovement(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	owner := AnchorOwnerBinding{OwnerNodeID: "foxctl::symbol:internal/foo.go:Guard", OwnerKind: "symbol", OwnerStableKey: "go:internal/foo.go:Guard"}
	first, findings := ParseInlineAnchor(policy, "[[foxctl:invariant/no-send-without-read]]")
	if len(findings) != 0 {
		t.Fatalf("unexpected parse findings: %+v", findings)
	}
	first.Span = SourceSpan{Path: "internal/foo.go", LineStart: 3, LineEnd: 3}
	first.OwnerBinding = owner
	second := first
	second.Span = SourceSpan{Path: "internal/foo.go", LineStart: 30, LineEnd: 30}
	second.OccurrenceID = ""

	resA, err := ResolveAnchorOccurrence(context.Background(), policy, first, nil)
	if err != nil {
		t.Fatal(err)
	}
	resB, err := ResolveAnchorOccurrence(context.Background(), policy, second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resA.Occurrence.OccurrenceID == "" || resA.Occurrence.OccurrenceID != resB.Occurrence.OccurrenceID {
		t.Fatalf("occurrence IDs = %q / %q, want stable across line movement", resA.Occurrence.OccurrenceID, resB.Occurrence.OccurrenceID)
	}
}

func TestAnchorTargetNodeIDRoundTripRejectsDoubleNamespacing(t *testing.T) {
	target, err := NewAnchorTargetID("repo", "invariant", "no-send-without-read")
	if err != nil {
		t.Fatal(err)
	}
	nodeID, err := AnchorTargetNodeID("foxctl", target)
	if err != nil {
		t.Fatal(err)
	}
	repoKey, decoded, ok := DecodeAnchorTargetNodeID(string(nodeID))
	if !ok || repoKey != "foxctl" || decoded != target {
		t.Fatalf("decoded = (%q, %q, %v), want foxctl/%q/true", repoKey, decoded, ok, target)
	}
	if _, _, ok := DecodeAnchorTargetNodeID("foxctl::foxctl::anchor:repo:doc:docs/foo.md"); ok {
		t.Fatal("double-namespaced anchor node ID decoded successfully")
	}
	if _, err := AnchorTargetNodeID("foxctl::again", target); err == nil {
		t.Fatal("AnchorTargetNodeID accepted namespaced repo key")
	}
	if _, err := NewAnchorTargetID("repo", "invariant", "foo::bar"); err == nil {
		t.Fatal("NewAnchorTargetID accepted namespace collision")
	}
}

func TestSemanticAnchorEdgeMetaValidation(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	occ, findings := ParseInlineAnchor(policy, "[[foxctl:invariant/no-send-without-read]]")
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
	owner := AnchorOwner{NodeID: "foxctl::symbol:internal/foo.go:Guard", Kind: "symbol", StableKey: "Guard"}
	occ.OwnerBinding = AnchorOwnerBinding{OwnerNodeID: owner.NodeID, OwnerKind: owner.Kind, OwnerStableKey: owner.StableKey}
	res, err := ResolveAnchorOccurrence(context.Background(), policy, occ, nil)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := NewSemanticAnchorEdgeMeta(res, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSemanticAnchorEdgeMeta(meta); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSemanticAnchorEdgeMeta(raw); err != nil {
		t.Fatal(err)
	}
	meta.AllowedAuthorityEffects = append(meta.AllowedAuthorityEffects, evidence.AuthorityEffectInstructionSource)
	if err := ValidateSemanticAnchorEdgeMeta(meta); err == nil {
		t.Fatal("metadata with instruction_source validated successfully")
	}
	if _, err := NewSemanticAnchorEdgeMeta(res, AnchorOwner{NodeID: "foxctl::symbol:other", Kind: "symbol", StableKey: "Other"}); err == nil {
		t.Fatal("owner mismatch constructed metadata successfully")
	}
}

func TestSemanticAnchorEvidenceCannotRenderAsInstruction(t *testing.T) {
	meta := evidence.EvidenceMeta{
		Source:            evidence.EvidenceSourceSemanticAnchor,
		SourcePlane:       evidence.EvidencePlaneSemanticAnchor,
		EvidenceClass:     evidence.EvidenceClassSourceComment,
		EvidenceAuthority: evidence.EvidenceAuthorityEvidenceOnly,
		AllowedAuthorityEffects: []evidence.AuthorityEffect{
			evidence.AuthorityEffectRetrievalRanking,
			evidence.AuthorityEffectReviewSignal,
		},
	}
	if err := evidence.ValidateRenderSurface(meta, evidence.RenderSurfaceInstruction); err == nil {
		t.Fatal("semantic anchor evidence rendered as instruction")
	}
	if err := evidence.ValidateRenderSurface(meta, evidence.RenderSurfaceEvidencePack); err != nil {
		t.Fatal(err)
	}
}

type stubResolver struct {
	status AnchorValidationStatus
	err    error
}

func (s stubResolver) ResolveAnchorTarget(context.Context, AnchorOccurrence) (TargetResolution, error) {
	if s.err != nil {
		return TargetResolution{}, s.err
	}
	if s.status == "" {
		return TargetResolution{Status: AnchorValidationValidReference}, nil
	}
	if s.status == AnchorValidationMissingTarget {
		finding := Finding{ID: "missing", Reason: AnchorFindingMissingTarget, Severity: AnchorFindingWarning}
		return TargetResolution{Status: s.status, Finding: &finding}, nil
	}
	return TargetResolution{Status: s.status}, nil
}

func TestStubResolverInterface(t *testing.T) {
	var resolver TargetResolver = stubResolver{err: errors.New("boom")}
	if _, err := resolver.ResolveAnchorTarget(context.Background(), AnchorOccurrence{}); err == nil {
		t.Fatal("stub resolver did not return error")
	}
}

func TestSemanticAnchorsCoreImportGuard(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	pkgs, err := parser.ParseDir(token.NewFileSet(), dir, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	blocked := []string{
		"/internal/intelligence/indexing/repoindex",
		"/internal/intelligence/indexing/searchindex",
		"/internal/context/contextplane",
		"/internal/memorycore",
		"/internal/storage",
		"/internal/obsidian",
		"/internal/v2",
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				for _, forbidden := range blocked {
					if strings.Contains(path, forbidden) {
						t.Fatalf("semanticanchors core imports forbidden package %q", path)
					}
				}
			}
		}
	}
}

type ownerResolverStub struct{}

func (ownerResolverStub) ResolveFileOwner(path string) AnchorOwner {
	return AnchorOwner{NodeID: "foxctl::file:" + path, Kind: "file", StableKey: "file:" + path, Path: path}
}

func (ownerResolverStub) ResolveSymbolOwner(path, lang string, span Span, qualifiedName string) (AnchorOwner, bool) {
	return AnchorOwner{
		NodeID:    "foxctl::symbol:" + path + ":" + qualifiedName,
		Kind:      "symbol",
		StableKey: lang + ":" + path + ":" + qualifiedName,
		Path:      path,
		Name:      qualifiedName,
		StartLine: span.LineStart,
		EndLine:   span.LineEnd,
	}, true
}

func TestExtractGoAnchorsBindsFunctionAndMethod(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	src := []byte(`package demo

type Bridge struct{}

// [[foxctl:invariant/no-send-without-read]]
func Guard() {}

// [[foxctl:risk/agent-terminal-desync]]
func (b *Bridge) Type() {}
`)
	result, err := ExtractAnchorsFromSource(context.Background(), policy, ownerResolverStub{}, "internal/demo.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if result.Support != AnchorSupportGraphBinding || len(result.Occurrences) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if got := result.Occurrences[0].OwnerBinding.OwnerStableKey; !strings.Contains(got, ":Guard") {
		t.Fatalf("function owner = %q", got)
	}
	if got := result.Occurrences[1].OwnerBinding.OwnerStableKey; !strings.Contains(got, ":Bridge.Type") {
		t.Fatalf("method owner = %q", got)
	}
}

func TestExtractGoAnchorsFileLevelFallback(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	src := []byte(`// [[doc:docs/general/anchors.md#overview]]
package demo

func Guard() {}
`)
	result, err := ExtractAnchorsFromSource(context.Background(), policy, ownerResolverStub{}, "internal/demo.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Occurrences) != 1 {
		t.Fatalf("occurrences = %+v", result.Occurrences)
	}
	if got := result.Occurrences[0].OwnerBinding.OwnerKind; got != "file" {
		t.Fatalf("owner kind = %q, want file", got)
	}
}

func TestExtractGoFileScopeAnchorCanBindSymbol(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	src := []byte(`package demo

// [[doc:docs/general/anchors.md#overview]]
func Guard() {}
`)
	result, err := ExtractAnchorsFromSource(context.Background(), policy, ownerResolverStub{}, "internal/demo.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Occurrences) != 1 {
		t.Fatalf("occurrences = %+v", result.Occurrences)
	}
	if got := result.Occurrences[0].OwnerBinding.OwnerKind; got != "symbol" {
		t.Fatalf("owner kind = %q, want symbol", got)
	}
}

func TestExtractGoAnchorsDoesNotBindOverGapBackwardOrCode(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "over gap",
			src: `package demo
// [[foxctl:invariant/no-send-without-read]]


func Guard() {}
`,
		},
		{
			name: "code between",
			src: `package demo
// [[foxctl:invariant/no-send-without-read]]
var x = 1
func Guard() {}
`,
		},
		{
			name: "no backward",
			src: `package demo
func Guard() {}
// [[foxctl:invariant/no-send-without-read]]
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ExtractAnchorsFromSource(context.Background(), policy, ownerResolverStub{}, "internal/demo.go", []byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Occurrences) != 1 {
				t.Fatalf("occurrences = %+v", result.Occurrences)
			}
			if result.Occurrences[0].OwnerBinding.OwnerNodeID != "" {
				t.Fatalf("unexpected owner = %+v", result.Occurrences[0].OwnerBinding)
			}
			if !hasFinding(result.Occurrences[0].Findings, AnchorFindingUnboundOwner) && !hasFinding(result.Findings, AnchorFindingUnboundOwner) {
				t.Fatalf("missing unbound finding: occ=%+v result=%+v", result.Occurrences[0].Findings, result.Findings)
			}
		})
	}
}

func TestExtractAnchorsCommentsOnlyAndNonGoLanguages(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	tests := []struct {
		path      string
		src       string
		ownerName string
	}{
		{"src/app.ts", `const s = "[[foxctl:invariant/string-literal]]";
// [[foxctl:invariant/comment-only]]
function run() {}
`, "run"},
		{"src/app.py", `s = "[[foxctl:invariant/string-literal]]"
# [[foxctl:invariant/comment-only]]
def run(): pass
`, "run"},
		{"src/app.rs", `let s = "[[foxctl:invariant/string-literal]]";
// [[foxctl:invariant/comment-only]]
fn run() {}
`, "run"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result, err := ExtractAnchorsFromSource(context.Background(), policy, ownerResolverStub{}, tc.path, []byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if result.Support != AnchorSupportGraphBinding {
				t.Fatalf("support = %q", result.Support)
			}
			if len(result.Occurrences) != 1 || result.Occurrences[0].Target != "comment-only" {
				t.Fatalf("occurrences = %+v", result.Occurrences)
			}
			if got := result.Occurrences[0].OwnerBinding.OwnerStableKey; !strings.Contains(got, ":"+tc.path+":"+tc.ownerName) {
				t.Fatalf("owner stable key = %q, want %s owner", got, tc.ownerName)
			}
		})
	}
}

func TestExtractNonGoAnchorsBindOnlyAttachedOwners(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	tests := []struct {
		name string
		path string
		src  string
	}{
		{
			name: "typescript code between",
			path: "src/app.ts",
			src: `// [[foxctl:invariant/no-send-without-read]]
const unrelated = 1
function run() {}
`,
		},
		{
			name: "python over gap",
			path: "src/app.py",
			src: `# [[foxctl:invariant/no-send-without-read]]


def run(): pass
`,
		},
		{
			name: "rust backward",
			path: "src/app.rs",
			src: `fn run() {}
// [[foxctl:invariant/no-send-without-read]]
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ExtractAnchorsFromSource(context.Background(), policy, ownerResolverStub{}, tc.path, []byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Occurrences) != 1 {
				t.Fatalf("occurrences = %+v", result.Occurrences)
			}
			if result.Occurrences[0].OwnerBinding.OwnerNodeID != "" {
				t.Fatalf("unexpected owner = %+v", result.Occurrences[0].OwnerBinding)
			}
			if !hasFinding(result.Occurrences[0].Findings, AnchorFindingUnboundOwner) && !hasFinding(result.Findings, AnchorFindingUnboundOwner) {
				t.Fatalf("missing unbound finding: occ=%+v result=%+v", result.Occurrences[0].Findings, result.Findings)
			}
		})
	}
}

func TestValidateOccurrenceSetFindings(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	policy.MaxAnchorsPerOwner = 2
	policy.MaxBeaconsPerOwner = 1
	owner := AnchorOwnerBinding{OwnerNodeID: "foxctl::symbol:x", OwnerKind: "symbol", OwnerStableKey: "x"}
	occurrences := []AnchorOccurrence{
		mustAnchor(t, policy, "[[foxctl:invariant/a]]", owner),
		mustAnchor(t, policy, "[[foxctl:invariant/a]]", owner),
		mustAnchor(t, policy, "[[foxctl:risk/b]]", owner),
		mustAnchor(t, policy, "[[beacon:c]]", owner),
		mustAnchor(t, policy, "[[beacon:d]]", owner),
	}
	findings := ValidateOccurrenceSet(policy, occurrences)
	for _, reason := range []AnchorFindingReason{
		AnchorFindingDuplicateOwnerTarget,
		AnchorFindingTooManyAnchors,
		AnchorFindingTooManyBeacons,
	} {
		if !hasFinding(findings, reason) {
			t.Fatalf("missing %s in %+v", reason, findings)
		}
	}
	onlyBeacon := []AnchorOccurrence{mustAnchor(t, policy, "[[beacon:c]]", owner)}
	if !hasFinding(ValidateOccurrenceSet(policy, onlyBeacon), AnchorFindingBeaconWithoutSupport) {
		t.Fatal("missing beacon_without_support finding")
	}
}

func TestGeneratedVendorAndAnchorDocStripping(t *testing.T) {
	policy := DefaultAnchorPolicy("foxctl", nil)
	result, err := ExtractAnchorsFromSource(context.Background(), policy, ownerResolverStub{}, "vendor/demo.go", []byte(`// Code generated by tool. DO NOT EDIT.
package demo
// [[foxctl:invariant/a]]
func Guard() {}
`))
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(result.Findings, AnchorFindingGeneratedOrVendor) || !hasFinding(result.Occurrences[0].Findings, AnchorFindingGeneratedOrVendor) {
		t.Fatalf("missing generated/vendor finding: %+v %+v", result.Findings, result.Occurrences[0].Findings)
	}
	doc := "Keep this.\n// [[foxctl:invariant/a]]\nAnd this."
	if got := StripAnchorOnlyDocLines(doc); strings.Contains(got, "[[") || !strings.Contains(got, "Keep this.") || !strings.Contains(got, "And this.") {
		t.Fatalf("stripped doc = %q", got)
	}
}

func mustAnchor(t *testing.T, policy AnchorPolicy, syntax string, owner AnchorOwnerBinding) AnchorOccurrence {
	t.Helper()
	occ, findings := ParseInlineAnchor(policy, syntax)
	if len(findings) != 0 {
		t.Fatalf("parse %s findings: %+v", syntax, findings)
	}
	occ.OwnerBinding = owner
	return occ
}

func hasFinding(findings []Finding, reason AnchorFindingReason) bool {
	for _, finding := range findings {
		if finding.Reason == reason {
			return true
		}
	}
	return false
}
