package daemon

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/XiaoConstantine/dspy-go/pkg/core"
)

// FakeLLM returns scripted responses for deterministic testing.
// Uses atomic operations for CallIndex to avoid races when dspy-go
// reflects over this struct for error formatting.
type FakeLLM struct {
	Responses       []string
	Errors          map[int]error
	callIndex       atomic.Int32
	mu              sync.Mutex
	CapturedPrompts []string // Captures all prompts for verification
}

func (f *FakeLLM) Generate(ctx context.Context, prompt string, opts ...core.GenerateOption) (*core.LLMResponse, error) {
	f.mu.Lock()
	f.CapturedPrompts = append(f.CapturedPrompts, prompt)
	f.mu.Unlock()

	idx := int(f.callIndex.Load())

	if err, ok := f.Errors[idx]; ok {
		f.callIndex.Add(1)
		return nil, err
	}

	if idx >= len(f.Responses) {
		return &core.LLMResponse{Content: "No more scripted responses"}, nil
	}
	response := f.Responses[idx]
	f.callIndex.Add(1)
	return &core.LLMResponse{Content: response}, nil
}

// GetCallIndex returns the current call index in a thread-safe manner.
func (f *FakeLLM) GetCallIndex() int {
	return int(f.callIndex.Load())
}

// NewFakeLLM creates a fake LLM with scripted responses.
func NewFakeLLM(responses ...string) *FakeLLM {
	return &FakeLLM{Responses: responses, Errors: make(map[int]error)}
}

func (f *FakeLLM) SetError(index int, err error) {
	f.Errors[index] = err
}

func (f *FakeLLM) Capabilities() []core.Capability {
	return []core.Capability{}
}

func (f *FakeLLM) CreateEmbedding(ctx context.Context, input string, opts ...core.EmbeddingOption) (*core.EmbeddingResult, error) {
	return &core.EmbeddingResult{}, nil
}

func (f *FakeLLM) CreateEmbeddings(ctx context.Context, inputs []string, opts ...core.EmbeddingOption) (*core.BatchEmbeddingResult, error) {
	return &core.BatchEmbeddingResult{}, nil
}

func (f *FakeLLM) GenerateWithContent(ctx context.Context, content []core.ContentBlock, opts ...core.GenerateOption) (*core.LLMResponse, error) {
	return f.Generate(ctx, "content-placeholder", opts...)
}

func (f *FakeLLM) GenerateWithFunctions(ctx context.Context, prompt string, functions []map[string]any, opts ...core.GenerateOption) (map[string]any, error) {
	resp, err := f.Generate(ctx, prompt, opts...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": resp.Content}, nil
}

func (f *FakeLLM) GenerateWithJSON(ctx context.Context, prompt string, opts ...core.GenerateOption) (map[string]any, error) {
	resp, err := f.Generate(ctx, prompt, opts...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": resp.Content}, nil
}

func (f *FakeLLM) ModelID() string {
	return "fake-model"
}

func (f *FakeLLM) ProviderName() string {
	return "fake-provider"
}

func (f *FakeLLM) StreamGenerate(ctx context.Context, prompt string, opts ...core.GenerateOption) (*core.StreamResponse, error) {
	return &core.StreamResponse{}, nil
}

func (f *FakeLLM) StreamGenerateWithContent(ctx context.Context, content []core.ContentBlock, opts ...core.GenerateOption) (*core.StreamResponse, error) {
	return &core.StreamResponse{}, nil
}
