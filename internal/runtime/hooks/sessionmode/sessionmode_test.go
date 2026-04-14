package sessionmode

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetAndReadFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionID := "sid-123"
	now := time.Now()

	if err := EnableTodo(sessionID, now); err != nil {
		t.Fatalf("EnableTodo: %v", err)
	}
	if err := SetAnchor(sessionID, "Ship hook cleanup", now); err != nil {
		t.Fatalf("SetAnchor: %v", err)
	}

	flags, err := Read(sessionID, now)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !flags.Todo {
		t.Fatalf("expected todo flag")
	}
	if flags.AnchorGoal != "Ship hook cleanup" {
		t.Fatalf("anchor goal = %q", flags.AnchorGoal)
	}
}

func TestReadExpiresStaleFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionID := "sid-stale"
	stale := time.Now().Add(-DefaultTTL - time.Hour)
	if err := EnableTodo(sessionID, stale); err != nil {
		t.Fatalf("EnableTodo: %v", err)
	}

	flags, err := Read(sessionID, time.Now())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if flags.Todo {
		t.Fatalf("expected stale todo flag to expire")
	}
	if _, err := os.Stat(filepath.Join(modeDir(), "todo-"+shortHash("todo:"+sessionID)+".json")); err == nil {
		t.Fatalf("expected stale todo flag file to be removed")
	}
}

func TestModeDirFallsBackWhenHomeUnavailable(t *testing.T) {
	orig := userHomeDir
	userHomeDir = func() (string, error) {
		return "", os.ErrNotExist
	}
	t.Cleanup(func() { userHomeDir = orig })
	t.Setenv("AGENTCTL_HOME", "")

	got := modeDir()
	want := filepath.Join(os.TempDir(), "agentctl", "cache", "session-modes")
	if got != want {
		t.Fatalf("modeDir() = %q, want %q", got, want)
	}
}
