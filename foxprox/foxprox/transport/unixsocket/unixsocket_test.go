package unixsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// shortSocketPath returns a socket path short enough for the macOS 104-byte
// sun_path limit by anchoring under /tmp rather than t.TempDir(). Each call
// returns a unique path; cleanup happens via t.Cleanup.
var socketSeq atomic.Uint64

func shortSocketPath(t *testing.T) string {
	t.Helper()
	n := socketSeq.Add(1)
	p := fmt.Sprintf("/tmp/fxtest-%d-%d.sock", os.Getpid(), n)
	t.Cleanup(func() { _ = os.Remove(p) })
	return p
}

func TestListen_RejectsEmptyPath(t *testing.T) {
	if _, err := Listen("", http.NotFoundHandler()); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestListen_RejectsNilHandler(t *testing.T) {
	if _, err := Listen(shortSocketPath(t), nil); err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestListen_CreatesSocketWith0600Perms(t *testing.T) {
	path := shortSocketPath(t)
	srv, err := Listen(path, http.NotFoundHandler())
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Errorf("mode = %v, expected socket flag", info.Mode())
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0600", perm)
	}
}

func TestServe_RoutesHTTPRequests(t *testing.T) {
	path := shortSocketPath(t)
	handler := http.NewServeMux()
	handler.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})
	srv, err := Listen(path, handler)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- srv.Serve() }()

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", path)
			},
		},
	}
	resp, err := client.Get("http://unix/ping")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("Serve returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("socket file still present after Shutdown: %v", err)
	}
}

// TestListen_RefusesToStealLiveSocket ensures a second broker cannot unlink
// the path while the first is still serving. Before this guard, the second
// Listen would delete the socket and steal the path, leaving the first broker
// alive but unreachable.
func TestListen_RefusesToStealLiveSocket(t *testing.T) {
	path := shortSocketPath(t)
	first, err := Listen(path, http.NotFoundHandler())
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	defer func() { _ = first.Close() }()
	go func() { _ = first.Serve() }()
	// Tiny sleep so the listener is accepting connections before we probe.
	time.Sleep(20 * time.Millisecond)

	_, err = Listen(path, http.NotFoundHandler())
	if err == nil {
		t.Fatal("expected second Listen to be rejected")
	}
	if !errors.Is(err, ErrBrokerAlreadyRunning) {
		t.Errorf("want ErrBrokerAlreadyRunning, got %v", err)
	}
}

func TestListen_ReplacesStaleSocketFile(t *testing.T) {
	path := shortSocketPath(t)
	first, err := Listen(path, http.NotFoundHandler())
	if err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	// Close listener but do NOT remove the file — simulate a crash leaving a
	// stale socket entry.
	_ = first.listen.Close()

	second, err := Listen(path, http.NotFoundHandler())
	if err != nil {
		t.Fatalf("second Listen should have recovered: %v", err)
	}
	_ = second.Close()
}

func TestListen_RefusesToClobberRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-socket")
	_ = dir // keep TempDir usage
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path, http.NotFoundHandler()); err == nil {
		t.Fatal("expected Listen to refuse clobbering a regular file")
	} else if !strings.Contains(err.Error(), "non-socket") {
		t.Errorf("error should mention non-socket: %v", err)
	}
}

func TestDefaultSocketPath_Fallbacks(t *testing.T) {
	// Clear the explicit override first — otherwise a pre-set
	// FOXCTL_Foxprox_SOCK in the developer's shell would short-circuit the
	// precedence checks below.
	t.Setenv(SocketEnv, "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", "")
	got := DefaultSocketPath()
	if !strings.HasSuffix(got, "foxctl-foxprox.sock") {
		t.Errorf("fallback default = %q", got)
	}
	t.Setenv("HOME", t.TempDir())
	got = DefaultSocketPath()
	if !strings.HasSuffix(got, "/.foxctl/foxprox.sock") {
		t.Errorf("HOME default = %q", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	got = DefaultSocketPath()
	if !strings.HasSuffix(got, "/foxctl/foxprox.sock") {
		t.Errorf("XDG default = %q", got)
	}
}

// TestDefaultSocketPath_EnvOverrideWins proves the FOXCTL_Foxprox_SOCK
// override beats every other env var. Operators rely on this to point
// multiple foxctl processes at a shared test socket in one place.
func TestDefaultSocketPath_EnvOverrideWins(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	want := "/tmp/override-foxprox.sock"
	t.Setenv(SocketEnv, want)
	if got := DefaultSocketPath(); got != want {
		t.Errorf("DefaultSocketPath = %q, want override %q", got, want)
	}
}
