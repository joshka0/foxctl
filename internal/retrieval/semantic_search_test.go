package retrieval

import (
	"context"
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/rs/zerolog"
)

type mockEmbedProvider struct {
	embedCalls      int
	embedQueryCalls int
	model           string
}

func (m *mockEmbedProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	m.embedCalls++
	return make([]float32, 4), nil
}

func (m *mockEmbedProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range result {
		m.embedCalls++
		result[i] = make([]float32, 4)
	}
	return result, nil
}

func (m *mockEmbedProvider) Model() string {
	if m.model == "" {
		return "mock"
	}
	return m.model
}

func (m *mockEmbedProvider) Dimensions() int {
	return 4
}

// mockQueryProvider implements QueryEmbeddingProvider.
type mockQueryProvider struct {
	mockEmbedProvider
}

func (m *mockQueryProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	m.embedQueryCalls++
	return make([]float32, 4), nil
}

var _ semantic.QueryEmbeddingProvider = (*mockQueryProvider)(nil)

func TestEmbedForQueryAutoUsesEmbedQuery(t *testing.T) {
	provider := &mockQueryProvider{}
	g := &Generator{
		embedProvider:  provider,
		embedQueryMode: config.EmbedQueryModeAuto,
		logger:         zerolog.Nop(),
	}

	if _, err := g.embedForQuery(context.Background(), "q"); err != nil {
		t.Fatalf("embedForQuery failed: %v", err)
	}
	if provider.embedQueryCalls != 1 {
		t.Fatalf("expected EmbedQuery call, got %d", provider.embedQueryCalls)
	}
	if provider.embedCalls != 0 {
		t.Fatalf("expected Embed not called, got %d", provider.embedCalls)
	}
}

func TestEmbedForQueryForcedEmbed(t *testing.T) {
	provider := &mockQueryProvider{}
	g := &Generator{
		embedProvider:  provider,
		embedQueryMode: config.EmbedQueryModeEmbed,
		logger:         zerolog.Nop(),
	}

	if _, err := g.embedForQuery(context.Background(), "q"); err != nil {
		t.Fatalf("embedForQuery failed: %v", err)
	}
	if provider.embedCalls != 1 {
		t.Fatalf("expected Embed call, got %d", provider.embedCalls)
	}
	if provider.embedQueryCalls != 0 {
		t.Fatalf("expected EmbedQuery not called, got %d", provider.embedQueryCalls)
	}
}

func TestEmbedForQueryForcedEmbedQueryFallback(t *testing.T) {
	provider := &mockEmbedProvider{}
	g := &Generator{
		embedProvider:  provider,
		embedQueryMode: config.EmbedQueryModeEmbedQuery,
		logger:         zerolog.Nop(),
	}

	if _, err := g.embedForQuery(context.Background(), "q"); err != nil {
		t.Fatalf("embedForQuery failed: %v", err)
	}
	if provider.embedCalls != 1 {
		t.Fatalf("expected Embed fallback call, got %d", provider.embedCalls)
	}
}

func TestEmbedForQueryEnvOverride(t *testing.T) {
	t.Setenv(config.EnvEmbedQueryMode, string(config.EmbedQueryModeEmbed))
	provider := &mockQueryProvider{}
	g := &Generator{
		embedProvider:  provider,
		embedQueryMode: config.EmbedQueryModeEmbedQuery,
		logger:         zerolog.Nop(),
	}

	if _, err := g.embedForQuery(context.Background(), "q"); err != nil {
		t.Fatalf("embedForQuery failed: %v", err)
	}
	if provider.embedCalls != 1 {
		t.Fatalf("expected Embed call due to env override, got %d", provider.embedCalls)
	}
	if provider.embedQueryCalls != 0 {
		t.Fatalf("expected EmbedQuery not called due to env override, got %d", provider.embedQueryCalls)
	}
}
