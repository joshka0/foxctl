// Package selector provides pluggable strategies for selecting relevant code spans.
//
// The selector package abstracts the logic for choosing which portions of a file
// are most relevant to a given query. This allows skills to use different selection
// strategies:
//
//   - HeuristicSelector: Fast keyword matching with stop-word filtering
//   - LLMSelector: Model-assisted selection for complex queries (future)
//
// Usage:
//
//	sel := selector.NewHeuristic(selector.HeuristicOpts{ContextLines: 3})
//	spans, err := sel.Select(ctx, "authentication flow", content, hints)
package selector
