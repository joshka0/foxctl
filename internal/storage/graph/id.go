package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ID generation utilities for stable graph node identifiers.
// These IDs are designed to survive file renames and code movements.

// SessionNodeID creates a node ID for a session.
func SessionNodeID(sessionID string) string {
	return fmt.Sprintf("session:%s", sessionID)
}

// TaskNodeID creates a node ID for a task.
func TaskNodeID(taskID string) string {
	return fmt.Sprintf("task:%s", taskID)
}

// MemoryNodeID creates a node ID for a memory entry.
func MemoryNodeID(memoryID string) string {
	return fmt.Sprintf("memory:%s", memoryID)
}

// FileNodeID creates a node ID for a file.
// Uses the workspace-relative path for stability.
func FileNodeID(relativePath string) string {
	return fmt.Sprintf("file:%s", relativePath)
}

// SymbolNodeID creates a stable node ID for a code symbol.
// The ID is based on a content hash prefix and the symbol name,
// allowing it to survive file renames while remaining identifiable.
//
// Parameters:
//   - bodyHash: Hash of the symbol's body/content (first 12 chars used)
//   - name: The symbol's qualified name (e.g., "MyClass.myMethod")
func SymbolNodeID(bodyHash, name string) string {
	// Truncate hash to first 12 characters for readability
	hashPrefix := bodyHash
	if len(hashPrefix) > 12 {
		hashPrefix = hashPrefix[:12]
	}
	return fmt.Sprintf("symbol:%s:%s", hashPrefix, name)
}

// SymbolNodeIDFromBody creates a symbol node ID by hashing the body content.
// This is useful when you have the full body content rather than a pre-computed hash.
func SymbolNodeIDFromBody(body, name string) string {
	hash := sha256.Sum256([]byte(body))
	hashHex := hex.EncodeToString(hash[:])
	return SymbolNodeID(hashHex, name)
}

// ParseNodeID extracts the type and identifier from a node ID.
// Returns (nodeType, identifier, ok).
func ParseNodeID(nodeID string) (NodeType, string, bool) {
	parts := strings.SplitN(nodeID, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	typeStr := parts[0]
	identifier := parts[1]

	switch typeStr {
	case "session":
		return NodeTypeSession, identifier, true
	case "task":
		return NodeTypeTask, identifier, true
	case "symbol":
		return NodeTypeSymbol, identifier, true
	case "memory":
		return NodeTypeMemory, identifier, true
	case "file":
		return NodeTypeFile, identifier, true
	default:
		return "", "", false
	}
}

// SymbolNodeIDComponents extracts the hash prefix and name from a symbol node ID.
// Returns (hashPrefix, name, ok).
func SymbolNodeIDComponents(nodeID string) (string, string, bool) {
	nodeType, identifier, ok := ParseNodeID(nodeID)
	if !ok || nodeType != NodeTypeSymbol {
		return "", "", false
	}

	// Symbol identifier format: hash:name
	parts := strings.SplitN(identifier, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}

	return parts[0], parts[1], true
}

// NodeIDType returns the type portion of a node ID without parsing.
func NodeIDType(nodeID string) NodeType {
	idx := strings.Index(nodeID, ":")
	if idx < 0 {
		return ""
	}
	return NodeType(nodeID[:idx])
}

// IsValidNodeID checks if a node ID has a valid format.
func IsValidNodeID(nodeID string) bool {
	_, _, ok := ParseNodeID(nodeID)
	return ok
}
