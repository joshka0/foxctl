package vector

import (
	"math"
	"testing"
	"testing/quick"
)

func TestSerializeDeserializeF32(t *testing.T) {
	original := []float32{0.1, -0.25, 3.5, 0}

	encoded := SerializeF32(original)
	if len(encoded) != len(original)*4 {
		t.Fatalf("encoded length = %d, want %d", len(encoded), len(original)*4)
	}

	decoded := DeserializeF32(encoded)
	if len(decoded) != len(original) {
		t.Fatalf("decoded length = %d, want %d", len(decoded), len(original))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Fatalf("decoded[%d] = %v, want %v", i, decoded[i], original[i])
		}
	}
}

func TestDeserializeF32RejectsInvalidByteLengths(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		{},
		{1},
		{1, 2, 3},
		{1, 2, 3, 4, 5},
	} {
		if got := DeserializeF32(data); got != nil {
			t.Fatalf("DeserializeF32(%v) = %v, want nil", data, got)
		}
	}
}

func TestDimsFromBytes(t *testing.T) {
	if got := DimsFromBytes(SerializeF32([]float32{1, 2, 3})); got != 3 {
		t.Fatalf("DimsFromBytes(valid) = %d, want 3", got)
	}
	if got := DimsFromBytes([]byte{1, 2, 3}); got != 0 {
		t.Fatalf("DimsFromBytes(invalid) = %d, want 0", got)
	}
}

func TestCosine(t *testing.T) {
	tests := []struct {
		name string
		a    []float32
		b    []float32
		want float64
	}{
		{name: "same direction", a: []float32{1, 0}, b: []float32{1, 0}, want: 1},
		{name: "orthogonal", a: []float32{1, 0}, b: []float32{0, 1}, want: 0},
		{name: "opposite", a: []float32{1, 0}, b: []float32{-1, 0}, want: -1},
		{name: "mismatch", a: []float32{1}, b: []float32{1, 2}, want: 0},
		{name: "zero vector", a: []float32{0, 0}, b: []float32{1, 2}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Cosine(tt.a, tt.b); math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("Cosine() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCosineRejectsNonFiniteVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    []float32
		b    []float32
	}{
		{name: "nan in left", a: []float32{1, float32(math.NaN())}, b: []float32{1, 2}},
		{name: "nan in right", a: []float32{1, 2}, b: []float32{1, float32(math.NaN())}},
		{name: "positive infinity", a: []float32{1, float32(math.Inf(1))}, b: []float32{1, 2}},
		{name: "negative infinity", a: []float32{1, 2}, b: []float32{1, float32(math.Inf(-1))}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Cosine(tt.a, tt.b); got != 0 {
				t.Fatalf("Cosine(%v, %v) = %v, want 0", tt.a, tt.b, got)
			}
		})
	}
}

func TestSerializeDeserializeF32PropertyPreservesBitsAndDimensions(t *testing.T) {
	t.Parallel()

	property := func(raw []uint32) bool {
		if len(raw) > 32 {
			raw = raw[:32]
		}
		values := make([]float32, len(raw))
		for i, bits := range raw {
			values[i] = math.Float32frombits(bits)
		}

		encoded := SerializeF32(values)
		if len(raw) == 0 {
			return encoded == nil && DeserializeF32(encoded) == nil && DimsFromBytes(encoded) == 0
		}
		if len(encoded) != len(raw)*4 {
			t.Logf("encoded len=%d want %d", len(encoded), len(raw)*4)
			return false
		}
		if got := DimsFromBytes(encoded); got != len(raw) {
			t.Logf("DimsFromBytes(encoded)=%d want %d", got, len(raw))
			return false
		}

		decoded := DeserializeF32(encoded)
		if len(decoded) != len(values) {
			t.Logf("decoded len=%d want %d", len(decoded), len(values))
			return false
		}
		for i := range decoded {
			if got := math.Float32bits(decoded[i]); got != raw[i] {
				t.Logf("decoded[%d] bits=%08x want %08x", i, got, raw[i])
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestCosinePropertySymmetricBoundedAndSelfSimilar(t *testing.T) {
	t.Parallel()

	property := func(rawA, rawB []uint8, scaleSeed uint8) bool {
		a, b := pairedFiniteVectors(rawA, rawB)
		if len(a) == 0 {
			return Cosine(a, b) == 0
		}

		ab := Cosine(a, b)
		ba := Cosine(b, a)
		if math.Abs(ab-ba) > 1e-12 {
			t.Logf("Cosine(a,b)=%v Cosine(b,a)=%v a=%v b=%v", ab, ba, a, b)
			return false
		}
		if math.Abs(ab) > 1+1e-12 {
			t.Logf("Cosine(a,b)=%v outside [-1,1] a=%v b=%v", ab, a, b)
			return false
		}

		if !isZeroVector(a) {
			self := Cosine(a, a)
			if math.Abs(self-1) > 1e-12 {
				t.Logf("Cosine(a,a)=%v want 1 for a=%v", self, a)
				return false
			}
			scale := float32(int(scaleSeed%7) + 1)
			scaled := make([]float32, len(a))
			for i := range a {
				scaled[i] = a[i] * scale
			}
			if got := Cosine(a, scaled); math.Abs(got-1) > 1e-12 {
				t.Logf("Cosine(a, positive-scale*a)=%v want 1 scale=%v a=%v", got, scale, a)
				return false
			}
		}
		if isZeroVector(a) && Cosine(a, b) != 0 {
			t.Logf("Cosine(zero,b)=%v want 0 b=%v", Cosine(a, b), b)
			return false
		}
		if isZeroVector(b) && Cosine(a, b) != 0 {
			t.Logf("Cosine(a,zero)=%v want 0 a=%v", Cosine(a, b), a)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestCosinePropertyAlwaysReturnsFiniteScore(t *testing.T) {
	t.Parallel()

	property := func(rawA, rawB []uint8) bool {
		a, b := pairedPossiblyNonFiniteVectors(rawA, rawB)
		score := Cosine(a, b)
		if math.IsNaN(score) || math.IsInf(score, 0) {
			t.Logf("Cosine(a,b)=%v for a=%v b=%v", score, a, b)
			return false
		}
		if math.Abs(score) > 1+1e-12 {
			t.Logf("Cosine(a,b)=%v outside [-1,1] a=%v b=%v", score, a, b)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func pairedFiniteVectors(rawA, rawB []uint8) ([]float32, []float32) {
	length := len(rawA)
	if len(rawB) < length {
		length = len(rawB)
	}
	if length > 16 {
		length = 16
	}
	a := make([]float32, length)
	b := make([]float32, length)
	for i := 0; i < length; i++ {
		a[i] = generatedCosineComponent(rawA[i])
		b[i] = generatedCosineComponent(rawB[i])
	}
	return a, b
}

func generatedCosineComponent(seed uint8) float32 {
	return float32(int(seed%7) - 3)
}

func pairedPossiblyNonFiniteVectors(rawA, rawB []uint8) ([]float32, []float32) {
	a, b := pairedFiniteVectors(rawA, rawB)
	if len(a) == 0 {
		return a, b
	}
	for i := range a {
		a[i] = generatedPossiblyNonFiniteComponent(rawA[i])
		b[i] = generatedPossiblyNonFiniteComponent(rawB[i])
	}
	return a, b
}

func generatedPossiblyNonFiniteComponent(seed uint8) float32 {
	switch seed % 11 {
	case 0:
		return float32(math.NaN())
	case 1:
		return float32(math.Inf(1))
	case 2:
		return float32(math.Inf(-1))
	default:
		return generatedCosineComponent(seed)
	}
}

func isZeroVector(values []float32) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value != 0 {
			return false
		}
	}
	return true
}
