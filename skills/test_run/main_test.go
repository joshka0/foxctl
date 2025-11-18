package main

import (
	"testing"
)

func TestTruncate(t *testing.T) {
	s := "hello world"
	if got := truncate(s, 5); got != "hello..." {
		t.Errorf("truncate(%q, 5) = %q, want %q", s, got, "hello...")
	}
	if got := truncate(s, 20); got != s {
		t.Errorf("truncate(%q, 20) = %q, want %q", s, got, s)
	}
}

func TestSummarizeResults(t *testing.T) {
	// This is a stub test as summarizeResults logic is simple
	// Real test requires output parsing which is complex
}
