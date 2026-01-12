# Advanced Search Guide: BM25, Vector, and Hybrid Search

This guide covers the advanced search capabilities in agentctl, including BM25 lexical search, vector similarity search, and hybrid search that combines both.

## Overview

Agentctl now supports three powerful search modes:

1. **BM25 (Lexical Search)** - Fast keyword-based search using the industry-standard BM25 algorithm
2. **Vector Search (Semantic Search)** - AI-powered similarity search using embeddings
3. **Hybrid Search** - Best of both worlds, combining lexical and semantic search

## Search Modes Comparison

| Feature | BM25 | Vector | Hybrid |
|---------|------|--------|--------|
| **Type** | Lexical/Keyword | Semantic/Meaning | Combined |
| **Speed** | Fast | Fast (with index) | Moderate |
| **Accuracy** | Exact matches | Conceptual matches | Best overall |
| **Use Case** | Known keywords | Conceptual search | General purpose |
| **Requirements** | Text only | Embeddings | Both |

### When to Use Each Mode

**BM25 (Lexical):**
- Searching for specific terms or phrases
- Technical documentation or code search
- Known keyword queries
- Fast exact-match search

**Vector (Semantic):**
- Conceptual or meaning-based search
- Finding similar items
- When query terms differ from document terms
- Cross-lingual search

**Hybrid:**
- General-purpose search (recommended)
- When you want both exact and conceptual matches
- Production search systems
- Best relevance across query types

## Quick Start

### 1. Enable Search on Memory Store

```go
package main

import (
    "context"
    "github.com/jkatigb/agentctl/internal/storage/memory"
    "github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

func main() {
    ctx := context.Background()

    // Open memory store
    store, err := memory.Open(ctx, "~/.agentctl", "~/.agentctl/cas")
    if err != nil {
        panic(err)
    }

    // Open database with Turso (for vector search) or SQLite
    cfg := dbdriver.DefaultTursoConfig(
        "libsql://your-db.turso.io",
        "your_token",
        "memory",
    )
    cfg.Turso.EnableVectorSearch = true

    db, err := dbdriver.OpenDB(ctx, cfg, nil)
    if err != nil {
        panic(err)
    }

    // Enable search capabilities
    searchStore, err := store.EnableSearch(db, "default-workspace")
    if err != nil {
        panic(err)
    }

    // Now you can use searchStore for advanced queries
}
```

### 2. BM25 Search Example

```go
// Search using BM25 (keyword-based)
results, err := searchStore.SearchBM25(
    ctx,
    "machine learning algorithms", // query
    "default-workspace",            // workspace
    10,                             // limit
)

if err != nil {
    panic(err)
}

for i, result := range results {
    fmt.Printf("%d. %s (score: %.3f)\n",
        result.Rank,
        result.Entry.Name,
        result.Score)
}
```

### 3. Vector Search Example

```go
// Assume you have an embedding model that converts text to vectors
queryEmbedding := getEmbedding("show me examples of neural networks")

// Search using vector similarity
results, err := searchStore.SearchVector(
    ctx,
    queryEmbedding,      // query vector
    "default-workspace", // workspace
    10,                  // limit
)

if err != nil {
    panic(err)
}

for _, result := range results {
    fmt.Printf("- %s (similarity: %.3f)\n",
        result.Entry.Name,
        result.VectorScore)
}
```

### 4. Hybrid Search Example

```go
// Combine BM25 and vector search for best results
queryText := "deep learning optimization techniques"
queryEmbedding := getEmbedding(queryText)

results, err := searchStore.SearchHybrid(
    ctx,
    queryText,           // text query
    queryEmbedding,      // vector query
    "default-workspace", // workspace
    10,                  // limit
)

if err != nil {
    panic(err)
}

for _, result := range results {
    fmt.Printf("- %s\n", result.Entry.Name)
    fmt.Printf("  Overall: %.3f | BM25: %.3f | Vector: %.3f\n",
        result.Score,
        result.BM25Score,
        result.VectorScore)
}
```

## Advanced Usage

### Custom BM25 Parameters

BM25 has two key parameters:
- **k1**: Term frequency saturation (typical: 1.2-2.0)
- **b**: Length normalization (typical: 0.75)

```go
// Create custom hybrid search options
options := dbdriver.HybridSearchOptions{
    Alpha: 0.6, // 60% BM25, 40% vector
    BM25Params: dbdriver.BM25Params{
        K1: 1.8,  // Higher = more weight on term frequency
        B:  0.8,  // Higher = more length normalization
    },
    Limit: 20,
}

// Use with search
hybridSearcher, err := dbdriver.NewHybridSearcher(db, corpusStats, options)
```

### Adjusting Hybrid Search Weights

The `alpha` parameter controls the balance between BM25 and vector search:

```go
options := dbdriver.HybridSearchOptions{
    Alpha: 0.7, // 70% BM25, 30% vector (more lexical)
}

options := dbdriver.HybridSearchOptions{
    Alpha: 0.3, // 30% BM25, 70% vector (more semantic)
}

options := dbdriver.HybridSearchOptions{
    Alpha: 0.5, // 50/50 balanced (default)
}
```

**Tuning Guidelines:**
- **High alpha (0.7-0.9)**: Use when exact keyword matching is important
- **Low alpha (0.1-0.3)**: Use when conceptual similarity matters most
- **Balanced (0.4-0.6)**: Good starting point for most use cases

### Refreshing Corpus Statistics

After bulk updates, refresh corpus statistics for accurate BM25 scores:

```go
err := searchStore.RefreshCorpusStats(ctx, "default-workspace")
if err != nil {
    panic(err)
}
```

## Understanding BM25

### What is BM25?

BM25 (Best Matching 25) is a ranking function used in information retrieval. It scores documents based on query term frequency, with diminishing returns for repeated terms.

### BM25 Formula

For each query term *t*:

```
IDF(t) = ln((N - n_t + 0.5) / (n_t + 0.5) + 1)

BM25(q, d) = Σ IDF(t) × (f(t,d) × (k1 + 1)) / (f(t,d) + k1 × (1 - b + b × |d|/avgdl))
```

Where:
- `N` = total documents
- `n_t` = documents containing term t
- `f(t,d)` = frequency of term t in document d
- `|d|` = document length
- `avgdl` = average document length
- `k1`, `b` = tuning parameters

### BM25 Parameters Explained

**k1 (Term Frequency Saturation)**
- Controls how much additional occurrences of a term increase the score
- Lower values (1.0-1.5): Quick saturation, less emphasis on term frequency
- Higher values (1.5-2.0): Slower saturation, more emphasis on term frequency
- Default: 1.5

**b (Length Normalization)**
- Controls how much document length affects scoring
- b = 0: No length normalization
- b = 1: Full length normalization
- b = 0.75: Balanced (default)

## Understanding Vector Search

### What is Vector Search?

Vector search finds documents based on semantic similarity. Documents and queries are converted to high-dimensional vectors (embeddings), and similarity is measured using distance metrics.

### Cosine Similarity

The most common metric for vector similarity:

```
cosine(v_q, v_d) = (v_q · v_d) / (||v_q|| × ||v_d||)
```

Result ranges from -1 (opposite) to +1 (identical).

### Vector Dimensions

Common embedding dimensions:
- **384**: all-MiniLM-L6-v2 (fast, good quality)
- **768**: BERT base, all-mpnet-base-v2 (higher quality)
- **1536**: OpenAI ada-002 (very high quality)

Configure in Turso setup:
```bash
export AGENTCTL_MEMORY_VECTOR_DIMS=384
```

## Understanding Hybrid Search

### How Hybrid Search Works

1. **Vector Retrieval**: Get top candidates using fast vector search
2. **BM25 Scoring**: Score candidates with BM25 for lexical relevance
3. **Score Normalization**: Scale both scores to [0, 1]
4. **Score Fusion**: Combine scores with weighted average
5. **Re-ranking**: Sort by final score

### Score Fusion

```
final_score = α × BM25_scaled + (1 - α) × vector_scaled
```

### Advantages

- **Robustness**: Works well across query types
- **Complementary**: BM25 catches exact matches, vectors catch conceptual matches
- **Tunable**: Adjust α to favor lexical or semantic

## Performance Optimization

### Vector Search Performance

**With Index (DiskANN):**
- Fast approximate nearest neighbor search
- Suitable for 10,000+ vectors
- Create with: `CREATE INDEX idx ON table(libsql_vector_idx(embedding))`

**Without Index:**
- Full table scan with exact distance
- Acceptable up to ~10,000 vectors
- Perfect recall but slower

### BM25 Performance

**Optimization Tips:**
1. Limit corpus to relevant workspace
2. Use pre-computed term frequencies
3. Cache corpus statistics
4. Use appropriate batch sizes

### Hybrid Search Performance

- Moderate overhead (vector search + BM25 scoring)
- Most time spent in vector retrieval phase
- Scale by limiting candidate pool size

## Best Practices

### 1. Choose the Right Mode

```go
// Known technical terms -> BM25
searchStore.SearchBM25(ctx, "kubernetes deployment yaml", workspace, 10)

// Conceptual queries -> Vector
searchStore.SearchVector(ctx, embedding, workspace, 10)

// General purpose -> Hybrid
searchStore.SearchHybrid(ctx, query, embedding, workspace, 10)
```

### 2. Tune for Your Use Case

```go
// Technical documentation (favor exact matches)
options.Alpha = 0.7

// Research papers (favor concepts)
options.Alpha = 0.3

// General search
options.Alpha = 0.5
```

### 3. Refresh Statistics Regularly

```go
// After bulk imports
if numImported > 100 {
    searchStore.RefreshCorpusStats(ctx, workspace)
}
```

### 4. Handle Empty Results

```go
results, err := searchStore.SearchHybrid(ctx, query, embedding, workspace, 10)
if err != nil {
    return err
}

if len(results) == 0 {
    // Try BM25 only as fallback
    results, err = searchStore.SearchBM25(ctx, query, workspace, 10)
}
```

### 5. Monitor Performance

```go
import "time"

start := time.Now()
results, err := searchStore.SearchHybrid(ctx, query, embedding, workspace, 10)
duration := time.Since(start)

log.Printf("Search took %v, returned %d results", duration, len(results))
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "github.com/jkatigb/agentctl/internal/storage/memory"
    "github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

func main() {
    ctx := context.Background()

    // 1. Setup
    store, _ := memory.Open(ctx, "~/.agentctl", "~/.agentctl/cas")

    cfg := dbdriver.DefaultTursoConfig(
        "libsql://your-db.turso.io",
        "your_token",
        "memory",
    )
    cfg.Turso.EnableVectorSearch = true

    db, _ := dbdriver.OpenDB(ctx, cfg, nil)
    searchStore, _ := store.EnableSearch(db, "my-workspace")

    // 2. Store some memories with embeddings
    entries := []struct {
        name    string
        summary string
        embedding dbdriver.Vector
    }{
        {
            name:    "neural-networks-basics",
            summary: "Introduction to artificial neural networks and deep learning",
            embedding: getEmbedding("neural networks deep learning"),
        },
        {
            name:    "ml-optimization",
            summary: "Gradient descent and optimization techniques for machine learning",
            embedding: getEmbedding("optimization gradient descent machine learning"),
        },
        // ... more entries
    }

    for _, e := range entries {
        vectorStore, _ := store.EnableVectorSearch(db)
        entry := memory.VectorEntry{
            NamedEntry: memory.NamedEntry{
                Name:      e.name,
                Summary:   e.summary,
                Workspace: "my-workspace",
            },
            Embedding: e.embedding,
        }
        vectorStore.SaveWithEmbedding(ctx, entry)
    }

    // 3. Search

    // BM25 search for specific keywords
    fmt.Println("BM25 Search Results:")
    query := "gradient descent optimization"
    bm25Results, _ := searchStore.SearchBM25(ctx, query, "my-workspace", 5)
    for _, r := range bm25Results {
        fmt.Printf("  %d. %s (%.3f)\n", r.Rank, r.Entry.Name, r.Score)
    }

    // Vector search for semantic similarity
    fmt.Println("\nVector Search Results:")
    queryEmbedding := getEmbedding("how to train neural networks")
    vectorResults, _ := searchStore.SearchVector(ctx, queryEmbedding, "my-workspace", 5)
    for i, r := range vectorResults {
        fmt.Printf("  %d. %s (%.3f)\n", i+1, r.Entry.Name, r.VectorScore)
    }

    // Hybrid search for best results
    fmt.Println("\nHybrid Search Results:")
    hybridResults, _ := searchStore.SearchHybrid(
        ctx,
        query,
        queryEmbedding,
        "my-workspace",
        5,
    )
    for _, r := range hybridResults {
        fmt.Printf("  %d. %s\n", r.Rank, r.Entry.Name)
        fmt.Printf("     Total: %.3f | BM25: %.3f | Vector: %.3f\n",
            r.Score, r.BM25Score, r.VectorScore)
    }
}

// Mock embedding function - replace with your actual embedding model
func getEmbedding(text string) dbdriver.Vector {
    // In production, use a real embedding model like:
    // - sentence-transformers
    // - OpenAI embeddings API
    // - Local BERT model
    embedding := make(dbdriver.Vector, 384)
    // ... generate real embeddings
    return embedding
}
```

## Embedding Models

To use vector search, you need an embedding model. Popular options:

### 1. Sentence Transformers (Python)

```python
from sentence_transformers import SentenceTransformer

model = SentenceTransformer('all-MiniLM-L6-v2')
embedding = model.encode("your text here")
```

### 2. OpenAI API

```go
import "github.com/openai/openai-go"

embedding, err := client.Embeddings.Create(ctx, &openai.EmbeddingRequest{
    Model: "text-embedding-ada-002",
    Input: "your text here",
})
```

### 3. Local Models (Ollama)

```bash
# Run locally with Ollama
ollama pull all-minilm
ollama embed all-minilm "your text here"
```

## Troubleshooting

### "Vector search is not enabled"

Ensure Turso is configured with vector support:
```bash
turso group update your-group
export AGENTCTL_MEMORY_VECTOR_SEARCH=true
```

### Poor BM25 Results

1. Check corpus statistics: `RefreshCorpusStats()`
2. Tune k1 and b parameters
3. Verify query tokenization
4. Ensure sufficient document diversity

### Poor Vector Results

1. Verify embedding quality
2. Check vector dimensions match
3. Ensure vectors are normalized
4. Use appropriate similarity metric

### Hybrid Search Not Working

1. Verify both BM25 and vector work individually
2. Check alpha parameter (0-1)
3. Ensure corpus stats are current
4. Monitor score distributions

## API Reference

### SearchableStore Methods

```go
// Enable search on a store
func (s *Store) EnableSearch(db dbdriver.DB, workspace string) (*SearchableStore, error)

// Generic search
func (ss *SearchableStore) Search(
    ctx context.Context,
    query string,
    queryVector dbdriver.Vector,
    workspace string,
    mode dbdriver.SearchMode,
    limit int,
) ([]MemorySearchResult, error)

// Convenience methods
func (ss *SearchableStore) SearchBM25(ctx context.Context, query, workspace string, limit int) ([]MemorySearchResult, error)
func (ss *SearchableStore) SearchVector(ctx context.Context, queryVector dbdriver.Vector, workspace string, limit int) ([]MemorySearchResult, error)
func (ss *SearchableStore) SearchHybrid(ctx context.Context, query string, queryVector dbdriver.Vector, workspace string, limit int) ([]MemorySearchResult, error)

// Maintenance
func (ss *SearchableStore) RefreshCorpusStats(ctx context.Context, workspace string) error
```

### Configuration Types

```go
type SearchMode string
const (
    SearchModeBM25    SearchMode = "bm25"
    SearchModeVector  SearchMode = "vector"
    SearchModeHybrid  SearchMode = "hybrid"
)

type BM25Params struct {
    K1 float64 // Term frequency saturation
    B  float64 // Length normalization
}

type HybridSearchOptions struct {
    Alpha      float64    // BM25 weight (0-1)
    BM25Params BM25Params
    Limit      int
}
```

## Resources

- [BM25 Algorithm](https://en.wikipedia.org/wiki/Okapi_BM25)
- [Vector Search Overview](https://www.pinecone.io/learn/vector-search/)
- [Hybrid Search Best Practices](https://www.elastic.co/guide/en/elasticsearch/reference/current/hybrid-search.html)
- [Turso Vector Search](https://docs.turso.tech/features/vector-search)

## Next Steps

1. Experiment with different search modes
2. Tune parameters for your use case
3. Monitor search performance
4. Implement result caching if needed
5. Consider query expansion techniques

For more information, see:
- `docs/turso-migration.md` - Database setup
- `internal/storage/dbdriver/README.md` - Driver documentation
