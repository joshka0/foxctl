package skillout

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillcas"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
)

func TestEmit(t *testing.T) {
	stdout := &bytes.Buffer{}
	rc := &skillmain.RunContext{Stdout: stdout}

	data := map[string]any{"result": "success", "count": 42}
	if err := Emit(rc, "test/skill", data); err != nil {
		t.Fatalf("Emit error: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, `"status":"ok"`) {
		t.Errorf("output should contain status ok, got: %s", output)
	}
	if !strings.Contains(output, `"command":"test/skill"`) {
		t.Errorf("output should contain command, got: %s", output)
	}
	if !strings.Contains(output, `"result":"success"`) {
		t.Errorf("output should contain data, got: %s", output)
	}
}

func TestBuildCASHint(t *testing.T) {
	artifact := Artifact{
		Digest: "sha256:abc123",
		Size:   10000,
		Kind:   "application/json",
	}

	t.Run("basic hint", func(t *testing.T) {
		hint := BuildCASHint(artifact, 0)
		if hint.Digest != artifact.Digest {
			t.Errorf("Digest = %q, want %q", hint.Digest, artifact.Digest)
		}
		if hint.TotalBytes != artifact.Size {
			t.Errorf("TotalBytes = %d, want %d", hint.TotalBytes, artifact.Size)
		}
		if !strings.Contains(hint.ReadCommand, artifact.Digest) {
			t.Errorf("ReadCommand should contain digest")
		}
	})

	t.Run("with pagination", func(t *testing.T) {
		hint := BuildCASHint(artifact, 50) // 50 lines = 4000 bytes
		if hint.PageCount == 0 {
			t.Error("PageCount should be > 0 for large artifact")
		}
		if hint.PageSize == 0 {
			t.Error("PageSize should be > 0")
		}
		if !strings.Contains(hint.ReadCommand, "--page-size") {
			t.Error("ReadCommand should contain --page-size for paginated content")
		}
	})

	t.Run("binary detection", func(t *testing.T) {
		binArtifact := Artifact{
			Digest: "sha256:bin123",
			Size:   1000,
			Kind:   "application/octet-stream",
		}
		hint := BuildCASHint(binArtifact, 0)
		if !hint.IsBinary {
			t.Error("IsBinary should be true for non-text content")
		}
	})

	t.Run("json not binary", func(t *testing.T) {
		hint := BuildCASHint(artifact, 0)
		if hint.IsBinary {
			t.Error("IsBinary should be false for application/json")
		}
	})
}

func TestArtifactJSON(t *testing.T) {
	artifact := Artifact{
		Digest: "sha256:test",
		Size:   1234,
		Kind:   "text/plain",
	}

	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded Artifact
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded != artifact {
		t.Errorf("round-trip failed: got %+v, want %+v", decoded, artifact)
	}
}

type fakeOutputContext struct {
	stdout       bytes.Buffer
	store        bool
	expose       skillcas.ExposePolicy
	stored       []byte
	storedKind   string
	storedTags   []string
	truncateOver int
}

func (c *fakeOutputContext) OutputWriter() io.Writer {
	return &c.stdout
}

func (c *fakeOutputContext) ShouldTruncate(dataSize int) bool {
	return c.truncateOver > 0 && dataSize > c.truncateOver
}

func (c *fakeOutputContext) ShouldStoreCAS() bool {
	return c.store
}

func (c *fakeOutputContext) CASExposePolicy() skillcas.ExposePolicy {
	return c.expose
}

func (c *fakeOutputContext) PutArtifact(_ context.Context, r io.Reader, kind string, tags []string) (skillcas.Artifact, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return skillcas.Artifact{}, err
	}
	c.stored = data
	c.storedKind = kind
	c.storedTags = append([]string(nil), tags...)
	return skillcas.Artifact{Digest: "sha256:stored", Size: int64(len(data)), Kind: kind}, nil
}

func TestEmitWithCASContextStoresTruncatedOutput(t *testing.T) {
	rc := &fakeOutputContext{
		store:        true,
		expose:       skillcas.ExposePolicyDigest,
		truncateOver: 8,
	}

	err := EmitWithCASContext(context.Background(), rc, "test/large", map[string]string{"result": "large output"})
	if err != nil {
		t.Fatalf("EmitWithCASContext returned error: %v", err)
	}

	output := rc.stdout.String()
	if !strings.Contains(output, `"artifact":"sha256:stored"`) {
		t.Fatalf("expected artifact digest in output, got: %s", output)
	}
	if strings.Contains(output, "large output") {
		t.Fatalf("expected large payload to be replaced by CAS result, got: %s", output)
	}
	if rc.storedKind != "application/json" {
		t.Fatalf("storedKind = %q, want application/json", rc.storedKind)
	}
	if got := strings.Join(rc.storedTags, ","); got != "test/large" {
		t.Fatalf("storedTags = %q, want test/large", got)
	}
	if !json.Valid(rc.stored) {
		t.Fatalf("stored content is not valid JSON: %q", rc.stored)
	}
}

func TestEmitWithCASContextFallsBackInlineWhenStoreDisabled(t *testing.T) {
	rc := &fakeOutputContext{
		store:        false,
		expose:       skillcas.ExposePolicyDigest,
		truncateOver: 8,
	}

	err := EmitWithCASContext(context.Background(), rc, "test/large", map[string]string{"result": "large output"})
	if err != nil {
		t.Fatalf("EmitWithCASContext returned error: %v", err)
	}

	output := rc.stdout.String()
	if !strings.Contains(output, "large output") {
		t.Fatalf("expected inline payload when CAS storage is disabled, got: %s", output)
	}
	if len(rc.stored) != 0 {
		t.Fatalf("stored content should be empty when CAS storage is disabled")
	}
}
