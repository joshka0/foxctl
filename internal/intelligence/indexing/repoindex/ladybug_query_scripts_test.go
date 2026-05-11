package repoindex

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLadybugQueryProofScripts(t *testing.T) {
	if os.Getenv("FOXCTL_LADYBUG_TEST") != "1" {
		t.Skip("set FOXCTL_LADYBUG_TEST=1 to run Ladybug CLI query proof test")
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
	rebuild := exec.Command(ladybugRebuildScriptPath(t),
		"--lbug-bin", lbugBin,
		"--export-dir", exportDir,
		"--db-path", dbPath,
		"--home", homeDir,
	)
	if output, err := rebuild.CombinedOutput(); err != nil {
		t.Fatalf("rebuild projection: %v\n%s", err, output)
	}

	trace := runLadybugProofScript(t, "ladybug_trace_path.sh", lbugBin, dbPath, homeDir,
		"--src-id", "repo-fixture::sym:go:example:A",
		"--dst-id", "repo-fixture::sym:go:example:B",
		"--max-depth", "3",
	)
	assertContainsAll(t, trace, []string{
		`"path_len":1`,
		`"node_names":["A","B"]`,
		`"node_files":["internal/a.go","internal/b.go"]`,
		`repo-fixture::sym:go:example:A`,
		`repo-fixture::sym:go:example:B`,
	})

	smartContext := runLadybugProofScript(t, "ladybug_smart_context.sh", lbugBin, dbPath, homeDir,
		"--node-id", "repo-fixture::sym:go:example:A",
		"--limit", "1",
	)
	assertContainsAll(t, smartContext, []string{
		`"section":"self"`,
		`"section":"callees"`,
		`"section":"docs_concepts"`,
		`"edge_type":"CALLS"`,
		`"edge_type":"DESCRIBED_BY"`,
		`"src_name":"A"`,
		`"dst_name":"B"`,
		`"dst_name":"alpha"`,
	})

	blastRadius := runLadybugProofScript(t, "ladybug_blast_radius.sh", lbugBin, dbPath, homeDir,
		"--node-id", "repo-fixture::file:go:example:internal/a.go",
		"--max-depth", "3",
		"--limit", "10",
	)
	assertContainsAll(t, blastRadius, []string{
		`"node_names":["a.go","A","B"]`,
		`repo-fixture::sym:go:example:B`,
		`"edge_type":"CO_CHANGES_WITH"`,
		`repo-fixture::file:go:example:internal/b.go`,
	})
}

func runLadybugProofScript(t *testing.T, scriptName, lbugBin, dbPath, homeDir string, args ...string) string {
	t.Helper()
	baseArgs := []string{
		"--lbug-bin", lbugBin,
		"--db-path", dbPath,
		"--home", homeDir,
	}
	cmd := exec.Command(ladybugScriptPath(t, scriptName), append(baseArgs, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", scriptName, err, output)
	}
	return string(output)
}

func assertContainsAll(t *testing.T, got string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}
