package scheduler

import (
	"context"
	"strconv"
	"testing"
)

func BenchmarkWFQEnqueueMixedNamespaces(b *testing.B) {
	namespaces := []string{"runtime", "repoindex", "rlm", "rooms"}
	execute := func(context.Context) error { return nil }

	b.ReportAllocs()
	for b.Loop() {
		s := NewWFQScheduler(Config{DefaultWeight: 1, WorkerCount: 8})
		s.SetWeight("runtime", 4)
		s.SetWeight("repoindex", 2)
		for i := 0; i < 256; i++ {
			if err := s.Enqueue(&Job{
				ID:        "job-" + strconv.Itoa(i),
				Namespace: namespaces[i%len(namespaces)],
				Execute:   execute,
			}); err != nil {
				b.Fatalf("Enqueue() error = %v", err)
			}
		}
	}
}

func BenchmarkWFQDispatchReadyJob(b *testing.B) {
	execute := func(context.Context) error { return nil }
	s := NewWFQScheduler(Config{DefaultWeight: 1, WorkerCount: 64})
	s.SetWeight("runtime", 4)
	s.SetWeight("rlm", 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if err := s.Enqueue(&Job{
			ID:        "job-" + strconv.Itoa(i),
			Namespace: "runtime",
			Execute:   execute,
		}); err != nil {
			b.Fatalf("Enqueue() error = %v", err)
		}
		s.dispatchNext()
		select {
		case <-s.workCh:
		default:
			b.Fatal("dispatchNext() did not dispatch a ready job")
		}
	}
}
