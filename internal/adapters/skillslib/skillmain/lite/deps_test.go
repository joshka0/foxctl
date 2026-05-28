package lite

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// forbiddenPrefixes lists internal package trees that the lite package must
// not directly import. These are the heavy monoliths we are splitting away
// from: storage, runtime, intelligence, and context.
var forbiddenPrefixes = []string{
	"github.com/joshka0/foxctl/internal/storage",
	"github.com/joshka0/foxctl/internal/runtime",
	"github.com/joshka0/foxctl/internal/intelligence",
	"github.com/joshka0/foxctl/internal/context",
}

func packageDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

// TestNoDirectForbiddenImports verifies that the lite package does not
// directly import any forbidden internal package.
func TestNoDirectForbiddenImports(t *testing.T) {
	cmd := exec.Command("go", "list", "-json", ".")
	cmd.Dir = packageDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}

	var info struct {
		Imports []string `json:"Imports"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatalf("parse go list output: %v", err)
	}

	for _, imp := range info.Imports {
		for _, forbidden := range forbiddenPrefixes {
			if strings.HasPrefix(imp, forbidden) {
				t.Errorf("lite package directly imports forbidden package: %s", imp)
			}
		}
	}
}

// TestNoForbiddenTransitiveImports verifies that no forbidden packages appear
// anywhere in the lite package dependency graph.
func TestNoForbiddenTransitiveImports(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Dir = packageDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps failed: %v\n%s", err, out)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var forbiddenFound []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "github.com/joshka0/foxctl/internal/") {
			continue
		}

		for _, forbidden := range forbiddenPrefixes {
			if strings.HasPrefix(line, forbidden) {
				forbiddenFound = append(forbiddenFound, line)
				break
			}
		}
	}

	if len(forbiddenFound) > 0 {
		t.Errorf("lite package transitively imports forbidden internal packages: %v", forbiddenFound)
	}
}

// TestForbiddenTransitiveLeakSetIsEmpty is a second guard with a clearer
// failure message for the original config leak regression.
func TestForbiddenTransitiveLeakSetIsEmpty(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Dir = packageDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps failed: %v\n%s", err, out)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var actualLeaks []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "github.com/joshka0/foxctl/internal/") {
			continue
		}
		for _, forbidden := range forbiddenPrefixes {
			if strings.HasPrefix(line, forbidden) {
				actualLeaks = append(actualLeaks, line)
				break
			}
		}
	}

	if len(actualLeaks) > 0 {
		t.Fatalf("lite package must have zero forbidden transitive deps; found %v", actualLeaks)
	}
}

// TestAllowedImportsAreStable documents the packages that lite is allowed to
// import. This is a canary: if an allowed package starts pulling in new
// forbidden transitive deps, the transitive leak tests above will catch it.
func TestAllowedImportsAreStable(t *testing.T) {
	cmd := exec.Command("go", "list", "-json", ".")
	cmd.Dir = packageDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list failed: %v\n%s", err, out)
	}

	var info struct {
		Imports []string `json:"Imports"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatalf("parse go list output: %v", err)
	}

	var internalImports []string
	for _, imp := range info.Imports {
		if strings.HasPrefix(imp, "github.com/joshka0/foxctl/") {
			internalImports = append(internalImports, imp)
		}
	}

	t.Logf("lite direct internal imports: %v", internalImports)

	// Sanity check: we expect at least these packages
	expected := []string{
		"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr",
		"github.com/joshka0/foxctl/internal/domain/envelope",
		"github.com/joshka0/foxctl/internal/domain/policy",
		"github.com/joshka0/foxctl/internal/platform/workspace",
	}

	importSet := make(map[string]bool, len(internalImports))
	for _, imp := range internalImports {
		importSet[imp] = true
	}

	for _, exp := range expected {
		if !importSet[exp] {
			t.Errorf("expected lite to import %s, but it does not", exp)
		}
	}
}

// TestGoModExists is a sanity check that we are running from a real module.
func TestGoModExists(t *testing.T) {
	cmd := exec.Command("go", "env", "GOMOD")
	cmd.Dir = packageDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go env GOMOD failed: %v\n%s", err, out)
	}
	modFile := strings.TrimSpace(string(out))
	if modFile == "" {
		t.Fatal("no go.mod found; tests must run inside the foxctl module")
	}
	if _, err := os.Stat(modFile); err != nil {
		t.Fatalf("go.mod does not exist: %s", modFile)
	}
}
