// Package expander provides language-specific code block boundary detection.
//
// The expander package finds complete code blocks (functions, classes, methods)
// given a starting line number or symbol name. This enables extracting full
// context around grep/search matches rather than arbitrary line ranges.
//
// Languages are supported through the BlockExpander interface:
//   - Brace-based: Go, JavaScript, TypeScript, Java, C, C++, Rust, etc.
//   - Indentation-based: Python, GDScript
//   - Generic: Fallback heuristics for unknown languages
//
// Usage:
//
//	expander := expander.Get("go")
//	start, end, symbol, err := expander.FindBlock(content, matchLine)
package expander
