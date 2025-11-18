package main

import (
	"testing"
)

func TestParseStatusOutput(t *testing.T) {
	output := `## main...origin/main
 M file1.go
A  file2.go
 D file3.go
?? untracked.go
`
	stats, files := parseStatusOutput([]byte(output), 10)
	
	if stats["modified"] != 1 {
		t.Errorf("expected 1 modified, got %d", stats["modified"])
	}
	if stats["added"] != 1 {
		t.Errorf("expected 1 added, got %d", stats["added"])
	}
	if stats["deleted"] != 1 {
		t.Errorf("expected 1 deleted, got %d", stats["deleted"])
	}
	if stats["untracked"] != 1 {
		t.Errorf("expected 1 untracked, got %d", stats["untracked"])
	}
	
	if len(files) != 4 {
		t.Errorf("expected 4 files, got %d", len(files))
	}
}

func TestParseDiffStat(t *testing.T) {
	output := ` file1.go | 10 +++++-----
 file2.go |  5 +++++
 2 files changed, 10 insertions(+), 5 deletions(-)
`
	stats := parseDiffStat([]byte(output))
	
	if stats["files_changed"] != 2 {
		t.Errorf("expected 2 files changed, got %d", stats["files_changed"])
	}
	if stats["insertions"] != 10 {
		t.Errorf("expected 10 insertions, got %d", stats["insertions"])
	}
	if stats["deletions"] != 5 {
		t.Errorf("expected 5 deletions, got %d", stats["deletions"])
	}
}
