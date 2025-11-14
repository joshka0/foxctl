package artifacts

import "context"

// MockManager is a test double for Manager.
type MockManager struct {
	ExtractDigestsFunc    func(envelope []byte) ([]string, error)
	PinFunc               func(ctx context.Context, digests ...string) error
	UnpinFunc             func(ctx context.Context, digests ...string) error
	PinFromEnvelopeFunc   func(ctx context.Context, envelope []byte) ([]string, error)
	UnpinFromEnvelopeFunc func(ctx context.Context, envelope []byte) ([]string, error)

	PinnedDigests   []string
	UnpinnedDigests []string
}

// ExtractDigests implements Manager.
func (m *MockManager) ExtractDigests(envelope []byte) ([]string, error) {
	if m.ExtractDigestsFunc != nil {
		return m.ExtractDigestsFunc(envelope)
	}
	// Default behavior: return test digests
	return []string{"sha256:abc123"}, nil
}

// Pin implements Manager.
func (m *MockManager) Pin(ctx context.Context, digests ...string) error {
	m.PinnedDigests = append(m.PinnedDigests, digests...)
	if m.PinFunc != nil {
		return m.PinFunc(ctx, digests...)
	}
	return nil
}

// Unpin implements Manager.
func (m *MockManager) Unpin(ctx context.Context, digests ...string) error {
	m.UnpinnedDigests = append(m.UnpinnedDigests, digests...)
	if m.UnpinFunc != nil {
		return m.UnpinFunc(ctx, digests...)
	}
	return nil
}

// PinFromEnvelope implements Manager.
func (m *MockManager) PinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error) {
	if m.PinFromEnvelopeFunc != nil {
		return m.PinFromEnvelopeFunc(ctx, envelope)
	}
	digests, err := m.ExtractDigests(envelope)
	if err != nil {
		return nil, err
	}
	return digests, m.Pin(ctx, digests...)
}

// UnpinFromEnvelope implements Manager.
func (m *MockManager) UnpinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error) {
	if m.UnpinFromEnvelopeFunc != nil {
		return m.UnpinFromEnvelopeFunc(ctx, envelope)
	}
	digests, err := m.ExtractDigests(envelope)
	if err != nil {
		return nil, err
	}
	return digests, m.Unpin(ctx, digests...)
}
