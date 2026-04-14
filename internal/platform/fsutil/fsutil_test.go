package fsutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFindFilesRespectingGitignoreSkipsIgnoredFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".foxctl/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".foxctl", "exports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.ts"), []byte("export const main = true;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".foxctl", "exports", "eval.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := FindFilesRespectingGitignore(root, "**/*", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("expected non-empty file set")
	}
	for _, file := range files {
		if file == filepath.Clean(filepath.Join(".foxctl", "exports", "eval.json")) {
			t.Fatalf("ignored file leaked into results: %v", files)
		}
	}
	foundMain := false
	for _, file := range files {
		if file == filepath.Clean(filepath.Join("src", "main.ts")) {
			foundMain = true
			break
		}
	}
	if !foundMain {
		t.Fatalf("expected src/main.ts in results: %v", files)
	}
}

func TestFindFilesRespectingGitignoreNestedWorkspaceIgnoresParentRepoPatterns(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoRoot := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run(repoRoot, "init")
	if err := os.WriteFile(filepath.Join(repoRoot, ".gitignore"), []byte("ios/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workspace := filepath.Join(repoRoot, "app")
	widgetDir := filepath.Join(workspace, "src", "features", "widgets", "ios")
	if err := os.MkdirAll(widgetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(widgetDir, "useIosWidgetGate.ts")
	if err := os.WriteFile(target, []byte("export const gate = true;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := FindFilesRespectingGitignore(workspace, "**/*.ts", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join("src", "features", "widgets", "ios", "useIosWidgetGate.ts"))
	found := false
	for _, file := range files {
		if file == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected nested workspace file in results, got %v", files)
	}
}
