package snapshots_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coreevents "github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/runtime/snapshots"
)

func TestSnapshotStore_LoadStoreAtomic(t *testing.T) {
	t.Parallel()

	store := snapshots.NewStore()

	initial := store.Load()
	if initial.Version != 0 {
		t.Fatalf("initial version=%d want=0", initial.Version)
	}

	in := snapshots.RuntimeSnapshot{
		Version:   1,
		UpdatedAt: time.Date(2026, time.February, 18, 21, 0, 0, 0, time.UTC),
		Digest: snapshots.DigestSnapshot{
			LastEventType: coreevents.EventRunStarted,
			RunStatus: map[string]string{
				"run-1": "running",
			},
		},
	}
	store.Store(in)

	// Mutating caller-owned input must not mutate store state.
	in.Digest.RunStatus["run-1"] = "corrupted"
	loaded := store.Load()
	if loaded.Digest.RunStatus["run-1"] != "running" {
		t.Fatalf("stored run status=%q want running", loaded.Digest.RunStatus["run-1"])
	}

	// Mutating loaded value must not mutate store state.
	loaded.Digest.RunStatus["run-1"] = "mutated"
	loadedAgain := store.Load()
	if loadedAgain.Digest.RunStatus["run-1"] != "running" {
		t.Fatalf("reloaded run status=%q want running", loadedAgain.Digest.RunStatus["run-1"])
	}
	if loadedAgain.Version != 1 {
		t.Fatalf("reloaded version=%d want=1", loadedAgain.Version)
	}
}

func TestSnapshotStore_ConcurrentReaders_NoContentionRegression(t *testing.T) {
	t.Parallel()

	store := snapshots.NewStore()
	store.Store(snapshots.RuntimeSnapshot{
		Version: 1,
		Digest: snapshots.DigestSnapshot{
			RunStatus: map[string]string{
				"run-1": "running",
			},
		},
	})

	const readerCount = 32
	const writes = 1000

	var reads atomic.Int64
	start := make(chan struct{})
	done := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < readerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				select {
				case <-done:
					return
				default:
					snap := store.Load()
					_ = snap.Version
					reads.Add(1)
				}
			}
		}()
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		<-start
		for i := 0; i < writes; i++ {
			snap := store.Load()
			snap.Version++
			if snap.Digest.RunStatus == nil {
				snap.Digest.RunStatus = map[string]string{}
			}
			snap.Digest.RunStatus["run-1"] = "running"
			snap.Digest.TotalEvents = snap.Version
			store.Store(snap)
		}
	}()

	close(start)

	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for writer completion")
	}

	close(done)
	wgDone := make(chan struct{})
	go func() {
		defer close(wgDone)
		wg.Wait()
	}()
	select {
	case <-wgDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for readers to stop")
	}

	if reads.Load() == 0 {
		t.Fatal("expected concurrent readers to observe snapshots")
	}

	got := store.Load()
	if got.Version <= 1 {
		t.Fatalf("final version=%d want >1", got.Version)
	}
	if got.Digest.TotalEvents != got.Version {
		t.Fatalf("total_events=%d want version=%d", got.Digest.TotalEvents, got.Version)
	}
}
