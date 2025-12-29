package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexCommand_Init(t *testing.T) {
	cmd := newIndexCommand()
	if cmd.Use != "index" {
		t.Fatalf("expected Use to be %q, got %q", "index", cmd.Use)
	}

	subCmds := cmd.Commands()
	var hasInit, hasStatus bool
	for _, sub := range subCmds {
		switch sub.Use {
		case "init":
			hasInit = true
		case "status":
			hasStatus = true
		}
	}

	if !hasInit || !hasStatus {
		t.Fatalf("expected init and status subcommands, got init=%v status=%v", hasInit, hasStatus)
	}
}

func TestIndexInit_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	cmd := newIndexInitCommand()
	cmd.SetContext(context.Background())

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	cmd.SetArgs([]string{
		"--workspace", tmpDir,
		"--scope", "symbols,memory,tasks,sessions",
		"--dry-run",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v; output: %s", err, buf.String())
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("parse envelope: %v; output: %s", err, buf.String())
	}

	if env["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", env["status"])
	}

	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data map, got %T", env["data"])
	}
	if data["dry_run"] != true {
		t.Fatalf("expected dry_run true, got %v", data["dry_run"])
	}

	scopes, ok := data["scopes"].([]any)
	if !ok || len(scopes) != 4 {
		t.Fatalf("expected 4 scopes, got %v", data["scopes"])
	}

	foundSymbols := false
	for _, s := range scopes {
		scopeMap, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("expected scope map, got %T", s)
		}
		if scopeMap["scope"] == "symbols" {
			foundSymbols = true
			if _, ok := scopeMap["files_count"]; !ok {
				t.Fatalf("expected files_count for symbols scope, got %v", scopeMap)
			}
		}
	}
	if !foundSymbols {
		t.Fatal("symbols scope not found in output")
	}
}
