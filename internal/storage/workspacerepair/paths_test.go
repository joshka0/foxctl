package workspacerepair

import (
	"path/filepath"
	"testing"
)

func TestRepairHomePath(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		userHome string
		want     string
	}{
		{
			name:     "expands tilde",
			raw:      "~/repo",
			userHome: "/tmp/home",
			want:     filepath.Join("/tmp/home", "repo"),
		},
		{
			name:     "empty home leaves raw",
			raw:      "~/repo",
			userHome: "",
			want:     "~/repo",
		},
		{
			name:     "same mac user leaves raw",
			raw:      "/Users/alice/repo",
			userHome: "/Users/alice",
			want:     "/Users/alice/repo",
		},
		{
			name:     "missing old mac home rewrites user",
			raw:      "/Users/foxctl-old-user/repo",
			userHome: "/Users/foxctl-new-user",
			want:     "/Users/foxctl-new-user/repo",
		},
		{
			name:     "trims whitespace",
			raw:      "  ~/repo  ",
			userHome: "/tmp/home",
			want:     filepath.Join("/tmp/home", "repo"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepairHomePath(tt.raw, tt.userHome); got != tt.want {
				t.Fatalf("RepairHomePath()=%q want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePathWorkspace(t *testing.T) {
	workspaceDir := t.TempDir()
	resolved, ok := ResolvePathWorkspace("  "+workspaceDir+"  ", "")
	if !ok {
		t.Fatal("ResolvePathWorkspace() ok=false, want true")
	}
	if resolved.RawPath != workspaceDir {
		t.Fatalf("RawPath=%q want %q", resolved.RawPath, workspaceDir)
	}
	if resolved.EffectivePath != workspaceDir {
		t.Fatalf("EffectivePath=%q want %q", resolved.EffectivePath, workspaceDir)
	}
	if resolved.WorkspaceID == "" || resolved.WorkspaceID == workspaceDir {
		t.Fatalf("WorkspaceID=%q must be stable non-path ID", resolved.WorkspaceID)
	}

	if _, ok := ResolvePathWorkspace("ws-golden", ""); ok {
		t.Fatal("ResolvePathWorkspace() ok=true for workspace ID")
	}
	if _, ok := ResolvePathWorkspace(filepath.Join(t.TempDir(), "missing"), ""); ok {
		t.Fatal("ResolvePathWorkspace() ok=true for missing path")
	}
}
