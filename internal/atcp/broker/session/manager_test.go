package session

import (
	"errors"
	"strings"
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
