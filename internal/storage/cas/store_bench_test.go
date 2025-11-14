package cas

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// BenchmarkBufferPoolPut measures CAS Put operation with buffer pooling.
func BenchmarkBufferPoolPut(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "cas-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			b.Fatalf("cleanup tmp dir: %v", err)
		}
	}()

	store, err := NewStore(tmpDir)
	if err != nil {
		b.Fatal(err)
	}

	// Test data of various sizes
	testData := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
		{"1MB", 1024 * 1024},
	}

	for _, td := range testData {
		data := bytes.Repeat([]byte("x"), td.size)

		b.Run(td.name, func(b *testing.B) {
			b.SetBytes(int64(td.size))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				reader := bytes.NewReader(data)
				if _, err := store.Put(context.Background(), reader, "benchmark", nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSlicePreallocation measures impact of pre-allocating slices.
func BenchmarkSlicePreallocation(b *testing.B) {
	testStrings := []string{"tag1", "tag2", "tag3", "tag4", "tag5"}

	b.Run("WithPreallocation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			result := make([]string, 0, len(testStrings))
			result = append(result, testStrings...)
			_ = result
		}
	})

	b.Run("WithoutPreallocation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var result []string
			result = append(result, testStrings...)
			_ = result
		}
	})
}

// BenchmarkContextCancellation measures context check overhead.
func BenchmarkContextCancellation(b *testing.B) {
	ctx := context.Background()
	var sink int

	b.Run("WithContextCheck", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for j := 0; j < 100; j++ {
				if err := ctx.Err(); err != nil {
					break
				}
			}
		}
	})

	b.Run("WithoutContextCheck", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for j := 0; j < 100; j++ {
				sink += j
			}
		}
	})
	_ = sink
}
