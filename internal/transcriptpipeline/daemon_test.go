package transcriptpipeline

import (
	"context"
	"testing"
	"time"
)

func TestDaemonQueue_DedupesSingleAndGroupedJobs(t *testing.T) {
	t.Parallel()

	queue := NewQueue(4)
	defer queue.Close()

	single := Job{
		Kind: JobKindSingle,
		Single: &SingleRunOptions{
			Provider:      "codex",
			SourceFile:    "/tmp/root.jsonl",
			PersistMemory: true,
		},
	}
	if accepted := queue.Enqueue(single); !accepted {
		t.Fatal("expected first single enqueue accepted")
	}
	if accepted := queue.Enqueue(single); accepted {
		t.Fatal("expected duplicate single enqueue rejected")
	}

	groupedA := Job{
		Kind: JobKindGrouped,
		Grouped: &GroupRunOptions{
			SourceFiles:   []string{"/tmp/b.jsonl", "/tmp/a.jsonl"},
			PersistMemory: true,
		},
	}
	groupedB := Job{
		Kind: JobKindGrouped,
		Grouped: &GroupRunOptions{
			SourceFiles:   []string{"/tmp/a.jsonl", "/tmp/b.jsonl"},
			PersistMemory: true,
		},
	}
	if accepted := queue.Enqueue(groupedA); !accepted {
		t.Fatal("expected first grouped enqueue accepted")
	}
	if accepted := queue.Enqueue(groupedB); accepted {
		t.Fatal("expected grouped enqueue with same file set rejected")
	}

	doctrineA := Job{
		Kind: JobKindSingleDoctrine,
		SingleDoctrine: &SingleRunOptions{
			Provider:      "codex",
			SourceFile:    "/tmp/root.jsonl",
			PersistMemory: true,
		},
	}
	doctrineB := Job{
		Kind: JobKindSingleDoctrine,
		SingleDoctrine: &SingleRunOptions{
			Provider:      "codex",
			SourceFile:    "/tmp/root.jsonl",
			PersistMemory: true,
		},
	}
	if accepted := queue.Enqueue(doctrineA); !accepted {
		t.Fatal("expected first doctrine enqueue accepted")
	}
	if accepted := queue.Enqueue(doctrineB); accepted {
		t.Fatal("expected duplicate doctrine enqueue rejected")
	}
}

func TestDaemonRunner_ReleasesDedupKeyAfterProcessing(t *testing.T) {
	t.Parallel()

	queue := NewQueue(4)
	defer queue.Close()

	single := Job{
		Kind: JobKindSingle,
		Single: &SingleRunOptions{
			Provider:   "codex",
			SourceFile: "/tmp/root.jsonl",
		},
	}

	done := make(chan JobResult, 1)
	runner := NewRunner(RunnerConfig{
		Queue: queue,
		Processor: ProcessorFuncSet{
			Single: func(context.Context, SingleRunOptions) (SingleRunResult, error) {
				return SingleRunResult{ConversationID: "conv-1"}, nil
			},
			Grouped: func(context.Context, GroupRunOptions) (GroupRunResult, error) {
				return GroupRunResult{}, nil
			},
		},
		OnResult: func(result JobResult) {
			done <- result
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- runner.Run(ctx) }()

	if accepted := queue.Enqueue(single); !accepted {
		t.Fatal("expected first enqueue accepted")
	}
	if accepted := queue.Enqueue(single); accepted {
		t.Fatal("expected duplicate enqueue rejected while in flight")
	}

	select {
	case result := <-done:
		if result.Err != nil {
			t.Fatalf("result error = %v", result.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for runner result")
	}

	if accepted := queue.Enqueue(single); !accepted {
		t.Fatal("expected enqueue accepted after dedupe key release")
	}

	cancel()
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("runner Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for runner shutdown")
	}
}

func TestDaemonRunner_ProcessesSingleDoctrineJob(t *testing.T) {
	t.Parallel()

	queue := NewQueue(4)
	defer queue.Close()

	job := Job{
		Kind: JobKindSingleDoctrine,
		SingleDoctrine: &SingleRunOptions{
			Provider:   "codex",
			SourceFile: "/tmp/root.jsonl",
		},
	}

	done := make(chan JobResult, 1)
	runner := NewRunner(RunnerConfig{
		Queue: queue,
		Processor: ProcessorFuncSet{
			Single: func(context.Context, SingleRunOptions) (SingleRunResult, error) {
				return SingleRunResult{}, nil
			},
			Grouped: func(context.Context, GroupRunOptions) (GroupRunResult, error) {
				return GroupRunResult{}, nil
			},
			SingleDoctrine: func(context.Context, SingleRunOptions) (SingleRunResult, error) {
				return SingleRunResult{ConversationID: "conv-doctrine"}, nil
			},
			GroupedDoctrine: func(context.Context, GroupRunOptions) (GroupRunResult, error) {
				return GroupRunResult{}, nil
			},
		},
		OnResult: func(result JobResult) {
			done <- result
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	if accepted := queue.Enqueue(job); !accepted {
		t.Fatal("expected doctrine enqueue accepted")
	}

	select {
	case result := <-done:
		if result.Err != nil {
			t.Fatalf("result error = %v", result.Err)
		}
		if result.Single == nil || result.Single.ConversationID != "conv-doctrine" {
			t.Fatalf("single doctrine result = %+v", result.Single)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for doctrine runner result")
	}
}

func TestEnqueueSingleDoctrine_Helper(t *testing.T) {
	t.Parallel()

	queue := NewQueue(2)
	defer queue.Close()
	if !EnqueueSingleDoctrine(queue, SingleRunOptions{
		Provider:   "codex",
		SourceFile: "/tmp/root.jsonl",
	}) {
		t.Fatal("expected helper enqueue accepted")
	}
}

func TestDaemonRunner_TracksActiveJobs(t *testing.T) {
	t.Parallel()

	queue := NewQueue(4)
	defer queue.Close()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	runner := NewRunner(RunnerConfig{
		Queue: queue,
		Processor: ProcessorFuncSet{
			Single: func(context.Context, SingleRunOptions) (SingleRunResult, error) {
				started <- struct{}{}
				<-release
				return SingleRunResult{}, nil
			},
			Grouped: func(context.Context, GroupRunOptions) (GroupRunResult, error) {
				return GroupRunResult{}, nil
			},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = runner.Run(ctx) }()

	job := Job{
		Kind: JobKindSingle,
		Single: &SingleRunOptions{
			Provider:   "codex",
			SourceFile: "/tmp/root.jsonl",
		},
	}
	if accepted := queue.Enqueue(job); !accepted {
		t.Fatal("expected enqueue accepted")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for processor start")
	}
	if got := runner.ActiveJobs(); got != 1 {
		t.Fatalf("active_jobs=%d want 1", got)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runner.ActiveJobs() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("active_jobs=%d want 0 after completion", runner.ActiveJobs())
}
