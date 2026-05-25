package envelope

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/quick"
	"time"
)

func TestOKSetsDefaults(t *testing.T) {
	fixed := time.Date(2025, 11, 8, 13, 0, 0, 0, time.UTC)
	origNow := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = origNow })

	env := OK("foxctl.test", map[string]string{"hello": "world"})

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

func TestOKMarshalIncludesErrorField(t *testing.T) {
	env := OK("foxctl.test", map[string]any{"ok": true})
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v, ok := m["error"]
	if !ok {
		t.Fatalf("expected top-level error field to be present")
	}
	if _, ok := v.(map[string]any); !ok {
		t.Fatalf("expected error to be an object, got %T", v)
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

func TestErrorRequiresMessage(t *testing.T) {
	env := Error("cmd", "ERR", "", nil)

	err := Validate(env)
	if !errors.Is(err, ErrValidation) || !errors.Is(err, ErrMissingErrMsg) {
		t.Fatalf("expected missing error message error, got %v", err)
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

func TestCanonicalDigestPropertyOrderIndependentForNestedData(t *testing.T) {
	check := func(nameInput, detailInput string, count uint16, final bool) bool {
		name := strings.ToValidUTF8(nameInput, "?")
		detail := strings.ToValidUTF8(detailInput, "?")

		left := map[string]any{
			"name":  name,
			"count": int(count),
			"items": []any{detail, int(count), final},
			"nested": map[string]any{
				"detail": detail,
				"final":  final,
			},
		}
		right := map[string]any{
			"nested": map[string]any{
				"final":  final,
				"detail": detail,
			},
			"items": []any{detail, int(count), final},
			"count": int(count),
			"name":  name,
		}

		leftDigest, err := CanonicalDigest(left)
		if err != nil {
			return false
		}
		rightDigest, err := CanonicalDigest(right)
		if err != nil {
			return false
		}
		return leftDigest == rightDigest
	}

	if err := quick.Check(check, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatalf("canonical digest should be independent of object insertion order: %v", err)
	}
}

func TestEnvelopeJSONRoundTripPropertyPreservesValidityAndDigest(t *testing.T) {
	fixed := time.Date(2025, 11, 8, 13, 0, 0, 0, time.UTC)
	check := func(payloadInput string, errorCase bool) bool {
		payload := strings.ToValidUTF8(payloadInput, "?")
		data := map[string]any{"payload": payload}

		var env Envelope
		if errorCase {
			env = Error("foxctl.test", "TEST_ERROR", "message: "+payload, data, WithTimestamp(fixed))
		} else {
			env = OK("foxctl.test", data, WithTimestamp(fixed))
		}

		if err := Validate(env); err != nil {
			return false
		}
		originalDigest, err := CanonicalDigest(env)
		if err != nil {
			return false
		}

		encoded, err := json.Marshal(env)
		if err != nil {
			return false
		}
		var decoded Envelope
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			return false
		}
		if err := Validate(decoded); err != nil {
			return false
		}
		if err := coreEnvelopeInvariantError(decoded); err != nil {
			return false
		}
		decodedDigest, err := CanonicalDigest(decoded)
		if err != nil {
			return false
		}
		return originalDigest == decodedDigest
	}

	if err := quick.Check(check, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatalf("valid envelopes should survive JSON round trip with stable canonical digest: %v", err)
	}
}

func TestWithMetaOverrides(t *testing.T) {
	env := OK("cmd", nil, WithMeta(Meta{CASDigest: "sha256:abc"}))
	if env.Meta.CASDigest != "sha256:abc" {
		t.Fatalf("expected cas digest override")
	}
	env = OK("cmd", nil, WithMeta(Meta{Workspace: "/tmp/ws"}))
	if env.Meta.Workspace != "/tmp/ws" {
		t.Fatalf("expected workspace override")
	}
}

func TestErrorEnvelope(t *testing.T) {
	fixed := time.Date(2025, 11, 8, 13, 0, 0, 0, time.UTC)
	origNow := now
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = origNow })

	env := Error("test.cmd", "TEST_ERROR", "something went wrong", nil)

	if env.Version != Version {
		t.Fatalf("expected version %d got %d", Version, env.Version)
	}
	if env.Status != StatusError {
		t.Fatalf("expected status error got %s", env.Status)
	}
	if env.Error.Code != "TEST_ERROR" {
		t.Fatalf("expected error code TEST_ERROR got %s", env.Error.Code)
	}
	if env.Error.Message != "something went wrong" {
		t.Fatalf("unexpected error message: %s", env.Error.Message)
	}
	if env.Meta.TS != fixed.Format(time.RFC3339) {
		t.Fatalf("unexpected timestamp %s", env.Meta.TS)
	}
	if err := Validate(env); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
}

func TestWithMeta(t *testing.T) {
	custom := Meta{
		TS:         "2024-01-01T00:00:00Z",
		DurationMS: 100,
		Source:     "test-source",
	}

	env := OK("cmd", nil, WithMeta(custom))

	if env.Meta.TS != custom.TS {
		t.Fatalf("expected custom timestamp, got %s", env.Meta.TS)
	}
	if env.Meta.DurationMS != custom.DurationMS {
		t.Fatalf("expected duration_ms %d, got %d", custom.DurationMS, env.Meta.DurationMS)
	}
	if env.Meta.Source != custom.Source {
		t.Fatalf("expected source %s, got %s", custom.Source, env.Meta.Source)
	}
}

func TestWithMetaMutator(t *testing.T) {
	env := OK("cmd", nil, WithMetaMutator(func(m *Meta) {
		m.Source = "mutated-source"
		m.DurationMS = 42
	}))

	if env.Meta.Source != "mutated-source" {
		t.Fatalf("expected mutated source, got %s", env.Meta.Source)
	}
	if env.Meta.DurationMS != 42 {
		t.Fatalf("expected duration_ms 42, got %d", env.Meta.DurationMS)
	}
}

func TestWithData(t *testing.T) {
	originalData := map[string]string{"key": "value"}
	replacedData := map[string]int{"count": 99}

	env := OK("cmd", originalData, WithData(replacedData))

	data, ok := env.Data.(map[string]int)
	if !ok {
		t.Fatalf("expected data to be map[string]int")
	}
	if data["count"] != 99 {
		t.Fatalf("expected count 99, got %d", data["count"])
	}
}

func TestValidateMissingCommand(t *testing.T) {
	env := OK("", nil)
	env.Command = ""

	err := Validate(env)
	if !errors.Is(err, ErrValidation) || !errors.Is(err, ErrMissingCommand) {
		t.Fatalf("expected missing command error, got %v", err)
	}
}

func TestValidateMissingTimestamp(t *testing.T) {
	env := OK("cmd", nil)
	env.Meta.TS = ""

	err := Validate(env)
	if !errors.Is(err, ErrValidation) || !errors.Is(err, ErrMissingTS) {
		t.Fatalf("expected missing timestamp error, got %v", err)
	}
}

func TestValidateInvalidTimestampFormat(t *testing.T) {
	env := OK("cmd", nil)
	env.Meta.TS = "invalid"

	err := Validate(env)
	if !errors.Is(err, ErrValidation) || !errors.Is(err, ErrInvalidTS) {
		t.Fatalf("expected invalid timestamp error, got %v", err)
	}
}

func TestValidateNonUTCTimestamp(t *testing.T) {
	env := OK("cmd", nil)
	env.Meta.TS = time.Date(2024, 1, 1, 10, 0, 0, 0, time.FixedZone("-0700", -7*3600)).Format(time.RFC3339)

	err := Validate(env)
	if !errors.Is(err, ErrValidation) || !errors.Is(err, ErrInvalidTS) {
		t.Fatalf("expected invalid timestamp error, got %v", err)
	}
}

func TestValidateInvalidVersion(t *testing.T) {
	env := OK("cmd", nil)
	env.Version = 999

	err := Validate(env)
	if !errors.Is(err, ErrValidation) || !errors.Is(err, ErrInvalidVersion) {
		t.Fatalf("expected invalid version error, got %v", err)
	}
}

func TestValidateErrorStatusWithoutCode(t *testing.T) {
	env := Error("cmd", "", "no code", nil)

	err := Validate(env)
	if !errors.Is(err, ErrValidation) || !errors.Is(err, ErrMissingErrCode) {
		t.Fatalf("expected missing error code error, got %v", err)
	}
}

func TestValidateOKStatusWithErrorFields(t *testing.T) {
	env := OK("cmd", nil)
	env.Error.Code = "UNEXPECTED"
	env.Error.Message = "should not be here"

	err := Validate(env)
	if !errors.Is(err, ErrValidation) || !errors.Is(err, ErrUnexpectedError) {
		t.Fatalf("expected unexpected error fields error, got %v", err)
	}
}

func TestApplyMetaDefaults(t *testing.T) {
	env := Envelope{
		Command: "test",
		Meta:    Meta{TS: ""},
	}

	applyMetaDefaults(&env)

	if env.Meta.TS == "" {
		t.Fatalf("expected timestamp to be set")
	}

	if env.Command != "test" {
		t.Fatalf("expected command to remain unchanged, got %q", env.Command)
	}
}

func TestApplyMetaDefaultsDoesNotFillCommand(t *testing.T) {
	env := Envelope{
		Command: "",
		Meta:    Meta{TS: "2024-01-01T00:00:00Z"},
	}

	applyMetaDefaults(&env)

	if env.Command != "" {
		t.Fatalf("expected command to remain empty, got %q", env.Command)
	}
}

func TestCanonicalString(t *testing.T) {
	input := map[string]int{"b": 2, "a": 1}
	str, err := CanonicalString(input)
	if err != nil {
		t.Fatalf("CanonicalString failed: %v", err)
	}
	if str == "" {
		t.Fatalf("expected non-empty canonical string")
	}
	if !strings.HasPrefix(str, "{\"a\":") {
		t.Fatalf("expected keys to be sorted, got %s", str)
	}
}

func TestWriteFunction(t *testing.T) {
	buf := &bytes.Buffer{}
	env := OK("cmd", map[string]string{"key": "value"})

	err := Write(buf, env)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("expected buffer to contain data")
	}

	// Verify the output is valid JSON
	var decoded Envelope
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestMemoryRef(t *testing.T) {
	memRef := &MemoryRef{
		Name:      "test-memory",
		Type:      "cache",
		Workspace: "ws1",
	}

	env := OK("cmd", nil, WithMetaMutator(func(m *Meta) {
		m.Memory = memRef
	}))

	if env.Meta.Memory == nil {
		t.Fatalf("expected memory ref to be set")
	}
	if env.Meta.Memory.Name != "test-memory" {
		t.Fatalf("expected memory name test-memory, got %s", env.Meta.Memory.Name)
	}
	if env.Meta.Memory.Type != "cache" {
		t.Fatalf("expected memory type cache, got %s", env.Meta.Memory.Type)
	}
}

func TestErrorWithData(t *testing.T) {
	errorData := map[string]string{"detail": "additional context"}
	env := Error("cmd", "TEST_ERR", "failed", errorData)

	if env.Data == nil {
		t.Fatalf("expected data to be set")
	}
	data, ok := env.Data.(map[string]string)
	if !ok {
		t.Fatalf("expected data to be map[string]string")
	}
	if data["detail"] != "additional context" {
		t.Fatalf("unexpected data value: %v", data)
	}
}

func TestMultipleOptions(t *testing.T) {
	env := OK(
		"cmd", nil,
		WithMetaMutator(func(m *Meta) {
			m.Source = "source1"
		}),
		WithData(map[string]int{"num": 42}),
		WithMetaMutator(func(m *Meta) {
			m.DurationMS = 100
		}),
	)

	if env.Meta.Source != "source1" {
		t.Fatalf("expected source1, got %s", env.Meta.Source)
	}
	if env.Meta.DurationMS != 100 {
		t.Fatalf("expected duration_ms 100, got %d", env.Meta.DurationMS)
	}
	data, ok := env.Data.(map[string]int)
	if !ok || data["num"] != 42 {
		t.Fatalf("expected data to be set correctly")
	}
}

func FuzzValidateEnvelopeJSONMaintainsCoreInvariants(f *testing.F) {
	validOK, err := json.Marshal(OK(
		"foxctl.test",
		map[string]any{"value": "ok"},
		WithTimestamp(time.Date(2025, 11, 8, 13, 0, 0, 0, time.UTC)),
	))
	if err != nil {
		f.Fatalf("marshal ok seed: %v", err)
	}
	validError, err := json.Marshal(Error(
		"foxctl.test",
		"TEST_ERROR",
		"something went wrong",
		map[string]any{"value": "error"},
		WithTimestamp(time.Date(2025, 11, 8, 13, 0, 0, 0, time.UTC)),
	))
	if err != nil {
		f.Fatalf("marshal error seed: %v", err)
	}

	f.Add(string(validOK))
	f.Add(string(validError))
	f.Add(`{"version":1,"status":"ok","command":"foxctl.test","meta":{"ts":"2025-11-08T13:00:00Z"},"error":{"code":"UNEXPECTED"}}`)
	f.Add(`{"version":1,"status":"error","command":"foxctl.test","meta":{"ts":"2025-11-08T13:00:00Z"},"error":{"code":"TEST_ERROR","message":""}}`)
	f.Add(`{"version":2,"status":"progress","command":"","meta":{"ts":"not-a-time"},"error":{}}`)

	f.Fuzz(func(t *testing.T, raw string) {
		var env Envelope
		if err := json.Unmarshal([]byte(raw), &env); err != nil {
			return
		}
		if err := Validate(env); err != nil {
			return
		}
		if err := coreEnvelopeInvariantError(env); err != nil {
			t.Fatalf("valid envelope violated core invariant: %v", err)
		}
		canonical, err := Canonicalize(env)
		if err != nil {
			t.Fatalf("valid envelope should canonicalize: %v", err)
		}
		if !json.Valid(canonical) {
			t.Fatalf("canonical envelope is not valid json: %s", canonical)
		}
	})
}

func coreEnvelopeInvariantError(env Envelope) error {
	if env.Version != Version {
		return fmt.Errorf("version = %d, want %d", env.Version, Version)
	}
	if strings.TrimSpace(env.Command) == "" {
		return fmt.Errorf("command must be non-empty")
	}
	switch env.Status {
	case StatusOK:
		if env.Error.Code != "" || env.Error.Message != "" {
			return fmt.Errorf("ok envelope must not contain error fields: %+v", env.Error)
		}
	case StatusError:
		if env.Error.Code == "" {
			return fmt.Errorf("error envelope must contain error code")
		}
		if strings.TrimSpace(env.Error.Message) == "" {
			return fmt.Errorf("error envelope must contain error message")
		}
	default:
		return fmt.Errorf("status = %q, want %q or %q", env.Status, StatusOK, StatusError)
	}

	ts := strings.TrimSpace(env.Meta.TS)
	if ts == "" {
		return fmt.Errorf("timestamp must be non-empty")
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return fmt.Errorf("timestamp must parse as RFC3339: %w", err)
	}
	if parsed.Location() != time.UTC {
		return fmt.Errorf("timestamp must be UTC")
	}
	return nil
}
