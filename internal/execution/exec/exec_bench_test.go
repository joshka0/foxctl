package execrunner

import (
	"bytes"
	"context"
	"testing"
)

// BenchmarkBufferPooling measures the performance impact of buffer pooling.
func BenchmarkBufferPooling(b *testing.B) {
	input := []byte("test input data for benchmarking")

	b.Run("WithPooling", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			stdout := bufferPool.Get().(*bytes.Buffer)
			stderr := bufferPool.Get().(*bytes.Buffer)
			stdout.Reset()
			stderr.Reset()

			stdout.Write(input)
			stderr.Write(input)

			_ = stdout.Bytes()
			_ = stderr.Bytes()

			bufferPool.Put(stdout)
			bufferPool.Put(stderr)
		}
	})

	b.Run("WithoutPooling", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			stdout.Write(input)
			stderr.Write(input)

			_ = stdout.Bytes()
			_ = stderr.Bytes()
		}
	})
}

// BenchmarkRunnerExecution benchmarks the full execution path.
func BenchmarkRunnerExecution(b *testing.B) {
	// This is a synthetic benchmark - in real usage, you'd benchmark with actual skills
	ctx := context.Background()
	input := []byte(`{"message": "test"}`)

	b.Run("BufferAllocation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			stdout := bufferPool.Get().(*bytes.Buffer)
			stderr := bufferPool.Get().(*bytes.Buffer)
			stdout.Reset()
			stderr.Reset()

			// Simulate skill output
			stdout.Write(input)

			result := stdout.Bytes()
			_ = result

			bufferPool.Put(stdout)
			bufferPool.Put(stderr)
		}
	})

	_ = ctx // Suppress unused warning
}
