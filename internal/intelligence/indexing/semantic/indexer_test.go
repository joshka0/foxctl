package semantic

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing"
	"github.com/joshka0/foxctl/internal/platform/fsutil"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

type recordingProvider struct {
	texts []string
}

func (p *recordingProvider) Embed(_ context.Context, text string) ([]float32, error) {
	p.texts = append(p.texts, text)
	return []float32{0}, nil
}

func (p *recordingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		embedding, err := p.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = embedding
	}
	return result, nil
}

func (p *recordingProvider) Model() string { return "recording" }

func (p *recordingProvider) Dimensions() int { return 1 }

func setupTestIndexer(t *testing.T, cfg Config) (*Indexer, *memory.Store, string, string) {
	t.Helper()

	// Create temp directories
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspace")
	storageDir := filepath.Join(tmpDir, "storage")
	casDir := filepath.Join(tmpDir, "cas")

	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Open memory store
	ctx := context.Background()
	store, err := memory.Open(ctx, storageDir, casDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	// Create indexer with no-op provider
	provider := NewNoOpProvider("test-model", 384)
	logger := zerolog.Nop()

	idx := NewIndexer(cfg, store, provider, workspaceDir, logger)

	workspaceID := workspace.ID(workspaceDir)
	return idx, store, workspaceDir, workspaceID
}

func createTestFile(t *testing.T, dir, path, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexer_ID(t *testing.T) {
	idx, _, _, _ := setupTestIndexer(t, Config{Enabled: true})
	if idx.ID() != IndexerID {
		t.Errorf("expected ID %q, got %q", IndexerID, idx.ID())
	}
}

func TestIndexer_Index_Disabled(t *testing.T) {
	idx, _, _, workspaceID := setupTestIndexer(t, Config{Enabled: false})

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "foo.go", ChangeKind: indexing.ChangeKindModified},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesSkipped != 1 {
		t.Errorf("expected 1 file skipped, got %d", result.FilesSkipped)
	}
	if result.FilesIndexed != 0 {
		t.Errorf("expected 0 files indexed, got %d", result.FilesIndexed)
	}
}

func TestIndexer_Index_SingleFile(t *testing.T) {
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	// Create test file
	createTestFile(t, workspaceDir, "main.go", "package main\n\nfunc main() {}\n")

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		TaskID:      "task-123",
		ReviewID:    "review-456",
		Reason:      "post_review",
		Files: []indexing.FileChange{
			{Path: "main.go", ChangeKind: indexing.ChangeKindModified, Language: "go"},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.FilesIndexed)
	}
	if result.FilesFailed != 0 {
		t.Errorf("expected 0 files failed, got %d", result.FilesFailed)
	}

	// Verify entry was saved
	ctx := context.Background()
	name := FileEmbeddingName(workspaceID, "main.go")
	entry, err := store.Get(ctx, name, workspaceID)
	if err != nil {
		t.Fatalf("Get entry failed: %v", err)
	}
	if entry.Type != FileEmbeddingType {
		t.Errorf("expected type %q, got %q", FileEmbeddingType, entry.Type)
	}

	// Verify result metadata
	fileResult, err := UnmarshalFileResult(entry.Result)
	if err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}
	if fileResult.Path != "main.go" {
		t.Errorf("expected path 'main.go', got %q", fileResult.Path)
	}
	if fileResult.Language != "go" {
		t.Errorf("expected language 'go', got %q", fileResult.Language)
	}
	if fileResult.Source == nil {
		t.Error("expected source to be set")
	} else {
		if fileResult.Source.TaskID != "task-123" {
			t.Errorf("expected task_id 'task-123', got %q", fileResult.Source.TaskID)
		}
		if fileResult.Source.ReviewID != "review-456" {
			t.Errorf("expected review_id 'review-456', got %q", fileResult.Source.ReviewID)
		}
	}
}

func TestIndexer_Index_ChunkedFile(t *testing.T) {
	cfg := Config{
		Enabled:           true,
		ChunkBytes:        50, // Small chunks for testing
		ChunkOverlapBytes: 10,
		ProviderModel:     "test-model",
	}
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, cfg)

	// Create a file larger than chunk size
	content := "This is a test file with enough content to trigger chunking behavior in the semantic indexer."
	createTestFile(t, workspaceDir, "large.txt", content)

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "large.txt", ChangeKind: indexing.ChangeKindAdded, SizeBytes: int64(len(content))},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.FilesIndexed)
	}

	// Verify parent entry was saved
	ctx := context.Background()
	name := FileEmbeddingName(workspaceID, "large.txt")
	entry, err := store.Get(ctx, name, workspaceID)
	if err != nil {
		t.Fatalf("Get parent entry failed: %v", err)
	}

	fileResult, err := UnmarshalFileResult(entry.Result)
	if err != nil {
		t.Fatalf("Unmarshal result failed: %v", err)
	}
	if fileResult.ChunkCount == 0 {
		t.Error("expected chunk count to be set")
	}
	if fileResult.ChunkingConfigHash == "" {
		t.Error("expected chunking config hash to be set")
	}

	// Verify chunk entries were saved
	configHash := cfg.ChunkingConfigHash()
	chunks := idx.planFileChunks("large.txt", computeDigest([]byte(content)), "text", []byte(content))
	chunkName := ChunkEmbeddingName(workspaceID, "large.txt", chunks[0].ID, configHash)
	chunkEntry, err := store.Get(ctx, chunkName, workspaceID)
	if err != nil {
		t.Fatalf("Get chunk entry failed: %v", err)
	}
	if chunkEntry.Type != FileEmbeddingChunkType {
		t.Errorf("expected type %q, got %q", FileEmbeddingChunkType, chunkEntry.Type)
	}

	chunkResult, err := UnmarshalChunkResult(chunkEntry.Result)
	if err != nil {
		t.Fatalf("Unmarshal chunk result failed: %v", err)
	}
	if chunkResult.Chunk.Index != 0 {
		t.Errorf("expected chunk index 0, got %d", chunkResult.Chunk.Index)
	}
	if chunkResult.Chunk.Kind != chunkKindFallback {
		t.Errorf("expected fallback chunk kind, got %q", chunkResult.Chunk.Kind)
	}
	if chunkResult.Chunk.SizeBytes == 0 {
		t.Error("expected chunk size metadata")
	}
	if chunkResult.Chunk.Of != fileResult.ChunkCount {
		t.Errorf("expected chunk.of %d, got %d", fileResult.ChunkCount, chunkResult.Chunk.Of)
	}
}

func TestIndexer_Index_DeletedFile(t *testing.T) {
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	// First, create and index a file
	createTestFile(t, workspaceDir, "to_delete.go", "package main")

	ctx := context.Background()
	addEvent := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "to_delete.go", ChangeKind: indexing.ChangeKindAdded},
		},
	}
	_, err := idx.Index(ctx, addEvent)
	if err != nil {
		t.Fatalf("Initial index failed: %v", err)
	}

	// Verify entry exists
	name := FileEmbeddingName(workspaceID, "to_delete.go")
	_, err = store.Get(ctx, name, workspaceID)
	if err != nil {
		t.Fatalf("Entry should exist after indexing: %v", err)
	}

	// Now delete the file
	deleteEvent := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "to_delete.go", ChangeKind: indexing.ChangeKindDeleted},
		},
	}

	result, err := idx.Index(ctx, deleteEvent)
	if err != nil {
		t.Fatalf("Delete index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed (deleted), got %d", result.FilesIndexed)
	}

	// Verify entry is deleted
	_, err = store.Get(ctx, name, workspaceID)
	if err == nil {
		t.Error("expected entry to be deleted")
	}
}

func TestIndexer_Index_MultipleFiles(t *testing.T) {
	idx, _, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	// Create multiple test files
	createTestFile(t, workspaceDir, "file1.go", "package main")
	createTestFile(t, workspaceDir, "file2.go", "package lib")
	createTestFile(t, workspaceDir, "file3.go", "package util")

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "file1.go", ChangeKind: indexing.ChangeKindModified},
			{Path: "file2.go", ChangeKind: indexing.ChangeKindAdded},
			{Path: "file3.go", ChangeKind: indexing.ChangeKindModified},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesIndexed != 3 {
		t.Errorf("expected 3 files indexed, got %d", result.FilesIndexed)
	}
}

func TestIndexer_Index_FileNotFound(t *testing.T) {
	idx, _, _, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "nonexistent.go", ChangeKind: indexing.ChangeKindModified},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index should not return error: %v", err)
	}
	if result.FilesFailed != 1 {
		t.Errorf("expected 1 file failed, got %d", result.FilesFailed)
	}
	if len(result.Failures) != 1 {
		t.Errorf("expected 1 failure, got %d", len(result.Failures))
	}
}

func TestConfig_ChunkingConfigHash(t *testing.T) {
	cfg1 := Config{ChunkBytes: 1000, ChunkOverlapBytes: 100, ProviderModel: "model-a"}
	cfg2 := Config{ChunkBytes: 1000, ChunkOverlapBytes: 100, ProviderModel: "model-a"}
	cfg3 := Config{ChunkBytes: 2000, ChunkOverlapBytes: 100, ProviderModel: "model-a"}
	cfg4 := Config{ChunkBytes: 0} // No chunking

	hash1 := cfg1.ChunkingConfigHash()
	hash2 := cfg2.ChunkingConfigHash()
	hash3 := cfg3.ChunkingConfigHash()
	hash4 := cfg4.ChunkingConfigHash()

	if hash1 != hash2 {
		t.Error("identical configs should have same hash")
	}
	if hash1 == hash3 {
		t.Error("different configs should have different hash")
	}
	if hash4 != "" {
		t.Error("no chunking should have empty hash")
	}
}

func TestFileEmbeddingName(t *testing.T) {
	name := FileEmbeddingName("workspace-123", "src/main.go")
	expected := "file://workspace-123/src/main.go"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}

func TestChunkEmbeddingName(t *testing.T) {
	name := ChunkEmbeddingName("workspace-123", "src/main.go", "0", "abc123")
	expected := "file://workspace-123/src/main.go#chunk-0?cfg=abc123"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}

func TestPlanFallbackChunks(t *testing.T) {
	cfg := Config{ChunkBytes: 10, ChunkOverlapBytes: 2}
	idx := &Indexer{config: cfg}

	content := []byte("0123456789abcdefghij") // 20 bytes
	digest := computeDigest(content)
	chunks := idx.planFileChunks("notes.txt", digest, "text", content)

	// Expected: [0:10], [8:18], [16:20]
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	// First chunk should start at 0
	if chunks[0].Start != 0 {
		t.Errorf("first chunk should start at 0, got %d", chunks[0].Start)
	}

	// Last chunk should end at content length
	lastChunk := chunks[len(chunks)-1]
	if lastChunk.End != len(content) {
		t.Errorf("last chunk should end at %d, got %d", len(content), lastChunk.End)
	}
	if chunks[0].Kind != chunkKindFallback {
		t.Errorf("expected fallback chunk kind, got %q", chunks[0].Kind)
	}
	again := idx.planFileChunks("notes.txt", digest, "text", content)
	if chunks[0].ID != again[0].ID {
		t.Errorf("expected deterministic chunk ID, got %q and %q", chunks[0].ID, again[0].ID)
	}
}

func TestPlanGoChunks_FunctionAndMethodSpans(t *testing.T) {
	idx := &Indexer{config: Config{ChunkBytes: 20}}
	content := []byte(`package sample

type Runner struct{}

func Build() string {
	return "ok"
}

func (r *Runner) Run() {}
`)

	chunks := idx.planFileChunks("sample.go", computeDigest(content), "go", content)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 Go chunks, got %d", len(chunks))
	}
	if chunks[0].Kind != chunkKindGoFunc {
		t.Fatalf("expected first chunk kind %q, got %q", chunkKindGoFunc, chunks[0].Kind)
	}
	if got := chunks[0].SymbolIdentifiers; len(got) != 1 || got[0] != "Build" {
		t.Fatalf("expected Build symbol, got %#v", got)
	}
	if chunks[1].Kind != chunkKindGoMethod {
		t.Fatalf("expected second chunk kind %q, got %q", chunkKindGoMethod, chunks[1].Kind)
	}
	if got := chunks[1].SymbolIdentifiers; len(got) != 1 || got[0] != "Runner.Run" {
		t.Fatalf("expected Runner.Run symbol, got %#v", got)
	}
	if !strings.Contains(string(chunks[0].Content), "func Build") {
		t.Fatalf("expected first chunk content to contain Build function, got %q", string(chunks[0].Content))
	}
	if chunks[0].ID == chunks[1].ID {
		t.Fatal("expected distinct deterministic chunk IDs for distinct Go spans")
	}
}

func TestPlanLanguageSymbolChunks_NonGoSupportedLanguages(t *testing.T) {
	idx := &Indexer{config: Config{ChunkBytes: 1000}}
	tests := []struct {
		name     string
		path     string
		language string
		content  string
		expected map[string]string
	}{
		{
			name:     "typescript",
			path:     "src/planner.ts",
			language: "typescript",
			content: `export function buildName(input: string) {
  return input.trim()
}

export class Runner {
  run() {
    return buildName("ok")
  }
}
`,
			expected: map[string]string{
				"buildName": chunkKindTypeScriptFunction,
				"Runner":    chunkKindTypeScriptClass,
			},
		},
		{
			name:     "javascript",
			path:     "src/planner.js",
			language: "javascript",
			content: `export const buildName = (input) => {
  return input.trim()
}
`,
			expected: map[string]string{
				"buildName": chunkKindJavaScriptFunction,
			},
		},
		{
			name:     "python",
			path:     "planner.py",
			language: "python",
			content: `class Runner:
    def run(self):
        return "ok"

def build():
    return Runner()
`,
			expected: map[string]string{
				"Runner":     chunkKindPythonClass,
				"Runner.run": chunkKindPythonFunction,
				"build":      chunkKindPythonFunction,
			},
		},
		{
			name:     "rust",
			path:     "src/planner.rs",
			language: "rust",
			content: `pub struct Runner {
    value: String,
}

impl Runner {
    pub fn run(&self) -> String {
        self.value.clone()
    }
}

pub fn build() -> Runner {
    Runner { value: String::new() }
}
`,
			expected: map[string]string{
				"Runner":     chunkKindRustType,
				"Runner.run": chunkKindRustMethod,
				"build":      chunkKindRustFunction,
			},
		},
		{
			name:     "elixir",
			path:     "lib/planner.ex",
			language: "elixir",
			content: `defmodule MyApp.Runner do
  @type id :: String.t()

  def run(value) do
    value
  end
end
`,
			expected: map[string]string{
				"MyApp.Runner": chunkKindElixirModule,
				"id":           chunkKindElixirType,
				"run":          chunkKindElixirFunction,
			},
		},
		{
			name:     "csharp",
			path:     "src/Planner.cs",
			language: "csharp",
			content: `public class Runner
{
    public string Run()
    {
        return "ok";
    }
}
`,
			expected: map[string]string{
				"Runner":     chunkKindCSharpClass,
				"Runner.Run": chunkKindCSharpFunction,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := []byte(tt.content)
			chunks := idx.planFileChunks(tt.path, computeDigest(content), tt.language, content)
			bySymbol := chunksBySymbol(chunks)
			for symbol, kind := range tt.expected {
				chunk, ok := bySymbol[symbol]
				if !ok {
					t.Fatalf("expected symbol chunk %q, got symbols %#v", symbol, chunkSymbols(chunks))
				}
				if chunk.Kind != kind {
					t.Fatalf("chunk %q kind=%q want %q", symbol, chunk.Kind, kind)
				}
				if chunk.Kind == chunkKindFallback {
					t.Fatalf("chunk %q unexpectedly used fallback", symbol)
				}
				if chunk.Start < 0 || chunk.End > len(content) || chunk.Start >= chunk.End {
					t.Fatalf("chunk %q has invalid span [%d:%d] for content length %d", symbol, chunk.Start, chunk.End, len(content))
				}
				if string(chunk.Content) != string(content[chunk.Start:chunk.End]) {
					t.Fatalf("chunk %q content does not match byte span", symbol)
				}
			}

			again := chunksBySymbol(idx.planFileChunks(tt.path, computeDigest(content), tt.language, content))
			for symbol := range tt.expected {
				if bySymbol[symbol].ID != again[symbol].ID {
					t.Fatalf("chunk %q ID is not deterministic: %q != %q", symbol, bySymbol[symbol].ID, again[symbol].ID)
				}
			}
		})
	}
}

func TestPlanLanguageSymbolChunks_FallsBackWhenNoSymbols(t *testing.T) {
	idx := &Indexer{config: Config{ChunkBytes: 12, ChunkOverlapBytes: 2}}
	content := []byte("const value = 1;\n")

	chunks := idx.planFileChunks("src/constants.ts", computeDigest(content), "typescript", content)
	if len(chunks) == 0 {
		t.Fatal("expected fallback chunks")
	}
	for _, chunk := range chunks {
		if chunk.Kind != chunkKindFallback {
			t.Fatalf("expected fallback chunk, got %q", chunk.Kind)
		}
	}
}

func TestIndexer_Index_TypeScriptChunkMetadata(t *testing.T) {
	cfg := Config{Enabled: true, ChunkBytes: 40, ProviderModel: "test-model"}
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, cfg)

	content := `export function buildName(input: string) {
  return input.trim()
}

const value = "extra content to force chunking"
`
	createTestFile(t, workspaceDir, "src/planner.ts", content)

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "src/planner.ts", ChangeKind: indexing.ChangeKindAdded, Language: "typescript", SizeBytes: int64(len(content))},
		},
	}
	if _, err := idx.Index(context.Background(), event); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	chunks := idx.planFileChunks("src/planner.ts", computeDigest([]byte(content)), "typescript", []byte(content))
	if len(chunks) == 0 {
		t.Fatal("expected planned TypeScript chunks")
	}

	chunkName := ChunkEmbeddingName(workspaceID, "src/planner.ts", chunks[0].ID, cfg.ChunkingConfigHash())
	chunkEntry, err := store.Get(context.Background(), chunkName, workspaceID)
	if err != nil {
		t.Fatalf("Get chunk entry failed: %v", err)
	}
	chunkResult, err := UnmarshalChunkResult(chunkEntry.Result)
	if err != nil {
		t.Fatalf("Unmarshal chunk result failed: %v", err)
	}
	if chunkResult.Language != "typescript" {
		t.Fatalf("language=%q want typescript", chunkResult.Language)
	}
	if chunkResult.Chunk.Kind != chunkKindTypeScriptFunction {
		t.Fatalf("chunk kind=%q want %q", chunkResult.Chunk.Kind, chunkKindTypeScriptFunction)
	}
	if got := chunkResult.Chunk.SymbolIdentifiers; len(got) != 1 || got[0] != "buildName" {
		t.Fatalf("symbol identifiers=%#v want [buildName]", got)
	}
	if chunkResult.Chunk.Span == nil || chunkResult.Chunk.Span.Unit != "byte" {
		t.Fatalf("expected byte span metadata, got %#v", chunkResult.Chunk.Span)
	}
}

func chunksBySymbol(chunks []Chunk) map[string]Chunk {
	bySymbol := make(map[string]Chunk, len(chunks))
	for _, chunk := range chunks {
		if len(chunk.SymbolIdentifiers) == 0 {
			continue
		}
		bySymbol[chunk.SymbolIdentifiers[0]] = chunk
	}
	return bySymbol
}

func chunkSymbols(chunks []Chunk) []string {
	symbols := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if len(chunk.SymbolIdentifiers) == 0 {
			continue
		}
		symbols = append(symbols, chunk.SymbolIdentifiers[0])
	}
	return symbols
}

func TestIndexer_ChunkEmbeddingTextDoesNotUseSyntheticLabels(t *testing.T) {
	cfg := Config{Enabled: true, ChunkBytes: 40, ProviderModel: "test-model"}
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, cfg)
	provider := &recordingProvider{}
	idx.provider = provider

	content := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda"
	createTestFile(t, workspaceDir, "large.txt", content)

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "large.txt", ChangeKind: indexing.ChangeKindAdded},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Fatalf("expected file indexed, got %d", result.FilesIndexed)
	}
	if len(provider.texts) == 0 {
		t.Fatal("expected chunk embedding calls")
	}
	for _, text := range provider.texts {
		if strings.Contains(text, "Chunk ") || strings.Contains(text, "Semantic embedding for") {
			t.Fatalf("embedding text contains synthetic operational label: %q", text)
		}
	}

	chunks := idx.planFileChunks("large.txt", computeDigest([]byte(content)), "text", []byte(content))
	entry, err := store.Get(context.Background(), ChunkEmbeddingName(workspaceID, "large.txt", chunks[0].ID, cfg.ChunkingConfigHash()), workspaceID)
	if err != nil {
		t.Fatalf("failed to get chunk entry: %v", err)
	}
	if strings.Contains(entry.Summary, "Chunk ") {
		t.Fatalf("chunk summary should avoid synthetic chunk labels, got %q", entry.Summary)
	}
}

func TestNoOpProvider(t *testing.T) {
	provider := NewNoOpProvider("test", 512)

	if provider.Model() != "test" {
		t.Errorf("expected model 'test', got %q", provider.Model())
	}
	if provider.Dimensions() != 512 {
		t.Errorf("expected dimensions 512, got %d", provider.Dimensions())
	}

	ctx := context.Background()
	embedding, err := provider.Embed(ctx, "test text")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embedding) != 512 {
		t.Errorf("expected embedding length 512, got %d", len(embedding))
	}

	embeddings, err := provider.EmbedBatch(ctx, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(embeddings) != 3 {
		t.Errorf("expected 3 embeddings, got %d", len(embeddings))
	}
}

func TestIndexer_readFileContent_PathTraversal(t *testing.T) {
	idx, _, workspaceDir, _ := setupTestIndexer(t, Config{Enabled: true})

	// Create a valid test file
	createTestFile(t, workspaceDir, "valid.txt", "test content")

	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "valid path",
			path:    "valid.txt",
			wantErr: "",
		},
		{
			name:    "path traversal with ..",
			path:    "../../../etc/passwd",
			wantErr: "path traversal not allowed",
		},
		{
			name:    "absolute path",
			path:    "/etc/passwd",
			wantErr: "absolute paths not allowed",
		},
		{
			name:    "traversal via subdirectory",
			path:    "foo/../../etc/passwd",
			wantErr: "path traversal not allowed", // After cleaning becomes ../etc/passwd
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := idx.readFileContent(tt.path)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Error("expected error, got nil")
				} else if !containsStr(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

func TestIndexer_readFileContent_FileSizeLimit(t *testing.T) {
	idx, _, workspaceDir, _ := setupTestIndexer(t, Config{Enabled: true})

	// Create a file larger than maxReadFileSize (10MB)
	largeContent := make([]byte, 11*1024*1024) // 11MB
	createTestFile(t, workspaceDir, "large.txt", string(largeContent))

	_, err := idx.readFileContent("large.txt")
	if err == nil {
		t.Error("expected error for large file")
	} else if !containsStr(err.Error(), "file too large") {
		t.Errorf("expected 'file too large' error, got %q", err.Error())
	}
}

func TestIndexer_Index_MaxFileKBSkipped(t *testing.T) {
	idx, _, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true, MaxFileKB: 1})

	content := strings.Repeat("x", 2*1024)
	createTestFile(t, workspaceDir, "too-large.txt", content)

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "too-large.txt", ChangeKind: indexing.ChangeKindAdded, SizeBytes: int64(len(content))},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesSkipped != 1 {
		t.Fatalf("expected 1 skipped file, got indexed=%d skipped=%d failed=%d", result.FilesIndexed, result.FilesSkipped, result.FilesFailed)
	}
	if result.FilesFailed != 0 {
		t.Fatalf("expected no failures, got %d", result.FilesFailed)
	}
}

func TestIndexer_EmbeddingStored(t *testing.T) {
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true})

	createTestFile(t, workspaceDir, "embed.txt", "test content for embedding")

	event := indexing.PostReviewEvent{
		WorkspaceID: workspaceID,
		Files: []indexing.FileChange{
			{Path: "embed.txt", ChangeKind: indexing.ChangeKindModified},
		},
	}

	result, err := idx.Index(context.Background(), event)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}
	if result.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.FilesIndexed)
	}

	// Verify the embedding was stored
	entryName := FileEmbeddingName(workspaceID, "embed.txt")
	entry, err := store.Get(context.Background(), entryName, workspaceID)
	if err != nil {
		t.Fatalf("failed to get stored entry: %v", err)
	}

	var result2 FileEmbeddingResult
	if err := json.Unmarshal(entry.Result, &result2); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	// NoOpProvider returns a zero vector of configured dimension (384 default)
	if len(result2.Embedding) != 384 {
		t.Errorf("expected embedding length 384 (NoOp default), got %d", len(result2.Embedding))
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// P3.S4: Job Execution Tests
// =============================================================================

func TestRunInitFilesJob_SingleFile(t *testing.T) {
	cfg := Config{Enabled: true}
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, cfg)

	// Create test file
	createTestFile(t, workspaceDir, "main.go", "package main\n\nfunc main() {}\n")

	args := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "main.go"},
		},
		Reason: ReasonInitialIndex,
		TaskID: "task-init-1",
	}

	result, err := idx.RunInitFilesJob(context.Background(), args)
	if err != nil {
		t.Fatalf("RunInitFilesJob failed: %v", err)
	}

	if result.Summary.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.Summary.FilesIndexed)
	}
	if result.Summary.ChunksIndexed != 0 {
		t.Errorf("expected 0 chunks for small file, got %d", result.Summary.ChunksIndexed)
	}
	if len(result.Failures) != 0 {
		t.Errorf("expected no failures, got %d", len(result.Failures))
	}

	// Verify stored entry
	entryName := FileEmbeddingName(workspaceID, "main.go")
	entry, err := store.Get(context.Background(), entryName, workspaceID)
	if err != nil {
		t.Fatalf("failed to get stored entry: %v", err)
	}

	var fileResult FileEmbeddingResult
	if err := json.Unmarshal(entry.Result, &fileResult); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if fileResult.Path != "main.go" {
		t.Errorf("expected path main.go, got %s", fileResult.Path)
	}
	if fileResult.Language != "go" {
		t.Errorf("expected language go, got %s", fileResult.Language)
	}
	if fileResult.Source == nil {
		t.Error("expected source to be set")
	} else if fileResult.Source.TaskID != "task-init-1" {
		t.Errorf("expected task_id task-init-1, got %s", fileResult.Source.TaskID)
	}
}

func TestRunInitFilesJob_ChunkedFile(t *testing.T) {
	cfg := Config{
		Enabled:           true,
		ChunkBytes:        50,
		ChunkOverlapBytes: 10,
		ProviderModel:     "test-model",
	}
	idx, _, workspaceDir, workspaceID := setupTestIndexer(t, cfg)

	// Create large file (200 bytes)
	content := make([]byte, 200)
	for i := range content {
		content[i] = byte('a' + (i % 26))
	}
	createTestFile(t, workspaceDir, "large.txt", string(content))

	args := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "large.txt"},
		},
		Reason: ReasonInitialIndex,
	}

	result, err := idx.RunInitFilesJob(context.Background(), args)
	if err != nil {
		t.Fatalf("RunInitFilesJob failed: %v", err)
	}

	if result.Summary.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.Summary.FilesIndexed)
	}
	if result.Summary.ChunksIndexed == 0 {
		t.Error("expected chunks for large file")
	}
	if result.Summary.ChunkPlannerCounts[chunkKindFallback] != result.Summary.ChunksIndexed {
		t.Fatalf("planner counts=%v want fallback count %d", result.Summary.ChunkPlannerCounts, result.Summary.ChunksIndexed)
	}
	if result.Summary.ChunkSizeBytes == nil {
		t.Fatal("expected chunk size summary")
	}
	if result.Summary.ChunkSizeBytes.Count != result.Summary.ChunksIndexed {
		t.Fatalf("size count=%d want %d", result.Summary.ChunkSizeBytes.Count, result.Summary.ChunksIndexed)
	}
	if result.Summary.ChunkSizeBytes.MinBytes <= 0 || result.Summary.ChunkSizeBytes.MaxBytes <= 0 || result.Summary.ChunkSizeBytes.AverageBytes <= 0 {
		t.Fatalf("invalid chunk size summary=%+v", result.Summary.ChunkSizeBytes)
	}
}

func TestRunInitFilesJob_ChunkDelayHonorsContextCancellation(t *testing.T) {
	cfg := Config{
		Enabled:           true,
		ChunkBytes:        50,
		ChunkOverlapBytes: 0,
		ChunkDelay:        50 * time.Millisecond,
		ProviderModel:     "test-model",
	}
	idx, _, workspaceDir, workspaceID := setupTestIndexer(t, cfg)

	content := make([]byte, 200)
	for i := range content {
		content[i] = byte('a' + (i % 26))
	}
	createTestFile(t, workspaceDir, "large.txt", string(content))

	args := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "large.txt"},
		},
		Reason: ReasonInitialIndex,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	result, err := idx.RunInitFilesJob(ctx, args)
	if err != nil {
		t.Fatalf("RunInitFilesJob should record per-file failure, got error: %v", err)
	}
	if result.Summary.FilesIndexed != 0 {
		t.Fatalf("expected no indexed files after chunk-delay cancellation, got %d", result.Summary.FilesIndexed)
	}
	if len(result.Failures) != 1 {
		t.Fatalf("expected one file failure, got %d", len(result.Failures))
	}
	if !containsStr(result.Failures[0].ErrorMessage, "context deadline exceeded") {
		t.Fatalf("expected context deadline failure, got %q", result.Failures[0].ErrorMessage)
	}
}

func TestRunUpdateFilesJob_DeletedFile(t *testing.T) {
	cfg := Config{Enabled: true}
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, cfg)

	// First, create and index a file
	createTestFile(t, workspaceDir, "todelete.go", "package main")

	initArgs := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "todelete.go"},
		},
		Reason: ReasonInitialIndex,
	}

	_, err := idx.RunInitFilesJob(context.Background(), initArgs)
	if err != nil {
		t.Fatalf("RunInitFilesJob failed: %v", err)
	}

	// Verify it exists
	entryName := FileEmbeddingName(workspaceID, "todelete.go")
	_, err = store.Get(context.Background(), entryName, workspaceID)
	if err != nil {
		t.Fatalf("entry should exist after init: %v", err)
	}

	// Now delete it via update job
	updateArgs := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "todelete.go", ChangeKind: ChangeKindDeleted},
		},
		Reason: ReasonPostReview,
	}

	result, err := idx.RunUpdateFilesJob(context.Background(), updateArgs)
	if err != nil {
		t.Fatalf("RunUpdateFilesJob failed: %v", err)
	}

	if result.Summary.FilesIndexed != 1 {
		t.Errorf("expected 1 file processed, got %d", result.Summary.FilesIndexed)
	}

	// Verify it no longer exists
	_, err = store.Get(context.Background(), entryName, workspaceID)
	if err == nil {
		t.Error("expected entry to be deleted")
	}
}

func TestRunUpdateFilesJob_ModifiedFile(t *testing.T) {
	cfg := Config{Enabled: true}
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, cfg)

	// Create and index initial file
	createTestFile(t, workspaceDir, "mod.go", "package main\n\nfunc old() {}\n")

	initArgs := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "mod.go"},
		},
		Reason: ReasonInitialIndex,
	}

	_, err := idx.RunInitFilesJob(context.Background(), initArgs)
	if err != nil {
		t.Fatalf("RunInitFilesJob failed: %v", err)
	}

	// Get initial digest
	entryName := FileEmbeddingName(workspaceID, "mod.go")
	entry1, err := store.Get(context.Background(), entryName, workspaceID)
	if err != nil {
		t.Fatalf("failed to get initial entry: %v", err)
	}
	if len(entry1.Result) == 0 {
		t.Fatalf("initial entry has empty result")
	}
	var result1 FileEmbeddingResult
	if err := json.Unmarshal(entry1.Result, &result1); err != nil {
		t.Fatalf("failed to unmarshal initial result: %v", err)
	}
	initialDigest := result1.Digest

	// Modify the file
	createTestFile(t, workspaceDir, "mod.go", "package main\n\nfunc new() {}\n")

	// Update via job
	updateArgs := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "mod.go", ChangeKind: ChangeKindModified},
		},
		Reason: ReasonPostReview,
	}

	result, err := idx.RunUpdateFilesJob(context.Background(), updateArgs)
	if err != nil {
		t.Fatalf("RunUpdateFilesJob failed: %v", err)
	}

	if result.Summary.FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed, got %d", result.Summary.FilesIndexed)
	}

	// Verify digest changed
	entry2, err := store.Get(context.Background(), entryName, workspaceID)
	if err != nil {
		t.Fatalf("failed to get updated entry: %v", err)
	}
	if len(entry2.Result) == 0 {
		t.Fatalf("updated entry has empty result")
	}
	var result2 FileEmbeddingResult
	if err := json.Unmarshal(entry2.Result, &result2); err != nil {
		t.Fatalf("failed to unmarshal updated result: %v", err)
	}

	if result2.Digest == initialDigest {
		t.Error("expected digest to change after modification")
	}
}

func TestRunInitFilesJob_FileNotFound(t *testing.T) {
	cfg := Config{Enabled: true}
	idx, _, _, workspaceID := setupTestIndexer(t, cfg)

	args := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "nonexistent.go"},
		},
		Reason: ReasonInitialIndex,
	}

	result, err := idx.RunInitFilesJob(context.Background(), args)
	if err != nil {
		t.Fatalf("RunInitFilesJob should not return error: %v", err)
	}

	if result.Summary.FilesIndexed != 0 {
		t.Errorf("expected 0 files indexed, got %d", result.Summary.FilesIndexed)
	}
	if len(result.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(result.Failures))
	}
	if result.Failures[0].File.Path != "nonexistent.go" {
		t.Errorf("expected failure for nonexistent.go, got %s", result.Failures[0].File.Path)
	}
}

func TestRunInitFilesJob_MaxFileKBSkipped(t *testing.T) {
	idx, _, workspaceDir, workspaceID := setupTestIndexer(t, Config{Enabled: true, MaxFileKB: 1})

	createTestFile(t, workspaceDir, "too-large.txt", strings.Repeat("x", 2*1024))

	args := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "too-large.txt"},
		},
		Reason: ReasonInitialIndex,
	}

	result, err := idx.RunInitFilesJob(context.Background(), args)
	if err != nil {
		t.Fatalf("RunInitFilesJob failed: %v", err)
	}
	if result.Summary.FilesSkipped != 1 {
		t.Fatalf("expected 1 skipped file, got indexed=%d skipped=%d failures=%d",
			result.Summary.FilesIndexed, result.Summary.FilesSkipped, len(result.Failures))
	}
	if len(result.Failures) != 0 {
		t.Fatalf("expected no failures, got %d", len(result.Failures))
	}
}

func TestRunInitFilesJob_ValidationError(t *testing.T) {
	cfg := Config{Enabled: true}
	idx, _, _, _ := setupTestIndexer(t, cfg)

	// Empty workspace ID
	args := JobArgs{
		WorkspaceID: "",
		Files: []JobFileInput{
			{Path: "test.go"},
		},
	}

	_, err := idx.RunInitFilesJob(context.Background(), args)
	if err == nil {
		t.Error("expected validation error for empty workspace_id")
	}
}

func TestRunInitFilesJob_MultipleFiles(t *testing.T) {
	cfg := Config{Enabled: true}
	idx, store, workspaceDir, workspaceID := setupTestIndexer(t, cfg)

	// Create multiple files
	createTestFile(t, workspaceDir, "a.go", "package a")
	createTestFile(t, workspaceDir, "b.py", "def b(): pass")
	createTestFile(t, workspaceDir, "c.rs", "fn c() {}")

	args := JobArgs{
		WorkspaceID: workspaceID,
		Files: []JobFileInput{
			{Path: "a.go"},
			{Path: "b.py"},
			{Path: "c.rs"},
		},
		Reason: ReasonManual,
	}

	result, err := idx.RunInitFilesJob(context.Background(), args)
	if err != nil {
		t.Fatalf("RunInitFilesJob failed: %v", err)
	}

	if result.Summary.FilesIndexed != 3 {
		t.Errorf("expected 3 files indexed, got %d", result.Summary.FilesIndexed)
	}

	// Verify each file was stored with correct language
	cases := []struct {
		path     string
		language string
	}{
		{"a.go", "go"},
		{"b.py", "python"},
		{"c.rs", "rust"},
	}

	for _, tc := range cases {
		entryName := FileEmbeddingName(workspaceID, tc.path)
		entry, err := store.Get(context.Background(), entryName, workspaceID)
		if err != nil {
			t.Errorf("failed to get entry for %s: %v", tc.path, err)
			continue
		}

		var fileResult FileEmbeddingResult
		if err := json.Unmarshal(entry.Result, &fileResult); err != nil {
			t.Errorf("failed to unmarshal result for %s: %v", tc.path, err)
			continue
		}

		if fileResult.Language != tc.language {
			t.Errorf("for %s: expected language %s, got %s", tc.path, tc.language, fileResult.Language)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		path     string
		expected string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.js", "javascript"},
		{"component.ts", "typescript"},
		{"lib.rs", "rust"},
		{"App.java", "java"},
		{"main.c", "c"},
		{"main.cpp", "cpp"},
		{"app.rb", "ruby"},
		{"README.md", "markdown"},
		{"config.json", "json"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"Cargo.toml", "toml"},
		{"script.sh", "shell"},
		{"unknown.xyz", "text"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := fsutil.DetectLanguage(tc.path)
			if got != tc.expected {
				t.Errorf("DetectLanguage(%q) = %q, want %q", tc.path, got, tc.expected)
			}
		})
	}
}

func TestComputeDigest(t *testing.T) {
	content := []byte("hello world")
	digest := computeDigest(content)

	if !containsStr(digest, "sha256:") {
		t.Errorf("expected digest to start with sha256:, got %s", digest)
	}

	// Same content should produce same digest
	digest2 := computeDigest(content)
	if digest != digest2 {
		t.Error("same content should produce same digest")
	}

	// Different content should produce different digest
	digest3 := computeDigest([]byte("different"))
	if digest == digest3 {
		t.Error("different content should produce different digest")
	}
}
