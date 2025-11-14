package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifestValidates(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte(`apiVersion: agentctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
  description: test
  tags: [test]
distribution:
  type: exec
  exec:
    entry: skills/text_grep/text_grep
io:
  format: JSON
  inline_output_kb: 32
signature:
  command: text/grep
  parameters: []
  returns: []
capabilities:
  network: "none"
  filesystem: []
`)
	path := filepath.Join(dir, "skill.yaml")
	if err := os.WriteFile(path, manifest, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := LoadManifest(path); err != nil {
		t.Fatalf("load manifest: %v", err)
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "skill"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill", "skill.yaml"), []byte(`apiVersion: agentctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
  description: test
  tags: []
distribution:
  type: exec
  exec:
    entry: skills/text_grep/text_grep
io:
  format: JSON
  inline_output_kb: 32
signature:
  command: text/grep
  parameters: []
  returns: []
capabilities:
  network: "none"
  filesystem: []
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifests, err := Discover(dir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(manifests))
	}
}
