package fakes

import (
	"fmt"
	"strings"
	"sync"
)

// FakeUUID returns deterministic monotonic IDs.
type FakeUUID struct {
	mu     sync.Mutex
	prefix string
	next   int
}

func NewFakeUUID(prefix string) *FakeUUID {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "id"
	}
	return &FakeUUID{prefix: p}
}

// New returns the next deterministic identifier.
func (f *FakeUUID) New() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.next++
	return fmt.Sprintf("%s-%04d", f.prefix, f.next)
}
