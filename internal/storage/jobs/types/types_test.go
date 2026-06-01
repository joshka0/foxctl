package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/quick"
)

func TestValidateStateAllowsOnlyDocumentedLifecycleStates(t *testing.T) {
	t.Parallel()

	validStates := []State{StateQueued, StateRunning, StateOK, StateError, StateCanceled}
	for _, state := range validStates {
		if err := ValidateState(state); err != nil {
			t.Fatalf("ValidateState(%q) = %v, want nil", state, err)
		}
	}

	for _, state := range []State{"", "pending", "paused", "failed", "ok "} {
		err := ValidateState(state)
		if !errors.Is(err, ErrInvalidState) {
			t.Fatalf("ValidateState(%q) = %v, want ErrInvalidState", state, err)
		}
	}
}

func TestValidateStatePropertyUnknownStatesFailClosed(t *testing.T) {
	t.Parallel()

	valid := map[State]bool{
		StateQueued:   true,
		StateRunning:  true,
		StateOK:       true,
		StateError:    true,
		StateCanceled: true,
	}
	property := func(raw string) bool {
		state := State(raw)
		err := ValidateState(state)
		if valid[state] {
			return err == nil
		}
		return errors.Is(err, ErrInvalidState)
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestHashArgsUsesFieldBoundaries(t *testing.T) {
	t.Parallel()

	first := HashArgs("ab", []byte("c"))
	second := HashArgs("a", []byte("bc"))
	if first == second {
		t.Fatalf("HashArgs collided when command/args boundary moved: %s", first)
	}
}

func TestHashArgsPropertyBoundaryChangesHash(t *testing.T) {
	t.Parallel()

	property := func(raw []byte) bool {
		payload := append([]byte("abc"), raw...)
		if len(payload) > 64 {
			payload = payload[:64]
		}

		leftSplit := 1
		rightSplit := len(payload) - 1
		if leftSplit == rightSplit {
			return true
		}

		leftHash := HashArgs(string(payload[:leftSplit]), payload[leftSplit:])
		rightHash := HashArgs(string(payload[:rightSplit]), payload[rightSplit:])
		return leftHash != rightHash
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestHashArgsDeterministicHexDigest(t *testing.T) {
	t.Parallel()

	first := HashArgs("skill/test", []byte(`{"input_sha256":"abc"}`))
	second := HashArgs("skill/test", []byte(`{"input_sha256":"abc"}`))
	if first != second {
		t.Fatalf("HashArgs is not deterministic: %q != %q", first, second)
	}
	if len(first) != sha256.Size*2 {
		t.Fatalf("HashArgs length = %d, want %d", len(first), sha256.Size*2)
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("HashArgs returned non-hex digest %q: %v", first, err)
	}
}

func TestMarshalSkillArgsRecordsDigestMetadataWithoutRawInput(t *testing.T) {
	t.Parallel()

	input := []byte("secret payload")
	payload := MarshalSkillArgs("test/skill", input)
	if strings.Contains(string(payload), string(input)) {
		t.Fatalf("MarshalSkillArgs leaked raw input in %q", payload)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("MarshalSkillArgs returned invalid JSON: %v", err)
	}
	if got["skill"] != "test/skill" {
		t.Fatalf("skill=%v want test/skill", got["skill"])
	}
	if got["input_size_bytes"] != float64(len(input)) {
		t.Fatalf("input_size_bytes=%v want %d", got["input_size_bytes"], len(input))
	}
	sum := sha256.Sum256(input)
	if got["input_sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("input_sha256=%v want %s", got["input_sha256"], hex.EncodeToString(sum[:]))
	}
}

func TestMarshalSkillArgsEmptyInputOmitsInputMetadata(t *testing.T) {
	t.Parallel()

	var got map[string]any
	if err := json.Unmarshal(MarshalSkillArgs("test/skill", nil), &got); err != nil {
		t.Fatalf("MarshalSkillArgs returned invalid JSON: %v", err)
	}
	if got["skill"] != "test/skill" {
		t.Fatalf("skill=%v want test/skill", got["skill"])
	}
	if _, ok := got["input_size_bytes"]; ok {
		t.Fatalf("empty input unexpectedly recorded input_size_bytes: %#v", got)
	}
	if _, ok := got["input_sha256"]; ok {
		t.Fatalf("empty input unexpectedly recorded input_sha256: %#v", got)
	}
}

func TestComputeSkillArgsHashPropertyInputDigestAffectsHash(t *testing.T) {
	t.Parallel()

	property := func(raw []byte, indexSeed uint8) bool {
		if len(raw) == 0 {
			raw = []byte{0}
		}
		if len(raw) > 64 {
			raw = raw[:64]
		}

		changed := append([]byte(nil), raw...)
		changed[int(indexSeed)%len(changed)] ^= 0x01

		return ComputeSkillArgsHash("test/skill", raw) != ComputeSkillArgsHash("test/skill", changed)
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}
