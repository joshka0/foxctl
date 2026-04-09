package webterm

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tmuxAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
}

func TestStartTmuxAttach_CreatesSession(t *testing.T) {
	tmuxAvailable(t)
	// tmux doesn't work well inside some CI environments
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux tests in CI")
	}

	sessionName := "webterm-test-create-" + time.Now().Format("20060102-150405")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Clean up session after test
	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	pty, err := StartTmuxAttach(ctx, TmuxOptions{
		Session: sessionName,
		Cols:    80,
		Rows:    24,
	})
	require.NoError(t, err)
	require.NotNil(t, pty)
	defer pty.Close()

	// Verify session was created
	time.Sleep(500 * time.Millisecond)
	out, err := exec.Command("tmux", "list-sessions").Output()
	require.NoError(t, err)
	assert.Contains(t, string(out), sessionName)

	// Verify PTY is running
	assert.True(t, pty.IsRunning())
}

func TestStartTmuxAttach_AttachExisting(t *testing.T) {
	tmuxAvailable(t)
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux tests in CI")
	}

	sessionName := "webterm-test-attach-" + time.Now().Format("20060102-150405")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Pre-create a tmux session
	err := exec.Command("tmux", "new-session", "-d", "-s", sessionName).Run()
	require.NoError(t, err)
	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	// StartTmuxAttach should attach to the existing session
	pty, err := StartTmuxAttach(ctx, TmuxOptions{
		Session: sessionName,
		Cols:    80,
		Rows:    24,
	})
	require.NoError(t, err)
	require.NotNil(t, pty)
	defer pty.Close()

	assert.True(t, pty.IsRunning())
}

func TestPTY_WriteInput(t *testing.T) {
	tmuxAvailable(t)
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux tests in CI")
	}

	sessionName := "webterm-test-input-" + time.Now().Format("20060102-150405")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	pty, err := StartTmuxAttach(ctx, TmuxOptions{
		Session: sessionName,
		Cols:    120,
		Rows:    24,
	})
	require.NoError(t, err)
	defer pty.Close()

	// Wait for shell to start
	time.Sleep(1 * time.Second)

	// Subscribe to output
	outputCh := make(chan []byte, 100)
	subID := pty.SubscribeOutput(outputCh)
	defer pty.UnsubscribeOutput(subID)

	// Write a command
	err = pty.WriteInput([]byte("echo hello-webterm\n"))
	require.NoError(t, err)

	// Read output and look for our echo
	timeout := time.After(5 * time.Second)
	found := false
	for !found {
		select {
		case data := <-outputCh:
			if strings.Contains(string(data), "hello-webterm") {
				found = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for echo output")
		}
	}
	assert.True(t, found, "should see echo output in PTY")
}

func TestPTY_Resize(t *testing.T) {
	tmuxAvailable(t)
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux tests in CI")
	}

	sessionName := "webterm-test-resize-" + time.Now().Format("20060102-150405")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	pty, err := StartTmuxAttach(ctx, TmuxOptions{
		Session: sessionName,
		Cols:    80,
		Rows:    24,
	})
	require.NoError(t, err)
	defer pty.Close()

	// Resize to 132x40
	err = pty.Resize(132, 40)
	require.NoError(t, err)

	// Give tmux a moment to register the resize
	time.Sleep(200 * time.Millisecond)

	// Verify tmux session width changed (height may be 1 less due to tmux status bar)
	out, err := exec.Command("tmux", "list-panes", "-t", sessionName, "-F", "#{pane_width}").Output()
	require.NoError(t, err)
	assert.Equal(t, "132", strings.TrimSpace(string(out)), "pane width should be 132")
}

func TestPTY_OutputBroadcast(t *testing.T) {
	tmuxAvailable(t)
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux tests in CI")
	}

	sessionName := "webterm-test-broadcast-" + time.Now().Format("20060102-150405")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	pty, err := StartTmuxAttach(ctx, TmuxOptions{
		Session: sessionName,
		Cols:    80,
		Rows:    24,
	})
	require.NoError(t, err)
	defer pty.Close()

	// Subscribe two clients
	ch1 := make(chan []byte, 100)
	ch2 := make(chan []byte, 100)
	sub1 := pty.SubscribeOutput(ch1)
	sub2 := pty.SubscribeOutput(ch2)
	defer pty.UnsubscribeOutput(sub1)
	defer pty.UnsubscribeOutput(sub2)

	// Wait for shell prompt
	time.Sleep(1 * time.Second)

	// Drain initial prompt output
	drainChannel(ch1)
	drainChannel(ch2)

	// Write something
	err = pty.WriteInput([]byte("echo broadcast-test\n"))
	require.NoError(t, err)

	// Both subscribers should receive output
	timeout := time.After(5 * time.Second)

	found1 := false
	found2 := false
	for !found1 || !found2 {
		select {
		case data := <-ch1:
			if strings.Contains(string(data), "broadcast-test") {
				found1 = true
			}
		case data := <-ch2:
			if strings.Contains(string(data), "broadcast-test") {
				found2 = true
			}
		case <-timeout:
			t.Fatalf("timed out waiting for broadcast (found1=%v found2=%v)", found1, found2)
		}
	}
	assert.True(t, found1, "subscriber 1 should receive output")
	assert.True(t, found2, "subscriber 2 should receive output")
}

func TestPTY_Close(t *testing.T) {
	tmuxAvailable(t)
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux tests in CI")
	}

	sessionName := "webterm-test-close-" + time.Now().Format("20060102-150405")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	defer func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	}()

	pty, err := StartTmuxAttach(ctx, TmuxOptions{
		Session: sessionName,
		Cols:    80,
		Rows:    24,
	})
	require.NoError(t, err)

	assert.True(t, pty.IsRunning())

	pty.Close()

	// PTY should no longer be running
	assert.False(t, pty.IsRunning())

	// Verify that pty.Close() does not panic or deadlock
	// Calling Close again should be safe (idempotent)
	pty.Close()
	assert.False(t, pty.IsRunning())
}

func TestPTY_WriteInput_NotRunning(t *testing.T) {
	pty := &PTYProcess{}
	err := pty.WriteInput([]byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestPTY_Resize_NotRunning(t *testing.T) {
	pty := &PTYProcess{}
	err := pty.Resize(80, 24)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func drainChannel(ch chan []byte) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
