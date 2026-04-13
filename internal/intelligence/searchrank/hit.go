package searchrank

// Hit is a source-local ranked result suitable for cross-source fusion.
//
// RawScore is source-local only and must not be compared directly across sources.
type Hit struct {
	Source   string         `json:"source"`
	DedupKey string         `json:"dedup_key"`
	Path     string         `json:"path,omitempty"`
	SymbolID string         `json:"symbol_id,omitempty"`
	LineHint int            `json:"line_hint,omitempty"`
	Rank     int            `json:"rank"`
	RawScore float64        `json:"raw_score,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
