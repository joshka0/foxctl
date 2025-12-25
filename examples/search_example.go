// Package main demonstrates search functionality examples.
package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// This example demonstrates BM25, vector, and hybrid search capabilities

func main() {
	ctx := context.Background()

	// Setup: Open memory store and enable search
	fmt.Println("=== Setting up search demo ===")

	store, db := setupStore(ctx)
	searchStore, err := store.EnableSearch(db, "demo-workspace")
	if err != nil {
		panic(err)
	}

	// Populate with sample data
	fmt.Println("Populating with sample memories...")
	populateSampleData(ctx, store, db)
	fmt.Println("Done!")

	// Demo 1: BM25 Lexical Search
	fmt.Println("=== Demo 1: BM25 Lexical Search ===")
	demoBM25Search(ctx, searchStore)

	// Demo 2: Vector Semantic Search
	if db.IsVectorSearchEnabled() {
		fmt.Println("\n=== Demo 2: Vector Semantic Search ===")
		demoVectorSearch(ctx, searchStore)

		// Demo 3: Hybrid Search
		fmt.Println("\n=== Demo 3: Hybrid Search ===")
		demoHybridSearch(ctx, searchStore)
	} else {
		fmt.Println("\nVector search not enabled. Skipping vector and hybrid demos.")
		fmt.Println("To enable: export AGENTCTL_MEMORY_DB_DRIVER=turso")
		fmt.Println("           export AGENTCTL_MEMORY_VECTOR_SEARCH=true")
	}

	// Demo 4: Parameter Tuning
	fmt.Println("\n=== Demo 4: Parameter Tuning ===")
	demoParameterTuning(ctx, searchStore)
}

func setupStore(ctx context.Context) (*memory.Store, dbdriver.DB) {
	// Try Turso first, fall back to SQLite
	loader := dbdriver.NewConfigLoader("~/.agentctl")
	cfg := loader.LoadMemoryConfig()

	db, err := dbdriver.OpenDB(ctx, cfg, nil)
	if err != nil {
		panic(err)
	}

	store, err := memory.Open(ctx, "~/.agentctl", "~/.agentctl/cas")
	if err != nil {
		panic(err)
	}

	return store, db
}

func populateSampleData(ctx context.Context, store *memory.Store, db dbdriver.DB) {
	samples := []struct {
		name    string
		summary string
		tags    []string
	}{
		{
			name:    "neural-networks-intro",
			summary: "Introduction to artificial neural networks and deep learning fundamentals",
			tags:    []string{"ml", "neural-networks", "deep-learning"},
		},
		{
			name:    "gradient-descent",
			summary: "Gradient descent optimization algorithm for training machine learning models",
			tags:    []string{"ml", "optimization", "algorithms"},
		},
		{
			name:    "transformer-architecture",
			summary: "Transformer model architecture using self-attention mechanisms",
			tags:    []string{"ml", "nlp", "transformers"},
		},
		{
			name:    "convolutional-networks",
			summary: "Convolutional neural networks for computer vision and image processing",
			tags:    []string{"ml", "cnn", "computer-vision"},
		},
		{
			name:    "reinforcement-learning",
			summary: "Q-learning and policy gradient methods for reinforcement learning",
			tags:    []string{"ml", "rl", "agents"},
		},
		{
			name:    "natural-language-processing",
			summary: "Techniques for processing and understanding human language with AI",
			tags:    []string{"nlp", "language", "text-processing"},
		},
		{
			name:    "hyperparameter-tuning",
			summary: "Methods for optimizing hyperparameters in machine learning models",
			tags:    []string{"ml", "optimization", "tuning"},
		},
		{
			name:    "data-preprocessing",
			summary: "Data cleaning, normalization, and feature engineering techniques",
			tags:    []string{"data", "preprocessing", "features"},
		},
	}

	var vectorStore *memory.VectorStore
	if db.IsVectorSearchEnabled() {
		var err error
		vectorStore, err = store.EnableVectorSearch(db)
		if err != nil {
			panic(fmt.Errorf("enable vector search: %w", err))
		}
	}

	for _, sample := range samples {
		entry := memory.NamedEntry{
			Name:      sample.name,
			Summary:   sample.summary,
			Workspace: "demo-workspace",
			Type:      "example",
		}

		// Save with vector if supported
		if vectorStore != nil {
			vectorEntry := memory.VectorEntry{
				NamedEntry: entry,
				Embedding:  generateMockEmbedding(sample.summary, 384),
			}
			if _, err := vectorStore.SaveWithEmbedding(ctx, vectorEntry); err != nil {
				panic(fmt.Errorf("save with embedding: %w", err))
			}
		} else {
			if _, err := store.Save(ctx, entry); err != nil {
				panic(fmt.Errorf("save entry: %w", err))
			}
		}
	}
}

func demoBM25Search(ctx context.Context, searchStore *memory.SearchableStore) {
	queries := []string{
		"neural networks deep learning",
		"optimization gradient descent",
		"natural language processing",
	}

	for _, query := range queries {
		fmt.Printf("\nQuery: \"%s\"\n", query)
		results, err := searchStore.SearchBM25(ctx, query, "demo-workspace", 3)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		if len(results) == 0 {
			fmt.Println("No results found")
			continue
		}

		for _, result := range results {
			fmt.Printf("  %d. %s (score: %.3f)\n",
				result.Rank,
				result.Entry.Name,
				result.Score)
			fmt.Printf("     %s\n", result.Entry.Summary)
		}
	}
}

func demoVectorSearch(ctx context.Context, searchStore *memory.SearchableStore) {
	queries := []struct {
		text string
		desc string
	}{
		{
			text: "how to train neural networks",
			desc: "Training neural networks",
		},
		{
			text: "understanding human language with computers",
			desc: "NLP concepts",
		},
		{
			text: "image recognition and visual processing",
			desc: "Computer vision",
		},
	}

	for _, query := range queries {
		fmt.Printf("\nQuery: \"%s\" (%s)\n", query.text, query.desc)

		// Generate query embedding
		queryEmbedding := generateMockEmbedding(query.text, 384)

		results, err := searchStore.SearchVector(ctx, queryEmbedding, "demo-workspace", 3)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		if len(results) == 0 {
			fmt.Println("No results found")
			continue
		}

		for i, result := range results {
			fmt.Printf("  %d. %s (similarity: %.3f)\n",
				i+1,
				result.Entry.Name,
				result.VectorScore)
			fmt.Printf("     %s\n", result.Entry.Summary)
		}
	}
}

func demoHybridSearch(ctx context.Context, searchStore *memory.SearchableStore) {
	queries := []struct {
		text string
		desc string
	}{
		{
			text: "neural network optimization",
			desc: "Combining keywords and concepts",
		},
		{
			text: "transformers for language understanding",
			desc: "NLP architecture",
		},
	}

	for _, query := range queries {
		fmt.Printf("\nQuery: \"%s\" (%s)\n", query.text, query.desc)

		// Generate query embedding
		queryEmbedding := generateMockEmbedding(query.text, 384)

		results, err := searchStore.SearchHybrid(
			ctx,
			query.text,
			queryEmbedding,
			"demo-workspace",
			3,
		)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		if len(results) == 0 {
			fmt.Println("No results found")
			continue
		}

		for _, result := range results {
			fmt.Printf("  %d. %s\n", result.Rank, result.Entry.Name)
			fmt.Printf("     Combined: %.3f | BM25: %.3f | Vector: %.3f\n",
				result.Score,
				result.BM25Score,
				result.VectorScore)
			fmt.Printf("     %s\n", result.Entry.Summary)
		}
	}
}

func demoParameterTuning(ctx context.Context, searchStore *memory.SearchableStore) {
	fmt.Println("\nShowing effect of different alpha values on hybrid search:")
	fmt.Println("(alpha = BM25 weight, 1-alpha = vector weight)")

	query := "optimization gradient descent"
	queryEmbedding := generateMockEmbedding(query, 384)

	alphas := []float64{0.2, 0.5, 0.8}

	for _, alpha := range alphas {
		fmt.Printf("Alpha = %.1f (%.0f%% BM25, %.0f%% vector)\n",
			alpha, alpha*100, (1-alpha)*100)

		// Note: In a real implementation, you would recreate the hybrid searcher
		// with different options. This is simplified for demonstration.
		results, err := searchStore.SearchHybrid(
			ctx,
			query,
			queryEmbedding,
			"demo-workspace",
			3,
		)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		for _, result := range results {
			fmt.Printf("  %d. %s (%.3f)\n",
				result.Rank,
				result.Entry.Name,
				result.Score)
		}
		fmt.Println()
	}

	fmt.Println("Tips:")
	fmt.Println("- Higher alpha (0.7-0.9): Favor exact keyword matches")
	fmt.Println("- Lower alpha (0.1-0.3): Favor conceptual similarity")
	fmt.Println("- Balanced (0.4-0.6): Good for general purpose search")
}

// generateMockEmbedding creates a simple mock embedding for demonstration
// In production, use a real embedding model like:
// - sentence-transformers (Python)
// - OpenAI embeddings API
// - Local BERT models
func generateMockEmbedding(text string, dimensions int) dbdriver.Vector {
	// Simple deterministic mock: hash text to seed random generator
	seed := int64(0)
	for _, char := range text {
		seed += int64(char)
	}

	rng := rand.New(rand.NewSource(seed))
	embedding := make(dbdriver.Vector, dimensions)

	// Generate random vector
	for i := range embedding {
		embedding[i] = float32(rng.NormFloat64())
	}

	// L2 normalize
	var sumSquares float64
	for _, val := range embedding {
		sumSquares += float64(val * val)
	}
	if sumSquares > 0 {
		scale := float32(1.0 / math.Sqrt(sumSquares))
		for i := range embedding {
			embedding[i] *= scale
		}
	}

	return embedding
}

// Real embedding example using sentence-transformers (conceptual)
/*
func getRealEmbedding(text string) (dbdriver.Vector, error) {
    // Option 1: Call Python service
    resp, err := http.Post("http://localhost:8000/embed",
        "application/json",
        strings.NewReader(fmt.Sprintf(`{"text": "%s"}`, text)))
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result struct {
        Embedding []float32 `json:"embedding"`
    }
    json.NewDecoder(resp.Body).Decode(&result)
    return dbdriver.Vector(result.Embedding), nil

    // Option 2: Use OpenAI API
    embedding, err := client.Embeddings.Create(ctx, &openai.EmbeddingRequest{
        Model: "text-embedding-ada-002",
        Input: text,
    })
    return dbdriver.Vector(embedding.Data[0].Embedding), nil

    // Option 3: Use local ONNX model
    // See: github.com/onnx/onnx-go
}
*/
