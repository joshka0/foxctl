package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWFQSchedulerEnqueue(t *testing.T) {
	config := Config{
		DefaultWeight: 1,
		WorkerCount:   2,
	}
	scheduler := NewWFQScheduler(config)

	job := &Job{
		ID:        "job-1",
		Namespace: "ns1",
		Execute: func(ctx context.Context) error {
			return nil
		},
	}

	err := scheduler.Enqueue(job)
	if err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}

	stats := scheduler.Stats()
	if stats.QueuedJobs != 1 {
		t.Errorf("expected 1 queued job, got %d", stats.QueuedJobs)
	}
}

func TestWFQSchedulerSetWeight(t *testing.T) {
	scheduler := NewWFQScheduler(DefaultConfig())

	scheduler.SetWeight("high-priority", 10)
	scheduler.SetWeight("low-priority", 1)

	highWeight := scheduler.GetWeight("high-priority")
	if highWeight != 10 {
		t.Errorf("expected weight 10, got %d", highWeight)
	}

	lowWeight := scheduler.GetWeight("low-priority")
	if lowWeight != 1 {
		t.Errorf("expected weight 1, got %d", lowWeight)
	}

	defaultWeight := scheduler.GetWeight("unknown")
	if defaultWeight != 1 {
		t.Errorf("expected default weight 1, got %d", defaultWeight)
	}
}

func TestWFQSchedulerExecution(t *testing.T) {
	config := Config{
		DefaultWeight: 1,
		WorkerCount:   2,
	}
	scheduler := NewWFQScheduler(config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	scheduler.Start(ctx)
	defer scheduler.Stop()

	var executed int32
	job := &Job{
		ID:        "job-1",
		Namespace: "ns1",
		Execute: func(ctx context.Context) error {
			atomic.AddInt32(&executed, 1)
			return nil
		},
	}

	err := scheduler.Enqueue(job)
	if err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}

	// Wait for execution
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&executed) != 1 {
		t.Errorf("expected job to be executed once, got %d", executed)
	}
}

func TestWFQSchedulerFairness(t *testing.T) {
	config := Config{
		DefaultWeight: 1,
		WorkerCount:   2,
	}
	scheduler := NewWFQScheduler(config)

	// Set weights: ns1 has 2x capacity of ns2
	scheduler.SetWeight("ns1", 2)
	scheduler.SetWeight("ns2", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	scheduler.Start(ctx)
	defer scheduler.Stop()

	var ns1Count, ns2Count int32
	var mu sync.Mutex

	// Enqueue 20 jobs for each namespace
	for i := 0; i < 20; i++ {
		job1 := &Job{
			ID:        "ns1-job-" + string(rune(i)),
			Namespace: "ns1",
			Execute: func(ctx context.Context) error {
				atomic.AddInt32(&ns1Count, 1)
				time.Sleep(10 * time.Millisecond)
				return nil
			},
		}

		job2 := &Job{
			ID:        "ns2-job-" + string(rune(i)),
			Namespace: "ns2",
			Execute: func(ctx context.Context) error {
				atomic.AddInt32(&ns2Count, 1)
				time.Sleep(10 * time.Millisecond)
				return nil
			},
		}

		mu.Lock()
		if err := scheduler.Enqueue(job1); err != nil {
			t.Errorf("failed to enqueue job1: %v", err)
		}
		if err := scheduler.Enqueue(job2); err != nil {
			t.Errorf("failed to enqueue job2: %v", err)
		}
		mu.Unlock()
	}

	// Wait for execution
	time.Sleep(1 * time.Second)

	n1 := atomic.LoadInt32(&ns1Count)
	n2 := atomic.LoadInt32(&ns2Count)

	// ns1 should execute approximately 2x more jobs than ns2
	// Allow for some variance due to timing
	if n1 < n2 {
		t.Errorf("expected ns1 (weight 2) to execute more jobs than ns2 (weight 1), got ns1=%d, ns2=%d", n1, n2)
	}

	t.Logf("Fairness test: ns1 (weight 2) executed %d jobs, ns2 (weight 1) executed %d jobs", n1, n2)
}

func TestWFQSchedulerVirtualTime(t *testing.T) {
	config := Config{
		DefaultWeight: 1,
		WorkerCount:   1,
	}
	scheduler := NewWFQScheduler(config)

	scheduler.SetWeight("ns1", 2)
	scheduler.SetWeight("ns2", 1)

	// Enqueue jobs
	job1 := &Job{
		ID:        "job1",
		Namespace: "ns1",
		Execute:   func(ctx context.Context) error { return nil },
	}

	job2 := &Job{
		ID:        "job2",
		Namespace: "ns2",
		Execute:   func(ctx context.Context) error { return nil },
	}

	if err := scheduler.Enqueue(job1); err != nil {
		t.Fatalf("failed to enqueue job1: %v", err)
	}
	if err := scheduler.Enqueue(job2); err != nil {
		t.Fatalf("failed to enqueue job2: %v", err)
	}

	// Virtual finish time for ns1 (weight 2) should be 0.5
	// Virtual finish time for ns2 (weight 1) should be 1.0
	if job1.virtualFinishTime >= job2.virtualFinishTime {
		t.Errorf("expected ns1 (weight 2) to have smaller virtual finish time, got ns1=%f, ns2=%f",
			job1.virtualFinishTime, job2.virtualFinishTime)
	}
}

func TestWFQSchedulerMultipleNamespaces(t *testing.T) {
	config := Config{
		DefaultWeight: 1,
		WorkerCount:   2,
	}
	scheduler := NewWFQScheduler(config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	scheduler.Start(ctx)
	defer scheduler.Stop()

	namespaces := []string{"ns1", "ns2", "ns3"}
	counts := make(map[string]*int32)

	for _, ns := range namespaces {
		count := int32(0)
		counts[ns] = &count

		for i := 0; i < 10; i++ {
			ns := ns // capture
			c := counts[ns]
			job := &Job{
				ID:        ns + "-job-" + string(rune(i)),
				Namespace: ns,
				Execute: func(ctx context.Context) error {
					atomic.AddInt32(c, 1)
					time.Sleep(5 * time.Millisecond)
					return nil
				},
			}
			scheduler.Enqueue(job)
		}
	}

	// Wait for execution
	time.Sleep(500 * time.Millisecond)

	// All namespaces should execute some jobs
	for ns, count := range counts {
		c := atomic.LoadInt32(count)
		if c == 0 {
			t.Errorf("namespace %s executed 0 jobs (starvation)", ns)
		}
		t.Logf("Namespace %s executed %d jobs", ns, c)
	}
}

func TestWFQSchedulerStats(t *testing.T) {
	config := Config{
		DefaultWeight: 1,
		WorkerCount:   1,
	}
	scheduler := NewWFQScheduler(config)

	scheduler.SetWeight("ns1", 5)
	scheduler.SetWeight("ns2", 2)

	job1 := &Job{
		ID:        "job1",
		Namespace: "ns1",
		Execute:   func(ctx context.Context) error { return nil },
	}

	job2 := &Job{
		ID:        "job2",
		Namespace: "ns2",
		Execute:   func(ctx context.Context) error { return nil },
	}

	scheduler.Enqueue(job1)
	scheduler.Enqueue(job2)

	stats := scheduler.Stats()

	if stats.QueuedJobs != 2 {
		t.Errorf("expected 2 queued jobs, got %d", stats.QueuedJobs)
	}

	if len(stats.NamespaceQueues) != 2 {
		t.Errorf("expected 2 namespace queues, got %d", len(stats.NamespaceQueues))
	}

	// Find ns1 stats
	var ns1Stats *NamespaceStats
	for i := range stats.NamespaceQueues {
		if stats.NamespaceQueues[i].Namespace == "ns1" {
			ns1Stats = &stats.NamespaceQueues[i]
			break
		}
	}

	if ns1Stats == nil {
		t.Fatal("expected ns1 stats")
	}

	if ns1Stats.Weight != 5 {
		t.Errorf("expected ns1 weight 5, got %d", ns1Stats.Weight)
	}
}

func TestWFQSchedulerStopAndStart(t *testing.T) {
	config := Config{
		DefaultWeight: 1,
		WorkerCount:   2,
	}
	scheduler := NewWFQScheduler(config)

	ctx := context.Background()

	// Start scheduler
	scheduler.Start(ctx)

	var executed int32
	job := &Job{
		ID:        "job1",
		Namespace: "ns1",
		Execute: func(ctx context.Context) error {
			atomic.AddInt32(&executed, 1)
			return nil
		},
	}

	scheduler.Enqueue(job)
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&executed) != 1 {
		t.Errorf("expected 1 execution, got %d", executed)
	}

	// Stop scheduler
	scheduler.Stop()

	// Enqueue another job (should not execute while stopped)
	job2 := &Job{
		ID:        "job2",
		Namespace: "ns1",
		Execute: func(ctx context.Context) error {
			atomic.AddInt32(&executed, 1)
			return nil
		},
	}

	scheduler.Enqueue(job2)
	time.Sleep(100 * time.Millisecond)

	// Should still be 1 (job2 not executed)
	if atomic.LoadInt32(&executed) != 1 {
		t.Errorf("expected 1 execution after stop, got %d", executed)
	}
}

func TestWFQSchedulerInvalidJobs(t *testing.T) {
	scheduler := NewWFQScheduler(DefaultConfig())

	// Nil job
	err := scheduler.Enqueue(nil)
	if err == nil {
		t.Error("expected error for nil job")
	}

	// Job without execute function
	job := &Job{
		ID:        "job1",
		Namespace: "ns1",
	}

	err = scheduler.Enqueue(job)
	if err == nil {
		t.Error("expected error for job without execute function")
	}
}
