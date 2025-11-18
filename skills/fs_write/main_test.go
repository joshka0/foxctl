package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

func newTestContext(t *testing.T, stdout *bytes.Buffer, workspace string) *runner.Context {
	t.Helper()
	t.Setenv("AGENTCTL_WORKSPACE", workspace)
	// We use a separate temp dir for agentctl home (config/cache/cas)
	state := t.TempDir()
	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   state + "/cas",
			Jobs:  state + "/jobs",
			Cache: state + "/cache",
		},
	}
	rc, err := runner.NewContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func TestParseInput(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    input
		wantErr bool
	}{
		{
			name: "valid input",
			json: `{"path": "foo.txt", "content": "bar", "permissions": "0644", "mode": "create"}`,
			want: input{
				Path:        "foo.txt",
				Content:     "bar",
				Permissions: "0644",
				Mode:        "create",
			},
		},
		{
			name:    "invalid json",
			json:    `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := parseInput(bytes.NewBufferString(tt.json))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && in != tt.want {
				t.Errorf("parseInput() = %v, want %v", in, tt.want)
			}
		})
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
		in      input
		wantErr bool
	}{
		{
			name:    "overwrite allowed",
			path:    f.Name(),
			in:      input{Mode: "overwrite"},
			wantErr: false,
		},
		{
			name:    "overwrite denied",
			path:    f.Name(),
			in:      input{Mode: "create"}, // create mode fails if file exists
			wantErr: true,
		},
		{
			name:    "append allowed",
			path:    f.Name(),
			in:      input{Mode: "append"},
			wantErr: false,
		},
		{
			name:    "new file",
			path:    f.Name() + ".new",
			in:      input{Mode: "create"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkWriteMode(tt.path, tt.in.Mode)
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
	rc := newTestContext(t, stdout, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
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
	_ = os.WriteFile(work+"/"+target, []byte("hello"), 0o644)

	stdout := &bytes.Buffer{}
	rc := newTestContext(t, stdout, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
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
