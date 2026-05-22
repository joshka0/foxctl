package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/turbovec"
)

const (
	embedDim    = 4096
	embedAPIURL = "http://127.0.0.1:8100/v1/embeddings"
	embedModel  = "Qwen3-Embedding-8B"
	topK        = 20
	oversampleK = 60
	bitWidth    = 4
	embedDelay  = 50 * time.Millisecond
	maxChars    = 512
)

var queries = []string{
	"vector search cosine similarity function",
	"embedding storage and retrieval",
	"sqlite database connection pool",
	"HTTP request handler middleware",
	"session management and authentication",
	"file system watcher for changes",
	"memory store with embeddings",
	"configuration loading from environment",
	"text search and indexing",
	"error handling and retry logic",
}

type fileEntry struct {
	path      string
	shortName string // relative path for display
	embedding []float32
}

type scored struct {
	idx   int
	score float64
}

// embedRequest is the JSON body for the embedding API.
type embedRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

// embedResponse is the JSON response from the embedding API.
type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
}

func main() {
	sourceDir := filepath.Join(os.Getenv("HOME"), "repos", "foxctl", "internal")

	// 1. Collect .go files (skip _test.go and generated).
	fmt.Println("Scanning foxctl source files...")
	var files []fileEntry
	if err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, "generated") {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			rel = path
		}
		files = append(files, fileEntry{
			path:      path,
			shortName: rel,
		})
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: walk %s: %v\n", sourceDir, err)
		os.Exit(1)
	}

	if len(files) > 300 {
		files = files[:300]
		fmt.Printf("(Capped to first 300 files for speed)\n")
	}

	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "FATAL: no .go files found in %s\n", sourceDir)
		os.Exit(1)
	}

	fmt.Printf("Found %d source files\n\n", len(files))

	// 2. Embed each file.
	fmt.Println("Embedding source files...")
	httpClient := &http.Client{Timeout: 120 * time.Second}

	for i := range files {
		text, err := readFileHead(files[i].path, maxChars)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  SKIP %s: %v\n", files[i].shortName, err)
			continue
		}
		if text == "" {
			fmt.Fprintf(os.Stderr, "  SKIP %s: empty\n", files[i].shortName)
			continue
		}

		emb, err := getEmbedding(httpClient, text)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  EMBED FAIL %s: %v\n", files[i].shortName, err)
			continue
		}
		files[i].embedding = emb
		fmt.Printf("  [%3d/%d] %s (d=%d)\n", i+1, len(files), files[i].shortName, len(emb))

		if i < len(files)-1 {
			time.Sleep(embedDelay)
		}
	}

	// Filter out files with no embedding.
	valid := files[:0]
	for _, f := range files {
		if len(f.embedding) == embedDim {
			valid = append(valid, f)
		}
	}
	files = valid

	fmt.Printf("\nIndexed %d files from foxctl/internal/ (d=%d)\n\n", len(files), embedDim)

	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "FATAL: no valid embeddings obtained")
		os.Exit(1)
	}

	// 3. Embed queries.
	fmt.Println("Embedding queries...")
	queryEmbeddings := make([][]float32, len(queries))
	for i, q := range queries {
		emb, err := getEmbedding(httpClient, q)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  FATAL: embed query %d: %v\n", i, err)
			os.Exit(1)
		}
		queryEmbeddings[i] = emb
		fmt.Printf("  Query %d embedded (d=%d)\n", i+1, len(emb))
		if i < len(queries)-1 {
			time.Sleep(embedDelay)
		}
	}
	fmt.Println()

	// 4. Connect to turbovec sidecar.
	socketPath := turbovec.DefaultSocketPath()
	client, err := turbovec.Dial(socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN: turbovec sidecar not available at %s: %v\n", socketPath, err)
		fmt.Fprintln(os.Stderr, "Running brute-force only mode.")
	}
	if client != nil {
		defer client.Close()
		if err := client.Ping(); err != nil {
			fmt.Fprintf(os.Stderr, "WARN: turbovec ping failed: %v\n", err)
			client.Close()
			client = nil
		}
	}

	var tvIndexName string
	if client != nil {
		tvIndexName = fmt.Sprintf("bench-real-%d", time.Now().UnixMilli())
		if err := client.Create(tvIndexName, embedDim, bitWidth); err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: turbovec create: %v\n", err)
			client.Close()
			client = nil
		} else {
			defer func() { _ = client.Drop(tvIndexName) }()
		}
	}

	// Add all vectors to turbovec.
	if client != nil {
		fmt.Println("Adding vectors to turbovec...")
		batchSize := 500
		allFlat := make([]float32, 0, len(files)*embedDim)
		allIDs := make([]uint64, 0, len(files))
		for i, f := range files {
			allFlat = append(allFlat, f.embedding...)
			allIDs = append(allIDs, uint64(i))
		}
		for batchStart := 0; batchStart < len(files); batchStart += batchSize {
			batchEnd := batchStart + batchSize
			if batchEnd > len(files) {
				batchEnd = len(files)
			}
			flatBatch := allFlat[batchStart*embedDim : batchEnd*embedDim]
			idsBatch := allIDs[batchStart:batchEnd]
			if _, err := client.AddBatch(tvIndexName, flatBatch, embedDim, idsBatch); err != nil {
				// Fallback to single adds.
				for j := batchStart; j < batchEnd; j++ {
					if _, err := client.Add(tvIndexName, uint64(j), files[j].embedding); err != nil {
						fmt.Fprintf(os.Stderr, "  FATAL: add %s: %v\n", files[j].shortName, err)
						os.Exit(1)
					}
				}
			}
			fmt.Printf("  Added batch [%d:%d]\n", batchStart, batchEnd)
		}
		_ = client.Prepare(tvIndexName)
		fmt.Println()
	}

	// 5. Run queries.
	var totalBruteTime, totalTVTime, totalTVRerankTime time.Duration
	var totalRawRecall, totalRerankRecall float64
	queryCount := 0

	for qi, q := range queries {
		queryVec := queryEmbeddings[qi]
		fmt.Printf("Query: %q\n", q)

		// Brute-force search.
		bruteStart := time.Now()
		hits := make([]scored, len(files))
		for i, f := range files {
			hits[i] = scored{idx: i, score: cosineSim(queryVec, f.embedding)}
		}
		sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		if len(hits) > topK {
			hits = hits[:topK]
		}
		bruteDur := time.Since(bruteStart)
		totalBruteTime += bruteDur

		// Display brute-force top-5.
		fmt.Printf("  Brute-force top-5: [")
		for i := 0; i < 5 && i < len(hits); i++ {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s (%.4f)", files[hits[i].idx].shortName, hits[i].score)
		}
		fmt.Println("]")

		bruteSet := make(map[int]bool)
		for _, h := range hits {
			bruteSet[h.idx] = true
		}

		// Turbovec raw search.
		if client != nil {
			tvStart := time.Now()
			tvHits, err := client.Search(tvIndexName, queryVec, topK)
			tvDur := time.Since(tvStart)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  turbovec search error: %v\n", err)
			} else {
				totalTVTime += tvDur

				fmt.Printf("  Turbovec  top-5: [")
				for i := 0; i < 5 && i < len(tvHits); i++ {
					if i > 0 {
						fmt.Print(", ")
					}
					idx := int(tvHits[i].ID)
					name := "???"
					if idx >= 0 && idx < len(files) {
						name = files[idx].shortName
					}
					fmt.Printf("%s (%.4f)", name, tvHits[i].Score)
				}
				fmt.Println("]")

				// Compute raw recall.
				tvSet := make(map[int]bool)
				for _, h := range tvHits {
					tvSet[int(h.ID)] = true
				}
				rawOverlap := 0
				for id := range bruteSet {
					if tvSet[id] {
						rawOverlap++
					}
				}
				rawRecall := float64(rawOverlap) / float64(topK)
				totalRawRecall += rawRecall
				fmt.Printf("  Recall@%d (raw):    %d/%d (%.2f)\n", topK, rawOverlap, topK, rawRecall)

				// Turbovec oversample + rerank.
				osStart := time.Now()
				osHits, err := client.Search(tvIndexName, queryVec, oversampleK)
				osSearchDur := time.Since(osStart)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  turbovec oversample error: %v\n", err)
				} else {
					type rerankEntry struct {
						id    uint64
						score float64
					}
					reranked := make([]rerankEntry, 0, len(osHits))
					for _, h := range osHits {
						idx := int(h.ID)
						if idx >= 0 && idx < len(files) {
							exactScore := cosineSim(queryVec, files[idx].embedding)
							reranked = append(reranked, rerankEntry{id: h.ID, score: exactScore})
						}
					}
					sort.Slice(reranked, func(i, j int) bool { return reranked[i].score > reranked[j].score })
					if len(reranked) > topK {
						reranked = reranked[:topK]
					}

					rerankTotal := osSearchDur + time.Since(osStart)
					totalTVRerankTime += rerankTotal

					fmt.Printf("  Turbovec+rr top-5: [")
					for i := 0; i < 5 && i < len(reranked); i++ {
						if i > 0 {
							fmt.Print(", ")
						}
						name := "???"
						idx := int(reranked[i].id)
						if idx >= 0 && idx < len(files) {
							name = files[idx].shortName
						}
						fmt.Printf("%s (%.4f)", name, reranked[i].score)
					}
					fmt.Println("]")

					rerankSet := make(map[int]bool)
					for _, h := range reranked {
						rerankSet[int(h.id)] = true
					}
					rerankOverlap := 0
					for id := range bruteSet {
						if rerankSet[id] {
							rerankOverlap++
						}
					}
					rerankRecall := float64(rerankOverlap) / float64(topK)
					totalRerankRecall += rerankRecall
					fmt.Printf("  Recall@%d (rerank): %d/%d (%.2f)\n", topK, rerankOverlap, topK, rerankRecall)
				}
			}
		}

		fmt.Printf("  Brute-force time: %v\n", bruteDur)
		fmt.Println()
		queryCount++
	}

	// 6. Summary.
	fmt.Println("=== Summary ===")
	fmt.Printf("Queries: %d\n", queryCount)
	if queryCount > 0 {
		fmt.Printf("Avg recall@%d (raw):      %.2f\n", topK, totalRawRecall/float64(queryCount))
		fmt.Printf("Avg recall@%d (rerank):   %.2f\n", topK, totalRerankRecall/float64(queryCount))
		fmt.Printf("Avg brute-force time:     %v\n", totalBruteTime/time.Duration(queryCount))
		if client != nil {
			fmt.Printf("Avg turbovec time:        %v\n", totalTVTime/time.Duration(queryCount))
			fmt.Printf("Avg turbovec+rerank time: %v\n", totalTVRerankTime/time.Duration(queryCount))
		} else {
			fmt.Println("Turbovec: not available (sidecar not running)")
		}
	}
}

func readFileHead(path string, maxChars int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	if len(text) > maxChars {
		text = text[:maxChars]
	}
	return text, nil
}

func getEmbedding(client *http.Client, text string) ([]float32, error) {
	reqBody := embedRequest{
		Input: text,
		Model: embedModel,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := client.Post(embedAPIURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, buf.String())
	}

	var embResp embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(embResp.Data) == 0 || len(embResp.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	// Convert []float64 to []float32.
	emb64 := embResp.Data[0].Embedding
	result := make([]float32, len(emb64))
	for i, v := range emb64 {
		result[i] = float32(v)
	}
	return result, nil
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
