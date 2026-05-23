package main

import "testing"

func TestParseNameStatusHandlesRename(t *testing.T) {
	raw := "M\x00internal/a.go\x00R100\x00internal/old.go\x00internal/new.go\x00"

	got := parseNameStatus(raw)
	if len(got) != 2 {
		t.Fatalf("change count=%d want 2: %#v", len(got), got)
	}
	if got[0].Status != "M" || got[0].Path != "internal/a.go" {
		t.Fatalf("first change=%+v want modified internal/a.go", got[0])
	}
	if got[1].Status != "R100" || got[1].OldPath != "internal/old.go" || got[1].Path != "internal/new.go" {
		t.Fatalf("rename change=%+v want old->new", got[1])
	}
}

func TestParseNumstatHandlesNormalAndBinaryFiles(t *testing.T) {
	raw := "10\t2\tinternal/a.go\x00-\t-\tassets/image.png\x00"

	got := parseNumstat(raw)
	if got["internal/a.go"].additions != 10 || got["internal/a.go"].deletions != 2 {
		t.Fatalf("stat for a.go=%+v want 10/2", got["internal/a.go"])
	}
	if got["assets/image.png"].additions != 0 || got["assets/image.png"].deletions != 0 {
		t.Fatalf("stat for binary=%+v want 0/0", got["assets/image.png"])
	}
}

func TestParseNumstatHandlesRenameNULFormat(t *testing.T) {
	raw := "3\t1\t\x00internal/old.go\x00internal/new.go\x001\t0\tinternal/next.go\x00"

	got := parseNumstat(raw)
	for _, path := range []string{"internal/old.go", "internal/new.go"} {
		if got[path].additions != 3 || got[path].deletions != 1 {
			t.Fatalf("stat for %s=%+v want 3/1", path, got[path])
		}
	}
	if got["internal/next.go"].additions != 1 || got["internal/next.go"].deletions != 0 {
		t.Fatalf("stat for next=%+v want 1/0", got["internal/next.go"])
	}
}

func TestDiffRangeArgsUsesCanonicalRefs(t *testing.T) {
	got := diffRangeArgs(Input{BaseRef: "main", HeadRef: "HEAD"})
	if len(got) != 1 || got[0] != "main...HEAD" {
		t.Fatalf("range args=%v want main...HEAD", got)
	}
}
