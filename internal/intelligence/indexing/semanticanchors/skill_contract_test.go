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

	policy := DefaultAnchorPolicy("foxctl", nil)
	seenTypes := make(map[AnchorType]bool)
	for _, match := range matches {
		syntax := match[1]
		occ, findings := ParseInlineAnchor(policy, syntax)
		if len(findings) != 0 {
			t.Fatalf("skill example %s does not parse cleanly: %+v", syntax, findings)
		}
		if occ.Type == AnchorTypeBeacon {
			t.Fatalf("skill presents beacon anchor as a positive example: %s", syntax)
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
}

func TestSemanticCommentingSkillKeepsSafetyContract(t *testing.T) {
	content := readSemanticCommentingSkill(t)
	normalizedContent := normalizeSkillContractText(content)
	required := []string{
		"There are two comment lanes:",
		"`Index:` blocks create broad repoindex soft edges for discoverability.",
		"`[[...]]` semantic anchors create typed, evidence-only semantic edges.",
		"they must not be treated as instruction, policy, or durable authority by themselves.",
		"`doc:` and `test:` anchors are repo-local path anchors and must be unscoped:",
		"not `[[foxctl:doc/docs/foo.md]]`",
		"Avoid `beacon` anchors in ordinary code.",
		"Do not add `Index:` blocks to every exported symbol.",
	}
	for _, want := range required {
		if !strings.Contains(normalizedContent, normalizeSkillContractText(want)) {
			t.Fatalf("semantic-commenting skill missing required safety guidance %q", want)
		}
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
