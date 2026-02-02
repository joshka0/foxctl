// Package atomic provides atomic fact processing for memories and tasks.
// It decomposes raw text into self-contained, searchable atomic facts using LLM.
//
// Based on SimpleMem: https://github.com/aiming-lab/SimpleMem
// Key ideas:
//   - Write-time disambiguation: resolve coreferences, anchor timestamps
//   - Atomic facts: each fact is self-contained and independently searchable
//   - Multi-layer retrieval: semantic (embeddings) + lexical (keywords) + symbolic (entities)
package atomic
