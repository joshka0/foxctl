package sourceimport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
)

// VoyageEmbedder adapts semantic.VoyageProvider to the sourceimport Embedder interface.
type VoyageEmbedder struct {
	provider *semantic.VoyageProvider
}

// NewVoyageEmbedder creates a Voyage-backed embedder.
func NewVoyageEmbedder(baseURL, model, apiKey string, timeout time.Duration) (*VoyageEmbedder, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = "voyage-4"
	}
	cfg := semantic.VoyageConfig{
		APIKey:  strings.TrimSpace(apiKey),
		Model:   model,
		BaseURL: strings.TrimSpace(baseURL),
		Timeout: timeout,
	}
	provider, err := semantic.NewVoyageProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("voyage embedder: %w", err)
	}
	return &VoyageEmbedder{provider: provider}, nil
}

// Embed generates one embedding vector from text.
func (e *VoyageEmbedder) Embed(ctx context.Context, text string) (EmbeddingResult, error) {
	if e == nil || e.provider == nil {
		return EmbeddingResult{}, fmt.Errorf("voyage embedder: nil receiver")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return EmbeddingResult{}, nil
	}

	vec, err := e.provider.Embed(ctx, text)
	if err != nil {
		return EmbeddingResult{}, err
	}
	return EmbeddingResult{
		Vector: append([]float32(nil), vec...),
		Model:  e.provider.Model(),
	}, nil
}

// Dimensions returns the fixed Voyage embedding dimensions for the configured model.
func (e *VoyageEmbedder) Dimensions() int {
	if e == nil || e.provider == nil {
		return 0
	}
	return e.provider.Dimensions()
}
