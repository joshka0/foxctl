package semantic

import (
	"context"
)

// EmbeddingProvider generates embeddings for text content.
// Implementations may call external APIs, use WASI skills, or local models.
type EmbeddingProvider interface {
	// Embed generates an embedding vector for the given text.
	// Returns the embedding as a float32 slice.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts.
	// Returns embeddings in the same order as inputs.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Model returns the model identifier used by this provider.
	Model() string

	// Dimensions returns the embedding vector dimension.
	Dimensions() int
}

// NoOpProvider is a stub provider that returns empty embeddings.
// Used when vector support is not enabled or for testing.
type NoOpProvider struct {
	model      string
	dimensions int
}

// NewNoOpProvider creates a no-op embedding provider.
func NewNoOpProvider(model string, dimensions int) *NoOpProvider {
	if model == "" {
		model = "noop"
	}
	if dimensions <= 0 {
		dimensions = 384 // Common default (e.g., sentence-transformers)
	}
	return &NoOpProvider{model: model, dimensions: dimensions}
}

// Embed returns a zero vector of the configured dimension.
func (p *NoOpProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, p.dimensions), nil
}

// EmbedBatch returns zero vectors for all inputs.
func (p *NoOpProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range result {
		result[i] = make([]float32, p.dimensions)
	}
	return result, nil
}

// Model returns the model identifier.
func (p *NoOpProvider) Model() string {
	return p.model
}

// Dimensions returns the embedding dimension.
func (p *NoOpProvider) Dimensions() int {
	return p.dimensions
}
