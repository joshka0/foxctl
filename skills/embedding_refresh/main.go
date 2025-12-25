// Package main implements the embedding/refresh skill for updating embeddings.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/rs/zerolog"
)

const (
	command       = "embedding/refresh"
	geminiModel   = "gemini-embedding-001"
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
)

// Input is the skill input schema.
type Input struct {
	// Scope is the type of item to refresh: "memory", "symbol", or "session".
	Scope string `json:"scope"`

	// Name is the identifier for the item (memory name, symbol ID, or session ID).
	Name string `json:"name"`

	// Workspace is the workspace context (optional, defaults to "default").
	Workspace string `json:"workspace,omitempty"`

	// DryRun if true, generates embedding but doesn't store it.
	DryRun bool `json:"dry_run,omitempty"`
}

// Output is the skill output.
type Output struct {
	Scope      string `json:"scope"`
	Name       string `json:"name"`
	Status     string `json:"status"` // "refreshed", "not_found", "no_content", "error", "dry_run"
	Dimensions int    `json:"dimensions,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Message    string `json:"message"`
	Hint       string `json:"hint,omitempty"`
}

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout); err != nil {
		env := envelope.Error(command, "ERUNTIME", err.Error(), nil)
		_ = json.NewEncoder(os.Stdout).Encode(env)
		os.Exit(1)
	}
}

func run(ctx context.Context, r io.Reader, w io.Writer) error {
	var input Input
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return fmt.Errorf("parse input: %w", err)
	}

	// Validate input
	if input.Scope == "" {
		return fmt.Errorf("scope is required (memory, symbol, or session)")
	}
	if input.Name == "" {
		return fmt.Errorf("name is required")
	}
	if input.Workspace == "" {
		input.Workspace = "default"
	}

	start := time.Now()
	output := Output{
		Scope: input.Scope,
		Name:  input.Name,
	}

	// Check for API key - prefer Voyage, fall back to Gemini
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if voyageKey == "" && geminiKey == "" && !input.DryRun {
		output.Status = "error"
		output.Message = "No embedding API key set"
		output.Hint = "Set VOYAGE_API_KEY (preferred) or GEMINI_API_KEY"
		output.DurationMs = time.Since(start).Milliseconds()
		return json.NewEncoder(w).Encode(envelope.OK(command, output))
	}

	// Get content based on scope
	var content string
	var err error

	switch input.Scope {
	case "memory":
		content, err = getMemoryContent(ctx, input.Name, input.Workspace)
	case "symbol":
		content, err = getSymbolContent(ctx, input.Name, input.Workspace)
	case "session":
		content, err = getSessionContent(ctx, input.Name)
	default:
		return fmt.Errorf("invalid scope: %s (must be memory, symbol, or session)", input.Scope)
	}

	if err != nil {
		if errors.Is(err, errNotFound) {
			output.Status = "not_found"
			output.Message = fmt.Sprintf("%s not found: %s", input.Scope, input.Name)
			output.DurationMs = time.Since(start).Milliseconds()
			return json.NewEncoder(w).Encode(envelope.OK(command, output))
		}
		return fmt.Errorf("get content: %w", err)
	}

	if content == "" {
		output.Status = "no_content"
		output.Message = fmt.Sprintf("%s has no content to embed", input.Scope)
		output.DurationMs = time.Since(start).Milliseconds()
		return json.NewEncoder(w).Encode(envelope.OK(command, output))
	}

	// Dry run - just validate content exists
	if input.DryRun {
		output.Status = "dry_run"
		output.Message = fmt.Sprintf("Would generate embedding for %s (content length: %d)", input.Scope, len(content))
		output.DurationMs = time.Since(start).Milliseconds()
		return json.NewEncoder(w).Encode(envelope.OK(command, output))
	}

	// Generate embedding - prefer Voyage, fall back to Gemini (both rate-limited)
	var embedding []float32
	var embeddingModel string

	if voyageKey != "" {
		vp, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey:        voyageKey,
			RateLimitWait: boolPtr(true),
		})
		if err != nil {
			output.Status = "error"
			output.Message = fmt.Sprintf("voyage provider failed: %v", err)
			output.DurationMs = time.Since(start).Milliseconds()
			return json.NewEncoder(w).Encode(envelope.OK(command, output))
		}
		embedding, err = vp.Embed(ctx, content)
		if err != nil {
			output.Status = "error"
			output.Message = fmt.Sprintf("embedding generation failed: %v", err)
			output.DurationMs = time.Since(start).Milliseconds()
			return json.NewEncoder(w).Encode(envelope.OK(command, output))
		}
		embeddingModel = vp.Model()
	} else {
		// Use GeminiProvider with rate limiting
		gp, err := semantic.NewGeminiProvider(semantic.GeminiConfig{
			APIKey:        geminiKey,
			RateLimitWait: boolPtr(true),
		})
		if err != nil {
			output.Status = "error"
			output.Message = fmt.Sprintf("gemini provider failed: %v", err)
			output.DurationMs = time.Since(start).Milliseconds()
			return json.NewEncoder(w).Encode(envelope.OK(command, output))
		}
		embedding, err = gp.Embed(ctx, content)
		if err != nil {
			output.Status = "error"
			output.Message = fmt.Sprintf("embedding generation failed: %v", err)
			output.DurationMs = time.Since(start).Milliseconds()
			return json.NewEncoder(w).Encode(envelope.OK(command, output))
		}
		embeddingModel = gp.Model()
	}

	// Store embedding based on scope
	switch input.Scope {
	case "memory":
		err = storeMemoryEmbedding(ctx, input.Name, input.Workspace, embedding)
	case "symbol":
		err = storeSymbolEmbedding(ctx, input.Name, input.Workspace, embedding)
	case "session":
		err = storeSessionEmbedding(ctx, input.Name, embedding, embeddingModel)
	}

	if err != nil {
		output.Status = "error"
		output.Message = fmt.Sprintf("failed to store embedding: %v", err)
		output.DurationMs = time.Since(start).Milliseconds()
		return json.NewEncoder(w).Encode(envelope.OK(command, output))
	}

	output.Status = "refreshed"
	output.Dimensions = len(embedding)
	output.Message = fmt.Sprintf("Refreshed %s embedding (%d dimensions)", input.Scope, len(embedding))
	output.DurationMs = time.Since(start).Milliseconds()

	return json.NewEncoder(w).Encode(envelope.OK(command, output))
}

var errNotFound = fmt.Errorf("not found")

// getMemoryContent retrieves content from a named memory entry.
func getMemoryContent(ctx context.Context, name, workspace string) (string, error) {
	root := getStorageRoot()
	store, err := memory.Open(ctx, root, filepath.Join(root, "cas"))
	if err != nil {
		return "", fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close() //nolint:errcheck

	entry, err := store.Get(ctx, name, workspace)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			return "", errNotFound
		}
		return "", err
	}

	// Use summary + name as embedding text
	content := entry.Summary
	if content == "" {
		content = entry.Name
	}
	return content, nil
}

// getSymbolContent retrieves content from a code symbol.
func getSymbolContent(ctx context.Context, symbolID, workspaceID string) (string, error) {
	// Symbol embeddings are handled by the embedding_queue/embedding_worker pipeline
	// This is a placeholder for direct symbol refresh if needed
	// For now, return not found to indicate symbols should use the queue
	return "", errNotFound
}

// getSessionContent retrieves content from a session.
func getSessionContent(ctx context.Context, sessionID string) (string, error) {
	root := getStorageRoot()
	store, err := sessions.Open(ctx, root)
	if err != nil {
		return "", fmt.Errorf("open sessions store: %w", err)
	}
	defer store.Close() //nolint:errcheck

	session, err := store.Get(ctx, sessionID)
	if err != nil {
		return "", errNotFound
	}

	// Build embedding text from session summary data
	var parts []string
	if session.Summary != "" {
		parts = append(parts, "Summary: "+session.Summary)
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
	if len(session.Tags) > 0 {
		parts = append(parts, "Tags: "+joinStrings(session.Tags, ", "))
	}

	if len(parts) == 0 {
		return "", errNotFound
	}

	return joinStrings(parts, "\n"), nil
}

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
func storeMemoryEmbedding(ctx context.Context, name, workspace string, embedding []float32) error {
	// Load config to check for Turso vector support
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Get provider and model from config/env
	model := os.Getenv("EMBEDDING_MODEL")
	if model == "" {
		model = cfg.Embedding.Model
	}
	if model == "" {
		model = geminiModel
	}

	// Route to Turso if vector is enabled
	if cfg.Database.Driver == "turso" && cfg.Database.Vector.Enabled {
		return storeMemoryEmbeddingTurso(ctx, cfg, name, workspace, embedding, model)
	}

	// Fallback to SQLite BM25-only path
	return storeMemoryEmbeddingSQLite(ctx, name, workspace, embedding, model)
}

// storeMemoryEmbeddingTurso stores embedding via Turso's native vector support.
func storeMemoryEmbeddingTurso(ctx context.Context, cfg config.Config, name, workspace string, embedding []float32, model string) error {
	// Get expected dimensions from config
	expectedDims := cfg.Embedding.Dimensions
	if expectedDims == 0 {
		expectedDims = 3072 // default for gemini-embedding-001
	}

	// Validate dimensions before attempting Turso store
	if len(embedding) != expectedDims {
		return fmt.Errorf("dimension mismatch: got %d, expected %d from config; update embedding.model or embedding.dimensions", len(embedding), expectedDims)
	}

	// First get the memory entry from SQLite store to get its data
	root := getStorageRoot()
	sqliteStore, err := memory.Open(ctx, root, filepath.Join(root, "cas"))
	if err != nil {
		return fmt.Errorf("open sqlite store: %w", err)
	}
	defer sqliteStore.Close() //nolint:errcheck

	entry, err := sqliteStore.Get(ctx, name, workspace)
	if err != nil {
		return fmt.Errorf("get memory entry: %w", err)
	}

	// Open Turso store with vector support
	tursoCfg := dbdriver.TursoConfig{
		URL:              cfg.Database.Turso.URL,
		AuthToken:        cfg.Database.Turso.AuthToken,
		VectorDimensions: expectedDims,
	}
	tursoStore, err := memory.OpenTurso(ctx, tursoCfg)
	if err != nil {
		return fmt.Errorf("open turso store: %w", err)
	}
	defer tursoStore.Close() //nolint:errcheck

	// Save with embedding to Turso
	_, err = tursoStore.SaveWithEmbedding(ctx, entry, embedding, model)
	if err != nil {
		return fmt.Errorf("save with embedding: %w", err)
	}

	return nil
}

// storeMemoryEmbeddingSQLite stores embedding via SQLite (BM25-only, no vector search).
func storeMemoryEmbeddingSQLite(ctx context.Context, name, workspace string, embedding []float32, model string) error {
	root := getStorageRoot()
	store, err := memory.Open(ctx, root, filepath.Join(root, "cas"))
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer store.Close() //nolint:errcheck

	provider := "gemini"
	dimensions := len(embedding)

	// Validate dimensions match existing metadata (if any)
	if err := store.ValidateEmbeddingDimensions(ctx, workspace, dimensions); err != nil {
		return err
	}

	// Store the embedding
	if err := store.UpdateEmbedding(ctx, name, workspace, embedding); err != nil {
		return err
	}

	// Update embedding metadata for workspace
	meta := memory.EmbeddingMetadata{
		Workspace:  workspace,
		Provider:   provider,
		Model:      model,
		Dimensions: dimensions,
	}
	if err := store.SetEmbeddingMetadata(ctx, meta); err != nil {
		return fmt.Errorf("store metadata: %w", err)
	}

	return nil
}

// storeSymbolEmbedding stores an embedding for a code symbol.
func storeSymbolEmbedding(ctx context.Context, symbolID, workspaceID string, embedding []float32) error {
	// Symbol embeddings are stored via embedding_queue
	// This would need to interact with that store
	return fmt.Errorf("symbol embedding refresh should use embedding/queue")
}

// storeSessionEmbedding stores an embedding for a session.
func storeSessionEmbedding(ctx context.Context, sessionID string, embedding []float32, model string) error {
	root := getStorageRoot()
	store, err := sessions.Open(ctx, root)
	if err != nil {
		return fmt.Errorf("open sessions store: %w", err)
	}
	defer store.Close() //nolint:errcheck

	// Serialize embedding as binary float32 (little-endian)
	embeddingBytes := serializeEmbedding(embedding)
	return store.SetEmbedding(ctx, sessionID, embeddingBytes, model)
}

// serializeEmbedding converts float32 slice to bytes.
func serializeEmbedding(embedding []float32) []byte {
	buf := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		bits := math.Float32bits(v)
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}
	return buf
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

func getStorageRoot() string {
	log := zerolog.New(os.Stderr).With().Str("skill", command).Logger()

	cfg, err := config.Load(context.Background())
	if err != nil {
		log.Warn().Err(err).Msg("failed to load config, using fallbacks")
	}
	if err == nil && cfg.Storage.Root != "" {
		return cfg.Storage.Root
	}

	if root := os.Getenv("AGENTCTL_HOME"); root != "" {
		return filepath.Join(root, "storage")
	}

	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agentctl", "storage")
}
