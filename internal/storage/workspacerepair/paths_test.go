package workspacerepair

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/quick"
)

func workspaceRepairQuickConfig() *quick.Config {
	return &quick.Config{MaxCount: 100}
}

func workspaceRepairToken(prefix string, seed uint64) string {
	return prefix + strconv.FormatUint(seed, 36)
}

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

func TestRepairHomePathPropertyExpandsTildeUnderHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	property := func(seed uint64) bool {
		suffix := workspaceRepairToken("repo-", seed)
		raw := "~/" + suffix
		want := filepath.Join(home, suffix)
		if got := RepairHomePath(raw, home); got != want {
			t.Logf("RepairHomePath(%q, %q) = %q, want %q", raw, home, got, want)
			return false
		}
		return true
	}

	if err := quick.Check(property, workspaceRepairQuickConfig()); err != nil {
		t.Fatal(err)
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

func TestResolvePathWorkspacePropertySkipsExistingOpaqueIDDirectories(t *testing.T) {
	root := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir temp root: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	property := func(seed uint64) bool {
		id := "ws-" + workspaceRepairToken("opaque-", seed)
		if err := os.MkdirAll(id, 0o755); err != nil {
			t.Logf("mkdir %q: %v", id, err)
			return false
		}
		if resolved, ok := ResolvePathWorkspace(id, ""); ok {
			t.Logf("ResolvePathWorkspace(%q) = %+v, true; want no repair", id, resolved)
			return false
		}
		return true
	}

	if err := quick.Check(property, workspaceRepairQuickConfig()); err != nil {
		t.Fatal(err)
	}
}
