package obsidian

import "testing"

func TestPolicyValidateCreate(t *testing.T) {
	p := DefaultPolicy()
	for _, path := range []string{
		"inbox/drafted-from-agentctl/test.md",
		"sessions/2026-03/test.md",
		"ops/exported-sessions/test.md",
	} {
		if err := p.ValidateCreate(path); err != nil {
			t.Fatalf("ValidateCreate(%q) unexpected error: %v", path, err)
		}
	}
	if err := p.ValidateCreate("notes/patterns/canonical.md"); err == nil {
		t.Fatalf("expected canonical create to be denied")
	}
}

func TestPolicyValidateAppend(t *testing.T) {
	p := DefaultPolicy()
	if err := p.ValidateAppend("notes/patterns/canonical.md", "Recent Findings"); err != nil {
		t.Fatalf("ValidateAppend unexpected error: %v", err)
	}
	if err := p.ValidateAppend("notes/patterns/canonical.md", "Conclusion"); err == nil {
		t.Fatalf("expected append to disallowed heading to fail")
	}
}

func TestPolicyValidateReviewedMergeTarget(t *testing.T) {
	p := DefaultPolicy()
	if err := p.ValidateReviewedMergeTarget("notes/patterns/canonical.md", "Review"); err != nil {
		t.Fatalf("ValidateReviewedMergeTarget unexpected error: %v", err)
	}
	if err := p.ValidateReviewedMergeTarget("notes/patterns/canonical.md", "Conclusion"); err == nil {
		t.Fatalf("expected reviewed merge to disallowed heading to fail")
	}
	if err := p.ValidateReviewedMergeTarget("inbox/drafted-from-agentctl/test.md", "Review"); err == nil {
		t.Fatalf("expected reviewed merge outside canonical prefixes to fail")
	}
}

func TestPolicyValidateReviewedMerge(t *testing.T) {
	p := DefaultPolicy()
	if err := p.ValidateReviewedMerge("inbox/drafted-from-agentctl/test.md", "notes/patterns/canonical.md", "Review"); err != nil {
		t.Fatalf("ValidateReviewedMerge unexpected error: %v", err)
	}
	if err := p.ValidateReviewedMerge("notes/patterns/draft.md", "notes/patterns/canonical.md", "Review"); err == nil {
		t.Fatalf("expected non-inbox draft to be denied")
	}
	if err := p.ValidateReviewedMerge("inbox/drafted-from-agentctl/test.md", "sessions/test.md", "Review"); err == nil {
		t.Fatalf("expected non-canonical target to be denied")
	}
	if err := p.ValidateReviewedMerge("inbox/drafted-from-agentctl/test.md", "notes/patterns/canonical.md", "Recent Sessions"); err == nil {
		t.Fatalf("expected disallowed reviewed merge heading to be denied")
	}
}
