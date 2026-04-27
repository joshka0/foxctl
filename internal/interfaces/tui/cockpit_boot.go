package tui

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// BootManager manages the async boot lifecycle for the cockpit screen.
// It performs a background HTTP health check against the daemon API and
// transitions the CockpitScreen through Loading → Ready or Loading → Error.
//
// No synchronous HTTP is performed on the UI thread. The caller starts the
// boot check via Start(), and the CockpitScreen transitions are driven by
// the background goroutine through a callback.
//
// Use NewBootManager to construct one. Call Start() after the go-tui event
// loop begins rendering. Call Stop() to cancel the background goroutine.
type BootManager struct {
	mu           sync.Mutex
	apiURL       string
	screen       *CockpitScreen
	client       *http.Client
	timeout      time.Duration
	cancelFn     context.CancelFunc
	done         chan struct{}
	retrying     bool
	agentAdapter *AgentAdapter
}

// BootConfig configures the BootManager.
type BootConfig struct {
	// APIURL is the daemon base URL to health-check.
	APIURL string

	// Screen is the CockpitScreen whose phase will be transitioned.
	Screen *CockpitScreen

	// Client is the HTTP client for health checks. Defaults to a short-timeout
	// client if nil.
	Client *http.Client

	// Timeout is the maximum time to wait for the daemon to become healthy
	// before transitioning to error. Must be > 0. Default 5s.
	Timeout time.Duration

	// AgentAdapter is optional. When provided, the BootManager fetches the
	// agent inventory via GET /api/agents after the health check succeeds and
	// populates the CockpitScreen before transitioning to Ready.
	AgentAdapter *AgentAdapter
}

// DefaultBootTimeout is the default time to wait for the daemon to become
// healthy before transitioning to the error phase.
const DefaultBootTimeout = 5 * time.Second

// healthCheckInterval is how often the boot manager polls the daemon health
// endpoint during the loading phase.
const healthCheckInterval = 250 * time.Millisecond

// NewBootManager creates a new BootManager. It does NOT start the background
// check — call Start() to begin.
func NewBootManager(cfg BootConfig) *BootManager {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultBootTimeout
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{
			Timeout: 2 * time.Second,
		}
	}
	return &BootManager{
		apiURL:       cfg.APIURL,
		screen:       cfg.Screen,
		client:       client,
		timeout:      timeout,
		agentAdapter: cfg.AgentAdapter,
		done:         make(chan struct{}),
	}
}

// Start begins the background health-check goroutine. It returns immediately
// and does not block the UI thread. The CockpitScreen should be in
// CockpitPhaseLoading when Start is called.
//
// Call Stop() to cancel the background goroutine and release resources.
func (bm *BootManager) Start() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), bm.timeout)
	bm.cancelFn = cancel

	go bm.run(ctx)
}

// Stop cancels the background goroutine and waits for it to finish.
// It is safe to call Stop() multiple times.
func (bm *BootManager) Stop() {
	bm.mu.Lock()
	cancelFn := bm.cancelFn
	bm.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}
	<-bm.done
}

// Retry re-attempts the health check after an error. The screen is reset to
// CockpitPhaseLoading and a new background check is started with a fresh
// timeout. It is a no-op if a check is already in progress.
func (bm *BootManager) Retry() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Don't retry if a check is already in progress.
	if bm.retrying {
		return
	}

	// Cancel any existing check.
	if bm.cancelFn != nil {
		bm.cancelFn()
	}
	// Drain the old done channel.
	select {
	case <-bm.done:
	default:
		<-bm.done
	}

	bm.done = make(chan struct{})
	bm.retrying = true
	bm.screen.SetPhase(CockpitPhaseLoading)

	ctx, cancel := context.WithTimeout(context.Background(), bm.timeout)
	bm.cancelFn = cancel

	go func() {
		bm.run(ctx)
		bm.mu.Lock()
		bm.retrying = false
		bm.mu.Unlock()
	}()
}

// run is the background goroutine that polls the daemon health endpoint.
func (bm *BootManager) run(ctx context.Context) {
	defer close(bm.done)

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Timeout or cancellation. If the context was cancelled by Stop(),
			// don't transition to error — the app is shutting down.
			// If it was a timeout, transition to error.
			if ctx.Err() == context.DeadlineExceeded {
				bm.screen.SetPhase(CockpitPhaseError)
			}
			return
		case <-ticker.C:
			healthy := bm.checkHealth(ctx)
			if healthy {
				bm.fetchAgentsAndTransitionReady(ctx)
				return
			}
		}
	}
}

// fetchAgentsAndTransitionReady fetches the agent inventory (if an adapter is
// configured) and transitions the screen to Ready. This runs on the background
// goroutine, not the UI thread.
func (bm *BootManager) fetchAgentsAndTransitionReady(ctx context.Context) {
	if bm.agentAdapter != nil {
		resp, err := bm.agentAdapter.ListAgents(ctx, 0)
		if err == nil {
			items := make([]AgentInventoryItem, len(resp.Agents))
			for i, a := range resp.Agents {
				items[i] = agentRecordToInventoryItem(a)
			}
			bm.screen.SetAgents(items)
		}
		// On error, we still transition to Ready — the screen will show an
		// empty inventory and the user can retry if needed.
	}
	bm.screen.SetPhase(CockpitPhaseReady)
}

// agentRecordToInventoryItem maps an AgentRecord from the API to an
// AgentInventoryItem for display in the Main lane.
func agentRecordToInventoryItem(a AgentRecord) AgentInventoryItem {
	parent := a.ParentID
	if parent == "" {
		parent = "—"
	}
	return AgentInventoryItem{
		ID:        a.ID,
		Role:      a.Role,
		Status:    a.State,
		Workspace: a.Namespace,
		ParentID:  parent,
		// LastActive is not available from AgentRecord; show "—" for now.
		LastActive: "—",
	}
}

// checkHealth performs a single HTTP GET to the daemon's /healthz endpoint.
func (bm *BootManager) checkHealth(ctx context.Context) bool {
	url := bm.apiURL + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := bm.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// IsRetrying returns whether a retry attempt is currently in progress.
func (bm *BootManager) IsRetrying() bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.retrying
}

// WaitForDone blocks until the background goroutine has finished, for testing.
func (bm *BootManager) WaitForDone() {
	<-bm.done
}
