//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
)

// TestGeminiProvider_Integration tests the Gemini embedding provider against the live API.
// Requires GEMINI_API_KEY environment variable to be set.
// Run with: GEMINI_API_KEY=your-key go test -v ./test/integration -run TestGeminiProvider
func TestGeminiProvider_Integration(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider, err := semantic.NewGeminiProvider(semantic.GeminiConfig{
		APIKey: apiKey,
		Model:  "text-embedding-004",
	})
	if err != nil {
		t.Fatalf("NewGeminiProvider failed: %v", err)
	}

	t.Run("single_embed", func(t *testing.T) {
		embedding, err := provider.Embed(ctx, "What is the meaning of life?")
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
		}

		expectedDim := 768
		if len(embedding) != expectedDim {
			t.Errorf("expected embedding dimension %d, got %d", expectedDim, len(embedding))
		}

		// Verify embedding is not all zeros
		var sum float32
		for _, v := range embedding {
			sum += v * v
		}
		if sum == 0 {
			t.Error("embedding is all zeros")
		}

		t.Logf("Embedding dimension: %d, L2 norm^2: %f", len(embedding), sum)
	})

	t.Run("batch_embed", func(t *testing.T) {
		texts := []string{
			"The quick brown fox jumps over the lazy dog.",
			"Hello, world!",
			"Artificial intelligence is transforming technology.",
		}

		embeddings, err := provider.EmbedBatch(ctx, texts)
		if err != nil {
			t.Fatalf("EmbedBatch failed: %v", err)
		}

		if len(embeddings) != len(texts) {
			t.Errorf("expected %d embeddings, got %d", len(texts), len(embeddings))
		}

		expectedDim := 768
		for i, emb := range embeddings {
			if len(emb) != expectedDim {
				t.Errorf("embedding %d: expected dimension %d, got %d", i, expectedDim, len(emb))
			}
		}

		t.Logf("Batch embeddings: %d texts processed", len(embeddings))
	})

	t.Run("similarity", func(t *testing.T) {
		// Test that similar texts have similar embeddings
		emb1, err := provider.Embed(ctx, "The cat sat on the mat.")
		if err != nil {
			t.Fatalf("Embed 1 failed: %v", err)
		}

		emb2, err := provider.Embed(ctx, "A cat was sitting on a mat.")
		if err != nil {
			t.Fatalf("Embed 2 failed: %v", err)
		}

		emb3, err := provider.Embed(ctx, "Quantum physics describes subatomic particles.")
		if err != nil {
			t.Fatalf("Embed 3 failed: %v", err)
		}

		sim12 := cosineSimilarity(emb1, emb2)
		sim13 := cosineSimilarity(emb1, emb3)

		t.Logf("Similarity (cat sentences): %f", sim12)
		t.Logf("Similarity (cat vs physics): %f", sim13)

		// Similar sentences should have higher similarity than unrelated ones
		if sim12 <= sim13 {
			t.Errorf("expected similar sentences to have higher similarity: sim(cat,cat)=%f <= sim(cat,physics)=%f", sim12, sim13)
		}
	})
}

// TestGeminiProvider_Integration_GeminiEmbedding001 tests the gemini-embedding-001 model.
func TestGeminiProvider_Integration_GeminiEmbedding001(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider, err := semantic.NewGeminiProvider(semantic.GeminiConfig{
		APIKey: apiKey,
		Model:  "gemini-embedding-001",
	})
	if err != nil {
		t.Fatalf("NewGeminiProvider failed: %v", err)
	}

	embedding, err := provider.Embed(ctx, "Test embedding with gemini-embedding-001 model.")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	expectedDim := 3072
	if len(embedding) != expectedDim {
		t.Errorf("expected embedding dimension %d, got %d", expectedDim, len(embedding))
	}

	t.Logf("gemini-embedding-001 dimension: %d", len(embedding))
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (sqrt32(normA) * sqrt32(normB))
}

// sqrt32 computes the square root of a float32 using Newton's method.
func sqrt32(x float32) float32 {
	if x <= 0 {
		return 0
	}
	// Newton's method
	z := x / 2
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}
