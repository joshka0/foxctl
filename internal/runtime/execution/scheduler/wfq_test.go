package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/quick"
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
		Execute: func(_ context.Context) error {
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

	var executed atomic.Int32
	job := &Job{
		ID:        "job-1",
		Namespace: "ns1",
		Execute: func(_ context.Context) error {
			executed.Add(1)
			return nil
		},
	}

	err := scheduler.Enqueue(job)
	if err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}

	// Wait for execution
	time.Sleep(100 * time.Millisecond)

	if executed.Load() != 1 {
		t.Errorf("expected job to be executed once, got %d", executed.Load())
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

	var ns1Count, ns2Count atomic.Int32
	var mu sync.Mutex

	// Enqueue 20 jobs for each namespace
	for i := 0; i < 20; i++ {
		job1 := &Job{
			ID:        "ns1-job-" + string(rune(i)),
			Namespace: "ns1",
			Execute: func(_ context.Context) error {
				ns1Count.Add(1)
				time.Sleep(10 * time.Millisecond)
				return nil
			},
		}

		job2 := &Job{
			ID:        "ns2-job-" + string(rune(i)),
			Namespace: "ns2",
			Execute: func(_ context.Context) error {
				ns2Count.Add(1)
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

	n1 := ns1Count.Load()
	n2 := ns2Count.Load()

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
		Execute:   func(_ context.Context) error { return nil },
	}

	job2 := &Job{
		ID:        "job2",
		Namespace: "ns2",
		Execute:   func(_ context.Context) error { return nil },
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

func TestWFQSchedulerPropertyHigherWeightDispatchesFirst(t *testing.T) {
	property := func(rawLowWeight, rawDelta uint8) bool {
		lowWeight := int(rawLowWeight%20) + 1
		highWeight := lowWeight + int(rawDelta%20) + 1
		scheduler := NewWFQScheduler(Config{DefaultWeight: 1, WorkerCount: 1})
		scheduler.SetWeight("low", lowWeight)
		scheduler.SetWeight("high", highWeight)

		if err := scheduler.Enqueue(&Job{ID: "low-job", Namespace: "low", Execute: func(context.Context) error { return nil }}); err != nil {
			t.Logf("enqueue low: %v", err)
			return false
		}
		if err := scheduler.Enqueue(&Job{ID: "high-job", Namespace: "high", Execute: func(context.Context) error { return nil }}); err != nil {
			t.Logf("enqueue high: %v", err)
			return false
		}

		scheduler.dispatchNext()
		select {
		case job := <-scheduler.workCh:
			if job.ID != "high-job" {
				t.Logf(
					"dispatched %q first with lowWeight=%d highWeight=%d lowFinish=%f highFinish=%f",
					job.ID,
					lowWeight,
					highWeight,
					scheduler.queues["low"].virtualTime,
					scheduler.queues["high"].virtualTime,
				)
				return false
			}
			return true
		default:
			t.Log("dispatchNext did not enqueue work")
			return false
		}
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("higher weight dispatch property failed: %v", err)
	}
}

func TestWFQSchedulerPropertyDispatchPreservesEnqueuedJobs(t *testing.T) {
	property := func(rawJobs []uint8) bool {
		if len(rawJobs) == 0 {
			rawJobs = []uint8{0}
		}
		if len(rawJobs) > 40 {
			rawJobs = rawJobs[:40]
		}

		scheduler := NewWFQScheduler(Config{DefaultWeight: 1, WorkerCount: len(rawJobs)})
		for i, namespace := range generatedSchedulerNamespaces() {
			scheduler.SetWeight(namespace, i+1)
		}

		wantIDs := make(map[string]struct{}, len(rawJobs))
		for i, raw := range rawJobs {
			id := schedulerGeneratedJobID(i, raw)
			namespace := generatedSchedulerNamespace(raw)
			wantIDs[id] = struct{}{}
			if err := scheduler.Enqueue(&Job{
				ID:        id,
				Namespace: namespace,
				Execute:   func(context.Context) error { return nil },
			}); err != nil {
				t.Logf("enqueue %s/%s: %v", namespace, id, err)
				return false
			}
		}

		if scheduler.Stats().QueuedJobs != len(rawJobs) {
			t.Logf("queued jobs=%d want %d", scheduler.Stats().QueuedJobs, len(rawJobs))
			return false
		}

		gotIDs := make(map[string]struct{}, len(rawJobs))
		for range rawJobs {
			scheduler.dispatchNext()
			select {
			case job := <-scheduler.workCh:
				if _, duplicate := gotIDs[job.ID]; duplicate {
					t.Logf("duplicate dispatch for %s", job.ID)
					return false
				}
				if _, expected := wantIDs[job.ID]; !expected {
					t.Logf("unexpected dispatch for %s", job.ID)
					return false
				}
				gotIDs[job.ID] = struct{}{}
			default:
				t.Log("dispatchNext did not emit a queued job")
				return false
			}
		}

		if scheduler.Stats().QueuedJobs != 0 {
			t.Logf("queued jobs after drain=%d want 0", scheduler.Stats().QueuedJobs)
			return false
		}
		if len(gotIDs) != len(wantIDs) {
			t.Logf("dispatched %d jobs, want %d", len(gotIDs), len(wantIDs))
			return false
		}
		for id := range wantIDs {
			if _, ok := gotIDs[id]; !ok {
				t.Logf("missing dispatched job %s", id)
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatalf("dispatch preservation property failed: %v", err)
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
	counts := make(map[string]*atomic.Int32)

	for _, ns := range namespaces {
		counts[ns] = &atomic.Int32{}

		for i := 0; i < 10; i++ {
			ns := ns // capture
			c := counts[ns]
			job := &Job{
				ID:        ns + "-job-" + string(rune(i)),
				Namespace: ns,
				Execute: func(_ context.Context) error {
					c.Add(1)
					time.Sleep(5 * time.Millisecond)
					return nil
				},
			}
			if err := scheduler.Enqueue(job); err != nil {
				t.Errorf("failed to enqueue job: %v", err)
			}
		}
	}

	// Wait for execution
	time.Sleep(500 * time.Millisecond)

	// All namespaces should execute some jobs
	for ns, count := range counts {
		c := count.Load()
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
		Execute:   func(_ context.Context) error { return nil },
	}

	job2 := &Job{
		ID:        "job2",
		Namespace: "ns2",
		Execute:   func(_ context.Context) error { return nil },
	}

	if err := scheduler.Enqueue(job1); err != nil {
		t.Fatalf("failed to enqueue job1: %v", err)
	}
	if err := scheduler.Enqueue(job2); err != nil {
		t.Fatalf("failed to enqueue job2: %v", err)
	}

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
		return
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

	var executed atomic.Int32
	job := &Job{
		ID:        "job1",
		Namespace: "ns1",
		Execute: func(_ context.Context) error {
			executed.Add(1)
			return nil
		},
	}

	if err := scheduler.Enqueue(job); err != nil {
		t.Fatalf("failed to enqueue job: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if executed.Load() != 1 {
		t.Errorf("expected 1 execution, got %d", executed.Load())
	}

	// Stop scheduler
	scheduler.Stop()

	// Enqueue another job (should not execute while stopped)
	job2 := &Job{
		ID:        "job2",
		Namespace: "ns1",
		Execute: func(_ context.Context) error {
			executed.Add(1)
			return nil
		},
	}

	if err := scheduler.Enqueue(job2); err != nil {
		t.Fatalf("failed to enqueue job2: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Should still be 1 (job2 not executed)
	if executed.Load() != 1 {
		t.Errorf("expected 1 execution after stop, got %d", executed.Load())
	}
}

func TestWFQSchedulerCanRestartAfterStop(t *testing.T) {
	scheduler := NewWFQScheduler(Config{DefaultWeight: 1, WorkerCount: 1})
	ctx := context.Background()
	var executed atomic.Int32
	execute := func(_ context.Context) error {
		executed.Add(1)
		return nil
	}

	scheduler.Start(ctx)
	if err := scheduler.Enqueue(&Job{ID: "job1", Namespace: "ns1", Execute: execute}); err != nil {
		t.Fatalf("enqueue first job: %v", err)
	}
	waitForExecutedCount(t, &executed, 1)
	scheduler.Stop()

	scheduler.Start(ctx)
	if err := scheduler.Enqueue(&Job{ID: "job2", Namespace: "ns1", Execute: execute}); err != nil {
		t.Fatalf("enqueue second job: %v", err)
	}
	waitForExecutedCount(t, &executed, 2)
	scheduler.Stop()
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

func waitForExecutedCount(t *testing.T, executed *atomic.Int32, want int32) {
	t.Helper()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if executed.Load() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("executed count=%d want at least %d", executed.Load(), want)
}

func generatedSchedulerNamespace(raw uint8) string {
	namespaces := generatedSchedulerNamespaces()
	return namespaces[int(raw)%len(namespaces)]
}

func generatedSchedulerNamespaces() []string {
	return []string{"alpha", "beta", "gamma", "delta"}
}

func schedulerGeneratedJobID(index int, raw uint8) string {
	return fmt.Sprintf("job-%02d-%03d", index, raw)
}
