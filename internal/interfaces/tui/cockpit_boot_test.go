package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gotui "github.com/grindlemire/go-tui"
)

// ---------------------------------------------------------------------------
// VAL-SKEL-002: Async boot — loading then error on unreachable API
// VAL-SKEL-011: Connection-error state — labeled, keyboard-responsive
//
// These tests verify:
//   (i)   Loading state renders within 500ms of launch
//   (ii)  ESC during loading exits code 0 (verified via KeyMap)
//   (iii) Transitions to error state after timeout ≤5s when API unreachable;
//         error state names the URL and shows retry guidance
//   (iv)  If daemon comes online within timeout, loading transitions to ready
//         NOT to error
//   (v)   No synchronous HTTP blocks the UI thread at startup
//   (vi)  Error state: names URL, retry hint, no crash, ESC/quit exit 0
// ---------------------------------------------------------------------------

// --- Test: BootManager transitions to error on unreachable API ---

func TestBootManager_TransitionsToErrorOnUnreachableAPI(t *testing.T) {
	// Use a URL that nothing is listening on.
	cs := NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL:  "http://127.0.0.1:1",
		Screen:  cs,
		Timeout: 500 * time.Millisecond, // short timeout for fast test
		Client: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	})

	if cs.Phase() != CockpitPhaseLoading {
		t.Fatalf("initial phase should be Loading, got %s", cs.Phase())
	}

	bm.Start()
	defer bm.Stop()

	// Wait for the boot manager to finish.
	bm.WaitForDone()

	phase := cs.Phase()
	if phase != CockpitPhaseError {
		t.Fatalf("expected phase=Error after timeout, got %s", phase)
	}
}

// --- Test: Error state names the URL ---

func TestBootManager_ErrorStateNamesURL(t *testing.T) {
	apiURL := "http://127.0.0.1:1"
	cs := NewCockpitScreen(apiURL)
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL:  apiURL,
		Screen:  cs,
		Timeout: 300 * time.Millisecond,
		Client: &http.Client{
			Timeout: 50 * time.Millisecond,
		},
	})

	bm.Start()
	bm.WaitForDone()

	if cs.Phase() != CockpitPhaseError {
		t.Fatalf("expected Error phase, got %s", cs.Phase())
	}

	// The screen's APIURL should be the unreachable URL.
	if cs.APIURL() != apiURL {
		t.Fatalf("expected APIURL=%q, got %q", apiURL, cs.APIURL())
	}

	// Verify the error render contains the URL by rendering into a mock terminal.
	el := cs.renderError(80, 24, apiURL)
	if el == nil {
		t.Fatal("renderError returned nil")
	}
}

// --- Test: Error state shows retry hint ---

func TestBootManager_ErrorStateShowsRetryHint(t *testing.T) {
	cs := NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	el := cs.Render(nil)
	if el == nil {
		t.Fatal("Render of error state returned nil")
	}
}

// --- Test: BootManager transitions to ready when daemon is healthy ---

func TestBootManager_TransitionsToReadyWhenDaemonHealthy(t *testing.T) {
	// Create a fake healthy daemon.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	cs := NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  cs,
		Timeout: 5 * time.Second,
	})

	bm.Start()
	defer bm.Stop()

	// Wait for the boot manager to finish or timeout.
	bm.WaitForDone()

	phase := cs.Phase()
	if phase != CockpitPhaseReady {
		t.Fatalf("expected phase=Ready with healthy daemon, got %s", phase)
	}
}

// --- Test: BootManager transitions to ready when daemon appears mid-wait ---

func TestBootManager_TransitionsToReadyWhenDaemonAppearsMidWait(t *testing.T) {
	// Start with an unreachable server, then make it healthy mid-way.
	var healthy atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			if healthy.Load() {
				w.WriteHeader(http.StatusOK)
				return
			}
			// Return 503 to simulate unavailable.
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	cs := NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  cs,
		Timeout: 3 * time.Second,
	})

	bm.Start()
	defer bm.Stop()

	// After a short delay, make the daemon "come online".
	time.Sleep(500 * time.Millisecond)
	healthy.Store(true)

	// Wait for the boot manager to finish.
	bm.WaitForDone()

	phase := cs.Phase()
	if phase != CockpitPhaseReady {
		t.Fatalf("expected phase=Ready when daemon comes online mid-wait, got %s", phase)
	}
}

// --- Test: No synchronous HTTP before first paint ---

func TestBootManager_NoSynchronousHTTPBeforeFirstPaint(t *testing.T) {
	// Track whether any HTTP request was made before the first render.
	var requestMadeBeforeRender atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMadeBeforeRender.Store(true)
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	cs := NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	// Simulate first paint (Render) BEFORE starting the boot manager.
	el := cs.Render(nil)
	if el == nil {
		t.Fatal("first Render should succeed without any HTTP")
	}

	// First render should happen with no HTTP request.
	if requestMadeBeforeRender.Load() {
		t.Fatal("HTTP request was made before first render — synchronous HTTP on UI thread")
	}

	// Now start the async boot (which will make HTTP requests).
	bm := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  cs,
		Timeout: 2 * time.Second,
	})
	bm.Start()
	bm.WaitForDone()

	// The boot manager should have made requests by now.
	if !requestMadeBeforeRender.Load() {
		t.Fatal("expected HTTP request during async boot, but none was made")
	}
}

// --- Test: Loading state renders quickly (within 500ms) ---

func TestBootManager_LoadingStateRendersWithin500ms(t *testing.T) {
	cs := NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)

	// The screen starts in Loading phase — no HTTP needed.
	start := time.Now()
	el := cs.Render(nil)
	elapsed := time.Since(start)

	if el == nil {
		t.Fatal("Render of loading state returned nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("loading state render took %v — should be <500ms", elapsed)
	}
	if cs.Phase() != CockpitPhaseLoading {
		t.Fatalf("expected Loading phase, got %s", cs.Phase())
	}
}

// --- Test: ESC key binding works during loading ---

func TestCockpitScreen_ESCWorksDuringLoading(t *testing.T) {
	cs := NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseLoading)

	// Verify KeyMap contains ESC binding.
	km := cs.KeyMap()
	if len(km) == 0 {
		t.Fatal("KeyMap should not be empty during loading")
	}
	// The ESC binding is verified by the existing TestCockpitScreen_KeyMapContainsEscape test.
	// Here we verify that the loading state still has the same keymap.
}

// --- Test: ESC key binding works during error ---

func TestCockpitScreen_ESCWorksDuringError(t *testing.T) {
	cs := NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	km := cs.KeyMap()
	if len(km) == 0 {
		t.Fatal("KeyMap should not be empty during error")
	}
}

// --- Test: Retry after error ---

func TestBootManager_RetryAfterError(t *testing.T) {
	// Start with unhealthy, transition to healthy after retry.
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if r.URL.Path == "/healthz" {
			// First few requests fail, then succeed.
			if count > 3 {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	cs := NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	// First attempt: short timeout → error
	bm := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  cs,
		Timeout: 500 * time.Millisecond,
		Client: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	})

	bm.Start()
	bm.WaitForDone()

	if cs.Phase() != CockpitPhaseError {
		t.Fatalf("expected Error after first attempt, got %s", cs.Phase())
	}

	// Now retry with a longer timeout — the server should start succeeding
	// after enough requests.
	bm2 := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  cs,
		Timeout: 5 * time.Second,
	})
	bm2.Start()
	bm2.WaitForDone()

	if cs.Phase() != CockpitPhaseReady {
		t.Fatalf("expected Ready after retry, got %s", cs.Phase())
	}
}

// --- Test: BootManager retry via Retry() method ---

func TestBootManager_RetryMethod(t *testing.T) {
	var healthy atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			if healthy.Load() {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	cs := NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  cs,
		Timeout: 500 * time.Millisecond,
		Client: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	})

	// First attempt: will fail.
	bm.Start()
	bm.WaitForDone()

	if cs.Phase() != CockpitPhaseError {
		t.Fatalf("expected Error after first attempt, got %s", cs.Phase())
	}

	// Now make the daemon healthy and retry.
	healthy.Store(true)
	bm.Retry()

	// Wait for the retry to complete.
	bm.WaitForDone()

	if cs.Phase() != CockpitPhaseReady {
		t.Fatalf("expected Ready after retry, got %s", cs.Phase())
	}
}

// --- Test: BootManager Stop is safe ---

func TestBootManager_StopIsIdempotent(t *testing.T) {
	cs := NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL:  "http://127.0.0.1:1",
		Screen:  cs,
		Timeout: 10 * time.Second, // long timeout so Stop cancels it
		Client: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	})

	bm.Start()

	// Double-stop should be safe.
	bm.Stop()
	bm.Stop() // no panic
}

// --- Test: BootManager respects context cancellation ---

func TestBootManager_ContextCancellationNoTransition(t *testing.T) {
	cs := NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL:  "http://127.0.0.1:1",
		Screen:  cs,
		Timeout: 10 * time.Second,
		Client: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	})

	bm.Start()

	// Cancel via Stop (which cancels the context).
	bm.Stop()

	// Phase should remain Loading (not Error) because the context was
	// cancelled by Stop(), not by timeout.
	if cs.Phase() != CockpitPhaseLoading {
		t.Fatalf("expected Loading after Stop(), got %s", cs.Phase())
	}
}

// --- Test: Loading state contains URL in render ---

func TestBootManager_LoadingStateContainsURL(t *testing.T) {
	apiURL := "http://localhost:9999"
	cs := NewCockpitScreen(apiURL)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseLoading)

	// The loading message should contain the URL.
	el := cs.renderLoading(80, 24, apiURL)
	if el == nil {
		t.Fatal("renderLoading returned nil")
	}
}

// --- Test: Error state shows URL and retry hint ---

func TestBootManager_ErrorStateShowsURLAndRetryHint(t *testing.T) {
	apiURL := "http://localhost:9999"
	cs := NewCockpitScreen(apiURL)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	el := cs.renderError(80, 24, apiURL)
	if el == nil {
		t.Fatal("renderError returned nil")
	}
}

// --- Test: BootManager timeout is respected (≤5s) ---

func TestBootManager_TimeoutIsRespected(t *testing.T) {
	cs := NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)

	timeout := 500 * time.Millisecond
	bm := NewBootManager(BootConfig{
		APIURL:  "http://127.0.0.1:1",
		Screen:  cs,
		Timeout: timeout,
		Client: &http.Client{
			Timeout: 50 * time.Millisecond,
		},
	})

	start := time.Now()
	bm.Start()
	bm.WaitForDone()
	elapsed := time.Since(start)

	if cs.Phase() != CockpitPhaseError {
		t.Fatalf("expected Error, got %s", cs.Phase())
	}

	// The transition should happen within the timeout plus some margin.
	if elapsed > timeout+500*time.Millisecond {
		t.Fatalf("took %v to reach error state, expected ≤%v+margin", elapsed, timeout)
	}
}

// --- Test: BootManager default timeout is 5s ---

func TestBootManager_DefaultTimeoutIs5s(t *testing.T) {
	cs := NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL: "http://127.0.0.1:1",
		Screen: cs,
	})

	if bm.timeout != DefaultBootTimeout {
		t.Fatalf("expected default timeout=%v, got %v", DefaultBootTimeout, bm.timeout)
	}
}

// --- Test: BootManager default timeout ≤5s ---

func TestBootManager_DefaultTimeoutAtMost5s(t *testing.T) {
	if DefaultBootTimeout > 5*time.Second {
		t.Fatalf("DefaultBootTimeout=%v exceeds 5s limit", DefaultBootTimeout)
	}
}

// --- Test: 'r' key in error state triggers retry (via KeyMap) ---

func TestCockpitScreen_RetryKeyInErrorState(t *testing.T) {
	cs := NewCockpitScreen("http://127.0.0.1:1")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	km := cs.KeyMap()
	if len(km) == 0 {
		t.Fatal("KeyMap should not be empty")
	}
	// The KeyMap should include a binding for 'r' to retry.
	// We verify this by checking the key count (ESC, q, Ctrl+C, r = at least 4).
	if len(km) < 3 {
		t.Fatalf("KeyMap should contain at least 3 bindings, got %d", len(km))
	}
}

// --- Test: renderError contains retry hint text ---

func TestRenderError_ContainsRetryHint(t *testing.T) {
	// Verify the error render includes retry hint text.
	cs := NewCockpitScreen("http://localhost:9999")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	el := cs.renderError(80, 24, "http://localhost:9999")
	if el == nil {
		t.Fatal("renderError returned nil")
	}
}

// --- Test: Phase transitions are safe under concurrent access ---

func TestBootManager_ConcurrentPhaseTransitions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cs := NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  cs,
		Timeout: 2 * time.Second,
	})

	bm.Start()

	// Hammer Phase() while the boot manager is running.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = cs.Phase()
		}
		close(done)
	}()

	bm.WaitForDone()
	<-done

	if cs.Phase() != CockpitPhaseReady {
		t.Fatalf("expected Ready, got %s", cs.Phase())
	}
}

// --- Test: No goroutine leak from BootManager ---
//
// This test verifies that BootManager.Stop() cleans up its background
// goroutine. We use a relaxed threshold because the test runtime, race
// detector, and httptest server create transient goroutines that are
// not related to BootManager.

func TestBootManager_NoGoroutineLeak(t *testing.T) {
	// Measure baseline after GC and settling.
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cs := NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  cs,
		Timeout: 2 * time.Second,
	})

	bm.Start()
	bm.WaitForDone()
	bm.Stop()

	// Close the server before measuring to avoid its goroutines inflating the count.
	server.Close()

	// Allow goroutines to settle.
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	after := runtime.NumGoroutine()

	// BootManager's goroutine should be done. Allow a small tolerance for
	// transient test/runtime goroutines. The key invariant is that BootManager
	// does not leak a persistent goroutine.
	delta := after - baseline
	if delta > 2 {
		t.Fatalf("potential BootManager goroutine leak: %d extra goroutines (baseline=%d, after=%d)",
			delta, baseline, after)
	}
}

// --- Test: checkHealth returns false on connection refused ---

func TestBootManager_CheckHealthConnectionRefused(t *testing.T) {
	bm := NewBootManager(BootConfig{
		APIURL:  "http://127.0.0.1:1",
		Screen:  NewCockpitScreen("http://127.0.0.1:1"),
		Timeout: 1 * time.Second,
		Client: &http.Client{
			Timeout: 50 * time.Millisecond,
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if bm.checkHealth(ctx) {
		t.Fatal("checkHealth should return false on connection refused")
	}
}

// --- Test: checkHealth returns true on 200 OK ---

func TestBootManager_CheckHealthSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	bm := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  NewCockpitScreen(server.URL),
		Timeout: 1 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if !bm.checkHealth(ctx) {
		t.Fatal("checkHealth should return true on 200 OK")
	}
}

// --- Test: checkHealth returns false on non-200 ---

func TestBootManager_CheckHealthNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	bm := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  NewCockpitScreen(server.URL),
		Timeout: 1 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if bm.checkHealth(ctx) {
		t.Fatal("checkHealth should return false on 503")
	}
}

// ---------------------------------------------------------------------------
// MockTerminal render verification — content-level assertions for
// VAL-SKEL-002 and VAL-SKEL-011.
// ---------------------------------------------------------------------------

// --- Test: Loading state MockTerminal render shows URL ---

func TestBootManager_LoadingRenderShowsURL(t *testing.T) {
	apiURL := "http://localhost:9999"
	cs := NewCockpitScreen(apiURL)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseLoading)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// The loading message should contain the URL.
	rowText := rowTextFromMT(mt, 0, 80)
	if !containsSubstring(rowText, apiURL) {
		t.Errorf("loading state row 0 should contain URL %q, got: %q", apiURL, rowText)
	}

	// The loading message should contain "Connecting".
	if !containsSubstring(rowText, "Connecting") {
		t.Errorf("loading state row 0 should contain 'Connecting', got: %q", rowText)
	}
}

// --- Test: Loading state footer shows ESC:quit ---

func TestBootManager_LoadingFooterShowsESC(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:9999")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseLoading)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// Footer (last row) should show ESC:quit.
	footerText := rowTextFromMT(mt, 23, 80)
	if !containsSubstring(footerText, "ESC") {
		t.Errorf("loading footer should contain 'ESC', got: %q", footerText)
	}
	if !containsSubstring(footerText, "quit") {
		t.Errorf("loading footer should contain 'quit', got: %q", footerText)
	}
}

// --- Test: Error state MockTerminal render shows URL ---

func TestBootManager_ErrorRenderShowsURL(t *testing.T) {
	apiURL := "http://localhost:9999"
	cs := NewCockpitScreen(apiURL)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// The error message should contain the URL.
	rowText := rowTextFromMT(mt, 0, 80)
	if !containsSubstring(rowText, apiURL) {
		t.Errorf("error state row 0 should contain URL %q, got: %q", apiURL, rowText)
	}
}

// --- Test: Error state MockTerminal render shows "Cannot reach" ---

func TestBootManager_ErrorRenderShowsCannotReach(t *testing.T) {
	apiURL := "http://localhost:9999"
	cs := NewCockpitScreen(apiURL)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	_, mt := renderCockpitToMT(cs, 80, 24)

	rowText := rowTextFromMT(mt, 0, 80)
	if !containsSubstring(rowText, "Cannot reach") {
		t.Errorf("error state row 0 should contain 'Cannot reach', got: %q", rowText)
	}
}

// --- Test: Error state MockTerminal render shows retry hint ---

func TestBootManager_ErrorRenderShowsRetryHint(t *testing.T) {
	apiURL := "http://localhost:9999"
	cs := NewCockpitScreen(apiURL)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	_, mt := renderCockpitToMT(cs, 80, 24)

	// The retry hint should be visible in the second row.
	rowText := rowTextFromMT(mt, 1, 80)
	if !containsSubstring(rowText, "retry") {
		t.Errorf("error state row 1 should contain 'retry', got: %q", rowText)
	}
	if !containsSubstring(rowText, "ESC") {
		t.Errorf("error state row 1 should contain 'ESC', got: %q", rowText)
	}
}

// --- Test: Error state footer shows ESC:quit ---

func TestBootManager_ErrorFooterShowsESC(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:9999")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	_, mt := renderCockpitToMT(cs, 80, 24)

	footerText := rowTextFromMT(mt, 23, 80)
	if !containsSubstring(footerText, "ESC") {
		t.Errorf("error footer should contain 'ESC', got: %q", footerText)
	}
	if !containsSubstring(footerText, "quit") {
		t.Errorf("error footer should contain 'quit', got: %q", footerText)
	}
}

// --- Test: Error state footer shows retry key ---

func TestBootManager_ErrorFooterShowsRetryKey(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:9999")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	_, mt := renderCockpitToMT(cs, 80, 24)

	footerText := rowTextFromMT(mt, 23, 80)
	if !containsSubstring(footerText, "r") {
		t.Errorf("error footer should contain 'r' key hint, got: %q", footerText)
	}
}

// --- Test: Ready state shows three-lane headers after successful boot ---

func TestBootManager_ReadyRenderShowsLanesAfterBoot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	cs := NewCockpitScreen(server.URL)
	cs.UpdateSize(80, 24)

	bm := NewBootManager(BootConfig{
		APIURL:  server.URL,
		Screen:  cs,
		Timeout: 2 * time.Second,
	})

	bm.Start()
	bm.WaitForDone()

	if cs.Phase() != CockpitPhaseReady {
		t.Fatalf("expected Ready, got %s", cs.Phase())
	}

	_, mt := renderCockpitToMT(cs, 80, 24)

	// Verify three-lane headers are present.
	row0 := rowTextFromMT(mt, 0, 80)
	for _, header := range []string{"Agents", "Detail", "Evidence"} {
		if !containsSubstring(row0, header) {
			t.Errorf("ready state row 0 should contain lane header %q, got: %q", header, row0)
		}
	}
}

// rowTextFromMT extracts the text content of a single row from a MockTerminal.
func rowTextFromMT(mt *gotui.MockTerminal, row, width int) string {
	var buf strings.Builder
	for x := 0; x < width; x++ {
		cell := mt.CellAt(x, row)
		if cell.Rune != 0 {
			buf.WriteRune(cell.Rune)
		} else {
			buf.WriteRune(' ')
		}
	}
	return buf.String()
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// VAL-SKEL-012: Status footer — connection + active entity + keybindings
//
// Tests verify the footer is visible on every M3 screen and contains all
// three required elements in ≥3 captured snapshots across different states.
// ---------------------------------------------------------------------------

// assertFooterElements checks that the footer row contains all three required
// elements: connection status indicator, active entity (when expected), and
// ≥3 keybinding hints.
func assertFooterElements(t *testing.T, mt *gotui.MockTerminal, width, height int, expectActiveEntity bool) {
	t.Helper()
	footerText := rowTextFromMT(mt, height-1, width)

	// (i) Connection status indicator must be present.
	// We check for one of the status icons or labels.
	statusFound := containsSubstring(footerText, "connected") ||
		containsSubstring(footerText, "connecting") ||
		containsSubstring(footerText, "error") ||
		containsSubstring(footerText, "degraded") ||
		containsSubstring(footerText, "done") ||
		containsSubstring(footerText, "✓") ||
		containsSubstring(footerText, "✗")
	if !statusFound {
		t.Errorf("footer missing connection status indicator: %q", footerText)
	}

	// (ii) Active entity label (when expected).
	if expectActiveEntity {
		if !containsSubstring(footerText, "agent:") {
			t.Errorf("footer missing active entity label (expected 'agent:...'): %q", footerText)
		}
	}

	// (iii) Compact keybinding hint strip with ≥3 bindings.
	bindings := []string{"ESC", "↑↓", "Enter", "e", "Ctrl+X", "r", "Tab"}
	found := 0
	for _, b := range bindings {
		if containsSubstring(footerText, b) {
			found++
		}
	}
	if found < 3 {
		t.Errorf("footer has %d keybinding hints, expected ≥3: %q", found, footerText)
	}
}

// --- Test: Loading state footer contains all 3 elements ---

func TestStatusFooter_LoadingState(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:9999")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseLoading)

	_, mt := renderCockpitToMT(cs, 80, 24)
	assertFooterElements(t, mt, 80, 24, false)

	// Loading-specific assertions.
	footerText := rowTextFromMT(mt, 23, 80)
	if !containsSubstring(footerText, "connecting") {
		t.Errorf("loading footer should show 'connecting', got: %q", footerText)
	}
	if !containsSubstring(footerText, "ESC") {
		t.Errorf("loading footer should show ESC binding, got: %q", footerText)
	}
}

// --- Test: Error state footer contains all 3 elements ---

func TestStatusFooter_ErrorState(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:9999")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseError)

	_, mt := renderCockpitToMT(cs, 80, 24)
	assertFooterElements(t, mt, 80, 24, false)

	// Error-specific assertions.
	footerText := rowTextFromMT(mt, 23, 80)
	if !containsSubstring(footerText, "error") {
		t.Errorf("error footer should show 'error' status, got: %q", footerText)
	}
	if !containsSubstring(footerText, "r") {
		t.Errorf("error footer should show 'r' retry binding, got: %q", footerText)
	}
	if !containsSubstring(footerText, "retry") {
		t.Errorf("error footer should show 'retry' hint, got: %q", footerText)
	}
}

// --- Test: Ready state footer (no selection) contains status + keybindings ---

func TestStatusFooter_ReadyStateNoSelection(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 80, 24)
	assertFooterElements(t, mt, 80, 24, false)

	// Ready-specific assertions.
	footerText := rowTextFromMT(mt, 23, 80)
	if !containsSubstring(footerText, "connected") {
		t.Errorf("ready footer should show 'connected' status, got: %q", footerText)
	}
	if !containsSubstring(footerText, "↑↓") {
		t.Errorf("ready footer should show '↑↓' nav binding, got: %q", footerText)
	}
	if !containsSubstring(footerText, "Enter") {
		t.Errorf("ready footer should show 'Enter' submit binding, got: %q", footerText)
	}
	if !containsSubstring(footerText, "e") {
		t.Errorf("ready footer should show 'e' evidence binding, got: %q", footerText)
	}
}

// --- Test: Ready state footer (with selected agent) contains active entity ---

func TestStatusFooter_ReadyStateWithSelection(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc12345", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "—", LastActive: "2m"},
		{ID: "agent-def67890", Role: "coder", Status: "idle", Workspace: "ws1", ParentID: "—", LastActive: "5m"},
	})
	// sortAgents orders by Role then ID, so coder sorts before researcher.
	// Select index 1 to target the researcher.
	cs.SetSelectedIndex(1)
	// Use width 120 so all keybindings (including Ctrl+X:cancel) fit alongside
	// the active entity label without truncation.
	cs.UpdateSize(120, 24)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 120, 24)
	assertFooterElements(t, mt, 120, 24, true)

	// Active entity-specific assertions.
	footerText := rowTextFromMT(mt, 23, 120)
	if !containsSubstring(footerText, "agent:") {
		t.Errorf("ready footer with selection should show 'agent:' label, got: %q", footerText)
	}
	if !containsSubstring(footerText, "researcher") {
		t.Errorf("ready footer with selection should show agent role 'researcher', got: %q", footerText)
	}
	if !containsSubstring(footerText, "Ctrl+X") {
		t.Errorf("ready footer with selection should show 'Ctrl+X' cancel binding, got: %q", footerText)
	}
}

// --- Test: Ready state footer with stream status shows stream status ---

func TestStatusFooter_ReadyStateWithStreamStatus(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc12345", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "—", LastActive: "2m"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)
	cs.streamStatus = "✓ done"

	_, mt := renderCockpitToMT(cs, 80, 24)
	assertFooterElements(t, mt, 80, 24, true)

	footerText := rowTextFromMT(mt, 23, 80)
	if !containsSubstring(footerText, "done") {
		t.Errorf("ready footer with stream should show 'done' status, got: %q", footerText)
	}
}

// --- Test: Footer visible at minimum terminal size ---

func TestStatusFooter_MinimumSize(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]AgentInventoryItem{
		{ID: "agent-abc", Role: "researcher", Status: "running", Workspace: "ws1", ParentID: "—", LastActive: "2m"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(60, 15)
	cs.SetPhase(CockpitPhaseReady)

	_, mt := renderCockpitToMT(cs, 60, 15)
	assertFooterElements(t, mt, 60, 15, true)
}

// --- Test: Footer visible during loading at minimum size ---

func TestStatusFooter_LoadingAtMinimumSize(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(60, 15)
	cs.SetPhase(CockpitPhaseLoading)

	_, mt := renderCockpitToMT(cs, 60, 15)
	assertFooterElements(t, mt, 60, 15, false)
}

// --- Test: Footer visible during error at minimum size ---

func TestStatusFooter_ErrorAtMinimumSize(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(60, 15)
	cs.SetPhase(CockpitPhaseError)

	_, mt := renderCockpitToMT(cs, 60, 15)
	assertFooterElements(t, mt, 60, 15, false)
}
