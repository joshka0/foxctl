package retrieval

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/rs/zerolog"
)

// errNotFound is a local error for mock store.
var errNotFound = errors.New("not found")

// mockMemoryStore implements storage.MemoryStore for testing.
type mockMemoryStore struct {
	entries map[string]storage.NamedEntry
}

func newMockStore() *mockMemoryStore {
	return &mockMemoryStore{
		entries: make(map[string]storage.NamedEntry),
	}
}

func (m *mockMemoryStore) Close() error {
	return nil
}

func (m *mockMemoryStore) Stats(ctx context.Context) (storage.MemoryStats, error) {
	return storage.MemoryStats{Named: int64(len(m.entries))}, nil
}

func (m *mockMemoryStore) Save(ctx context.Context, entry storage.NamedEntry) (storage.NamedEntry, error) {
	m.entries[entry.Name] = entry
	return entry, nil
}

func (m *mockMemoryStore) SaveFromResult(ctx context.Context, name, typ, workspace, summary string, result []byte) (storage.NamedEntry, error) {
	entry := storage.NamedEntry{
		Name:      name,
		Type:      typ,
		Workspace: workspace,
		Summary:   summary,
		Result:    result,
	}
	m.entries[name] = entry
	return entry, nil
}

func (m *mockMemoryStore) Get(ctx context.Context, name, workspace string) (storage.NamedEntry, error) {
	entry, ok := m.entries[name]
	if !ok {
		return storage.NamedEntry{}, errNotFound
	}
	return entry, nil
}

func (m *mockMemoryStore) List(ctx context.Context, workspace string, limit int) ([]storage.NamedEntry, error) {
	var entries []storage.NamedEntry
	for _, e := range m.entries {
		if e.Workspace == workspace {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func (m *mockMemoryStore) Delete(ctx context.Context, name, workspace string) error {
	delete(m.entries, name)
	return nil
}

func (m *mockMemoryStore) DeleteByNamePrefix(ctx context.Context, workspace, namePrefix string) (int, error) {
	count := 0
	for name := range m.entries {
		if len(name) >= len(namePrefix) && name[:len(namePrefix)] == namePrefix {
			delete(m.entries, name)
			count++
		}
	}
	return count, nil
}

func (m *mockMemoryStore) Search(ctx context.Context, workspace, query string, limit int) ([]storage.ScoredEntry, error) {
	var results []storage.ScoredEntry
	queryLower := query
	for _, e := range m.entries {
		if e.Workspace != workspace {
			continue
		}
		// Simple contains matching
		if contains(e.Name, queryLower) || contains(e.Summary, queryLower) {
			results = append(results, storage.ScoredEntry{
				Entry: e,
				Score: 1.0,
			})
		}
	}
	return results, nil
}

func (m *mockMemoryStore) Update(ctx context.Context, name, workspace string, summary, typ *string) (storage.NamedEntry, error) {
	entry, ok := m.entries[name]
	if !ok {
		return storage.NamedEntry{}, errNotFound
	}
	if summary != nil {
		entry.Summary = *summary
	}
	if typ != nil {
		entry.Type = *typ
	}
	m.entries[name] = entry
	return entry, nil
}

func (m *mockMemoryStore) Relevant(ctx context.Context, workspace string, limit int) ([]storage.ScoredEntry, error) {
	entries, err := m.List(ctx, workspace, limit)
	if err != nil {
		return nil, err
	}
	var scored []storage.ScoredEntry
	for _, e := range entries {
		scored = append(scored, storage.ScoredEntry{Entry: e, Score: 1.0})
	}
	return scored, nil
}

func (m *mockMemoryStore) SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]storage.ScoredEntry, error) {
	// Simple mock: return all entries in workspace with score 1.0
	entries, err := m.List(ctx, workspace, limit)
	if err != nil {
		return nil, err
	}
	var scored []storage.ScoredEntry
	for _, e := range entries {
		scored = append(scored, storage.ScoredEntry{Entry: e, Score: 1.0})
	}
	return scored, nil
}

func (m *mockMemoryStore) UpdateEmbedding(ctx context.Context, name, workspace string, embedding []float32) error {
	// Mock: no-op since we don't track embeddings in this mock
	return nil
}

// helper for simple contains check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// addSymbolToStore adds a symbol entry to the mock store.
func addSymbolToStore(store *mockMemoryStore, workspace, filePath, name string, kind symbol.Kind) {
	sym := symbol.Symbol{
		ID:        symbol.ID(filePath, name),
		FilePath:  filePath,
		Name:      name,
		Kind:      kind,
		Language:  "go",
		StartLine: 10,
	}
	result := symbol.Result{Symbol: sym}
	data, err := json.Marshal(result)
	if err != nil {
		panic(err) // Test helper - should never fail
	}

	entryName := symbol.EntryName(workspace, filePath, name)
	store.entries[entryName] = storage.NamedEntry{
		ID:        entryName,
		Name:      entryName,
		Type:      symbol.SymbolType,
		Workspace: workspace,
		Summary:   name,
		Result:    data,
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"the a an", nil}, // All stop words
		{"authentication", []string{"authentication"}},
		{"How does login work?", []string{"login", "work"}},
		{"getUserByID", []string{"getuserbyid"}},
		{"auth_handler middleware", []string{"auth_handler", "middleware"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := tokenize(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("tokenize(%q) = %v, want %v", tt.input, result, tt.expected)
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("tokenize(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestBuildSearchTerms(t *testing.T) {
	tests := []struct {
		question string
		want     int // Expected number of terms
	}{
		{"", 1}, // Falls back to the question itself
		{"How does authentication work?", 2},
		{"a the is", 1}, // All stop words, falls back to raw question
		{"getUserHandler authenticateRequest validateToken processLogin", 4},
	}

	for _, tt := range tests {
		t.Run(tt.question, func(t *testing.T) {
			terms := buildSearchTerms(tt.question)
			if len(terms) < 1 {
				t.Errorf("buildSearchTerms(%q) returned empty", tt.question)
			}
			if len(terms) > 6 {
				t.Errorf("buildSearchTerms(%q) returned %d terms, want <= 6", tt.question, len(terms))
			}
		})
	}
}

func TestScoreSymbol(t *testing.T) {
	sym := symbol.Symbol{
		Name:          "AuthenticateUser",
		Signature:     "func AuthenticateUser(ctx context.Context, credentials Credentials) error",
		Documentation: "AuthenticateUser validates user credentials against the database.",
		FilePath:      "internal/auth/handler.go",
		Kind:          symbol.KindFunction,
	}

	tests := []struct {
		tokens   []string
		minScore float64
	}{
		{[]string{"authenticate"}, 2.0},           // Name contains
		{[]string{"authenticateuser"}, 5.0},       // Exact name match
		{[]string{"credentials"}, 1.0},            // Signature match
		{[]string{"database"}, 0.5},               // Doc match
		{[]string{"auth"}, 2.0 + 1.0 + 0.5 + 0.3}, // Multiple matches
		{[]string{"unrelated"}, 0.1},              // No match, minimum
	}

	for _, tt := range tests {
		t.Run(tt.tokens[0], func(t *testing.T) {
			score := scoreSymbol(sym, tt.tokens)
			// Apply kind boost (function = 1.2x)
			if score < tt.minScore {
				t.Errorf("scoreSymbol with tokens %v = %v, want >= %v", tt.tokens, score, tt.minScore)
			}
		})
	}
}

func TestMergeCandidates(t *testing.T) {
	// Create candidates from different sources
	symbolCandidates := []Candidate{
		{Path: "auth/login.go", SymbolID: "auth/login.go:Login", Score: 0.9, Source: SourceSymbol},
		{Path: "auth/session.go", SymbolID: "auth/session.go:Session", Score: 0.7, Source: SourceSymbol},
	}
	semanticCandidates := []Candidate{
		{Path: "auth/login.go", Score: 0.8, Source: SourceSemantic},
		{Path: "auth/token.go", Score: 0.6, Source: SourceSemantic},
	}
	ripgrepCandidates := []Candidate{
		{Path: "auth/handler.go", Score: 0.5, Source: SourceRipgrep},
	}

	weights := map[string]float64{
		SourceSymbol:   1.0,
		SourceSemantic: 0.7,
		SourceRipgrep:  0.5,
	}

	merged := mergeCandidates([][]Candidate{symbolCandidates, semanticCandidates, ripgrepCandidates}, weights, 10)

	// Should have 4 unique paths
	if len(merged) != 4 {
		t.Errorf("mergeCandidates returned %d candidates, want 4", len(merged))
	}

	// auth/login.go should be first (appears in both symbol and semantic)
	if merged[0].Path != "auth/login.go" {
		t.Errorf("First candidate path = %q, want auth/login.go", merged[0].Path)
	}

	// auth/login.go should have source = "merged" since it came from multiple sources
	if merged[0].Source != "merged" {
		t.Errorf("Multi-source candidate source = %q, want merged", merged[0].Source)
	}

	// auth/login.go should preserve symbol metadata
	if merged[0].SymbolID == "" {
		t.Error("Merged candidate lost symbol metadata")
	}
}

func TestMergeCandidates_Limit(t *testing.T) {
	candidates := make([]Candidate, 20)
	for i := range candidates {
		candidates[i] = Candidate{
			Path:   "file" + string(rune('a'+i)) + ".go",
			Score:  float64(20-i) / 20.0,
			Source: SourceSymbol,
		}
	}

	weights := map[string]float64{SourceSymbol: 1.0}
	merged := mergeCandidates([][]Candidate{candidates}, weights, 5)

	if len(merged) != 5 {
		t.Errorf("mergeCandidates with limit 5 returned %d candidates", len(merged))
	}
}

func TestGeneratorGenerate_NoStore(t *testing.T) {
	logger := zerolog.Nop()
	gen := NewGenerator(nil, nil, "/tmp/workspace", logger)

	result, err := gen.Generate(context.Background(), "default", "test query", DefaultOptions())
	if err != nil {
		t.Fatalf("Generate with nil store returned error: %v", err)
	}

	if len(result.Candidates) != 0 {
		t.Errorf("Generate with nil store returned %d candidates, want 0", len(result.Candidates))
	}
}

func TestGeneratorGenerate_WithSymbols(t *testing.T) {
	store := newMockStore()
	addSymbolToStore(store, "default", "auth/login.go", "Login", symbol.KindFunction)
	addSymbolToStore(store, "default", "auth/logout.go", "Logout", symbol.KindFunction)
	addSymbolToStore(store, "default", "user/profile.go", "GetProfile", symbol.KindFunction)

	logger := zerolog.Nop()
	gen := NewGenerator(store, nil, "/tmp/workspace", logger)

	result, err := gen.Generate(context.Background(), "default", "login authentication", DefaultOptions())
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	// Should find at least the Login symbol
	if result.Stats.SymbolCount == 0 {
		t.Error("Expected at least one symbol match")
	}
}

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"symbol://default/auth/login.go:Login", "auth/login.go"},
		{"symbol://workspace1/internal/handler.go:Handle", "internal/handler.go"},
		{"file://default/auth/token.go", "auth/token.go"},
		{"semantic://default/pkg/util.go", "pkg/util.go"},
		{"random-entry-name", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFilePath(tt.name)
			if got != tt.want {
				t.Errorf("extractFilePath(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		question string
		minCount int
		maxCount int
	}{
		{"How does authentication work?", 1, 5},
		{"", 0, 0},
		{"the is a", 0, 0}, // All stop words
		{"getUserById validateToken processRequest", 3, 5},
	}

	for _, tt := range tests {
		t.Run(tt.question, func(t *testing.T) {
			keywords := extractKeywords(tt.question)
			if len(keywords) < tt.minCount || len(keywords) > tt.maxCount {
				t.Errorf("extractKeywords(%q) returned %d keywords, want %d-%d",
					tt.question, len(keywords), tt.minCount, tt.maxCount)
			}
		})
	}
}

func TestBuildRipgrepPattern(t *testing.T) {
	tests := []struct {
		keywords []string
		want     string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"auth"}, "auth"},
		{[]string{"auth", "login"}, "auth|login"},
		{[]string{"func.name"}, `func\.name`}, // Escaped special char
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := buildRipgrepPattern(tt.keywords)
			if got != tt.want {
				t.Errorf("buildRipgrepPattern(%v) = %q, want %q", tt.keywords, got, tt.want)
			}
		})
	}
}

func TestOptions(t *testing.T) {
	// Test default options
	opts := DefaultOptions()
	if opts.MaxSymbolCandidates != 30 {
		t.Errorf("DefaultOptions MaxSymbolCandidates = %d, want 30", opts.MaxSymbolCandidates)
	}
	if !opts.EnableSymbols {
		t.Error("DefaultOptions should have EnableSymbols = true")
	}

	// Test quick options
	quick := QuickOptions()
	if quick.MaxTotalCandidates >= opts.MaxTotalCandidates {
		t.Error("QuickOptions should have smaller limits than DefaultOptions")
	}

	// Test thorough options
	thorough := ThoroughOptions()
	if thorough.MaxTotalCandidates <= opts.MaxTotalCandidates {
		t.Error("ThoroughOptions should have larger limits than DefaultOptions")
	}

	// Test WithMaxCandidates
	adjusted := opts.WithMaxCandidates(20)
	if adjusted.MaxTotalCandidates != 20 {
		t.Errorf("WithMaxCandidates(20) = %d, want 20", adjusted.MaxTotalCandidates)
	}
	// Original should be unchanged
	if opts.MaxTotalCandidates != 50 {
		t.Error("WithMaxCandidates mutated original options")
	}
}
