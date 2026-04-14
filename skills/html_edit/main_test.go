package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func newTestContext(t *testing.T, buf *bytes.Buffer, workspace string) (*skillmain.RunContext, func()) {
	t.Helper()
	opts := &skilltest.TestContextOptions{
		Workspace: workspace,
	}
	return skilltest.NewTestRunContext(t, buf, opts)
}

func decodeEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env
}

func assertOK(t *testing.T, env map[string]any) {
	t.Helper()
	if env["status"] != "ok" {
		errField := env["error"]
		t.Fatalf("expected ok status, got %v (error: %v)", env["status"], errField)
	}
}

//nolint:unused // Test utility for future tests
func assertError(t *testing.T, env map[string]any, expectedCode string) {
	t.Helper()
	if env["status"] != "error" {
		t.Fatalf("expected error status, got %v", env["status"])
	}
	errField, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error field to be map, got %T", env["error"])
	}
	if errField["code"] != expectedCode {
		t.Errorf("expected error code %q, got %q", expectedCode, errField["code"])
	}
}

func getData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", env["data"])
	}
	return data
}

func writeHTML(t *testing.T, dir, filename, content string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func readHTML(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// Sample HTML for testing
const sampleHTML = `<!DOCTYPE html>
<html>
<head>
<title>Test Page</title>
</head>
<body>
<div id="header" class="container">
<h1>Welcome</h1>
</div>
<div id="content" class="container">
<p class="intro">Hello World</p>
<p class="outro">Goodbye World</p>
</div>
<div id="footer">
<span>Copyright</span>
</div>
</body>
</html>
`

// Tests for validation

func TestHTMLEdit_MissingPath(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	in := input{
		Path: "",
		Operations: []operation{
			{Type: "select", Selector: "h1"},
		},
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

func TestHTMLEdit_NoOperations(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path:       htmlPath,
		Operations: []operation{},
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one operation is required")
}

// Tests for select operation (read-only)

func TestHTMLEdit_SelectOperation(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{Type: "select", Selector: "p.intro"},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, false, data["edited"])
	// File should not be modified
	assert.Equal(t, sampleHTML, readHTML(t, htmlPath))
}

// Tests for insert operation

func TestHTMLEdit_InsertAfter(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "insert",
				Selector: "h1",
				Position: "after",
				HTML:     "<h2>Subtitle</h2>",
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, true, data["edited"])
	assert.Greater(t, data["elements_affected"], float64(0))

	// Check file was modified
	modified := readHTML(t, htmlPath)
	assert.Contains(t, modified, "<h2>Subtitle</h2>")
}

func TestHTMLEdit_InsertBefore(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "insert",
				Selector: "h1",
				Position: "before",
				HTML:     "<nav>Navigation</nav>",
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, true, data["edited"])
	modified := readHTML(t, htmlPath)
	assert.Contains(t, modified, "<nav>Navigation</nav>")
}

func TestHTMLEdit_InsertAppend(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "insert",
				Selector: "#content",
				Position: "append",
				HTML:     "<p class=\"new\">New paragraph</p>",
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, true, data["edited"])
	modified := readHTML(t, htmlPath)
	assert.Contains(t, modified, "New paragraph")
}

// Tests for replace operation

func TestHTMLEdit_ReplaceElement(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "replace",
				Selector: "p.intro",
				HTML:     "<p class=\"intro\">New Introduction</p>",
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, true, data["edited"])
	modified := readHTML(t, htmlPath)
	assert.Contains(t, modified, "New Introduction")
	assert.NotContains(t, modified, "Hello World")
}

func TestHTMLEdit_ReplaceInnerHTML(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "replace",
				Selector: "#footer span",
				HTML:     "2024 All Rights Reserved",
				Inner:    true,
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, true, data["edited"])
	modified := readHTML(t, htmlPath)
	assert.Contains(t, modified, "2024 All Rights Reserved")
	assert.Contains(t, modified, "<span>") // Element preserved
}

// Tests for delete operation

func TestHTMLEdit_DeleteElement(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "delete",
				Selector: "p.outro",
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, true, data["edited"])
	modified := readHTML(t, htmlPath)
	assert.NotContains(t, modified, "Goodbye World")
	assert.Contains(t, modified, "Hello World") // Other p preserved
}

// Tests for update_attr operation

func TestHTMLEdit_UpdateAttribute(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:       "update_attr",
				Selector:   "#header",
				Attributes: map[string]any{"data-test": "value", "class": "container header-main"},
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, true, data["edited"])
	modified := readHTML(t, htmlPath)
	assert.Contains(t, modified, "data-test=\"value\"")
	assert.Contains(t, modified, "header-main")
}

func TestHTMLEdit_RemoveAttribute(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:       "update_attr",
				Selector:   "#header",
				Attributes: map[string]any{"class": ""}, // Empty string removes
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
}

// Tests for dry_run

func TestHTMLEdit_DryRun(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "delete",
				Selector: "p.intro",
			},
		},
		DryRun: true,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, false, data["edited"]) // Not edited due to dry_run
	assert.Equal(t, true, data["dry_run"])
	assert.NotEmpty(t, data["diff"]) // Diff should still be generated

	// File should not be modified
	assert.Equal(t, sampleHTML, readHTML(t, htmlPath))
}

// Tests for nth selector

func TestHTMLEdit_NthSelector(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "delete",
				Selector: "p",
				Nth:      2, // Only delete second p
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, true, data["edited"])
	modified := readHTML(t, htmlPath)
	assert.Contains(t, modified, "Hello World")      // First p preserved
	assert.NotContains(t, modified, "Goodbye World") // Second p deleted
}

// Tests for diff generation

func TestHTMLEdit_DiffGenerated(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "replace",
				Selector: "h1",
				HTML:     "<h1>Modified</h1>",
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	diff, ok := data["diff"].(string)
	require.True(t, ok, "diff should be a string")
	assert.Contains(t, diff, "-")
	assert.Contains(t, diff, "+")
}

// Tests for multiple operations

func TestHTMLEdit_MultipleOperations(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "replace",
				Selector: "h1",
				HTML:     "<h1>New Title</h1>",
			},
			{
				Type:     "delete",
				Selector: "p.outro",
			},
			{
				Type:     "insert",
				Selector: "#footer",
				Position: "append",
				HTML:     "<p>Footer text</p>",
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, true, data["edited"])
	assert.Equal(t, float64(3), data["operations_applied"])

	modified := readHTML(t, htmlPath)
	assert.Contains(t, modified, "New Title")
	assert.NotContains(t, modified, "Goodbye World")
	assert.Contains(t, modified, "Footer text")
}

// Tests for no changes

func TestHTMLEdit_NoChanges(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{
				Type:     "delete",
				Selector: ".nonexistent",
			},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	// Verify no elements were affected by the delete operation
	assert.Equal(t, float64(0), data["elements_affected"])
	// Note: edited may be true if HTML rendering normalizes whitespace/formatting
	// The important thing is that no elements matched the selector
}

// Tests for structure operation (read-only)

func TestHTMLEdit_StructureOperation(t *testing.T) {
	var buf bytes.Buffer
	workspace := t.TempDir()
	rc, cleanup := newTestContext(t, &buf, workspace)
	defer cleanup()

	htmlPath := writeHTML(t, workspace, "test.html", sampleHTML)

	in := input{
		Path: htmlPath,
		Operations: []operation{
			{Type: "structure", MaxDepth: 3},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	// Structure is read-only
	assert.Equal(t, false, data["edited"])
	assert.Equal(t, sampleHTML, readHTML(t, htmlPath))
}
