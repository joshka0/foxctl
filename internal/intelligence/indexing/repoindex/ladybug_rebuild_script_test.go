package repoindex

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLadybugRebuildScriptRejectsUnknownEdgeCSV(t *testing.T) {
	exportDir := t.TempDir()
	snapshot, exportedAt, _ := ladybugExportFixture()
	artifact, err := BuildLadybugExport(snapshot, exportedAt)
	if err != nil {
		t.Fatalf("build export: %v", err)
	}
	if err := WriteLadybugExport(exportDir, artifact); err != nil {
		t.Fatalf("write export: %v", err)
	}
	if err := os.WriteFile(filepath.Join(exportDir, "edges_surprise.csv"), []byte("src_id,dst_id,weight,source,meta_json\n"), 0o644); err != nil {
		t.Fatalf("write unknown edge csv: %v", err)
	}

	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Fatalf("false binary not found: %v", err)
	}
	script := ladybugRebuildScriptPath(t)
	cmd := exec.Command(script,
		"--lbug-bin", falseBin,
		"--export-dir", exportDir,
		"--db-path", filepath.Join(t.TempDir(), "projection.lbug"),
		"--home", filepath.Join(t.TempDir(), "home"),
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected script failure for unknown edge csv, output:\n%s", output)
	}
	if !strings.Contains(string(output), "unsupported edge CSV: edges_surprise.csv") {
		t.Fatalf("output=%s want unsupported edge CSV error", output)
	}
}

func TestLadybugRebuildScriptRoundTrip(t *testing.T) {
	if os.Getenv("FOXCTL_LADYBUG_TEST") != "1" {
		t.Skip("set FOXCTL_LADYBUG_TEST=1 to run Ladybug CLI integration test")
	}
	lbugBin := os.Getenv("LBUG_BIN")
	if lbugBin == "" {
		var err error
		lbugBin, err = exec.LookPath("lbug")
		if err != nil {
			t.Skip("lbug binary not found; set LBUG_BIN")
		}
	}

	exportDir := t.TempDir()
	snapshot, exportedAt, _ := ladybugExportFixture()
	artifact, err := BuildLadybugExport(snapshot, exportedAt)
	if err != nil {
		t.Fatalf("build export: %v", err)
	}
	if err := WriteLadybugExport(exportDir, artifact); err != nil {
		t.Fatalf("write export: %v", err)
	}

	root := t.TempDir()
	dbPath := filepath.Join(root, "projection.lbug")
	homeDir := filepath.Join(root, "home")
	cmd := exec.Command(ladybugRebuildScriptPath(t),
		"--lbug-bin", lbugBin,
		"--export-dir", exportDir,
		"--db-path", dbPath,
		"--home", homeDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rebuild projection: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), `"node_count":7`) {
		t.Fatalf("rebuild output missing node count:\n%s", output)
	}

	query := `:mode json
MATCH (n:GraphNode) RETURN count(n) AS node_count;
MATCH ()-[r:Contains]->() RETURN count(r) AS contains_count;
MATCH p = (a:GraphNode)-[e:Calls* SHORTEST 1..3]->(b:GraphNode)
WHERE a.id = "repo-fixture::sym:go:example:A" AND b.id = "repo-fixture::sym:go:example:B"
RETURN length(e) AS path_len, properties(nodes(p), 'id') AS node_ids;
`
	readOnly := exec.Command(lbugBin, dbPath, "-r", "-b", "-s")
	readOnly.Env = append(os.Environ(), "HOME="+homeDir)
	readOnly.Stdin = strings.NewReader(query)
	queryOutput, err := readOnly.CombinedOutput()
	if err != nil {
		t.Fatalf("read-only query: %v\n%s", err, queryOutput)
	}
	for _, want := range []string{`"node_count":7`, `"contains_count":1`, `"path_len":1`} {
		if !strings.Contains(string(queryOutput), want) {
			t.Fatalf("query output missing %s:\n%s", want, queryOutput)
		}
	}
}

func ladybugRebuildScriptPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "scripts", "ladybug_rebuild_projection.sh")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("scripts/ladybug_rebuild_projection.sh not found from %s", dir)
		}
		dir = parent
	}
}
