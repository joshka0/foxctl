package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

func TestEmitDoesNotSetMetaCasDigestWhenArtifactPresent(t *testing.T) {
	buf := &bytes.Buffer{}
	c := &RunnerContext{Stdout: buf}

	data := map[string]any{
		"artifact": "sha256:abc123",
		"foo":      "bar",
	}
	if err := Emit(c, "demo", data, "application/json", envelope.Meta{Source: "run"}); err != nil {
		t.Fatalf("emit: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Meta.CASDigest != "" {
		t.Fatalf("expected cas_digest omitted, got %q", env.Meta.CASDigest)
	}
}

func TestEmitPreservesExistingCasDigest(t *testing.T) {
	buf := &bytes.Buffer{}
	c := &RunnerContext{Stdout: buf}

	digest := "sha256:existing"
	meta := envelope.Meta{Source: "run", CASDigest: digest}
	data := map[string]any{"artifact": digest}
	if err := Emit(c, "demo", data, "application/json", meta); err != nil {
		t.Fatalf("emit: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Meta.CASDigest != digest {
		t.Fatalf("expected cas digest preserved, got %q", env.Meta.CASDigest)
	}
}

func TestEmitWithNilData(t *testing.T) {
	buf := &bytes.Buffer{}
	c := &RunnerContext{Stdout: buf}

	if err := Emit(c, "cmd", nil, "application/json", envelope.Meta{}); err != nil {
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
	c := &RunnerContext{}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
}

func TestArtifactDigest(t *testing.T) {
	tests := []struct {
		name string
		data any
		want string
	}{
		{name: "nil data", data: nil, want: ""},
		{name: "map with artifact", data: map[string]any{"artifact": "sha256:abc123"}, want: "sha256:abc123"},
		{name: "map without artifact", data: map[string]any{"foo": "bar"}, want: ""},
		{name: "map with non-sha256 artifact", data: map[string]any{"artifact": "not-a-digest"}, want: ""},
		{name: "struct-like map", data: map[string]string{"artifact": "sha256:xyz789"}, want: "sha256:xyz789"},
		{name: "non-map data", data: "just a string", want: ""},
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
		{name: "valid sha256 artifact", m: map[string]any{"artifact": "sha256:abc123"}, want: "sha256:abc123"},
		{name: "missing artifact key", m: map[string]any{"foo": "bar"}, want: ""},
		{name: "artifact without sha256 prefix", m: map[string]any{"artifact": "abc123"}, want: ""},
		{name: "artifact is not a string", m: map[string]any{"artifact": 123}, want: ""},
		{name: "empty map", m: map[string]any{}, want: ""},
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

func TestNewContext(t *testing.T) {
	temp := t.TempDir()
	cfg := config.Config{Paths: config.Paths{CAS: filepath.Join(temp, "cas")}}
	stdout := &bytes.Buffer{}

	c, err := NewRunnerContext(cfg, stdout)
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	if c.PathValidator == nil {
		t.Fatalf("expected path validator")
	}
	if c.CASStore == nil {
		t.Fatalf("expected cas store")
	}
}

func TestPersistBufferAndJSON(t *testing.T) {
	temp := t.TempDir()
	store, err := cas.NewStore(temp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	c := &RunnerContext{CASStore: store}

	buf := bytes.NewBufferString("hello world")
	art, err := PersistBuffer(context.Background(), c, buf, "text/plain", "tag1")
	if err != nil {
		t.Fatalf("PersistBuffer: %v", err)
	}
	if art.Digest == "" || art.Size == 0 {
		t.Fatalf("expected populated artifact: %+v", art)
	}

	_, err = PersistJSON(context.Background(), c, math.NaN())
	if err == nil {
		t.Fatalf("expected json marshal error")
	}

	value := map[string]string{"foo": "bar"}
	art, err = PersistJSON(context.Background(), c, value, "tag2")
	if err != nil {
		t.Fatalf("PersistJSON: %v", err)
	}
	if art.Digest == "" || art.Kind != "application/json" {
		t.Fatalf("unexpected artifact %+v", art)
	}
}

func TestPersistBufferNilBuffer(t *testing.T) {
	c := &RunnerContext{}
	if _, err := PersistBuffer(context.Background(), c, nil, "application/octet-stream"); err == nil {
		t.Fatalf("expected error for nil buffer")
	}
}
