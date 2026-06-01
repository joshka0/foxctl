package dbdriver

import (
	"math"
	"strings"
	"testing"
	"testing/quick"
)

func TestVectorJSONRoundTripPropertyPreservesGeneratedFiniteValues(t *testing.T) {
	t.Parallel()

	property := func(raw []int16) bool {
		if len(raw) > 32 {
			raw = raw[:32]
		}
		vector := make(Vector, len(raw))
		for i, value := range raw {
			vector[i] = float32(value) / 16
		}

		encoded, err := vector.MarshalJSON()
		if err != nil {
			t.Logf("MarshalJSON(%v) error=%v", vector, err)
			return false
		}
		parsed, err := ParseVector(string(encoded))
		if err != nil {
			t.Logf("ParseVector(%s) error=%v", string(encoded), err)
			return false
		}
		if len(parsed) != len(vector) {
			t.Logf("parsed len=%d want %d", len(parsed), len(vector))
			return false
		}
		for i := range parsed {
			if parsed[i] != vector[i] {
				t.Logf("parsed[%d]=%v want %v encoded=%s", i, parsed[i], vector[i], string(encoded))
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestVectorStringOutputIsParseableAndUsesCanonicalSQLShape(t *testing.T) {
	t.Parallel()

	vector := Vector{1, -2.5, 0.125}
	rendered := vector.String()
	if rendered != "[1.000000,-2.500000,0.125000]" {
		t.Fatalf("Vector.String()=%q", rendered)
	}
	if strings.Contains(rendered, " ") {
		t.Fatalf("Vector.String() contains spaces: %q", rendered)
	}
	parsed, err := ParseVector(rendered)
	if err != nil {
		t.Fatalf("ParseVector(Vector.String()) error=%v", err)
	}
	if len(parsed) != len(vector) {
		t.Fatalf("parsed len=%d want %d", len(parsed), len(vector))
	}
	for i := range vector {
		if parsed[i] != vector[i] {
			t.Fatalf("parsed[%d]=%v want %v", i, parsed[i], vector[i])
		}
	}
}

func TestParseVectorRejectsNullJSON(t *testing.T) {
	t.Parallel()

	vector, err := ParseVector("null")
	if err == nil {
		t.Fatalf("ParseVector(null) error=nil, vector=%v; want rejection", vector)
	}
}

func TestVectorHelperValidateVectorEnforcesDimensionsAndFiniteValues(t *testing.T) {
	t.Parallel()

	helper := &VectorHelper{dimensions: 3}
	if err := helper.ValidateVector(Vector{1, 2, 3}); err != nil {
		t.Fatalf("ValidateVector(valid) error=%v", err)
	}
	for _, tc := range []struct {
		name   string
		vector Vector
	}{
		{name: "too short", vector: Vector{1, 2}},
		{name: "too long", vector: Vector{1, 2, 3, 4}},
		{name: "nan", vector: Vector{1, float32(math.NaN()), 3}},
		{name: "positive infinity", vector: Vector{1, float32(math.Inf(1)), 3}},
		{name: "negative infinity", vector: Vector{1, float32(math.Inf(-1)), 3}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := helper.ValidateVector(tc.vector); err == nil {
				t.Fatalf("ValidateVector(%v) error=nil, want rejection", tc.vector)
			}
		})
	}
}

func TestVectorHelperValidateVectorPropertyAcceptsOnlyExactFiniteDimensions(t *testing.T) {
	t.Parallel()

	property := func(dimSeed uint8, rawValues []int16, invalidKind uint8) bool {
		dimensions := int(dimSeed%8) + 1
		helper := &VectorHelper{dimensions: dimensions}
		vector := generatedFiniteVector(rawValues, dimensions)
		if err := helper.ValidateVector(vector); err != nil {
			t.Logf("ValidateVector(valid len=%d) error=%v vector=%v", dimensions, err, vector)
			return false
		}

		invalid := append(Vector(nil), vector...)
		switch invalidKind % 4 {
		case 0:
			invalid = invalid[:len(invalid)-1]
		case 1:
			invalid = append(invalid, 1)
		case 2:
			invalid[dimensions/2] = float32(math.NaN())
		default:
			invalid[dimensions/2] = float32(math.Inf(-1))
		}
		if err := helper.ValidateVector(invalid); err == nil {
			t.Logf("ValidateVector(invalid kind=%d len=%d dims=%d vector=%v) error=nil", invalidKind%4, len(invalid), dimensions, invalid)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func generatedFiniteVector(raw []int16, dimensions int) Vector {
	vector := make(Vector, dimensions)
	for i := 0; i < dimensions; i++ {
		if i < len(raw) {
			vector[i] = float32(raw[i]) / 16
			continue
		}
		vector[i] = float32(i + 1)
	}
	return vector
}
