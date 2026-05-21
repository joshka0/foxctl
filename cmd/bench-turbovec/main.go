package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/turbovec"
)

const (
	benchDim      = 4096 // Qwen3-Embedding-8B production dims
	benchNVecs    = 1000
	benchK        = 20
	benchBitWidth = 4
)

func main() {
	fmt.Printf("turbovec vs brute-force benchmark\n")
	fmt.Printf("  dim=%d  n_vectors=%d  k=%d  bit_width=%d\n\n", benchDim, benchNVecs, benchK, benchBitWidth)

	// Generate test data.
	fmt.Print("Generating vectors... ")
	vectors := make([][]float32, benchNVecs)
	for i := range vectors {
		vectors[i] = randomUnitVector(benchDim)
	}
	query := randomUnitVector(benchDim)
	fmt.Printf("done (%d vectors of dim %d)\n\n", benchNVecs, benchDim)

	// --- Brute-force (in-process cosine similarity) ---
	fmt.Println("=== Brute-force cosine similarity ===")
	start := time.Now()

	type scored struct {
		idx   int
		score float64
	}
	hits := make([]scored, benchNVecs)
	for i, vec := range vectors {
		hits[i] = scored{idx: i, score: cosineSim(query, vec)}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > benchK {
		hits = hits[:benchK]
	}

	bruteTime := time.Since(start)
	fmt.Printf("  Time: %v\n", bruteTime)
	fmt.Printf("  Top-5: ")
	for i := 0; i < 5 && i < len(hits); i++ {
		fmt.Printf("[%d:%.4f] ", hits[i].idx, hits[i].score)
	}
	fmt.Println()

	// --- Brute-force with JSON serialization (simulates SQLite path) ---
	fmt.Println("\n=== Brute-force + JSON deserialization (simulates SQLite) ===")
	start = time.Now()

	for i, vec := range vectors {
		// Serialize to JSON (like SQLite stores it).
		b, _ := json.Marshal(vec)
		// Deserialize back.
		var deserialized []float32
		_ = json.Unmarshal(b, &deserialized)
		_ = cosineSim(query, deserialized)
		_ = b
		_ = i
	}

	jsonTime := time.Since(start)
	fmt.Printf("  Time: %v (%.1fx slower than raw)\n", jsonTime, float64(jsonTime)/float64(bruteTime))

	// --- Turbovec sidecar ---
	fmt.Println("\n=== Turbovec sidecar ===")
	socketPath := turbovec.DefaultSocketPath()

	client, err := turbovec.Dial(socketPath)
	if err != nil {
		fmt.Printf("  SKIP: turbovecd not available at %s: %v\n", socketPath, err)
		printSummary(bruteTime, jsonTime, 0, 0, 0, 0)
		return
	}
	defer client.Close()

	if err := client.Ping(); err != nil {
		fmt.Printf("  SKIP: ping failed: %v\n", err)
		printSummary(bruteTime, jsonTime, 0, 0, 0, 0)
		return
	}

	// Create index.
	indexName := fmt.Sprintf("bench-%d", time.Now().UnixMilli())
	if err := client.Create(indexName, benchDim, benchBitWidth); err != nil {
		fmt.Printf("  FAIL: create: %v\n", err)
		return
	}
	defer client.Drop(indexName)

	// Add vectors in batches, fall back to single adds.
	fmt.Print("  Adding vectors... ")
	addStart := time.Now()
	batchSize := 500
	added := 0
	for batch := 0; batch < benchNVecs; batch += batchSize {
		end := batch + batchSize
		if end > benchNVecs {
			end = benchNVecs
		}

		flat := make([]float32, 0, (end-batch)*benchDim)
		ids := make([]uint64, 0, end-batch)
		for i := batch; i < end; i++ {
			flat = append(flat, vectors[i]...)
			ids = append(ids, uint64(i))
		}
		if _, err := client.AddBatch(indexName, flat, benchDim, ids); err != nil {
			// Fallback: add one at a time.
			for i := batch; i < end; i++ {
				if _, err := client.Add(indexName, uint64(i), vectors[i]); err != nil {
					fmt.Printf("FAIL: add %d: %v\n", i, err)
					return
				}
			}
		}
		added = end
	}
	addTime := time.Since(addStart)
	fmt.Printf("done in %v (%.0f vecs/sec)\n", addTime, float64(added)/addTime.Seconds())

	// Prepare caches.
	_ = client.Prepare(indexName)

	// Search benchmark: warm up + measure.
	fmt.Print("  Searching... ")
	// Warm up.
	_, _ = client.Search(indexName, query, benchK)

	runs := 10
	searchStart := time.Now()
	var tvHits []turbovec.SearchHit
	for i := 0; i < runs; i++ {
		tvHits, err = client.Search(indexName, query, benchK)
		if err != nil {
			fmt.Printf("FAIL: search: %v\n", err)
			return
		}
	}
	searchTime := time.Since(searchStart) / time.Duration(runs)

	fmt.Printf("done (avg of %d runs)\n", runs)
	fmt.Printf("  Time: %v\n", searchTime)
	fmt.Printf("  Top-5: ")
	for i := 0; i < 5 && i < len(tvHits); i++ {
		fmt.Printf("[%d:%.4f] ", tvHits[i].ID, tvHits[i].Score)
	}
	fmt.Println()

	// Recall comparison for raw turbovec.
	bruteSet := make(map[int]bool)
	for _, h := range hits[:benchK] {
		bruteSet[h.idx] = true
	}
	tvSet := make(map[int]bool)
	for _, h := range tvHits {
		tvSet[int(h.ID)] = true
	}
	overlap := 0
	for id := range bruteSet {
		if tvSet[id] {
			overlap++
		}
	}
	rawRecall := float64(overlap) / float64(benchK)
	fmt.Printf("  Recall@%d: %.2f (%d/%d overlap with brute-force)\n", benchK, rawRecall, overlap, benchK)

	// --- Oversample + exact rerank ---
	oversampleFactor := 3
	oversampleK := uint32(benchK * oversampleFactor)
	fmt.Printf("\n=== Oversample + Exact Rerank (3x oversample, k=%d → rerank to k=%d) ===\n", oversampleK, benchK)

	fmt.Print("  Searching with oversampling... ")
	_, _ = client.Search(indexName, query, oversampleK) // warm up
	searchStartOS := time.Now()
	var osHits []turbovec.SearchHit
	for i := 0; i < runs; i++ {
		osHits, err = client.Search(indexName, query, oversampleK)
		if err != nil {
			fmt.Printf("FAIL: oversampled search: %v\n", err)
			return
		}
	}
	osSearchTime := time.Since(searchStartOS) / time.Duration(runs)
	fmt.Printf("done (avg of %d runs)\n", runs)
	fmt.Printf("  Turbovec search (3x): %v\n", osSearchTime)

	// Simulate exact rerank using the brute-force vectors.
	rerankStart := time.Now()
	type rerankEntry struct {
		id    uint64
		score float64
	}
	reranked := make([]rerankEntry, 0, len(osHits))
	for _, h := range osHits {
		idx := int(h.ID)
		if idx >= 0 && idx < len(vectors) {
			exactScore := cosineSim(query, vectors[idx])
			reranked = append(reranked, rerankEntry{id: h.ID, score: exactScore})
		}
	}
	sort.Slice(reranked, func(i, j int) bool { return reranked[i].score > reranked[j].score })
	if len(reranked) > benchK {
		reranked = reranked[:benchK]
	}
	rerankTime := time.Since(rerankStart)

	totalRerankTime := osSearchTime + rerankTime
	fmt.Printf("  Exact rerank:         %v\n", rerankTime)
	fmt.Printf("  Total (search+rerank): %v\n", totalRerankTime)

	// Recall after rerank.
	rerankSet := make(map[int]bool)
	for _, h := range reranked {
		rerankSet[int(h.id)] = true
	}
	overlap2 := 0
	for id := range bruteSet {
		if rerankSet[id] {
			overlap2++
		}
	}
	rerankRecall := float64(overlap2) / float64(benchK)

	fmt.Printf("  Top-5 (reranked): ")
	for i := 0; i < 5 && i < len(reranked); i++ {
		fmt.Printf("[%d:%.4f] ", reranked[i].id, reranked[i].score)
	}
	fmt.Println()
	fmt.Printf("  Recall@%d (reranked): %.2f (%d/%d overlap with brute-force)\n", benchK, rerankRecall, overlap2, benchK)

	// Compression estimate.
	vecBytes := uint64(benchNVecs) * uint64(benchDim) * 4 // float32
	compressedBytes := uint64(benchNVecs) * uint64(benchDim) * benchBitWidth / 8
	fmt.Printf("\n  Memory: %d MB raw → %d MB compressed (%.1fx)\n",
		vecBytes/1024/1024, compressedBytes/1024/1024,
		float64(vecBytes)/float64(compressedBytes))

	printSummary(bruteTime, jsonTime, searchTime, rawRecall, totalRerankTime, rerankRecall)

	// Save the benchmark index.
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".foxctl", "storage"), 0755); err == nil {
		savePath := filepath.Join(os.Getenv("HOME"), ".foxctl", "storage", "benchmark.tvim")
		if err := client.Save(indexName, savePath); err == nil {
			fi, _ := os.Stat(savePath)
			fmt.Printf("\nSaved index: %s (%d bytes)\n", savePath, fi.Size())
		}
	}
}

func printSummary(bruteTime, jsonTime, tvTime time.Duration, rawRecall float64, rerankTotal time.Duration, rerankRecall float64) {
	fmt.Println("\n=== Summary ===")
	fmt.Printf("  Brute-force (raw):        %v\n", bruteTime)
	fmt.Printf("  Brute-force (JSON):       %v\n", jsonTime)
	if tvTime > 0 {
		fmt.Printf("  Turbovec raw:             %v  (recall@%d: %.2f)\n", tvTime, benchK, rawRecall)
		fmt.Printf("  Turbovec + rerank (3x):   %v  (recall@%d: %.2f)\n", rerankTotal, benchK, rerankRecall)
		fmt.Printf("  Speedup vs JSON (raw):    %.1fx\n", float64(jsonTime)/float64(tvTime))
		fmt.Printf("  Speedup vs JSON (rerank): %.1fx\n", float64(jsonTime)/float64(rerankTotal))
		fmt.Printf("  Recall improvement:       %.2f → %.2f\n", rawRecall, rerankRecall)
	}
}

func randomUnitVector(dim int) []float32 {
	vec := make([]float32, dim)
	var norm float64
	for i := range vec {
		v := rand.NormFloat64()
		vec[i] = float32(v)
		norm += v * v
	}
	norm = math.Sqrt(norm)
	for i := range vec {
		vec[i] /= float32(norm)
	}
	return vec
}

func cosineSim(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
