package timeutil

import (
	"testing"
	"time"
)

var testTimestamp = "2024-01-15T10:30:45.123456789Z"

// BenchmarkTimeFormatting measures time formatting performance.
func BenchmarkTimeFormatting(b *testing.B) {
	now := time.Now().UTC()

	b.Run("FormatRFC3339Nano", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = FormatRFC3339Nano(now)
		}
	})

	b.Run("DirectFormat", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = now.Format(time.RFC3339Nano)
		}
	})
}

// BenchmarkTimeParsing measures time parsing performance.
func BenchmarkTimeParsing(b *testing.B) {
	b.Run("ParseRFC3339Nano", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = ParseRFC3339Nano(testTimestamp)
		}
	})

	b.Run("MustParseRFC3339Nano", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = MustParseRFC3339Nano(testTimestamp)
		}
	})

	b.Run("DirectParse", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = time.Parse(time.RFC3339Nano, testTimestamp)
		}
	})
}

// BenchmarkNowUTC measures current time retrieval.
func BenchmarkNowUTC(b *testing.B) {
	b.Run("NowUTC", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = NowUTC()
		}
	})

	b.Run("FormatNowUTC", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = FormatNowUTC()
		}
	})

	b.Run("DirectNowFormat", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = time.Now().UTC().Format(time.RFC3339Nano)
		}
	})
}
