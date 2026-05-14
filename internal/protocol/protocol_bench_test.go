package protocol

import (
	"bytes"
	"testing"
)

func BenchmarkEnvelopeWriteOKSmallPayload(b *testing.B) {
	payload := map[string]any{
		"summary": "benchmark payload",
		"artifact": map[string]any{
			"digest": "sha256:0123456789abcdef",
		},
	}

	b.ReportAllocs()
	for b.Loop() {
		var buf bytes.Buffer
		if err := WriteOK(&buf, "benchmark.envelope", payload, WithSource("bench")); err != nil {
			b.Fatalf("WriteOK() error = %v", err)
		}
	}
}

func BenchmarkEnvelopeDecodeSmallPayload(b *testing.B) {
	var buf bytes.Buffer
	if err := WriteOK(&buf, "benchmark.envelope", map[string]any{
		"summary": "benchmark payload",
		"artifact": map[string]any{
			"digest": "sha256:0123456789abcdef",
		},
	}, WithSource("bench")); err != nil {
		b.Fatalf("WriteOK() error = %v", err)
	}
	raw := append([]byte(nil), buf.Bytes()...)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		env, err := DecodeEnvelope(raw)
		if err != nil {
			b.Fatalf("DecodeEnvelope() error = %v", err)
		}
		if env.Command != "benchmark.envelope" {
			b.Fatalf("command=%q", env.Command)
		}
	}
}
