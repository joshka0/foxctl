package updater

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/contextbuffer"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// SessionStoreAdapter adapts the sessions.Store to the SessionProvider interface.
type SessionStoreAdapter struct {
	store     *sessions.Store
	workspace string // Optional workspace filter
}

// NewSessionStoreAdapter creates a new session store adapter.
func NewSessionStoreAdapter(store *sessions.Store, workspace string) *SessionStoreAdapter {
	return &SessionStoreAdapter{
		store:     store,
		workspace: workspace,
	}
}

// ActiveSessions returns IDs of currently active (running) sessions.
func (a *SessionStoreAdapter) ActiveSessions(ctx context.Context) ([]string, error) {
	// List sessions and filter by running status
	opts := sessions.ListOptions{
		Limit: 100,
	}
	if a.workspace != "" {
		opts.WorkspacePath = a.workspace
	}

	sessionList, err := a.store.List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list active sessions: %w", err)
	}

	// Filter for running sessions
	var ids []string
	for _, s := range sessionList {
		if s.Status == storage.SessionStatusRunning {
			ids = append(ids, s.ID)
		}
	}
	return ids, nil
}

// RecentTurns returns the most recent turns for a session.
func (a *SessionStoreAdapter) RecentTurns(ctx context.Context, sessionID string, limit int) ([]Turn, error) {
	opts := sessions.TurnListOptions{
		SessionID: sessionID,
		Limit:     limit,
	}

	turnList, err := a.store.GetTurns(ctx, sessionID, opts)
	if err != nil {
		return nil, fmt.Errorf("get turns: %w", err)
	}

	// Convert to updater.Turn format
	turns := make([]Turn, len(turnList))
	for i, t := range turnList {
		// Extract file paths from tool calls and files_touched
		var filePaths []string
		filePaths = append(filePaths, t.FilesTouched...)

		// Determine tool name from tool calls
		var toolName string
		if len(t.ToolCalls) > 0 {
			toolName = t.ToolCalls[0].Name
		}

		turns[i] = Turn{
			Role:      t.Role,
			Content:   t.ContentPreview,
			ToolName:  toolName,
			FilePaths: filePaths,
		}
	}

	return turns, nil
}

// LastTurnID returns the ID of the most recent turn for a session.
func (a *SessionStoreAdapter) LastTurnID(ctx context.Context, sessionID string) (string, error) {
	// GetTurns orders by turn_index ASC, so we need to get enough turns
	// and return the last one (highest index = most recent).
	opts := sessions.TurnListOptions{
		SessionID: sessionID,
		Limit:     100, // Get enough to find the most recent
	}

	turns, err := a.store.GetTurns(ctx, sessionID, opts)
	if err != nil {
		return "", fmt.Errorf("get last turn: %w", err)
	}

	if len(turns) == 0 {
		return "", nil
	}

	// Return the last turn (highest index since ordered ASC)
	return turns[len(turns)-1].ID, nil
}

// GetSessionWorkspace returns the workspace path for a session.
func (a *SessionStoreAdapter) GetSessionWorkspace(ctx context.Context, sessionID string) (string, error) {
	session, err := a.store.Get(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("get session: %w", err)
	}
	return session.WorkspacePath, nil
}

// MemoryStoreAdapter adapts the memory store to the MemorySearcher interface.
// It uses semantic search via embeddings when available, falling back to text search.
type MemoryStoreAdapter struct {
	store     storage.MemoryStore
	embedder  semantic.EmbeddingProvider
	workspace string
	logger    *slog.Logger
}

// NewMemoryStoreAdapter creates a new memory store adapter.
// Pass an embedder for semantic search, or nil to use text-based search.
func NewMemoryStoreAdapter(store storage.MemoryStore, embedder semantic.EmbeddingProvider, workspace string, logger *slog.Logger) *MemoryStoreAdapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryStoreAdapter{
		store:     store,
		embedder:  embedder,
		workspace: workspace,
		logger:    logger.With("component", "memory-adapter"),
	}
}

// SearchByQuery searches memories using semantic similarity.
// Uses embedding-based search when embedder is available, otherwise falls back to text.
// The workspace parameter scopes the search; if empty, falls back to adapter's default workspace.
func (a *MemoryStoreAdapter) SearchByQuery(ctx context.Context, workspace, query string, limit int) ([]MemoryResult, error) {
	// Use provided workspace or fall back to adapter default
	ws := workspace
	if ws == "" {
		ws = a.workspace
	}
	a.logger.Debug("searching memories", "query", query, "workspace", ws, "has_embedder", a.embedder != nil)

	// Try semantic search first
	if a.embedder != nil {
		results, err := a.semanticSearch(ctx, ws, query, limit)
		if err == nil && len(results) > 0 {
			a.logger.Debug("semantic search succeeded", "count", len(results))
			return results, nil
		}
		if err != nil {
			a.logger.Debug("semantic search failed, falling back to text", "error", err)
		}
	}

	// Fall back to text-based search
	results, err := a.textSearch(ctx, ws, query, limit)
	a.logger.Debug("text search completed", "count", len(results))
	return results, err
}

// semanticSearch performs embedding-based search.
func (a *MemoryStoreAdapter) semanticSearch(ctx context.Context, workspace, query string, limit int) ([]MemoryResult, error) {
	// Embed the query using the appropriate method
	var embedding []float32
	var err error

	// Try EmbedQuery if provider supports it (for query-optimized embeddings)
	if qp, ok := a.embedder.(semantic.QueryEmbeddingProvider); ok {
		embedding, err = qp.EmbedQuery(ctx, query)
	} else {
		// Fall back to regular Embed
		embedding, err = a.embedder.Embed(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	a.logger.Debug("embedded query", "dims", len(embedding), "workspace", workspace)

	// Search by similarity
	results, err := a.store.SearchSimilar(ctx, workspace, embedding, limit)
	if err != nil {
		a.logger.Debug("SearchSimilar failed", "error", err)
		return nil, fmt.Errorf("search similar: %w", err)
	}

	memResults := make([]MemoryResult, len(results))
	for i, r := range results {
		memResults[i] = MemoryResult{
			ID:      r.Entry.ID,
			Type:    r.Entry.Type,
			Summary: r.Entry.Summary,
			Score:   float32(r.Score),
		}
	}

	return memResults, nil
}

// textSearch performs LIKE-based text search.
func (a *MemoryStoreAdapter) textSearch(ctx context.Context, workspace, query string, limit int) ([]MemoryResult, error) {
	// First try the full query
	results, err := a.store.Search(ctx, workspace, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}

	// If no results, try word-based search with important keywords
	if len(results) == 0 {
		words := extractSearchWords(query)
		for _, word := range words {
			if len(word) < 4 { // Skip short words
				continue
			}
			wordResults, err := a.store.Search(ctx, workspace, word, limit)
			if err == nil && len(wordResults) > 0 {
				results = append(results, wordResults...)
			}
			if len(results) >= limit {
				break
			}
		}
		// Deduplicate by name
		results = deduplicateScoredEntries(results)
	}

	memResults := make([]MemoryResult, len(results))
	for i, r := range results {
		memResults[i] = MemoryResult{
			ID:      r.Entry.ID,
			Type:    r.Entry.Type,
			Summary: r.Entry.Summary,
			Score:   float32(r.Score),
		}
	}

	return memResults, nil
}

// extractSearchWords extracts meaningful words from a search query.
func extractSearchWords(query string) []string {
	query = strings.ToLower(query)
	words := strings.Fields(query)
	// Filter out common words
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"in": true, "on": true, "at": true, "to": true, "for": true,
		"with": true, "and": true, "or": true, "of": true, "how": true,
		"what": true, "best": true, "practices": true, "go": true,
	}
	var filtered []string
	for _, w := range words {
		if !stopWords[w] && len(w) > 2 {
			filtered = append(filtered, w)
		}
	}
	return filtered
}

// deduplicateScoredEntries removes duplicate entries by name.
func deduplicateScoredEntries(entries []memory.ScoredEntry) []memory.ScoredEntry {
	seen := make(map[string]bool)
	var unique []memory.ScoredEntry
	for _, e := range entries {
		if !seen[e.Entry.Name] {
			seen[e.Entry.Name] = true
			unique = append(unique, e)
		}
	}
	return unique
}

// SessionLearningsAdapter searches past sessions for learnings.
type SessionLearningsAdapter struct {
	store     *sessions.Store
	workspace string
}

// NewSessionLearningsAdapter creates a new session learnings adapter.
func NewSessionLearningsAdapter(store *sessions.Store, workspace string) *SessionLearningsAdapter {
	return &SessionLearningsAdapter{
		store:     store,
		workspace: workspace,
	}
}

// SearchSessions searches past session learnings.
func (a *SessionLearningsAdapter) SearchSessions(ctx context.Context, query string, limit int) ([]SessionResult, error) {
	// List recent completed sessions
	opts := sessions.ListOptions{
		WorkspacePath: a.workspace,
		Limit:         50, // Get more to filter
	}

	sessionList, err := a.store.List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	var results []SessionResult
	queryLower := strings.ToLower(query)

	for _, s := range sessionList {
		// Skip running sessions
		if s.Status == storage.SessionStatusRunning {
			continue
		}

		// Check gotchas
		for _, gotcha := range s.Gotchas {
			if strings.Contains(strings.ToLower(gotcha), queryLower) {
				results = append(results, SessionResult{
					SessionID: s.ID,
					Content:   gotcha,
					Type:      "gotcha",
					Score:     0.8,
				})
			}
		}

		// Check decisions
		for _, decision := range s.Decisions {
			if strings.Contains(strings.ToLower(decision), queryLower) {
				results = append(results, SessionResult{
					SessionID: s.ID,
					Content:   decision,
					Type:      "decision",
					Score:     0.7,
				})
			}
		}

		// Check summary
		if strings.Contains(strings.ToLower(s.Summary), queryLower) {
			results = append(results, SessionResult{
				SessionID: s.ID,
				Content:   s.Summary,
				Type:      "learning",
				Score:     0.6,
			})
		}

		if len(results) >= limit {
			break
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// BoardStoreInjector adapts the BoardStore to the MessageSender interface.
type BoardStoreInjector struct {
	store     blackboard.BoardStore
	workspace string
	actorID   string // Actor ID to use as sender
}

// NewBoardStoreInjector creates a new board store injector.
func NewBoardStoreInjector(store blackboard.BoardStore, workspace, actorID string) *BoardStoreInjector {
	if actorID == "" {
		actorID = "actor:system:context-updater"
	}
	return &BoardStoreInjector{
		store:     store,
		workspace: workspace,
		actorID:   actorID,
	}
}

// SendMessage sends a message to a stream for a session.
func (a *BoardStoreInjector) SendMessage(ctx context.Context, sessionID, workspace, stream string, payload []byte) error {
	// Use provided workspace or fall back to adapter's default
	ws := workspace
	if ws == "" {
		ws = a.workspace
	}
	msg := &agent.BoardMessage{
		WorkspaceID: ws,
		TaskID:      sessionID,
		Stream:      stream,
		Sender:      a.actorID,
		Recipient:   agent.BroadcastRecipient, // Broadcast so any actor can pick it up
		Kind:        agent.BoardMessageKindInfo,
		Priority:    agent.DefaultPriority,
		Subject:     "Context Update",
		Body:        string(payload),
	}

	return a.store.SendMessage(ctx, msg)
}

// ContextBufferInjector adapts the contextbuffer store to the MessageSender interface.
// This is the preferred adapter since contextbuffer is designed for hook context injection.
type ContextBufferInjector struct {
	store     contextbuffer.Store
	workspace string
}

// NewContextBufferInjector creates a new context buffer injector.
func NewContextBufferInjector(store contextbuffer.Store, workspace string) *ContextBufferInjector {
	return &ContextBufferInjector{
		store:     store,
		workspace: workspace,
	}
}

// SendMessage sends a message to the context buffer for later drain by hooks.
func (a *ContextBufferInjector) SendMessage(ctx context.Context, sessionID, workspace, stream string, payload []byte) error {
	// Use provided workspace or fall back to adapter's default
	ws := workspace
	if ws == "" {
		ws = a.workspace
	}
	// The stream name becomes the source in contextbuffer
	params := contextbuffer.EnqueueParams{
		WorkspaceID: ws,
		SessionID:   sessionID,
		Source:      stream, // e.g., "context-updater"
		Text:        string(payload),
		Priority:    2,               // Normal priority
		TTL:         5 * time.Minute, // Context valid for 5 minutes
		Dedupe:      true,            // Avoid duplicate context
	}

	_, err := a.store.Enqueue(ctx, params)
	return err
}

// CombinedFinder combines multiple search sources into one ContextFinder.
type CombinedFinder struct {
	memory   MemorySearcher
	sessions SessionSearcher
	codemaps CodemapSearcher
	config   FinderConfig
}

// NewCombinedFinder creates a finder that searches across all sources.
func NewCombinedFinder(memory MemorySearcher, sessions SessionSearcher, codemaps CodemapSearcher) *CombinedFinder {
	return &CombinedFinder{
		memory:   memory,
		sessions: sessions,
		codemaps: codemaps,
		config:   DefaultFinderConfig(),
	}
}

// FindContext implements ContextFinder by delegating to the Finder.
func (c *CombinedFinder) FindContext(ctx context.Context, analysis *AnalysisResult, sessionID, workspace string) ([]ContextCandidate, error) {
	finder := NewFinder(c.memory, c.sessions, c.codemaps, c.config)
	return finder.FindContext(ctx, analysis, sessionID, workspace)
}
