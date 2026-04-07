package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewShellCommandDefaults(t *testing.T) {
	cmd := newShellCommand()
	if cmd.Use != "shell [-- command...]" {
		t.Fatalf("Use=%q", cmd.Use)
	}
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "bash" {
		t.Fatalf("Aliases=%v want bash", cmd.Aliases)
	}
	if flag := cmd.Flags().Lookup("workspace"); flag == nil {
		t.Fatal("workspace flag missing")
	} else if flag.DefValue != "." {
		t.Fatalf("workspace default=%q want .", flag.DefValue)
	}
	if flag := cmd.Flags().Lookup("command"); flag == nil {
		t.Fatal("command flag missing")
	}
	if flag := cmd.Flags().Lookup("measure"); flag == nil {
		t.Fatal("measure flag missing")
	}
	if flag := cmd.Flags().Lookup("token-model"); flag == nil {
		t.Fatal("token-model flag missing")
	}
	foundReport := false
	for _, child := range cmd.Commands() {
		if child.Name() == "report" {
			foundReport = true
			break
		}
	}
	if !foundReport {
		t.Fatal("report subcommand missing")
	}
	if report := newShellReportCommand(); report.Flags().Lookup("preset") == nil {
		t.Fatal("report preset flag missing")
	}
}

func TestShellHelpMentionsStructuredRouting(t *testing.T) {
	cmd := newShellCommand()
	var buf strings.Builder
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.Help(); err != nil {
		t.Fatalf("Help: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		"git log",
		"find",
		"--measure",
		"go test",
		"Unsupported commands return a structured error",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("help missing %q\n%s", want, got)
		}
	}
}

func TestResolveShellArgv(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		args        []string
		want        []string
		wantErrText string
	}{
		{
			name:    "string command",
			command: `grep -rn "pub fn" src/`,
			want:    []string{"grep", "-rn", "pub fn", "src/"},
		},
		{
			name: "argv",
			args: []string{"ls", "-la", "src/"},
			want: []string{"ls", "-la", "src/"},
		},
		{
			name:        "both disallowed",
			command:     "ls",
			args:        []string{"ls"},
			wantErrText: "either --command or argv",
		},
		{
			name:        "missing",
			wantErrText: "command is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveShellArgv(tc.command, tc.args)
			if tc.wantErrText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrText) {
					t.Fatalf("err=%v want %q", err, tc.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveShellArgv: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("argv[%d]=%q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestLoadShellReportCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.txt")
	if err := os.WriteFile(path, []byte("# comment\n\ngit log --stat -5\nfind internal -name '*.go'\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := loadShellReportCommands(".", []string{"git diff --name-only"}, path, "", "all", 0, "~/.claude/transcripts", "~/.codex")
	if err != nil {
		t.Fatalf("loadShellReportCommands: %v", err)
	}
	want := []string{"git diff --name-only", "git log --stat -5", "find internal -name '*.go'"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Command != want[i] {
			t.Fatalf("command[%d]=%q want %q", i, got[i].Command, want[i])
		}
		if got[i].Weight != 1 {
			t.Fatalf("weight[%d]=%d want 1", i, got[i].Weight)
		}
	}
}

func TestLoadShellReportCommandsPreset(t *testing.T) {
	got, err := loadShellReportCommands(".", nil, "", "typical-bash", "all", 0, "~/.claude/transcripts", "~/.codex")
	if err != nil {
		t.Fatalf("loadShellReportCommands: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected preset commands")
	}
	if got[0].Weight <= 0 || got[0].Operation == "" {
		t.Fatalf("unexpected preset row: %+v", got[0])
	}
}
