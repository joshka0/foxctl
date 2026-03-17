package searchindex

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Scope identifies a logical retrieval domain.
//
// It is intentionally small in phase-1 and can be extended as retrieval expands.
type Scope string

const (
	// ScopeCode is for code symbols and file summaries.
	ScopeCode Scope = "code"
)

// Kind identifies the retrieval document class.
type Kind string

const (
	// KindSymbol documents represent symbol-level nodes with symbol anchor metadata.
	KindSymbol Kind = "symbol"
	// KindFile documents represent file-level nodes with file summaries.
	KindFile Kind = "file"
	// KindText is a fallback document class.
	KindText Kind = "text"
)

// AnchorType categorizes anchor semantics.
type AnchorType string

const (
	// AnchorSymbol anchors a symbol span inside a file.
	AnchorSymbol AnchorType = "symbol"
	// AnchorLine anchors a single file line.
	AnchorLine AnchorType = "line"
)

// Anchor is a retrieval hit anchor carried through the recall phase.
type Anchor struct {
	Type      AnchorType `json:"type"`
	Path      string     `json:"path,omitempty"`
	Line      int        `json:"line,omitempty"`
	StartLine int        `json:"start_line,omitempty"`
	EndLine   int        `json:"end_line,omitempty"`
	StartByte int        `json:"start_byte,omitempty"`
	EndByte   int        `json:"end_byte,omitempty"`
}

// Document is the typed retrieval record persisted by the SQL index.
type Document struct {
	ID             string         `json:"id"`
	WorkspaceID    string         `json:"workspace_id"`
	Scope          Scope          `json:"scope"`
	Kind           Kind           `json:"kind"`
	GroupKey       string         `json:"group_key"`
	Path           string         `json:"path"`
	SymbolID       string         `json:"symbol_id,omitempty"`
	SymbolName     string         `json:"symbol_name,omitempty"`
	Title          string         `json:"title"`
	Summary        string         `json:"summary,omitempty"`
	SearchText     string         `json:"search_text"`
	Keywords       []string       `json:"keywords,omitempty"`
	Anchor         Anchor         `json:"anchor,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	Embedding      []float32      `json:"embedding,omitempty"`
	EmbeddingModel string         `json:"embedding_model,omitempty"`
}

// SearchHit is a raw scored retrieval hit returned by recall.
type SearchHit struct {
	Doc   Document `json:"document"`
	Score float64  `json:"score"`
}

// EmbeddingMetadata captures the expected embedding contract for one workspace.
type EmbeddingMetadata struct {
	WorkspaceID string `json:"workspace_id"`
	Model       string `json:"model"`
	Dimensions  int    `json:"dimensions"`
}

func normalizeKeywords(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		n := strings.TrimSpace(strings.ToLower(value))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func splitSearchTerms(query string) []string {
	parts := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	return normalizeKeywords(parts)
}

func encodeJSON(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	return string(b), nil
}

func encodeSearchText(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, strings.TrimSpace(value))
	}
	return strings.Join(parts, " ")
}
