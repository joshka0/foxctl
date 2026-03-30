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
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".agentctl/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agentctl", "exports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.ts"), []byte("export const main = true;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agentctl", "exports", "eval.json"), []byte("{}\n"), 0o644); err != nil {
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
		if file == filepath.Clean(filepath.Join(".agentctl", "exports", "eval.json")) {
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
