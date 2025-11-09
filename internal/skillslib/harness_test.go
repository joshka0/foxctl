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
