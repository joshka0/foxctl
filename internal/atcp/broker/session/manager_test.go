package session

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManager_CreateAndGet(t *testing.T) {
	m := NewManager(ManagerOptions{})
	defer m.Stop()

	sess, err := m.Create(Spec{Cmd: []string{"sh", "-c", "printf hi"}}, OutputLogOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := m.Get(sess.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != sess.ID() {
		t.Errorf("Get returned different session")
	}
	<-sess.Done()
	assertContains(t, sess, "hi", 3*time.Second)
}

func TestManager_GetUnknownReturnsError(t *testing.T) {
	m := NewManager(ManagerOptions{})
	defer m.Stop()
	_, err := m.Get("nope")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}

func TestManager_ListIncludesActiveSessions(t *testing.T) {
	m := NewManager(ManagerOptions{})
	defer m.Stop()

	a, err := m.Create(Spec{Cmd: []string{"sleep", "30"}}, OutputLogOptions{})
	if err != nil {
		t.Fatalf("Create a: %v", err)
	}
	b, err := m.Create(Spec{Cmd: []string{"sleep", "30"}}, OutputLogOptions{})
	if err != nil {
		t.Fatalf("Create b: %v", err)
	}

	list := m.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	ids := map[string]bool{list[0].ID: true, list[1].ID: true}
	if !ids[a.ID()] || !ids[b.ID()] {
		t.Errorf("List missing one of the created sessions: %v", ids)
	}
}

func TestManager_DeleteStopsSession(t *testing.T) {
	m := NewManager(ManagerOptions{})
	defer m.Stop()

	s, err := m.Create(Spec{Cmd: []string{"sleep", "30"}}, OutputLogOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Delete(s.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Get(s.ID()); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("Get after delete want ErrSessionNotFound, got %v", err)
	}
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("session did not terminate after Delete")
	}
}

func TestManager_DeleteUnknownReturnsError(t *testing.T) {
	m := NewManager(ManagerOptions{})
	defer m.Stop()
	if err := m.Delete("nope"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}

func TestManager_StopClosesAllSessions(t *testing.T) {
	m := NewManager(ManagerOptions{})

	for i := 0; i < 3; i++ {
		if _, err := m.Create(Spec{Cmd: []string{"sleep", "30"}}, OutputLogOptions{}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	start := time.Now()
	m.Stop()
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Stop took %v, expected fast shutdown", elapsed)
	}
	if _, err := m.Create(Spec{Cmd: []string{"sh"}}, OutputLogOptions{}); !errors.Is(err, ErrManagerClosed) {
		t.Errorf("Create after Stop: want ErrManagerClosed, got %v", err)
	}
}

// TestManager_CreateStopRaceClosesInflightSession exercises the race between
// Create and Stop: concurrent goroutines call Create while the main test
// issues Stop. Any Create that wins the race must either return a live
// session (which Stop will then close) OR return ErrManagerClosed with the
// PTY already torn down. Neither path may leave a PTY running after Stop
// returns.
//
// Before the fix, a Create that observed ctx.Err() == nil in its fast-path
// check could race with Stop's walk of m.sessions: the session was spawned,
// then inserted into the map *after* Stop had already snapshotted it,
// leaking a child process past the manager's lifetime.
func TestManager_CreateStopRaceClosesInflightSession(t *testing.T) {
	for attempt := 0; attempt < 10; attempt++ {
		m := NewManager(ManagerOptions{})
		var (
			wg       sync.WaitGroup
			leakedMu sync.Mutex
			leaked   []*Session
		)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s, err := m.Create(Spec{Cmd: []string{"sleep", "30"}}, OutputLogOptions{})
				if err != nil {
					return
				}
				// A session we created that Stop might not know about —
				// collect it so we can verify it's torn down post-Stop.
				leakedMu.Lock()
				leaked = append(leaked, s)
				leakedMu.Unlock()
			}()
		}
		// Sleep briefly to let some Creates land, then stop. We want the
		// interleaving — Stop during Create's second phase — to be
		// reachable on most iterations.
		time.Sleep(time.Duration(attempt) * time.Millisecond)
		m.Stop()
		wg.Wait()

		// Every session we saw returned from Create must be done — either
		// Stop finished it, or Create noticed the race and finished it
		// itself.
		for _, s := range leaked {
			select {
			case <-s.Done():
			case <-time.After(2 * time.Second):
				t.Fatalf("attempt %d: session %s leaked past manager.Stop", attempt, s.ID())
			}
		}
	}
}

func TestManager_ReaperRemovesNaturallyExited(t *testing.T) {
	prev := reapInterval
	reapInterval = 5 * time.Millisecond
	t.Cleanup(func() { reapInterval = prev })

	m := NewManager(ManagerOptions{})
	defer m.Stop()

	s, err := m.Create(Spec{Cmd: []string{"sh", "-c", "printf bye"}}, OutputLogOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	<-s.Done()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := m.Get(s.ID()); errors.Is(err, ErrSessionNotFound) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("reaper never evicted the exited session")
}

func assertContains(t *testing.T, s *Session, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		chunks := s.Log().Since(0, 0)
		var buf strings.Builder
		for _, c := range chunks {
			buf.Write(c.Bytes)
		}
		if strings.Contains(buf.String(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("output did not contain %q within %v", want, timeout)
}
