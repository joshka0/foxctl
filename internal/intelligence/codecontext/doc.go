// Package codecontext provides shared code extraction and context retrieval
// functionality used by skills like code/snippet_extract, code/context_ripgrep, and
// code/semantic_search.
//
// It implements the Code Context Funnel architecture:
//
//	semantic_search → swe_grep (evidence) → counsel (analysis)
//	     ↓                  ↓                      ↓
//	"where to look"   "what's relevant"    "what does it mean"
//
// Skills should use Collect() to gather evidence and Render() to format output.
// Never re-implement file reading or snippet extraction in skills.
package codecontext
