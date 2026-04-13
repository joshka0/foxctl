// Package memory provides progressive context management for actors.
//
// The memory system maintains a compact, relevant context window by progressively
// summarizing and distilling conversation history (L0 -> L1 -> L2).
//
// See docs/designs/actor-progressive-memory.md for the full design.
package memory
