package skillmain

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestNewStoreProvider(t *testing.T) {
	cfg := config.Config{}
	sp := NewStoreProvider(cfg)
	if sp == nil {
		t.Fatal("expected non-nil StoreProvider")
	}
}

func TestStoreProviderCloseNoStores(t *testing.T) {
	cfg := config.Config{}
	sp := NewStoreProvider(cfg)
	// Close with no stores opened should not error
	if err := sp.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStoreProviderCloseIdempotent(t *testing.T) {
	cfg := config.Config{}
	sp := NewStoreProvider(cfg)
	_ = sp.Close()
	// Second close should be safe
	if err := sp.Close(); err != nil {
		t.Fatalf("unexpected error on second close: %v", err)
	}
}
