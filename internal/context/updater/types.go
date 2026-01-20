// Package updater provides a background service that monitors conversations
// and proactively surfaces relevant context using semantic search tools.
package updater

import (
	"time"
)

// AnalysisResult holds the output from the LLM conversation analyzer.
// The cheap LLM extracts structured information about what the user is working on.
type AnalysisResult struct {
	// Topics are the current work topics (e.g., "auth", "validation", "database")
	Topics []string `json:"topics"`

	// Intent describes what the user is trying to accomplish
	Intent string `json:"intent"`

	// FilesActive are the files currently being worked on
	FilesActive []string `json:"files_active"`

	// SearchQueries are suggested queries to find relevant context
	SearchQueries []string `json:"search_queries"`

	// DriftDetected indicates if the topic has shifted significantly
	DriftDetected bool `json:"drift_detected"`

	// Confidence is how confident the analysis is (0.0-1.0)
	Confidence float32 `json:"confidence"`
}

// ContextCandidate represents a piece of context that might be relevant.
type ContextCandidate struct {
	// ID uniquely identifies this context (for deduplication)
	ID string

	// Type is the kind of context: memory, session, codemap, file
	Type string

	// Content is the context content to inject
	Content string

	// Source describes where this came from (e.g., "memory:gotcha", "session:abc123")
	Source string

	// Score is the relevance score (0.0-1.0)
	Score float32

	// Query is the search query that found this
	Query string

	// Timestamp is when this context was found
	Timestamp time.Time
}

// InjectionRecord tracks what context has been injected to avoid repetition.
type InjectionRecord struct {
	// ID of the injected context
	ID string

	// ContentHash is a hash of the content (for dedup even with different IDs)
	ContentHash string

	// SessionID is the session this was injected to
	SessionID string

	// Timestamp is when this was injected
	Timestamp time.Time

	// Topics are the topics active when this was injected
	Topics []string
}

// SessionState tracks per-session processing state.
type SessionState struct {
	// SessionID is the session being tracked
	SessionID string

	// LastTurnID is the last processed turn ID (watermark)
	LastTurnID string

	// LastAnalysis is the most recent analysis result
	LastAnalysis *AnalysisResult

	// LastAnalysisTime is when the last analysis was performed
	LastAnalysisTime time.Time

	// InjectionCount tracks injections in the current window (for rate limiting)
	InjectionCount int

	// WindowStart is when the current rate limit window started
	WindowStart time.Time
}

// WorkerMetrics tracks operational metrics for observability.
type WorkerMetrics struct {
	// TickCount is the number of polling ticks processed
	TickCount int64

	// AnalysisCount is the number of LLM analyses performed
	AnalysisCount int64

	// InjectionCount is the number of contexts injected
	InjectionCount int64

	// ErrorCount is the number of errors encountered
	ErrorCount int64

	// LastTickTime is when the last tick was processed
	LastTickTime time.Time

	// AverageLLMLatencyMs is the rolling average LLM call latency
	AverageLLMLatencyMs float64

	// TotalLLMCostEstimate is the estimated total LLM cost (based on token counts)
	TotalLLMCostEstimate float64
}
