package executil

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHasRunnableToolRejectsBrokenShebang(t *testing.T) {
	dir := t.TempDir()
	toolPath := filepath.Join(dir, "broken-tool")
	script := "#!/definitely/missing/interpreter\nexit 0\n"
	if err := os.WriteFile(toolPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)

	if HasTool("broken-tool") == false {
		t.Fatal("expected broken-tool to resolve in PATH")
	}
	if HasRunnableTool(context.Background(), "broken-tool", "--help") {
		t.Fatal("expected broken-tool to be treated as non-runnable")
	}
}
