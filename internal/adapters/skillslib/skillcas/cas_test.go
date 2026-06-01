package skillcas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeWriter struct {
	content []byte
	kind    string
	tags    []string
	err     error
}

func (w *fakeWriter) PutArtifact(_ context.Context, r io.Reader, kind string, tags []string) (Artifact, error) {
	if w.err != nil {
		return Artifact{}, w.err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return Artifact{}, err
	}
	w.content = data
	w.kind = kind
	w.tags = append([]string(nil), tags...)
	return Artifact{Digest: "sha256:test", Size: int64(len(data)), Kind: kind}, nil
}

func TestPersistBufferStoresContentThroughWriter(t *testing.T) {
	writer := &fakeWriter{}
	buf := bytes.NewBufferString("stored output")

	artifact, err := PersistBuffer(context.Background(), writer, buf, "text/plain", "tag1", "tag2")
	if err != nil {
		t.Fatalf("PersistBuffer returned error: %v", err)
	}

	if artifact.Digest != "sha256:test" {
		t.Fatalf("Digest = %q, want sha256:test", artifact.Digest)
	}
	if string(writer.content) != "stored output" {
		t.Fatalf("stored content = %q, want stored output", writer.content)
	}
	if writer.kind != "text/plain" {
		t.Fatalf("kind = %q, want text/plain", writer.kind)
	}
	if got := strings.Join(writer.tags, ","); got != "tag1,tag2" {
		t.Fatalf("tags = %q, want tag1,tag2", got)
	}
	if buf.String() != "stored output" {
		t.Fatalf("PersistBuffer consumed caller buffer: %q", buf.String())
	}
}

func TestPersistJSONStoresMarshaledPayload(t *testing.T) {
	writer := &fakeWriter{}
	payload := map[string]any{"name": "foxctl", "count": float64(2)}

	artifact, err := PersistJSON(context.Background(), writer, payload, "json-output")
	if err != nil {
		t.Fatalf("PersistJSON returned error: %v", err)
	}

	if artifact.Kind != "application/json" {
		t.Fatalf("Kind = %q, want application/json", artifact.Kind)
	}

	var decoded map[string]any
	if err := json.Unmarshal(writer.content, &decoded); err != nil {
		t.Fatalf("stored content is not JSON: %v", err)
	}
	if decoded["name"] != "foxctl" || decoded["count"] != float64(2) {
		t.Fatalf("decoded payload = %#v, want original values", decoded)
	}
}

func TestPersistBufferRequiresWriterAndBuffer(t *testing.T) {
	if _, err := PersistBuffer(context.Background(), nil, bytes.NewBufferString("x"), "text/plain"); err == nil {
		t.Fatal("PersistBuffer with nil writer returned nil error")
	}
	if _, err := PersistBuffer(context.Background(), &fakeWriter{}, nil, "text/plain"); err == nil {
		t.Fatal("PersistBuffer with nil buffer returned nil error")
	}
}

func TestPersistBufferWrapsWriterError(t *testing.T) {
	want := errors.New("disk full")
	_, err := PersistBuffer(context.Background(), &fakeWriter{err: want}, bytes.NewBufferString("x"), "text/plain")
	if err == nil {
		t.Fatal("PersistBuffer returned nil error")
	}
	if !strings.Contains(err.Error(), "put artifact") || !strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("error = %q, want wrapped writer error", err)
	}
}

func TestBuildCASHint(t *testing.T) {
	artifact := Artifact{Digest: "sha256:abc123", Size: 10000, Kind: "application/json"}

	t.Run("basic hint", func(t *testing.T) {
		hint := BuildCASHint(artifact, 0)
		if hint.Digest != artifact.Digest {
			t.Fatalf("Digest = %q, want %q", hint.Digest, artifact.Digest)
		}
		if hint.TotalBytes != artifact.Size {
			t.Fatalf("TotalBytes = %d, want %d", hint.TotalBytes, artifact.Size)
		}
		if !strings.Contains(hint.ReadCommand, artifact.Digest) {
			t.Fatalf("ReadCommand = %q, want digest", hint.ReadCommand)
		}
	})

	t.Run("with pagination", func(t *testing.T) {
		hint := BuildCASHint(artifact, 50)
		if hint.PageCount == 0 {
			t.Fatal("PageCount should be set for large artifact")
		}
		if !strings.Contains(hint.ReadCommand, "--page-size") {
			t.Fatalf("ReadCommand = %q, want page size flag", hint.ReadCommand)
		}
	})

	t.Run("binary detection", func(t *testing.T) {
		hint := BuildCASHint(Artifact{Digest: "sha256:bin", Size: 10, Kind: "application/octet-stream"}, 0)
		if !hint.IsBinary {
			t.Fatal("IsBinary should be true for non-text content")
		}
	})
}

func TestBuildCASResultHonorsExposePolicy(t *testing.T) {
	artifact := Artifact{Digest: "sha256:abc123", Size: 42, Kind: "text/plain"}

	tests := []struct {
		name         string
		policy       ExposePolicy
		wantStored   bool
		wantArtifact bool
		wantHint     bool
	}{
		{name: "off", policy: ExposePolicyOff, wantStored: true},
		{name: "digest", policy: ExposePolicyDigest, wantArtifact: true},
		{name: "hint", policy: ExposePolicyHint, wantArtifact: true, wantHint: true},
		{name: "unknown defaults to stored", policy: ExposePolicy("unknown"), wantStored: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildCASResult(artifact, tt.policy)
			if got["truncated"] != true {
				t.Fatalf("truncated = %#v, want true", got["truncated"])
			}
			if got["size"] != artifact.Size {
				t.Fatalf("size = %#v, want %d", got["size"], artifact.Size)
			}
			if _, ok := got["stored"]; ok != tt.wantStored {
				t.Fatalf("stored present = %v, want %v", ok, tt.wantStored)
			}
			if _, ok := got["artifact"]; ok != tt.wantArtifact {
				t.Fatalf("artifact present = %v, want %v", ok, tt.wantArtifact)
			}
			if _, ok := got["hint"]; ok != tt.wantHint {
				t.Fatalf("hint present = %v, want %v", ok, tt.wantHint)
			}
		})
	}
}
