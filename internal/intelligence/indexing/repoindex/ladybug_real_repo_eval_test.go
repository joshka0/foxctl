package repoindex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLadybugRealRepoNavigationEval(t *testing.T) {
	if os.Getenv("FOXCTL_LADYBUG_EVAL") != "1" {
		t.Skip("set FOXCTL_LADYBUG_EVAL=1 to run the real-repo Ladybug navigation eval")
	}
	lbugBin := os.Getenv("LBUG_BIN")
	if lbugBin == "" {
		var err error
		lbugBin, err = exec.LookPath("lbug")
		if err != nil {
			t.Fatal("lbug binary not found; set LBUG_BIN")
		}
	}

	ctx := context.Background()
	repoRoot := ladybugRepoRoot(t)
	storageRoot := os.Getenv("FOXCTL_LADYBUG_EVAL_STORAGE_ROOT")
	if storageRoot == "" {
		home := os.Getenv("HOME")
		if home == "" {
			t.Fatal("HOME is required to locate ~/.foxctl/storage; set FOXCTL_LADYBUG_EVAL_STORAGE_ROOT")
		}
		storageRoot = filepath.Join(home, ".foxctl", "storage")
	}

	store, err := Open(ctx, storageRoot, repoRoot)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("repoindex stats: %v", err)
	}
	minNodes := ladybugEvalIntEnv(t, "FOXCTL_LADYBUG_EVAL_MIN_NODES", 1000)
	if stats.NodesTotal < minNodes {
		t.Fatalf("repoindex has %d nodes, want at least %d; build it first with `go run ./cmd/foxctl index repo build --workspace . --go --typescript --include-tests --semantic-anchors --terraform --kubernetes --shell`", stats.NodesTotal, minNodes)
	}

	start := time.Now()
	snapshot, err := store.ExportSnapshot(ctx)
	if err != nil {
		t.Fatalf("export snapshot: %v", err)
	}
	snapshotDuration := time.Since(start)

	buildExportID := findLadybugEvalNodeID(t, snapshot, NodeSymbol, "BuildLadybugExport", "internal/intelligence/indexing/repoindex/ladybug_export.go")
	testWriterID := findLadybugEvalNodeID(t, snapshot, NodeSymbol, "TestWriteLadybugExportReplaceAll", "internal/intelligence/indexing/repoindex/ladybug_export_test.go")
	writeExportID := findLadybugEvalNodeID(t, snapshot, NodeSymbol, "WriteLadybugExport", "internal/intelligence/indexing/repoindex/ladybug_export_writer.go")
	exportFileID := findLadybugEvalNodeID(t, snapshot, NodeFile, "ladybug_export.go", "internal/intelligence/indexing/repoindex/ladybug_export.go")

	start = time.Now()
	artifact, err := BuildLadybugExport(snapshot, time.Now().UTC())
	if err != nil {
		t.Fatalf("build ladybug export: %v", err)
	}
	buildDuration := time.Since(start)

	root := t.TempDir()
	exportDir := filepath.Join(root, "export")
	dbPath := filepath.Join(root, "projection.lbug")
	homeDir := filepath.Join(root, "home")
	start = time.Now()
	if err := WriteLadybugExport(exportDir, artifact); err != nil {
		t.Fatalf("write ladybug export: %v", err)
	}
	writeDuration := time.Since(start)

	rebuildOutput, rebuildDuration := runLadybugEvalCommand(t, ladybugRebuildScriptPath(t),
		"--lbug-bin", lbugBin,
		"--export-dir", exportDir,
		"--db-path", dbPath,
		"--home", homeDir,
	)
	assertContainsAll(t, rebuildOutput, []string{`"node_count":` + strconv.Itoa(len(snapshot.Nodes)+1)})

	traceOutput, traceDuration := runLadybugEvalCommand(t, ladybugScriptPath(t, "ladybug_trace_path.sh"),
		"--lbug-bin", lbugBin,
		"--db-path", dbPath,
		"--home", homeDir,
		"--src-id", testWriterID,
		"--dst-id", writeExportID,
		"--max-depth", "4",
	)
	assertContainsAll(t, traceOutput, []string{`"path_len":1`, `"node_names"`, `TestWriteLadybugExportReplaceAll`, `WriteLadybugExport`})

	smartOutput, smartDuration := runLadybugEvalCommand(t, ladybugScriptPath(t, "ladybug_smart_context.sh"),
		"--lbug-bin", lbugBin,
		"--db-path", dbPath,
		"--home", homeDir,
		"--node-id", buildExportID,
	)
	assertContainsAll(t, smartOutput, []string{`"section":"callees"`, `"section":"callers"`, `"src_name"`, `"dst_name"`, `buildLadybugGraphNodeCSV`})

	blastOutput, blastDuration := runLadybugEvalCommand(t, ladybugScriptPath(t, "ladybug_blast_radius.sh"),
		"--lbug-bin", lbugBin,
		"--db-path", dbPath,
		"--home", homeDir,
		"--node-id", exportFileID,
		"--max-depth", "2",
		"--limit", "20",
	)
	assertContainsAll(t, blastOutput, []string{`"section":"forward_reason"`, `"node_names"`, `BuildLadybugExport`})

	ladybugAssertEvalDuration(t, "snapshot", snapshotDuration, "FOXCTL_LADYBUG_EVAL_MAX_SNAPSHOT_MS", 10000)
	ladybugAssertEvalDuration(t, "build_export", buildDuration, "FOXCTL_LADYBUG_EVAL_MAX_BUILD_EXPORT_MS", 5000)
	ladybugAssertEvalDuration(t, "write_export", writeDuration, "FOXCTL_LADYBUG_EVAL_MAX_WRITE_MS", 5000)
	ladybugAssertEvalDuration(t, "rebuild", rebuildDuration, "FOXCTL_LADYBUG_EVAL_MAX_REBUILD_MS", 15000)
	ladybugAssertEvalDuration(t, "trace_path", traceDuration, "FOXCTL_LADYBUG_EVAL_MAX_TRACE_MS", 2000)
	ladybugAssertEvalDuration(t, "smart_context", smartDuration, "FOXCTL_LADYBUG_EVAL_MAX_SMART_MS", 3000)
	ladybugAssertEvalDuration(t, "blast_radius", blastDuration, "FOXCTL_LADYBUG_EVAL_MAX_BLAST_MS", 3000)

	t.Logf("ladybug_eval repo_root=%s nodes=%d edges=%d artifact_files=%d", repoRoot, stats.NodesTotal, stats.EdgesTotal, len(artifact.Files))
	t.Logf("ladybug_eval durations snapshot=%s build_export=%s write=%s rebuild=%s trace=%s smart=%s blast=%s", snapshotDuration, buildDuration, writeDuration, rebuildDuration, traceDuration, smartDuration, blastDuration)
}

func ladybugRepoRoot(t *testing.T) string {
	t.Helper()
	script := ladybugRebuildScriptPath(t)
	return filepath.Dir(filepath.Dir(script))
}

func findLadybugEvalNodeID(t *testing.T, snapshot ExportSnapshot, kind NodeKind, name, file string) string {
	t.Helper()
	for _, node := range snapshot.Nodes {
		if node.Kind == kind && node.Name == name && node.File == file {
			return node.ID
		}
	}
	t.Fatalf("missing eval node kind=%s name=%q file=%q", kind, name, file)
	return ""
}

func runLadybugEvalCommand(t *testing.T, name string, args ...string) (string, time.Duration) {
	t.Helper()
	start := time.Now()
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)
	if err != nil {
		t.Fatalf("%s failed after %s: %v\n%s", filepath.Base(name), duration, err, output)
	}
	return string(output), duration
}

func ladybugAssertEvalDuration(t *testing.T, name string, got time.Duration, env string, defaultMaxMS int) {
	t.Helper()
	maxMS := ladybugEvalIntEnv(t, env, defaultMaxMS)
	max := time.Duration(maxMS) * time.Millisecond
	if got > max {
		t.Fatalf("%s took %s, exceeds %s=%s", name, got, env, max)
	}
}

func ladybugEvalIntEnv(t *testing.T, name string, defaultValue int) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		t.Fatalf("%s must be a positive integer, got %q", name, raw)
	}
	return value
}
