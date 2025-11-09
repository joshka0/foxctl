package envelope

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOKSetsDefaults(t *testing.T) {
	fixed := time.Date(2025, 11, 8, 13, 0, 0, 0, time.UTC)
	origNow := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = origNow })

	env := OK("agentctl.test", map[string]string{"hello": "world"})

	if env.Version != Version {
		t.Fatalf("expected version %d got %d", Version, env.Version)
	}
	if env.Status != StatusOK {
		t.Fatalf("expected status ok got %s", env.Status)
	}
	if env.Meta.TS != fixed.Format(time.RFC3339) {
		t.Fatalf("unexpected timestamp %s", env.Meta.TS)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
}

func TestErrorRequiresCode(t *testing.T) {
	env := OK("cmd", nil)
	env.Status = StatusError
	env.Error.Code = ""
	env.Error.Message = "boom"

	err := Validate(env)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error got %v", err)
	}
}

func TestValidateRejectsInvalidStatus(t *testing.T) {
	env := OK("cmd", nil)
	env.Status = "maybe"

	if err := Validate(env); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error got %v", err)
	}
}

func TestCanonicalizationOrdersKeys(t *testing.T) {
	input := map[string]any{
		"b": 1,
		"a": []any{map[string]int{"z": 1, "y": 2}},
	}
	canon, err := Canonicalize(input)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}

	if !json.Valid(canon) {
		t.Fatalf("canonical output not valid json: %s", canon)
	}

	if !strings.HasPrefix(string(canon), "{\"a\":") {
		t.Fatalf("expected keys sorted, got %s", string(canon))
	}
}

func TestWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewWriter(buf)
	env := OK("cmd", map[string]any{"n": 1})
	if err := w.Write(env); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("expected buffer to contain data")
	}
}

func TestCanonicalDigest(t *testing.T) {
	digest, err := CanonicalDigest(map[string]int{"b": 1, "a": 2})
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("unexpected digest prefix: %s", digest)
	}

	other, err := CanonicalDigest(map[string]int{"a": 2, "b": 1})
	if err != nil {
		t.Fatalf("digest(2): %v", err)
	}
	if digest != other {
		t.Fatalf("expected canonical digest determinism")
	}
}
