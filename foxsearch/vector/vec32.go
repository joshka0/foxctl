package vector

import (
	"encoding/binary"
	"math"
)

// SerializeF32 converts a float32 slice to binary bytes (little-endian).
// Returns nil for empty input.
func SerializeF32(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DeserializeF32 converts binary bytes back to a float32 slice (little-endian).
// Returns nil if data is empty or not a multiple of 4 bytes.
func DeserializeF32(data []byte) []float32 {
	if len(data) == 0 || len(data)%4 != 0 {
		return nil
	}
	result := make([]float32, len(data)/4)
	for i := range result {
		bits := binary.LittleEndian.Uint32(data[i*4:])
		result[i] = math.Float32frombits(bits)
	}
	return result
}

// Cosine computes the cosine similarity between two vectors.
// Returns 0 if vectors have different lengths, are empty, or either has zero magnitude.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		if math.IsNaN(av) || math.IsInf(av, 0) || math.IsNaN(bv) || math.IsInf(bv, 0) {
			return 0
		}
		dotProduct += av * bv
		normA += av * av
		normB += bv * bv
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	score := dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0
	}
	return score
}

// DimsFromBytes returns the number of float32 dimensions from byte length.
// Returns 0 if bytes is not a valid float32 array.
func DimsFromBytes(data []byte) int {
	if len(data) == 0 || len(data)%4 != 0 {
		return 0
	}
	return len(data) / 4
}
