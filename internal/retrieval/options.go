package retrieval

// Options controls candidate generation behavior.
type Options struct {
	// Source limits - how many candidates to retrieve from each source
	MaxSymbolCandidates   int // Max from symbol index (default: 30)
	MaxSemanticCandidates int // Max from semantic index (default: 20)
	MaxRipgrepCandidates  int // Max from ripgrep fallback (default: 10)

	// Thresholds
	MinTotalCandidates int // Minimum candidates before ripgrep kicks in (default: 5)
	MaxTotalCandidates int // Final limit after merge (default: 50)

	// Weights for scoring (applied after normalization)
	SymbolWeight   float64 // Weight for symbol index scores (default: 1.0)
	SemanticWeight float64 // Weight for semantic scores (default: 0.7)
	RipgrepWeight  float64 // Weight for ripgrep scores (default: 0.5)

	// Feature toggles
	EnableSymbols  bool // Enable symbol index search (default: true)
	EnableSemantic bool // Enable semantic/vector search (default: true)
	EnableRipgrep  bool // Enable ripgrep fallback (default: true)

	// Filtering
	IncludePatterns []string // Glob patterns to include (empty = all)
	ExcludePatterns []string // Glob patterns to exclude

	// Debug options
	IncludeSourceDetails bool // Include source breakdown in candidates
}

// DefaultOptions returns sensible defaults for candidate generation.
func DefaultOptions() Options {
	return Options{
		// Source limits
		MaxSymbolCandidates:   30,
		MaxSemanticCandidates: 20,
		MaxRipgrepCandidates:  10,

		// Thresholds
		MinTotalCandidates: 5,
		MaxTotalCandidates: 50,

		// Weights
		SymbolWeight:   1.0,
		SemanticWeight: 0.7,
		RipgrepWeight:  0.5,

		// Feature toggles (all enabled by default)
		EnableSymbols:  true,
		EnableSemantic: true,
		EnableRipgrep:  true,

		// Filtering (no default filters)
		IncludePatterns: nil,
		ExcludePatterns: defaultExcludePatterns(),

		// Debug off by default
		IncludeSourceDetails: false,
	}
}

// defaultExcludePatterns returns common patterns to exclude from candidate generation.
func defaultExcludePatterns() []string {
	return []string{
		"**/vendor/**",
		"**/node_modules/**",
		"**/.git/**",
		"**/dist/**",
		"**/build/**",
		"**/__pycache__/**",
		"**/*.min.js",
		"**/*.min.css",
		"**/go.sum",
		"**/package-lock.json",
		"**/yarn.lock",
	}
}

// QuickOptions returns options optimized for speed over thoroughness.
// Good for interactive use cases where latency matters.
func QuickOptions() Options {
	opts := DefaultOptions()
	opts.MaxSymbolCandidates = 15
	opts.MaxSemanticCandidates = 10
	opts.MaxRipgrepCandidates = 5
	opts.MaxTotalCandidates = 25
	return opts
}

// ThoroughOptions returns options optimized for thoroughness over speed.
// Good for batch processing or deep analysis.
func ThoroughOptions() Options {
	opts := DefaultOptions()
	opts.MaxSymbolCandidates = 50
	opts.MaxSemanticCandidates = 40
	opts.MaxRipgrepCandidates = 20
	opts.MaxTotalCandidates = 100
	opts.MinTotalCandidates = 10
	return opts
}

// SymbolOnlyOptions returns options that only use the symbol index.
// Fastest option when you know symbols are well-indexed.
func SymbolOnlyOptions() Options {
	opts := DefaultOptions()
	opts.EnableSemantic = false
	opts.EnableRipgrep = false
	return opts
}

// WithMaxCandidates returns a copy of options with adjusted max candidates.
func (o Options) WithMaxCandidates(max int) Options {
	o.MaxTotalCandidates = max
	// Adjust source limits proportionally
	if max < 50 {
		ratio := float64(max) / 50.0
		o.MaxSymbolCandidates = int(float64(o.MaxSymbolCandidates) * ratio)
		o.MaxSemanticCandidates = int(float64(o.MaxSemanticCandidates) * ratio)
		o.MaxRipgrepCandidates = int(float64(o.MaxRipgrepCandidates) * ratio)
		if o.MaxSymbolCandidates < 5 {
			o.MaxSymbolCandidates = 5
		}
		if o.MaxSemanticCandidates < 3 {
			o.MaxSemanticCandidates = 3
		}
		if o.MaxRipgrepCandidates < 2 {
			o.MaxRipgrepCandidates = 2
		}
	}
	return o
}

// WithExcludePatterns returns a copy of options with additional exclude patterns.
func (o Options) WithExcludePatterns(patterns ...string) Options {
	o.ExcludePatterns = append(o.ExcludePatterns, patterns...)
	return o
}
