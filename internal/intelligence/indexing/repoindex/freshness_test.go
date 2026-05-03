package repoindex

import "testing"

func TestCompareIndexFreshnessCurrent(t *testing.T) {
	meta := IndexMeta{
		HeadSHA:         "head",
		WorktreeDirty:   true,
		DirtyStatusHash: "dirty",
	}
	current := GitSnapshot{
		HeadSHA:         "head",
		WorktreeDirty:   true,
		DirtyStatusHash: "dirty",
	}
	got := CompareIndexFreshness(meta, current)
	if got.Level != FreshnessCurrent {
		t.Fatalf("level=%s want current: %#v", got.Level, got)
	}
}

func TestCompareIndexFreshnessDetectsHeadMismatch(t *testing.T) {
	got := CompareIndexFreshness(IndexMeta{HeadSHA: "old"}, GitSnapshot{HeadSHA: "new"})
	if got.Level != FreshnessStale {
		t.Fatalf("level=%s want stale: %#v", got.Level, got)
	}
	if !hasReason(got.Reasons, "head_mismatch") {
		t.Fatalf("reasons=%v missing head_mismatch", got.Reasons)
	}
}

func TestCompareIndexFreshnessDetectsDirtyChange(t *testing.T) {
	got := CompareIndexFreshness(
		IndexMeta{HeadSHA: "head", WorktreeDirty: false},
		GitSnapshot{HeadSHA: "head", WorktreeDirty: true, DirtyStatusHash: "dirty"},
	)
	if got.Level != FreshnessDirty {
		t.Fatalf("level=%s want dirty: %#v", got.Level, got)
	}
}

func TestCompareIndexFreshnessDetectsBehindDefaultRef(t *testing.T) {
	got := CompareIndexFreshness(
		IndexMeta{HeadSHA: "head"},
		GitSnapshot{HeadSHA: "head", DefaultRef: "origin/main", DefaultRefSHA: "default", CommitsBehind: 4},
	)
	if got.Level != FreshnessBehind {
		t.Fatalf("level=%s want behind: %#v", got.Level, got)
	}
	if got.CommitsBehind != 4 {
		t.Fatalf("commits behind=%d want 4", got.CommitsBehind)
	}
}

func hasReason(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
