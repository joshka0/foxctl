// Package main implements the embedding/refresh skill for updating embeddings.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/vector"
)

const (
	command       = "embedding/refresh"
	geminiModel   = "gemini-embedding-001"
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

// Input is the skill input schema for embedding/refresh operations.
type Input struct {
	// Scope is the type of item to refresh: "memory", "symbol", or "session".
	Scope string `json:"scope" validate:"required,oneof=memory symbol session"`

	// Name is the identifier for the item (memory name, symbol ID, or session ID).
	Name string `json:"name" validate:"required"`

	// Workspace is the workspace context (optional, defaults to detected workspace).
	Workspace string `json:"workspace,omitempty"`

	// DryRun if true, generates embedding but doesn't store it.
	DryRun bool `json:"dry_run,omitempty"`
}

// Output is the skill output for embedding/refresh operations.
type Output struct {
	Scope      string `json:"scope"`
	Name       string `json:"name"`
	Status     string `json:"status"` // "refreshed", "not_found", "no_content", "error", "dry_run"
	Dimensions int    `json:"dimensions,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Message    string `json:"message"`
	Hint       string `json:"hint,omitempty"`
}

// main is the skill entry point for embedding/refresh.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates embedding refresh for memories, symbols, and sessions.
//
// Index:
//   Purpose: Regenerate embeddings for specific items with content formatting and storage routing
//   Flow: validate input → get content → generate embedding → store based on scope → emit results
//   SideEffects: embedding API calls; database updates; content formatting; dimension validation
//   FailureModes: missing API keys, item not found, no content, embedding failures, storage errors
//   Observability: emits refresh status with dimensions, timing, and detailed error messages
//   Related: getMemoryContent, getSymbolContent, getSessionContent, formatMemoryContent, formatSessionContent
//   Keywords: embedding/refresh, regeneration, memories, symbols, sessions, vector_search
//
// [[domain:embedding-regeneration]]
// [[protocol:scope-aware-embedding-storage]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Set default workspace
	in.Workspace = workspaceutil.Resolve(in.Workspace, "", rc.Workspace)

	start := time.Now()
	output := Output{
		Scope: in.Scope,
		Name:  in.Name,
	}

	// Check for API key - prefer Voyage, fall back to Gemini
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if voyageKey == "" && geminiKey == "" && !in.DryRun {
		output.Status = "error"
		output.Message = "No embedding API key set"
		output.Hint = "Set VOYAGE_API_KEY (preferred) or GEMINI_API_KEY"
		output.DurationMs = time.Since(start).Milliseconds()
		return skillout.Emit(rc, command, output)
	}

	// Get content based on scope
	var content string
	var err error

	switch in.Scope {
	case "memory":
		content, err = getMemoryContent(ctx, rc, in.Name, in.Workspace)
	case "symbol":
		content, err = getSymbolContent(ctx, in.Name, in.Workspace)
	case "session":
		content, err = getSessionContent(ctx, rc, in.Name)
	}

	if err != nil {
		if errors.Is(err, errNotFound) {
			output.Status = "not_found"
			output.Message = fmt.Sprintf("%s not found: %s", in.Scope, in.Name)
			output.DurationMs = time.Since(start).Milliseconds()
			return skillout.Emit(rc, command, output)
		}
		return skillerr.Runtime("get content", skillerr.WithCause(err))
	}

	if content == "" {
		output.Status = "no_content"
		output.Message = fmt.Sprintf("%s has no content to embed", in.Scope)
		output.DurationMs = time.Since(start).Milliseconds()
		return skillout.Emit(rc, command, output)
	}

	// Dry run - just validate content exists
	if in.DryRun {
		output.Status = "dry_run"
		output.Message = fmt.Sprintf("Would generate embedding for %s (content length: %d)", in.Scope, len(content))
		output.DurationMs = time.Since(start).Milliseconds()
		return skillout.Emit(rc, command, output)
	}

	scope := semantic.ScopeSymbols
	switch in.Scope {
	case "memory":
		scope = semantic.ScopeMemory
	case "session":
		scope = semantic.ScopeSessions
	}
	embedder, err := semantic.NewEmbedderFromConfig(
		scope,
		rc.Config,
		semantic.WithVoyageKey(voyageKey),
		semantic.WithGeminiKey(geminiKey),
		skillmain.EmbeddingGuard(rc),
	)
	if err != nil {
		output.Status = "error"
		output.Message = fmt.Sprintf("embedding provider failed: %v", err)
		output.DurationMs = time.Since(start).Milliseconds()
		return skillout.Emit(rc, command, output)
	}

	embeddingResult, err := embedder.Embed(ctx, content)
	if err != nil {
		output.Status = "error"
		output.Message = fmt.Sprintf("embedding generation failed: %v", err)
		output.DurationMs = time.Since(start).Milliseconds()
		return skillout.Emit(rc, command, output)
	}

	embedding := embeddingResult.Vec
	embeddingModel := embeddingResult.Model

	// Store embedding based on scope
	switch in.Scope {
	case "memory":
		err = storeMemoryEmbedding(ctx, rc, in.Name, in.Workspace, embedding)
	case "symbol":
		err = storeSymbolEmbedding(ctx, in.Name, in.Workspace, embedding)
	case "session":
		err = storeSessionEmbedding(ctx, rc, in.Name, embedding, embeddingModel)
	}

	if err != nil {
		output.Status = "error"
		output.Message = fmt.Sprintf("failed to store embedding: %v", err)
		output.DurationMs = time.Since(start).Milliseconds()
		return skillout.Emit(rc, command, output)
	}

	output.Status = "refreshed"
	output.Dimensions = len(embedding)
	output.Message = fmt.Sprintf("Refreshed %s embedding (%d dimensions)", in.Scope, len(embedding))
	output.DurationMs = time.Since(start).Milliseconds()

	return skillout.Emit(rc, command, output)
}

var errNotFound = errors.New("not found")

// getMemoryContent retrieves content from a named memory entry.
// Format: [Jan 2026] [type] Summary text
// This enables natural language date/type searches like "January gotchas".
func getMemoryContent(ctx context.Context, rc *skillmain.RunContext, name, workspace string) (string, error) {
	store, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		return "", skillerr.WrapIO("open memory store", err)
	}
	defer store.Close() //nolint:errcheck

	entry, err := store.Get(ctx, name, workspace)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			return "", errNotFound
		}
		return "", err
	}

	return formatMemoryContent(entry), nil
}

// formatMemoryContent builds embedding text with date and type prefixes.
// Format: [Jan 2026] [gotcha] Summary text here
func formatMemoryContent(entry storage.NamedEntry) string {
	// Date prefix from creation time
	dateStr := entry.CreatedAt.Format("Jan 2006")

	// Type prefix (default to "note" if empty)
	typeStr := entry.Type
	if typeStr == "" {
		typeStr = "note"
	}

	// Content from summary or name
	content := entry.Summary
	if content == "" {
		content = entry.Name
	}

	// Format: [Jan 2026] [gotcha] Summary
	return fmt.Sprintf("[%s] [%s] %s", dateStr, typeStr, content)
}

// getSymbolContent retrieves content from a code symbol.
func getSymbolContent(ctx context.Context, symbolID, workspaceID string) (string, error) {
	// Symbol embeddings are handled by the embedding_queue/embedding_worker pipeline
	// This is a placeholder for direct symbol refresh if needed
	// For now, return not found to indicate symbols should use the queue
	return "", errNotFound
}

// getSessionContent retrieves content from a session.
// Format: [Jan 2, 2026] [activity] Summary...
// This enables natural language date/activity searches like "January debugging sessions".
func getSessionContent(ctx context.Context, rc *skillmain.RunContext, sessionID string) (string, error) {
	store, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return "", skillerr.WrapIO("open sessions store", err)
	}

	session, err := store.Get(ctx, sessionID)
	if err != nil {
		return "", errNotFound
	}

	return formatSessionContent(session), nil
}

// formatSessionContent builds embedding text with date and activity prefixes.
// Format: [Jan 2, 2026] [debugging] Summary. Accomplished: ... Files: ...
func formatSessionContent(session storage.Session) string {
	var parts []string

	// Date prefix from session start time
	dateStr := session.StartedAt.Format("Jan 2, 2006")

	// Activity type inferred from tags
	activity := inferActivityType(session.Tags)

	// Header with date and activity
	parts = append(parts, fmt.Sprintf("[%s] [%s]", dateStr, activity))

	if session.Summary != "" {
		parts = append(parts, session.Summary)
	}
	if len(session.Accomplished) > 0 {
		parts = append(parts, "Accomplished: "+joinStrings(session.Accomplished, "; "))
	}
	if len(session.Decisions) > 0 {
		parts = append(parts, "Decisions: "+joinStrings(session.Decisions, "; "))
	}
	if len(session.Gotchas) > 0 {
		parts = append(parts, "Gotchas: "+joinStrings(session.Gotchas, "; "))
	}
	if len(session.KeyFiles) > 0 {
		parts = append(parts, "Files: "+joinStrings(session.KeyFiles, ", "))
	}
	if len(session.Tags) > 0 {
		parts = append(parts, "Topics: "+joinStrings(session.Tags, ", "))
	}

	return joinStrings(parts, "\n")
}

// inferActivityType extracts an activity type from session tags.
// Maps common tags to activity categories for searchability.
func inferActivityType(tags []string) string {
	for _, tag := range tags {
		lower := strings.ToLower(tag)
		switch {
		case strings.Contains(lower, "debug"):
			return "debugging"
		case strings.Contains(lower, "fix") || strings.Contains(lower, "bug"):
			return "bug-fix"
		case strings.Contains(lower, "feature") || strings.Contains(lower, "implement"):
			return "feature"
		case strings.Contains(lower, "refactor"):
			return "refactoring"
		case strings.Contains(lower, "test"):
			return "testing"
		case strings.Contains(lower, "doc"):
			return "documentation"
		case strings.Contains(lower, "review"):
			return "code-review"
		case strings.Contains(lower, "setup") || strings.Contains(lower, "config"):
			return "setup"
		}
	}
	return "development"
}

// joinStrings concatenates string slice with separator.
func joinStrings(s []string, sep string) string {
	if len(s) == 0 {
		return ""
	}
	result := s[0]
	for i := 1; i < len(s); i++ {
		result += sep + s[i]
	}
	return result
}

// storeMemoryEmbedding stores an embedding for a memory entry.
// Routes to Turso with vector support when enabled, otherwise uses SQLite.
func storeMemoryEmbedding(ctx context.Context, rc *skillmain.RunContext, name, workspace string, embedding []float32) error {
	cfg := rc.Config

	// Get provider and model from config/env
	model := os.Getenv("EMBEDDING_MODEL")
	if model == "" {
		model = semantic.ResolveModelForScope(semantic.ScopeMemory, cfg)
	}
	if model == "" {
		model = geminiModel
	}

	// Route to Turso if vector is enabled
	if cfg.Database.Driver == "turso" && cfg.Database.Vector.Enabled {
		return storeMemoryEmbeddingTurso(ctx, rc, name, workspace, embedding, model)
	}

	// Fallback to SQLite BM25-only path
	return storeMemoryEmbeddingSQLite(ctx, rc, name, workspace, embedding, model)
}

// storeMemoryEmbeddingTurso stores embedding via Turso's native vector support.
func storeMemoryEmbeddingTurso(ctx context.Context, rc *skillmain.RunContext, name, workspace string, embedding []float32, model string) error {
	cfg := rc.Config

	// Get expected dimensions from config, with model-specific fallbacks
	expectedDims := cfg.Embedding.Dimensions
	if expectedDims == 0 {
		// Model-specific dimension defaults
		switch model {
		case "gemini-embedding-001":
			expectedDims = 3072
		case "text-embedding-004":
			expectedDims = 768
		default:
			expectedDims = dbdriver.GetDefaultVectorDimensions()
		}
	}

	// Validate dimensions before attempting Turso store
	if len(embedding) != expectedDims {
		return skillerr.Validationf("dimension mismatch: got %d, expected %d from config; update embedding.model or embedding.dimensions", len(embedding), expectedDims)
	}

	// First get the memory entry from SQLite store to get its data
	sqliteStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return skillerr.WrapIO("open sqlite store", err)
	}
	defer sqliteStore.Close() //nolint:errcheck

	entry, err := sqliteStore.Get(ctx, name, workspace)
	if err != nil {
		return skillerr.WrapIO("get memory entry", err)
	}

	// Open Turso store with vector support
	tursoCfg := dbdriver.TursoConfig{
		URL:              cfg.Database.Turso.URL,
		AuthToken:        cfg.Database.Turso.AuthToken,
		VectorDimensions: expectedDims,
	}
	tursoStore, err := memory.OpenTurso(ctx, tursoCfg)
	if err != nil {
		return skillerr.WrapIO("open turso store", err)
	}
	defer tursoStore.Close() //nolint:errcheck

	// Save with embedding to Turso
	_, err = tursoStore.SaveWithEmbedding(ctx, entry, embedding, model)
	if err != nil {
		return skillerr.WrapIO("save with embedding", err)
	}

	return nil
}

// storeMemoryEmbeddingSQLite stores embedding via SQLite (BM25-only, no vector search).
func storeMemoryEmbeddingSQLite(ctx context.Context, rc *skillmain.RunContext, name, workspace string, embedding []float32, model string) error {
	store, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		return skillerr.WrapIO("open memory store", err)
	}
	defer store.Close() //nolint:errcheck

	provider := "gemini"
	dimensions := len(embedding)

	// Validate dimensions match existing metadata (if any)
	if err := store.ValidateEmbeddingDimensions(ctx, workspace, dimensions); err != nil {
		return skillerr.WrapValidation("validate embedding dimensions", err)
	}

	// Store the embedding
	if err := store.UpdateEmbedding(ctx, name, workspace, embedding); err != nil {
		return skillerr.WrapIO("update embedding", err)
	}

	// Update embedding metadata for workspace
	meta := memory.EmbeddingMetadata{
		Workspace:  workspace,
		Provider:   provider,
		Model:      model,
		Dimensions: dimensions,
	}
	if err := store.SetEmbeddingMetadata(ctx, meta); err != nil {
		return skillerr.WrapIO("store metadata", err)
	}

	return nil
}

// storeSymbolEmbedding stores an embedding for a code symbol.
func storeSymbolEmbedding(ctx context.Context, symbolID, workspaceID string, embedding []float32) error {
	// Symbol embeddings are stored via embedding_queue
	// This would need to interact with that store
	return skillerr.Validation("symbol embedding refresh should use embedding/queue")
}

// storeSessionEmbedding stores an embedding for a session.
func storeSessionEmbedding(ctx context.Context, rc *skillmain.RunContext, sessionID string, embedding []float32, model string) error {
	store, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.WrapIO("open sessions store", err)
	}

	// Serialize embedding as binary float32 (little-endian)
	embeddingBytes := vector.SerializeF32(embedding)
	return store.SetEmbedding(ctx, sessionID, embeddingBytes, model)
}

// boolPtr returns a pointer to a bool value.
