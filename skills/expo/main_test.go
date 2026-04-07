package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectExpoProjectInfo(t *testing.T) {
	dir := t.TempDir()
	pkg := []byte(`{
  "name": "demo-app",
  "dependencies": {
    "expo": "~54.0.0",
    "react-native": "0.81.0"
  }
}`)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), pkg, 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	info := detectExpoProjectInfo(dir)
	if got := info["type"]; got != "expo" {
		t.Fatalf("type=%v want expo", got)
	}
	if got := info["name"]; got != "demo-app" {
		t.Fatalf("name=%v want demo-app", got)
	}
	if got := info["has_expo_dependency"]; got != true {
		t.Fatalf("has_expo_dependency=%v want true", got)
	}
	if got := info["has_react_native_dependency"]; got != true {
		t.Fatalf("has_react_native_dependency=%v want true", got)
	}
}

func TestAnyRecentLogSource(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	sources := []map[string]any{
		{
			"path":        "/tmp/debug.log",
			"exists":      true,
			"modified_at": now.Add(-5 * time.Minute).Format(time.RFC3339),
		},
	}
	if !anyRecentLogSource(sources, now, 15*time.Minute) {
		t.Fatal("expected recent log source to be detected")
	}
	if anyRecentLogSource(sources, now, 2*time.Minute) {
		t.Fatal("expected source to be outside tighter window")
	}
}

func TestCollectExpoLogsNoFiles(t *testing.T) {
	dir := t.TempDir()
	got := collectExpoLogs(dir, "", 10)
	if got["success"] != false {
		t.Fatalf("success=%v want false", got["success"])
	}
	if got["message"] == "" {
		t.Fatal("expected missing-logs message")
	}
}

func TestMetroProcessMatches(t *testing.T) {
	lines := []string{
		"12345 node /opt/homebrew/bin/expo start --clear",
		"12346 node /opt/homebrew/bin/react-native start",
		"12347 some-other-process",
	}
	got := metroProcessMatches(lines)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0]["pid"] != "12345" {
		t.Fatalf("pid=%v want 12345", got[0]["pid"])
	}
}
