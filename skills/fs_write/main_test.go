package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/policy"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/rs/zerolog"
)

func newTestRunContext(t *testing.T, stdout *bytes.Buffer, workspace string) *skillmain.RunContext {
	t.Helper()
	t.Setenv("AGENTCTL_WORKSPACE", workspace)
	state := t.TempDir()
	casPath := filepath.Join(state, "cas")
	casStore, err := cas.NewStore(casPath)
	if err != nil {
		t.Fatalf("open cas: %v", err)
	}

	pv, err := policy.NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("path validator: %v", err)
	}

	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   casPath,
			Jobs:  filepath.Join(state, "jobs"),
			Cache: filepath.Join(state, "cache"),
		},
	}

	return &skillmain.RunContext{
		Config:        cfg,
		CASStore:      casStore,
		Workspace:     workspace,
		Logger:        zerolog.Nop(),
		PathValidator: pv,
		Validator:     validator.New(),
		Stdout:        stdout,
		Now:           time.Now,
		InlineKB:      cfg.InlineOutputKB,
		MaxPreview:    100,
	}
}

func TestCheckWriteMode(t *testing.T) {
	// Create temp file
	f, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { errs.Ignore(os.Remove(f.Name()), "cleanup") }()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		mode    string
		wantErr bool
	}{
		{
			name:    "overwrite allowed",
			path:    f.Name(),
			mode:    "overwrite",
			wantErr: false,
		},
		{
			name:    "overwrite denied",
			path:    f.Name(),
			mode:    "create", // create mode fails if file exists
			wantErr: true,
		},
		{
			name:    "append allowed",
			path:    f.Name(),
			mode:    "append",
			wantErr: false,
		},
		{
			name:    "new file",
			path:    f.Name() + ".new",
			mode:    "create",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkWriteMode(tt.path, tt.mode)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkWriteMode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunFsWrite(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()

	target := "output.txt"

	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Path:        target,
		Content:     "hello world",
		Mode:        "create",
		Permissions: "0644",
	}

	if err := run(ctx, rc, in); err != nil {
		t.Errorf("run failed: %v", err)
	}

	// Verify file content
	content, err := os.ReadFile(work + "/" + target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(content))
	}

	// Check output envelope
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data := env["data"].(map[string]any)

	if data["bytes_written"].(float64) != 11 {
		t.Errorf("expected 11 bytes written, got %v. Path: %v", data["bytes_written"], data["path"])
	}
}

func TestRunFsWriteAppend(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()

	target := "output.txt"
	if err := os.WriteFile(work+"/"+target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("setup test file: %v", err)
	}

	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Path:        target,
		Content:     " world",
		Mode:        "append",
		Permissions: "0644",
	}

	if err := run(ctx, rc, in); err != nil {
		t.Errorf("run failed: %v", err)
	}

	// Verify file content
	content, err := os.ReadFile(work + "/" + target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(content))
	}
}
