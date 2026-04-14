package fakes

import (
	"context"
	"sync"

	"github.com/joshka0/foxctl/internal/v2/runtime/runner"
)

// FakeModel is a deterministic model fake for runner tests.
type FakeModel struct {
	mu            sync.Mutex
	responses     []runner.ModelResponse
	errByCall     map[int]error
	inputs        []runner.ModelInput
	defaultResult runner.ModelResponse
	onCall        func(call int, in runner.ModelInput)
}

func NewFakeModel(responses ...runner.ModelResponse) *FakeModel {
	return &FakeModel{
		responses:     append([]runner.ModelResponse(nil), responses...),
		errByCall:     map[int]error{},
		defaultResult: runner.ModelResponse{Done: true, Message: "completed"},
	}
}

// WithDefault sets the default response used after scripted responses are exhausted.
func (m *FakeModel) WithDefault(resp runner.ModelResponse) *FakeModel {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultResult = resp
	return m
}

// SetError configures an error returned on the 1-based call index.
func (m *FakeModel) SetError(call int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errByCall[call] = err
}

// SetOnCall configures a callback for each model call.
func (m *FakeModel) SetOnCall(fn func(call int, in runner.ModelInput)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onCall = fn
}

// Inputs returns a defensive copy of model inputs.
func (m *FakeModel) Inputs() []runner.ModelInput {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]runner.ModelInput, len(m.inputs))
	copy(out, m.inputs)
	return out
}

// Complete returns deterministic scripted responses.
func (m *FakeModel) Complete(ctx context.Context, in runner.ModelInput) (runner.ModelResponse, error) {
	select {
	case <-ctx.Done():
		return runner.ModelResponse{}, ctx.Err()
	default:
	}

	m.mu.Lock()
	m.inputs = append(m.inputs, in)
	call := len(m.inputs)
	err := m.errByCall[call]
	var resp runner.ModelResponse
	if call <= len(m.responses) {
		resp = m.responses[call-1]
	} else {
		resp = m.defaultResult
	}
	onCall := m.onCall
	m.mu.Unlock()

	if onCall != nil {
		onCall(call, in)
	}
	if err != nil {
		return runner.ModelResponse{}, err
	}
	return resp, nil
}

var _ runner.Model = (*FakeModel)(nil)
