package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
)

func writeManifest(t *testing.T, manifest string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "skill.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func manifestWithFilesystemPath(capabilityType, capabilityPath string) string {
	return fmt.Sprintf(`apiVersion: foxctl/v1
kind: Skill
metadata:
  name: test/filesystem-path
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/test/filesystem-path
signature:
  command: test/filesystem-path
capabilities:
  network: "none"
  filesystem:
    - type: %q
      path: %q
`, capabilityType, capabilityPath)
}

func TestLoadManifestValidates(t *testing.T) {
	manifest := []byte(`apiVersion: foxctl/v1
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
	if _, err := LoadManifest(writeManifest(t, string(manifest))); err != nil {
		t.Fatalf("load manifest: %v", err)
	}
}

func TestLoadManifestNormalizesTopLevelReturns(t *testing.T) {
	manifest := `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
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
returns:
  - name: matches
    type: array
capabilities:
  network: "none"
  filesystem: []
`

	got, err := LoadManifest(writeManifest(t, manifest))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(got.Signature.Returns) != 1 || got.Signature.Returns[0].Name != "matches" {
		t.Fatalf("signature returns were not normalized: %#v", got.Signature.Returns)
	}
	if len(got.Returns) != 0 {
		t.Fatalf("top-level returns should be consumed after normalization: %#v", got.Returns)
	}
}

func TestLoadManifestNormalizesParameterNamesAndTypes(t *testing.T) {
	manifest := `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/text_grep/text_grep
signature:
  command: text/grep
  parameters:
    - name: " query "
      type: " String "
    - name: options
      type: object
      properties:
        limit:
          name: " limit "
          type: " Integer "
  returns:
    - name: " matches "
      type: " Array "
      items:
        type: " String "
capabilities:
  network: "none"
  filesystem: []
`

	got, err := LoadManifest(writeManifest(t, manifest))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if got.Signature.Parameters[0].Name != "query" {
		t.Fatalf("parameter name=%q want query", got.Signature.Parameters[0].Name)
	}
	if got.Signature.Parameters[0].Type != "string" {
		t.Fatalf("parameter type=%q want string", got.Signature.Parameters[0].Type)
	}
	child := got.Signature.Parameters[1].Properties["limit"]
	if child.Name != "limit" {
		t.Fatalf("property name=%q want limit", child.Name)
	}
	if child.Type != "integer" {
		t.Fatalf("property type=%q want integer", child.Type)
	}
	if got.Signature.Returns[0].Name != "matches" {
		t.Fatalf("return name=%q want matches", got.Signature.Returns[0].Name)
	}
	if got.Signature.Returns[0].Type != "array" {
		t.Fatalf("return type=%q want array", got.Signature.Returns[0].Type)
	}
	if got.Signature.Returns[0].Items == nil || got.Signature.Returns[0].Items.Type != "string" {
		t.Fatalf("return item=%#v want string item", got.Signature.Returns[0].Items)
	}
}

func TestLoadManifestRejectsPolicyViolations(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wantErr  string
	}{
		{
			name: "wasi network egress",
			manifest: `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: test/wasi-egress
  version: 0.1.0
distribution:
  type: wasi
  wasi:
    module: skills/test/module.wasm
signature:
  command: test/wasi-egress
capabilities:
  network: "egress"
  filesystem: []
`,
			wantErr: "policy validation failed",
		},
		{
			name: "unknown filesystem capability",
			manifest: `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: test/bad-filesystem
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/test/bad-filesystem
signature:
  command: test/bad-filesystem
capabilities:
  network: "none"
  filesystem:
    - type: root
`,
			wantErr: "filesystem capabilities validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadManifest(writeManifest(t, tt.manifest))
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadManifestAllowsScopedFilesystemCapabilityPath(t *testing.T) {
	got, err := LoadManifest(writeManifest(t, manifestWithFilesystemPath("home", ".foxctl/observability")))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(got.Capabilities.Filesystem) != 1 {
		t.Fatalf("filesystem capabilities=%#v, want one scoped capability", got.Capabilities.Filesystem)
	}
	if got.Capabilities.Filesystem[0].Path != ".foxctl/observability" {
		t.Fatalf("filesystem path=%q want .foxctl/observability", got.Capabilities.Filesystem[0].Path)
	}
}

func TestLoadManifestRejectsFilesystemCapabilityPathEscapes(t *testing.T) {
	tests := []struct {
		name           string
		capabilityType string
		path           string
		wantErr        string
	}{
		{
			name:           "absolute home path",
			capabilityType: "home",
			path:           "/etc/passwd",
			wantErr:        "path must be relative",
		},
		{
			name:           "workdir parent traversal",
			capabilityType: "workdir",
			path:           "../outside",
			wantErr:        "path must not contain parent traversal",
		},
		{
			name:           "tmp nested parent traversal",
			capabilityType: "tmp",
			path:           "logs/../../secret",
			wantErr:        "path must not contain parent traversal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadManifest(writeManifest(t, manifestWithFilesystemPath(tt.capabilityType, tt.path)))
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadManifestPropertyFilesystemCapabilityPathCannotEscapeRoot(t *testing.T) {
	capabilityTypes := []string{"workdir", "home", "tmp"}
	err := quick.Check(func(rawLeaf, rawCapability uint8) bool {
		leaf := fmt.Sprintf("target%d", rawLeaf%64)
		capabilityType := capabilityTypes[int(rawCapability)%len(capabilityTypes)]
		for _, path := range []string{
			"/" + leaf,
			"../" + leaf,
			"safe/../" + leaf,
			`safe\..\` + leaf,
		} {
			if _, err := LoadManifest(writeManifest(t, manifestWithFilesystemPath(capabilityType, path))); err == nil {
				t.Logf("LoadManifest accepted escaping filesystem path type=%q path=%q", capabilityType, path)
				return false
			}
		}
		return true
	}, &quick.Config{MaxCount: 100})
	if err != nil {
		t.Fatalf("filesystem capability path escape property failed: %v", err)
	}
}

func TestLoadManifestRejectsSchemaInvariantViolations(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		wantErr  string
	}{
		{
			name: "returns declared twice",
			manifest: `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/text_grep/text_grep
signature:
  command: text/grep
  returns:
    - name: matches
      type: array
returns:
  - name: matches
    type: array
capabilities:
  network: "none"
  filesystem: []
`,
			wantErr: "returns must be declared in only one location",
		},
		{
			name: "duplicate parameter",
			manifest: `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/text_grep/text_grep
signature:
  command: text/grep
  parameters:
    - name: query
      type: string
    - name: query
      type: string
capabilities:
  network: "none"
  filesystem: []
`,
			wantErr: `duplicate signature.parameters name "query"`,
		},
		{
			name: "blank parameter name",
			manifest: `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/text_grep/text_grep
signature:
  command: text/grep
  parameters:
    - name: ""
      type: string
capabilities:
  network: "none"
  filesystem: []
`,
			wantErr: "signature.parameters[0].name required",
		},
		{
			name: "unsupported parameter type",
			manifest: `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/text_grep/text_grep
signature:
  command: text/grep
  parameters:
    - name: stream
      type: channel
capabilities:
  network: "none"
  filesystem: []
`,
			wantErr: `signature.parameters["stream"].type "channel" unsupported`,
		},
		{
			name: "invalid nested item type",
			manifest: `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/text_grep/text_grep
signature:
  command: text/grep
  parameters:
    - name: paths
      type: array
      items:
        type: filehandle
capabilities:
  network: "none"
  filesystem: []
`,
			wantErr: `signature.parameters["paths"].items.type "filehandle" unsupported`,
		},
		{
			name: "negative inline output",
			manifest: `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/text_grep/text_grep
io:
  format: JSON
  inline_output_kb: -1
signature:
  command: text/grep
capabilities:
  network: "none"
  filesystem: []
`,
			wantErr: "io.inline_output_kb must be non-negative",
		},
		{
			name: "unsupported io format",
			manifest: `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: text/grep
  version: 0.1.0
distribution:
  type: exec
  exec:
    entry: skills/text_grep/text_grep
io:
  format: YAML
signature:
  command: text/grep
capabilities:
  network: "none"
  filesystem: []
`,
			wantErr: "io.format must be JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadManifest(writeManifest(t, tt.manifest))
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "skill"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill", "skill.yaml"), []byte(`apiVersion: foxctl/v1
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
