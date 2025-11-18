package main

import (
	"testing"
)

func TestParseInput(_ *testing.T) {
	// Test valid input parsing
}

func TestTruncate(t *testing.T) {
	s := "hello world"
	if got := truncate(s, 5); got != "hello... (truncated)" {
		t.Errorf("truncate failed: got %s", got)
	}
}

func TestGetExitCode(t *testing.T) {
	// Basic coverage for helper
	code := getExitCode(nil)
	if code != 1 {
		t.Errorf("expected 1 for nil error (default fallback behavior in helper checks), got %d", code)
	}
}
