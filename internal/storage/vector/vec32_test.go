package vector

import (
	"math"
	"testing"
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
