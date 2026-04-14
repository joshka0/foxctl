package companion

import (
	"context"
	"testing"
	"time"
)

func TestConversationLockID_Deterministic(t *testing.T) {
	t.Parallel()

	const conversationID = "conversation-123"
	got1 := conversationLockID(conversationID)
	got2 := conversationLockID(conversationID)

	if got1 != got2 {
		t.Fatalf("conversationLockID not deterministic: %d != %d", got1, got2)
	}
}

func TestConversationLockID_DifferentIDs(t *testing.T) {
	t.Parallel()

	ids := []string{
		"conversation-1",
		"conversation-2",
		"conversation-3",
		"user-123:session-1",
		"user-123:session-2",
		"discord:channel:abc",
		"telegram:chat:xyz",
	}

	seen := make(map[int64]string, len(ids))
	for _, id := range ids {
		lockID := conversationLockID(id)
		if prev, exists := seen[lockID]; exists && prev != id {
			t.Fatalf("unexpected collision: %q and %q -> %d", prev, id, lockID)
		}
		seen[lockID] = id
	}
}

func TestPgTurnLock_Lock_NilPool(t *testing.T) {
	t.Parallel()

	pl := NewPgTurnLock(nil)
	if _, err := pl.Lock(context.Background(), "conv-1"); err == nil {
		t.Fatal("expected error for nil pool")
	}
}

func TestPgTurnLock_TryLock_NilPool(t *testing.T) {
	t.Parallel()

	pl := NewPgTurnLock(nil)
	if _, acquired, err := pl.TryLock(context.Background(), "conv-1"); err == nil || acquired {
		t.Fatalf("expected err with acquired=false, got err=%v acquired=%v", err, acquired)
	}
}

func TestLockerContract_InMemoryTurnLock_SerializesSameConversation(t *testing.T) {
	t.Parallel()

	var locker Locker = NewTurnLock()
	const conversationID = "conv-locker-serial"

	unlock1, err := locker.Lock(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("first lock failed: %v", err)
	}

	secondAcquired := make(chan struct{}, 1)
	secondDone := make(chan struct{}, 1)
	go func() {
		unlock2, err := locker.Lock(context.Background(), conversationID)
		if err != nil {
			t.Errorf("second lock failed: %v", err)
			secondDone <- struct{}{}
			return
		}
		secondAcquired <- struct{}{}
		unlock2()
		secondDone <- struct{}{}
	}()

	select {
	case <-secondAcquired:
		t.Fatal("second lock acquired before first unlock")
	case <-time.After(100 * time.Millisecond):
		// Expected: second lock should still be blocked.
	}

	unlock1()

	select {
	case <-secondAcquired:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second lock acquisition after unlock")
	}

	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second lock release")
	}
}

func TestLockerContract_InMemoryTurnLock_AllowsDifferentConversations(t *testing.T) {
	t.Parallel()

	var locker Locker = NewTurnLock()

	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)

	lockFn := func(conversationID string) {
		defer func() { done <- struct{}{} }()
		unlock, err := locker.Lock(context.Background(), conversationID)
		if err != nil {
			t.Errorf("lock failed for %s: %v", conversationID, err)
			return
		}
		ready <- struct{}{}
		<-release
		unlock()
	}

	go lockFn("conv-a")
	go lockFn("conv-b")

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("first lock did not acquire")
	}
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("second lock did not acquire")
	}

	close(release)
	<-done
	<-done
}
