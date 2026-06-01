package contextplane

import (
	"reflect"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

var contextplaneEvidenceRefTypes = []contextengine.RefType{
	contextengine.RefTypePath,
	contextengine.RefTypeSymbol,
	contextengine.RefTypeTask,
	contextengine.RefTypeSession,
	contextengine.RefTypeMemoryClaim,
	contextengine.RefTypeNote,
	contextengine.RefTypeArtifact,
	contextengine.RefTypeTrajectory,
	contextengine.RefTypeCommit,
	contextengine.RefTypeEvent,
	contextengine.RefTypeRun,
	contextengine.RefTypeToolCall,
}

func TestEvidenceRefsToStringsCanonicalizesRefs(t *testing.T) {
	t.Parallel()

	got := EvidenceRefsToStrings([]contextengine.EvidenceRef{
		{Type: " path ", Ref: " internal/foo.go "},
		{Ref: " bare.go "},
		{Type: contextengine.RefTypePath, Ref: "   "},
	})
	want := []string{"path:internal/foo.go", "bare.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EvidenceRefsToStrings() = %#v, want %#v", got, want)
	}
}

func TestStringsToEvidenceRefsCanonicalizesRefs(t *testing.T) {
	t.Parallel()

	got := StringsToEvidenceRefs([]string{" path:internal/foo.go ", " bare.go ", "\t"})
	want := []contextengine.EvidenceRef{
		{Type: contextengine.RefTypePath, Ref: "internal/foo.go"},
		{Type: contextengine.RefTypePath, Ref: "bare.go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StringsToEvidenceRefs() = %#v, want %#v", got, want)
	}
}

func TestUniqueEvidenceRefsCanonicalizesBeforeDedupe(t *testing.T) {
	t.Parallel()

	got := UniqueEvidenceRefs([]contextengine.EvidenceRef{
		{Type: " path ", Ref: " internal/foo.go ", WorkspaceID: " ws-1 "},
		{Type: contextengine.RefTypePath, Ref: "internal/foo.go", WorkspaceID: "ws-2"},
		{Type: contextengine.RefTypeSymbol, Ref: "internal/foo.go"},
	})
	want := []contextengine.EvidenceRef{
		{Type: contextengine.RefTypePath, Ref: "internal/foo.go", WorkspaceID: "ws-1"},
		{Type: contextengine.RefTypeSymbol, Ref: "internal/foo.go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UniqueEvidenceRefs() = %#v, want %#v", got, want)
	}
}

func TestEvidenceRefsPropertyStringRoundTripStable(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(typeSeed uint8, rawValue string) bool {
		refType := contextplaneEvidenceRefTypes[int(typeSeed)%len(contextplaneEvidenceRefTypes)]
		refValue := contextplanePropertyRefValue(rawValue)
		input := []contextengine.EvidenceRef{{
			Type: contextengine.RefType(" " + string(refType) + " "),
			Ref:  " " + refValue + " ",
		}}

		stringsOnce := EvidenceRefsToStrings(input)
		refs := StringsToEvidenceRefs(stringsOnce)
		stringsTwice := EvidenceRefsToStrings(refs)
		return len(refs) == 1 &&
			refs[0].Type == refType &&
			refs[0].Ref == refValue &&
			reflect.DeepEqual(stringsOnce, stringsTwice)
	}, cfg)
	if err != nil {
		t.Fatalf("evidence ref string round-trip property failed: %v", err)
	}
}

func TestUniqueEvidenceRefsPropertyIdempotent(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(typeSeedA, typeSeedB uint8, rawA, rawB string) bool {
		typeA := contextplaneEvidenceRefTypes[int(typeSeedA)%len(contextplaneEvidenceRefTypes)]
		typeB := contextplaneEvidenceRefTypes[int(typeSeedB)%len(contextplaneEvidenceRefTypes)]
		valueA := contextplanePropertyRefValue(rawA)
		valueB := contextplanePropertyRefValue(rawB)
		refs := []contextengine.EvidenceRef{
			{Type: typeA, Ref: valueA},
			{Type: contextengine.RefType(" " + string(typeA) + " "), Ref: " " + valueA + " "},
			{Type: typeB, Ref: valueB},
		}

		once := UniqueEvidenceRefs(refs)
		twice := UniqueEvidenceRefs(once)
		if !reflect.DeepEqual(once, twice) {
			return false
		}

		seen := map[string]struct{}{}
		for _, ref := range once {
			formatted := contextengine.FormatEvidenceRef(ref)
			if formatted == "" || strings.TrimSpace(formatted) != formatted {
				return false
			}
			if _, ok := seen[formatted]; ok {
				return false
			}
			seen[formatted] = struct{}{}
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("unique evidence ref property failed: %v", err)
	}
}

func contextplanePropertyRefValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "value"
	}
	return value
}
