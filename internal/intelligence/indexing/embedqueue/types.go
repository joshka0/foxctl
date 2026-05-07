package embedqueue

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	// StoreName is the canonical store registry name for embedding queue data.
	StoreName = "EMBEDDING_QUEUE"

	// DefaultDBFile is the canonical embedding queue database file.
	DefaultDBFile = "embedding_queue.db"
)

type TaskKind string

const (
	TaskKindSymbol       TaskKind = "symbol"
	TaskKindSemanticFile TaskKind = "semantic_file"
	TaskKindAnnotation   TaskKind = "annotation"
)

// Task describes the provider-independent identity of one embedding job.
type Task struct {
	Kind          TaskKind `json:"kind"`
	Scope         string   `json:"scope,omitempty"`
	WorkspaceID   string   `json:"workspace_id,omitempty"`
	TargetID      string   `json:"target_id"`
	ContentDigest string   `json:"content_digest,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	Model         string   `json:"model,omitempty"`
	Dimensions    int      `json:"dimensions,omitempty"`
	InputType     string   `json:"input_type,omitempty"`
}

// StableDedupeKey creates a compact deterministic key from semantic identity parts.
func StableDedupeKey(parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, strings.TrimSpace(part))
	}
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x00")))
	return hex.EncodeToString(sum[:])
}
