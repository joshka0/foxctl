package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunSkillInspect(t *testing.T) {
	// We need to mock the finding of skills or create a fake skill structure
	// Since findSkill looks in "skills" dir relative to cwd, we can set up a fake environment
	// But since we can't easily change the CWD for the whole process safely in tests without side effects,
	// we might need to test the helper functions instead of run, or ensure "skills" dir exists.

	// Alternatively, we can skip the 'run' test that depends on filesystem structure and test logic.

	// Let's test showManifest logic by mocking the info
	tmp := t.TempDir()
	manifestPath := filepath.Join(tmp, "skill.yaml")
	if err := os.WriteFile(manifestPath, []byte("name: test-skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &skillInfo{
		Name:         "test-skill",
		ManifestPath: manifestPath,
	}

	data, err := showManifest(info)
	if err != nil {
		t.Fatalf("showManifest failed: %v", err)
	}

	if data["skill_name"] != "test-skill" {
		t.Errorf("expected skill_name test-skill, got %v", data["skill_name"])
	}
}

func TestShowTypes(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\ntype Foo struct { Bar string }"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := &skillInfo{
		Name:       "test-skill",
		MainGoPath: mainPath,
	}

	data, err := showTypes(info)
	if err != nil {
		t.Fatalf("showTypes failed: %v", err)
	}

	types := data["types"].([]map[string]any)
	if len(types) != 1 {
		t.Errorf("expected 1 type, got %d", len(types))
	}
	if types[0]["name"] != "Foo" {
		t.Errorf("expected Foo type")
	}
}
