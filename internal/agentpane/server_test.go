package agentpane

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServeDeliversRawBytesToChildPTY(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	skipIfUnixSocketsUnavailable(t, tempDir)
	socketPath := shortSocketPath(t, "raw")
	capturePath := filepath.Join(tempDir, "capture.txt")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout bytes.Buffer
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, ServeOptions{
			SocketPath:    socketPath,
			ParticipantID: "probe-a",
			RoomID:        "room-1",
			Command: []string{"sh", "-lc", fmt.Sprintf(
				"stty -icanon min 1 time 0 -echo; dd bs=1 count=%d of=%q status=none",
				len("probe payload"),
				capturePath,
			)},
			DefaultSubmitMode: SubmitModeNewline,
			Stdin:             strings.NewReader(""),
			Stdout:            &stdout,
		})
	}()

	waitForSocket(t, socketPath, errCh)

	resp, err := Deliver(context.Background(), socketPath, ControlMessage{
		Kind:       "room_message",
		RoomID:     "room-1",
		Recipient:  "probe-a",
		Content:    "probe payload",
		SubmitMode: SubmitModeRaw,
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("Deliver() response = %+v", resp)
	}

	waitForFileContent(t, capturePath, "probe payload")

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve() did not exit after cancel")
	}
}

func TestServeDefaultsToNewlineSubmitMode(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	skipIfUnixSocketsUnavailable(t, tempDir)
	socketPath := shortSocketPath(t, "newline")
	capturePath := filepath.Join(tempDir, "capture.txt")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, ServeOptions{
			SocketPath:        socketPath,
			ParticipantID:     "probe-b",
			RoomID:            "room-2",
			Command:           []string{"tee", "-a", capturePath},
			DefaultSubmitMode: SubmitModeNewline,
			Stdin:             strings.NewReader(""),
			Stdout:            &bytes.Buffer{},
		})
	}()

	waitForSocket(t, socketPath, errCh)

	if _, err := Deliver(context.Background(), socketPath, ControlMessage{
		Content: "line payload",
	}); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	waitForFileContent(t, capturePath, "line payload\n")

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve() did not exit after cancel")
	}
}

func TestDefaultSocketPath(t *testing.T) {
	t.Parallel()

	got := DefaultSocketPath("room/alpha", "claude-a")
	if !strings.HasSuffix(got, filepath.Join("agentctl-pane", "room_alpha", "claude-a.sock")) {
		t.Fatalf("DefaultSocketPath() = %q", got)
	}
}

func TestDefaultReadyPath(t *testing.T) {
	t.Parallel()

	got := DefaultReadyPath("room/alpha", "claude-a")
	if !strings.HasSuffix(got, filepath.Join("agentctl-pane", "room_alpha", "claude-a.ready")) {
		t.Fatalf("DefaultReadyPath() = %q", got)
	}
}

func TestRenderPayloadComposerCtrlEnterSkipsInterruptEscape(t *testing.T) {
	t.Parallel()

	got, err := renderPayload("hello", SubmitModeComposerCtrlEnter, SubmitModeNewline, true)
	if err != nil {
		t.Fatalf("renderPayload() error = %v", err)
	}
	if string(got) != "hello\x1b[13;5u" {
		t.Fatalf("renderPayload() = %q", string(got))
	}
}

func TestRenderPayloadNewlineAddsInterruptEscape(t *testing.T) {
	t.Parallel()

	got, err := renderPayload("hello", SubmitModeNewline, SubmitModeNewline, true)
	if err != nil {
		t.Fatalf("renderPayload() error = %v", err)
	}
	if string(got) != "\x1bhello\n" {
		t.Fatalf("renderPayload() = %q", string(got))
	}
}

func TestRenderPayloadEnterAddsInterruptEscape(t *testing.T) {
	t.Parallel()

	got, err := renderPayload("hello", SubmitModeEnter, SubmitModeNewline, true)
	if err != nil {
		t.Fatalf("renderPayload() error = %v", err)
	}
	if string(got) != "\x1bhello\r" {
		t.Fatalf("renderPayload() = %q", string(got))
	}
}

func TestRenderPayloadEnterSplitAddsInterruptEscapeAndSeparateEnter(t *testing.T) {
	t.Parallel()

	got, err := renderPayloadParts("hello", SubmitModeEnterSplit, SubmitModeNewline, true)
	if err != nil {
		t.Fatalf("renderPayloadParts() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(renderPayloadParts()) = %d, want 2", len(got))
	}
	if string(got[0]) != "\x1bhello" {
		t.Fatalf("first payload = %q", string(got[0]))
	}
	if string(got[1]) != "\r" {
		t.Fatalf("second payload = %q", string(got[1]))
	}
}

func TestRenderPayloadRawAddsInterruptEscape(t *testing.T) {
	t.Parallel()

	got, err := renderPayload("hello", SubmitModeRaw, SubmitModeNewline, true)
	if err != nil {
		t.Fatalf("renderPayload() error = %v", err)
	}
	if string(got) != "\x1bhello" {
		t.Fatalf("renderPayload() = %q", string(got))
	}
}

func TestRenderControlMessagePartsSubmitEnter(t *testing.T) {
	t.Parallel()

	got, err := renderControlMessageParts(ControlMessage{Kind: "submit", SubmitMode: SubmitModeEnter}, SubmitModeNewline)
	if err != nil {
		t.Fatalf("renderControlMessageParts() error = %v", err)
	}
	if len(got) != 1 || string(got[0]) != "\r" {
		t.Fatalf("submit enter payload = %#v, want [\\r]", got)
	}
}

func TestRenderControlMessagePartsInterrupt(t *testing.T) {
	t.Parallel()

	got, err := renderControlMessageParts(ControlMessage{Kind: "interrupt"}, SubmitModeNewline)
	if err != nil {
		t.Fatalf("renderControlMessageParts() error = %v", err)
	}
	if len(got) != 1 || string(got[0]) != "\x1b" {
		t.Fatalf("interrupt payload = %#v, want [ESC]", got)
	}
}

func TestNormalizeStartupProfile(t *testing.T) {
	t.Parallel()

	got, err := normalizeStartupProfile(StartupProfileDroidAutoHigh)
	if err != nil {
		t.Fatalf("normalizeStartupProfile() error = %v", err)
	}
	if got != StartupProfileDroidAutoHigh {
		t.Fatalf("normalizeStartupProfile() = %q, want %q", got, StartupProfileDroidAutoHigh)
	}
	if _, err := normalizeStartupProfile("nope"); err == nil {
		t.Fatal("expected error for unsupported startup profile")
	}
}

func TestStartupTriggerMatchedDroidAutoHigh(t *testing.T) {
	t.Parallel()

	window, matched := updateStartupTrigger(StartupProfileDroidAutoHigh, nil, []byte("banner Auto (Off) ready"))
	if !matched {
		t.Fatal("expected startup trigger match for droid auto high")
	}
	if !startupTriggerMatched(StartupProfileDroidAutoHigh, window) {
		t.Fatal("expected persisted startup trigger match")
	}
}

func TestServeAppliesDroidAutoHighStartupProfile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	skipIfUnixSocketsUnavailable(t, tempDir)
	socketPath := shortSocketPath(t, "startup")
	capturePath := filepath.Join(tempDir, "capture.txt")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, ServeOptions{
			SocketPath:     socketPath,
			ParticipantID:  "droid-a",
			Command:        []string{"sh", "-lc", fmt.Sprintf("stty -icanon min 1 time 0 -echo; printf 'Auto (Off)'; dd bs=1 count=3 of=%q status=none", capturePath)},
			StartupProfile: StartupProfileDroidAutoHigh,
			Stdin:          strings.NewReader(""),
			Stdout:         &bytes.Buffer{},
		})
	}()

	waitForSocket(t, socketPath, errCh)
	waitForFileContent(t, capturePath, "\f\f\f")

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve() did not exit after cancel")
	}
}

func skipIfUnixSocketsUnavailable(t *testing.T, dir string) {
	t.Helper()
	socketPath := shortSocketPath(t, "probe")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EINVAL) || os.IsPermission(err) {
			t.Skipf("unix sockets not permitted: %v", err)
		}
		t.Fatalf("probe unix socket: %v", err)
	}
	_ = listener.Close()
	_ = os.Remove(socketPath)
}

func shortSocketPath(t *testing.T, label string) string {
	t.Helper()
	f, err := os.CreateTemp("", fmt.Sprintf("agentpane-*-%s.sock", label))
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	path := f.Name()
	_ = f.Close()
	_ = os.Remove(path)
	return path
}

func waitForSocket(t *testing.T, path string, errCh <-chan error) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case err := <-errCh:
			t.Fatalf("Serve() exited before socket readiness: %v", err)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket %q was not created", path)
}

func waitForFileContent(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && string(data) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("file %q = %q, want %q", path, string(data), want)
}
