// Package skilltest provides test utilities for skill tests.
package skilltest

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cas"
)

// TestContextOptions configures the test RunContext.
type TestContextOptions struct {
	// Workspace overrides the workspace path (default: t.TempDir())
	Workspace string

	// SessionID sets the session ID (default: "test-session")
	SessionID string

	// AgentID sets the agent ID (default: "test-agent")
	AgentID string

	// SkipCAS disables CAS store initialization
	SkipCAS bool

	// EnvVars sets environment variables for the test (cleaned up after)
	EnvVars map[string]string
}

// NewTestRunContext creates a RunContext suitable for testing.
// It creates a temporary storage root and configures paths appropriately.
// The returned cleanup function should be deferred to clean up resources.
func NewTestRunContext(t *testing.T, stdout io.Writer, opts *TestContextOptions) (*skillmain.RunContext, func()) {
	t.Helper()

	if opts == nil {
		opts = &TestContextOptions{}
	}

	// Create temp directories
	storageRoot := t.TempDir()
	cacheRoot := filepath.Join(storageRoot, "cache")
	casRoot := filepath.Join(storageRoot, "cas")

	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}
	if err := os.MkdirAll(casRoot, 0o755); err != nil {
		t.Fatalf("create cas dir: %v", err)
	}

	// Determine workspace
	workspace := opts.Workspace
	if workspace == "" {
		workspace = t.TempDir()
	}

	// Create path validator for the workspace
	pathValidator, err := policy.NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("create path validator: %v", err)
	}

	// Set env vars and track for cleanup
	var envCleanup []func()
	for k, v := range opts.EnvVars {
		old, hadOld := os.LookupEnv(k)
		os.Setenv(k, v)
		if hadOld {
			envCleanup = append(envCleanup, func() { os.Setenv(k, old) })
		} else {
			envCleanup = append(envCleanup, func() { os.Unsetenv(k) })
		}
	}

	// Build config
	cfg := config.Config{
		Home:           storageRoot,
		InlineOutputKB: 100,
		MaxCaptureKB:   1024,
		Paths: config.Paths{
			CAS:   casRoot,
			Cache: cacheRoot,
		},
		Storage: config.StorageSettings{
			Root: storageRoot,
		},
	}

	// Initialize CAS store if not skipped
	var casStore *cas.Store
	if !opts.SkipCAS {
		var err error
		casStore, err = cas.NewStore(casRoot)
		if err != nil {
			t.Fatalf("open CAS store: %v", err)
		}
	}

	// Session ID
	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = "test-session"
	}

	// Agent ID
	agentID := opts.AgentID
	if agentID == "" {
		agentID = "test-agent"
	}

	// Create logger that discards output (or writes to test log)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()

	rc := &skillmain.RunContext{
		Config:        cfg,
		CASStore:      casStore,
		Workspace:     workspace,
		SessionID:     sessionID,
		AgentID:       agentID,
		Logger:        logger,
		Stdout:        stdout,
		InlineKB:      cfg.InlineOutputKB,
		MaxPreview:    100,
		PathValidator: pathValidator,
	}

	cleanup := func() {
		if casStore != nil {
			casStore.Close()
		}
		for _, fn := range envCleanup {
			fn()
		}
	}

	return rc, cleanup
}

// NewTestConfig creates a minimal config for testing with temp directories.
func NewTestConfig(t *testing.T) config.Config {
	t.Helper()

	root := t.TempDir()
	return config.Config{
		Home:           root,
		InlineOutputKB: 100,
		MaxCaptureKB:   1024,
		Paths: config.Paths{
			CAS:   filepath.Join(root, "cas"),
			Cache: filepath.Join(root, "cache"),
		},
		Storage: config.StorageSettings{
			Root: root,
		},
	}
}
