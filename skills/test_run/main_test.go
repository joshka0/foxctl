package main

import (
	"strings"
	"testing"
)

func TestSummarizePytest(t *testing.T) {
	got := summarizePytest("=== 2 passed, 1 skipped in 0.12s ===", "", 0)
	if got["passed"] != 2 || got["skipped"] != 1 || got["failed"] != 0 {
		t.Fatalf("unexpected pytest summary: %v", got)
	}
	if got["runner_status"] != "pass" {
		t.Fatalf("runner_status=%v want pass", got["runner_status"])
	}
}

func TestSummarizeNPM(t *testing.T) {
	stdout := strings.Join([]string{
		"Test Suites: 1 failed, 2 passed, 3 total",
		"Tests:       4 failed, 10 passed, 14 total",
	}, "\n")
	got := summarizeNPM(stdout, "", 1)
	if got["failed"] != 4 || got["passed"] != 10 {
		t.Fatalf("unexpected npm test counts: %v", got)
	}
	if got["failed_suites"] != 1 || got["total_suites"] != 3 {
		t.Fatalf("unexpected npm suite counts: %v", got)
	}
	if got["runner_status"] != "fail" {
		t.Fatalf("runner_status=%v want fail", got["runner_status"])
	}
}

func TestParseNamedCounts(t *testing.T) {
	got := parseNamedCounts("2 passed, 1 skipped, 3 total", []string{"passed", "skipped", "total"})
	if got["passed"] != 2 || got["skipped"] != 1 || got["total"] != 3 {
		t.Fatalf("unexpected counts: %v", got)
	}
}

func TestSummarizeCargo(t *testing.T) {
	got := summarizeCargo("test result: ok. 3 passed; 0 failed; 1 ignored; 0 measured; 0 filtered out", "", 0)
	if got["passed"] != 3 || got["failed"] != 0 || got["skipped"] != 1 {
		t.Fatalf("unexpected cargo summary: %v", got)
	}
	if got["runner_status"] != "pass" {
		t.Fatalf("runner_status=%v want pass", got["runner_status"])
	}
}
