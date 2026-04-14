package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func newTestContext(t *testing.T, buf *bytes.Buffer) (*skillmain.RunContext, func()) {
	t.Helper()
	return skilltest.NewTestRunContext(t, buf, nil)
}

func decodeEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nbuffer: %s", err, buf.String())
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

func getData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", env["data"])
	}
	return data
}

func putToCAS(t *testing.T, rc *skillmain.RunContext, content []byte, kind string, tags []string) string {
	t.Helper()
	obj, err := rc.CASStore.Put(context.Background(), bytes.NewReader(content), kind, tags)
	require.NoError(t, err)
	return obj.Digest
}

// Tests for validation

func TestCASGet_MissingDigest(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest is required")
}

func TestCASGet_InvalidDigestPrefix(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Digest: "md5:abc123",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must start with 'sha256:'")
}

func TestCASGet_InvalidDigestLength(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := input{
		Digest: "sha256:tooshort",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid digest length")
}

// Tests for successful retrieval

func TestCASGet_BasicRetrieval(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Put content into CAS
	content := []byte("Hello, CAS!")
	digest := putToCAS(t, rc, content, "text/plain", []string{"test"})

	in := input{
		Digest: digest,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, digest, data["digest"])
	assert.Equal(t, float64(len(content)), data["size"])
	assert.Equal(t, "text/plain", data["kind"])
	assert.NotEmpty(t, data["path"])

	// Verify content was written
	retrievedPath := data["path"].(string)
	retrievedContent, err := os.ReadFile(retrievedPath)
	require.NoError(t, err)
	assert.Equal(t, content, retrievedContent)
}

func TestCASGet_WithOutputPath(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Put content into CAS
	content := []byte(`{"key": "value"}`)
	digest := putToCAS(t, rc, content, "application/json", nil)

	outputPath := filepath.Join(rc.Workspace, "retrieved.json")

	in := input{
		Digest: digest,
		Output: outputPath,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	// Compare basenames to avoid /var vs /private/var issues on macOS
	assert.Equal(t, filepath.Base(outputPath), filepath.Base(data["path"].(string)))

	// Verify content was written to the specified path
	retrievedContent, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Equal(t, content, retrievedContent)
}

func TestCASGet_CreatesParentDirectories(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Put content into CAS
	content := []byte("nested content")
	digest := putToCAS(t, rc, content, "text/plain", nil)

	outputPath := filepath.Join(rc.Workspace, "nested", "deep", "file.txt")

	in := input{
		Digest: digest,
		Output: outputPath,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)

	// Verify file was created
	_, err = os.Stat(outputPath)
	require.NoError(t, err)
}

// Tests for content types

func TestCASGet_JSONContent(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	content := []byte(`{"hello": "world"}`)
	digest := putToCAS(t, rc, content, "application/json", nil)

	in := input{
		Digest: digest,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	path := data["path"].(string)
	assert.True(t, strings.HasSuffix(path, ".json"))
}

func TestCASGet_MarkdownContent(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	content := []byte("# Hello\n\nThis is markdown.")
	digest := putToCAS(t, rc, content, "text/markdown", nil)

	in := input{
		Digest: digest,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	path := data["path"].(string)
	assert.True(t, strings.HasSuffix(path, ".md"))
}

func TestCASGet_HTMLContent(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	content := []byte("<html><body>Hello</body></html>")
	digest := putToCAS(t, rc, content, "text/html", nil)

	in := input{
		Digest: digest,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	path := data["path"].(string)
	assert.True(t, strings.HasSuffix(path, ".html"))
}

// Tests for tags

func TestCASGet_WithTags(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	content := []byte("tagged content")
	tags := []string{"tag1", "tag2", "tag3"}
	digest := putToCAS(t, rc, content, "text/plain", tags)

	in := input{
		Digest: digest,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	returnedTags := data["tags"].([]any)
	assert.Len(t, returnedTags, 3)
}

// Tests for pinned content

func TestCASGet_PinnedContent(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	content := []byte("pinned content")
	digest := putToCAS(t, rc, content, "text/plain", nil)

	// Pin the content
	err := rc.CASStore.Pin(context.Background(), digest)
	require.NoError(t, err)

	in := input{
		Digest: digest,
	}

	err = run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.True(t, data["pinned"].(bool))
}

// Tests for not found

func TestCASGet_NotFound(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Use a valid format but non-existent digest
	nonExistentDigest := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	in := input{
		Digest: nonExistentDigest,
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	// Should fail when trying to get non-existent content
}

// Tests for large content

func TestCASGet_LargeContent(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Create 1MB content
	content := bytes.Repeat([]byte("x"), 1024*1024)
	digest := putToCAS(t, rc, content, "application/octet-stream", nil)

	in := input{
		Digest: digest,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.Equal(t, float64(1024*1024), data["size"])
}

// Tests for summary

func TestCASGet_SummaryIncluded(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	content := []byte("summary test")
	digest := putToCAS(t, rc, content, "text/plain", nil)

	in := input{
		Digest: digest,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	summary := data["summary"].(string)
	assert.Contains(t, summary, "Retrieved")
	assert.Contains(t, summary, "bytes")
}

// Test extensionFromKind helper

func TestExtensionFromKind(t *testing.T) {
	tests := []struct {
		kind     string
		expected string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"text/plain", ".txt"},
		{"text/markdown", ".md"},
		{"application/json", ".json"},
		{"application/yaml", ".yaml"},
		{"text/html", ".html"},
		{"application/pdf", ".pdf"},
		{"application/octet-stream", ""},
		{"text/plain; charset=utf-8", ".txt"},
		{"application/json; charset=utf-8", ".json"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			result := extensionFromKind(tt.kind)
			assert.Equal(t, tt.expected, result)
		})
	}
}
