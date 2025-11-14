package skillslib

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/envelope"
)

func TestEmitSetsMetaCasDigestWhenArtifactPresent(t *testing.T) {
	buf := &bytes.Buffer{}
	rc := &RunnerContext{Stdout: buf}

	data := map[string]any{
		"artifact": "sha256:abc123",
		"foo":      "bar",
	}
	if err := rc.Emit("demo", data, "application/json", envelope.Meta{Source: "run"}); err != nil {
		t.Fatalf("emit: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Meta.CASDigest != "sha256:abc123" {
		t.Fatalf("expected cas_digest set, got %q", env.Meta.CASDigest)
	}
}

func TestEmitPreservesExistingCasDigest(t *testing.T) {
	buf := &bytes.Buffer{}
	rc := &RunnerContext{Stdout: buf}

	meta := envelope.Meta{Source: "run", CASDigest: "sha256:existing"}
	if err := rc.Emit("demo", map[string]any{}, "application/json", meta); err != nil {
		t.Fatalf("emit: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Meta.CASDigest != "sha256:existing" {
		t.Fatalf("expected cas digest preserved, got %q", env.Meta.CASDigest)
	}
}

func TestEmitWithNilData(t *testing.T) {
	buf := &bytes.Buffer{}
	rc := &RunnerContext{Stdout: buf}

	if err := rc.Emit("cmd", nil, "application/json", envelope.Meta{}); err != nil {
		t.Fatalf("emit: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Meta.CASDigest != "" {
		t.Fatalf("expected empty cas digest for nil data, got %q", env.Meta.CASDigest)
	}
}

func TestClose(t *testing.T) {
	rc := &RunnerContext{}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
}

func TestArtifactDigest(t *testing.T) {
	tests := []struct {
		name string
		data any
		want string
	}{
		{
			name: "nil data",
			data: nil,
			want: "",
		},
		{
			name: "map with artifact",
			data: map[string]any{"artifact": "sha256:abc123"},
			want: "sha256:abc123",
		},
		{
			name: "map without artifact",
			data: map[string]any{"foo": "bar"},
			want: "",
		},
		{
			name: "map with non-sha256 artifact",
			data: map[string]any{"artifact": "not-a-digest"},
			want: "",
		},
		{
			name: "struct-like map",
			data: map[string]string{"artifact": "sha256:xyz789"},
			want: "sha256:xyz789",
		},
		{
			name: "non-map data",
			data: "just a string",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := artifactDigest(tt.data)
			if got != tt.want {
				t.Errorf("artifactDigest() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractArtifact(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		want string
	}{
		{
			name: "valid sha256 artifact",
			m:    map[string]any{"artifact": "sha256:abc123"},
			want: "sha256:abc123",
		},
		{
			name: "missing artifact key",
			m:    map[string]any{"foo": "bar"},
			want: "",
		},
		{
			name: "artifact without sha256 prefix",
			m:    map[string]any{"artifact": "abc123"},
			want: "",
		},
		{
			name: "artifact is not a string",
			m:    map[string]any{"artifact": 123},
			want: "",
		},
		{
			name: "empty map",
			m:    map[string]any{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractArtifact(tt.m)
			if got != tt.want {
				t.Errorf("extractArtifact() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDepth(t *testing.T) {
	tests := []struct {
		rel  string
		want int
	}{
		{rel: ".", want: 0},
		{rel: "file.txt", want: 1},
		{rel: "dir/file.txt", want: 2},
		{rel: "dir/subdir/file.txt", want: 3},
		{rel: "a/b/c/d/e.txt", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.rel, func(t *testing.T) {
			got := depth(tt.rel)
			if got != tt.want {
				t.Errorf("depth(%q) = %d, want %d", tt.rel, got, tt.want)
			}
		})
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		globs []string
		want  bool
	}{
		{
			name:  "matches basename",
			path:  "/path/to/file.txt",
			globs: []string{"*.txt"},
			want:  true,
		},
		{
			name:  "no match",
			path:  "/path/to/file.txt",
			globs: []string{"*.go"},
			want:  false,
		},
		{
			name:  "empty globs",
			path:  "/path/to/file.txt",
			globs: []string{},
			want:  false,
		},
		{
			name:  "matches multiple globs",
			path:  "/path/to/test.go",
			globs: []string{"*.txt", "*.go"},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matches(tt.path, tt.globs)
			if got != tt.want {
				t.Errorf("matches(%q, %v) = %v, want %v", tt.path, tt.globs, got, tt.want)
			}
		})
	}
}

func TestShouldSkip(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		globs []string
		want  bool
	}{
		{
			name:  "skip node_modules",
			path:  "/path/to/node_modules",
			globs: []string{"node_modules"},
			want:  true,
		},
		{
			name:  "skip .git",
			path:  "/path/to/.git",
			globs: []string{".git"},
			want:  true,
		},
		{
			name:  "don't skip normal dir",
			path:  "/path/to/src",
			globs: []string{".git", "node_modules"},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkip(tt.path, tt.globs)
			if got != tt.want {
				t.Errorf("shouldSkip(%q, %v) = %v, want %v", tt.path, tt.globs, got, tt.want)
			}
		})
	}
}
