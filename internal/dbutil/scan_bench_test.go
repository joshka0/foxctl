package dbutil

import (
	"testing"
)

var (
	testTimestamps = []string{
		"2024-01-15T10:30:45.123456789Z",
		"2024-01-15T10:31:45.123456789Z",
		"2024-01-15T10:32:45.123456789Z",
	}
	testJSONArray = `["digest1","digest2","digest3","digest4"]`
)

// BenchmarkScanTimestamps measures timestamp scanning performance.
func BenchmarkScanTimestamps(b *testing.B) {
	b.Run("ScanTimestamps", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = ScanTimestamps(testTimestamps...)
		}
	})

	b.Run("ScanTimestampsMust", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = ScanTimestampsMust(testTimestamps...)
		}
	})
}

// BenchmarkScanJSONArray measures JSON array parsing performance.
func BenchmarkScanJSONArray(b *testing.B) {
	b.Run("ScanJSONArray", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = ScanJSONArray(testJSONArray)
		}
	})
}

// BenchmarkTimeScanner measures TimeScanner performance.
func BenchmarkTimeScanner(b *testing.B) {
	scanner := &TimeScanner{
		Created:  testTimestamps[0],
		Updated:  testTimestamps[1],
		Accessed: testTimestamps[2],
	}

	b.Run("Parse", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _, _, _ = scanner.Parse()
		}
	})

	b.Run("MustParse", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _, _ = scanner.MustParse()
		}
	})
}
