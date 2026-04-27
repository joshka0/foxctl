package safeprompt

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/joshka/foxprox/foxprox/broker/session"
)

func newRunningSession(t *testing.T, cmd []string) *session.Session {
	t.Helper()
	s, err := session.New(session.Spec{Cmd: cmd}, session.OutputLogOptions{MaxChunks: 256, MaxBytes: 64 * 1024})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		s.Close()
		<-s.Done()
	})
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return s
}

func TestCheck_BusyBeforeOutput(t *testing.T) {
	s := newRunningSession(t, []string{"sleep", "30"})
	d, _ := Check(s, Options{})
	if d != Busy {
		t.Errorf("Decision = %s, want busy", d)
	}
}

func TestCheck_BlockedWhenSessionDead(t *testing.T) {
	s := newRunningSession(t, []string{"sh", "-c", "printf ''"})
	<-s.Done()
	d, _ := Check(s, Options{})
	if d != Blocked {
		t.Errorf("Decision = %s, want blocked", d)
	}
}

func TestCheck_NilSessionBlocked(t *testing.T) {
	d, _ := Check(nil, Options{})
	if d != Blocked {
		t.Errorf("Decision = %s, want blocked", d)
	}
}

func TestCheck_ReadyAtBashPrompt(t *testing.T) {
	s := newRunningSession(t, []string{"bash", "--noprofile", "--norc", "-i"})
	opts := Options{IdleWindow: 200 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if d, _ := Check(s, opts); d == Ready {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("bash never reached ready; last tail = %q", string(collectTail(s, 256)))
}

func TestWait_ReadyBeforeTimeout(t *testing.T) {
	s := newRunningSession(t, []string{"bash", "--noprofile", "--norc", "-i"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d, reason, err := Wait(ctx, s, Options{IdleWindow: 200 * time.Millisecond}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Wait err = %v (reason=%s decision=%s)", err, reason, d)
	}
	if d != Ready {
		t.Errorf("decision = %s, want ready", d)
	}
}

func TestWait_TimeoutWhenNoPrompt(t *testing.T) {
	// A command that never prints anything beyond the initial exec — will
	// idle but the prompt regex never matches.
	s := newRunningSession(t, []string{"sleep", "30"})
	// Give session a chance to reach running.
	time.Sleep(50 * time.Millisecond)
	opts := Options{
		Regex:      regexp.MustCompile(`never-matches-this-prompt`),
		IdleWindow: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _, err := Wait(ctx, s, opts, 50*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
}

func TestWait_SessionDiesReturnsErrSessionDead(t *testing.T) {
	s := newRunningSession(t, []string{"sh", "-c", "sleep 0.1; exit 0"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := Wait(ctx, s, Options{
		Regex:      regexp.MustCompile(`never`),
		IdleWindow: 50 * time.Millisecond,
	}, 25*time.Millisecond)
	if !errors.Is(err, ErrSessionDead) {
		t.Fatalf("want ErrSessionDead, got %v", err)
	}
}

func TestCheck_RequireNotAltScreenBlocks(t *testing.T) {
	s := newRunningSession(t, []string{"sleep", "30"})
	// Synthetically toggle alt-screen by feeding the tracker directly.
	s.Tracker().Feed([]byte("\x1b[?1049h"))
	d, reason := Check(s, Options{RequireNotAltScreen: true, IdleWindow: 50 * time.Millisecond})
	if d != Blocked {
		t.Errorf("Decision = %s reason=%s, want blocked", d, reason)
	}
}

func TestCheck_CustomRegex(t *testing.T) {
	s := newRunningSession(t, []string{"sh", "-c", "printf 'READY<<<'; sleep 10"})
	opts := Options{
		Regex:      regexp.MustCompile(`<<<$`),
		IdleWindow: 100 * time.Millisecond,
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if d, _ := Check(s, opts); d == Ready {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("custom regex never matched tail=%q", string(collectTail(s, 64)))
}

func TestDecisionString(t *testing.T) {
	cases := map[Decision]string{
		Unknown:     "unknown",
		Ready:       "ready",
		Busy:        "busy",
		Blocked:     "blocked",
		Decision(9): "unknown",
	}
	for d, want := range cases {
		if got := d.String(); got != want {
			t.Errorf("%d.String()=%q, want %q", d, got, want)
		}
	}
}
