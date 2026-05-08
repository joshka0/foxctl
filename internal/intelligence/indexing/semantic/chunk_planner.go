package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

const (
	chunkKindFallback = "fallback"
	chunkKindGoFunc   = "go_function"
	chunkKindGoMethod = "go_method"
)

type chunkPlannerAdapter interface {
	Plan(path, digest string, content []byte, maxBytes int) []Chunk
}

// Chunk represents a portion of file content selected for embedding.
type Chunk struct {
	ID                string
	Kind              string
	Content           []byte
	Start             int
	End               int
	SymbolIdentifiers []string
}

type chunkPlanStats struct {
	counts map[string]int
	sizes  ChunkSizeSummary
}

// planFileChunks selects semantic embedding chunks for a file.
//
// Index:
//
//	Purpose: Plan semantic file chunks by language-aware symbol spans before bounded fallback
//	Keywords: semantic_file, chunk planner, language adapters, symbol spans
//	Related: chunkPlannerFor, indexChunkedFile, RunInitFilesJob
func (idx *Indexer) planFileChunks(path, digest, language string, content []byte) []Chunk {
	if planner := chunkPlannerFor(path, language); planner != nil {
		if chunks := planner.Plan(path, digest, content, idx.config.ChunkBytes); len(chunks) > 0 {
			return chunks
		}
	}
	return idx.planFallbackChunks(path, digest, content)
}

type goChunkPlanner struct{}

func (goChunkPlanner) Plan(path, digest string, content []byte, _ int) []Chunk {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return nil
	}

	var chunks []Chunk
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}

		start := fset.Position(fn.Pos()).Offset
		end := fset.Position(fn.End()).Offset
		if start < 0 || end > len(content) || start >= end {
			continue
		}

		kind := chunkKindGoFunc
		symbol := fn.Name.Name
		if receiver := receiverTypeName(fn); receiver != "" {
			kind = chunkKindGoMethod
			symbol = receiver + "." + fn.Name.Name
		}

		chunks = append(chunks, Chunk{
			ID:                stableChunkID(path, digest, kind, start, end, []string{symbol}),
			Kind:              kind,
			Content:           content[start:end],
			Start:             start,
			End:               end,
			SymbolIdentifiers: []string{symbol},
		})
	}
	return chunks
}

func (idx *Indexer) planFallbackChunks(path, digest string, content []byte) []Chunk {
	chunkSize := idx.config.ChunkBytes
	overlap := idx.config.ChunkOverlapBytes

	if chunkSize <= 0 {
		return []Chunk{{
			ID:      stableChunkID(path, digest, chunkKindFallback, 0, len(content), nil),
			Kind:    chunkKindFallback,
			Content: content,
			Start:   0,
			End:     len(content),
		}}
	}

	var chunks []Chunk
	start := 0
	for start < len(content) {
		end := start + chunkSize
		if end > len(content) {
			end = len(content)
		}

		chunks = append(chunks, Chunk{
			ID:      stableChunkID(path, digest, chunkKindFallback, start, end, nil),
			Kind:    chunkKindFallback,
			Content: content[start:end],
			Start:   start,
			End:     end,
		})

		nextStart := end - overlap
		if nextStart <= start {
			break
		}
		start = nextStart
	}
	return chunks
}

func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	return exprTypeName(fn.Recv.List[0].Type)
}

func exprTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprTypeName(t.X)
	case *ast.IndexExpr:
		return exprTypeName(t.X)
	case *ast.IndexListExpr:
		return exprTypeName(t.X)
	case *ast.SelectorExpr:
		left := exprTypeName(t.X)
		if left == "" {
			return t.Sel.Name
		}
		return left + "." + t.Sel.Name
	default:
		return ""
	}
}

func stableChunkID(path, digest, kind string, start, end int, symbols []string) string {
	data := fmt.Sprintf("%s\n%s\n%s\n%d\n%d\n%s", path, digest, kind, start, end, strings.Join(symbols, "\x00"))
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])[:16]
}

func summarizeChunkPlan(chunks []Chunk) chunkPlanStats {
	stats := chunkPlanStats{counts: make(map[string]int)}
	for _, chunk := range chunks {
		kind := strings.TrimSpace(chunk.Kind)
		if kind == "" {
			kind = chunkKindFallback
		}
		stats.counts[kind]++
		stats.sizes.add(int64(len(chunk.Content)))
	}
	return stats
}

func mergeChunkPlanStats(summary *JobSummary, stats chunkPlanStats) {
	if summary == nil {
		return
	}
	if len(stats.counts) > 0 && summary.ChunkPlannerCounts == nil {
		summary.ChunkPlannerCounts = make(map[string]int, len(stats.counts))
	}
	for kind, count := range stats.counts {
		summary.ChunkPlannerCounts[kind] += count
	}
	if stats.sizes.Count > 0 {
		if summary.ChunkSizeBytes == nil {
			copied := stats.sizes
			summary.ChunkSizeBytes = &copied
			return
		}
		summary.ChunkSizeBytes.merge(stats.sizes)
	}
}

func (s *ChunkSizeSummary) add(size int64) {
	if size < 0 {
		size = 0
	}
	if s.Count == 0 || size < s.MinBytes {
		s.MinBytes = size
	}
	if size > s.MaxBytes {
		s.MaxBytes = size
	}
	s.Count++
	s.TotalBytes += size
	s.AverageBytes = s.TotalBytes / int64(s.Count)
}

func (s *ChunkSizeSummary) merge(other ChunkSizeSummary) {
	if other.Count == 0 {
		return
	}
	if s.Count == 0 || other.MinBytes < s.MinBytes {
		s.MinBytes = other.MinBytes
	}
	if other.MaxBytes > s.MaxBytes {
		s.MaxBytes = other.MaxBytes
	}
	s.Count += other.Count
	s.TotalBytes += other.TotalBytes
	s.AverageBytes = s.TotalBytes / int64(s.Count)
}
