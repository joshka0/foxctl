package semanticanchors

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestSemanticCommentingSkillExamplesMatchParser(t *testing.T) {
	content := readSemanticCommentingSkill(t)
	anchorRE := regexp.MustCompile(`(?m)^\s*//\s*(\[\[[^\]\n]+\]\])\s*$`)
	matches := anchorRE.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		t.Fatal("semantic-commenting skill has no positive comment anchor examples")
	}

	policy := DefaultAnchorPolicy("example.com/acme/project", []AnchorScope{"acme"})
	seenTypes := make(map[AnchorType]bool)
	scopedConceptExamples := 0
	for _, match := range matches {
		syntax := match[1]
		occ, findings := ParseInlineAnchor(policy, syntax)
		if len(findings) != 0 {
			t.Fatalf("skill example %s does not parse cleanly: %+v", syntax, findings)
		}
		if occ.Type == AnchorTypeBeacon {
			t.Fatalf("skill presents beacon anchor as a positive example: %s", syntax)
		}
		if occ.Scope != RepoLocalAnchorScope {
			if occ.Scope != AnchorScope("acme") || occ.Type == AnchorTypeDoc || occ.Type == AnchorTypeTest {
				t.Fatalf("portable skill example %s used unexpected scope/type %q/%q", syntax, occ.Scope, occ.Type)
			}
			scopedConceptExamples++
		}
		if (occ.Type == AnchorTypeDoc || occ.Type == AnchorTypeTest) && occ.Scope != RepoLocalAnchorScope {
			t.Fatalf("path anchor %s used scope %q, want repo-local unscoped", syntax, occ.Scope)
		}
		seenTypes[occ.Type] = true
	}

	for _, want := range []AnchorType{
		AnchorTypeInvariant,
		AnchorTypeRisk,
		AnchorTypeProtocol,
		AnchorTypeDomain,
		AnchorTypeDecision,
		AnchorTypeTestContract,
		AnchorTypeDoc,
		AnchorTypeTest,
	} {
		if !seenTypes[want] {
			t.Fatalf("semantic-commenting skill examples missing anchor type %q; seen=%v", want, seenTypes)
		}
	}
	if scopedConceptExamples != 1 {
		t.Fatalf("semantic-commenting skill has %d scoped concept examples, want exactly one configured-scope example", scopedConceptExamples)
	}
}

func TestSemanticCommentingSkillKeepsSafetyContract(t *testing.T) {
	content := readSemanticCommentingSkill(t)
	normalizedContent := normalizeSkillContractText(content)
	required := []string{
		"There are two comment lanes:",
		"`Index:` blocks create broad repoindex soft edges for discoverability.",
		"`[[...]]` semantic anchors create typed, evidence-only semantic edges.",
		"they must not be treated as instruction, policy, or durable authority by themselves.",
		"Use repo-local concept anchors by default: `[[type:slug]]`.",
		"Use explicit scoped concept anchors, `[[scope:type/slug]]`, only when the target repo or indexer defines that scope",
		"do not hardcode one repository name, including `foxctl`, into portable semantic comments.",
		"Configured-scope example, only after confirming the repo/indexer defines `acme` as a valid scope:",
		"Example: `[[acme:protocol/read-guard]]`.",
		"`doc:` and `test:` anchors are repo-local path anchors and must be unscoped:",
		"not `[[project:doc/docs/foo.md]]`",
		"Avoid `beacon` anchors in ordinary code.",
		"Do not add `Index:` blocks to every exported symbol.",
		"For AI-generated comments, score the resulting diff rather than the model's wording:",
	}
	for _, want := range required {
		if !strings.Contains(normalizedContent, normalizeSkillContractText(want)) {
			t.Fatalf("semantic-commenting skill missing required safety guidance %q", want)
		}
	}
}

func TestSemanticCommentingSkillHasNoHardcodedFoxctlExamples(t *testing.T) {
	content := readSemanticCommentingSkill(t)
	foxctlExampleRE := regexp.MustCompile(`(?m)^\s*//\s*\[\[foxctl:`)
	if foxctlExampleRE.MatchString(content) {
		t.Fatal("semantic-commenting skill has a foxctl-scoped positive example; examples must stay repo-portable")
	}
	if strings.Contains(content, "foxctl is the default project scope") {
		t.Fatal("semantic-commenting skill still describes foxctl as the default project scope")
	}
}

func normalizeSkillContractText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func readSemanticCommentingSkill(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	path := filepath.Join(repoRoot, "configs", "skills-pack", "semantic-commenting", "SKILL.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read semantic-commenting skill: %v", err)
	}
	return string(raw)
}
