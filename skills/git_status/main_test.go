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
	files, _, _ := parseStatusOutput(output)

	if countByStatus(files, "M") != 1 {
		t.Errorf("expected 1 modified, got %d", countByStatus(files, "M"))
	}
	if countByStatus(files, "A") != 1 {
		t.Errorf("expected 1 added, got %d", countByStatus(files, "A"))
	}
	if countByStatus(files, "D") != 1 {
		t.Errorf("expected 1 deleted, got %d", countByStatus(files, "D"))
	}
	if countByStatus(files, "?") != 1 {
		t.Errorf("expected 1 untracked, got %d", countByStatus(files, "?"))
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
	stats := parseDiffStat(output)

	if len(stats) != 2 {
		t.Errorf("expected 2 files changed, got %d", len(stats))
	}
	if stats["file1.go"]["additions"] != 5 {
		t.Errorf("expected 5 insertions for file1.go, got %d", stats["file1.go"]["additions"])
	}
	if stats["file1.go"]["deletions"] != 5 {
		t.Errorf("expected 5 deletions for file1.go, got %d", stats["file1.go"]["deletions"])
	}
	if stats["file2.go"]["additions"] != 5 {
		t.Errorf("expected 5 insertions for file2.go, got %d", stats["file2.go"]["additions"])
	}
}
