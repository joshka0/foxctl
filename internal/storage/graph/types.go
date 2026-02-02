package graph

import (
	"time"
)

// NodeType represents the type of entity in the graph.
type NodeType string

// Node type constants.
const (
	NodeTypeSession NodeType = "session"
	NodeTypeTask    NodeType = "task"
	NodeTypeSymbol  NodeType = "symbol"
	NodeTypeMemory  NodeType = "memory"
	NodeTypeFile    NodeType = "file"
)

// EdgeType represents the relationship between nodes.
type EdgeType string

// Edge type constants.
const (
	// Session-related edges
	EdgeTypeTouched  EdgeType = "touched"   // session → symbol (read/accessed)
	EdgeTypeModified EdgeType = "modified"  // session/task → symbol (edited)
	EdgeTypeWorkedOn EdgeType = "worked_on" // session → task

	// Code-related edges
	EdgeTypeCalls   EdgeType = "calls"   // symbol → symbol (function call)
	EdgeTypeImports EdgeType = "imports" // symbol/file → symbol/file

	// Task-related edges
	EdgeTypeDependsOn EdgeType = "depends_on" // task → task
	EdgeTypeParentOf  EdgeType = "parent_of"  // task → task (subtask relationship)

	// General edges
	EdgeTypeAbout     EdgeType = "about"      // memory → entity (what the memory is about)
	EdgeTypeRelatesTo EdgeType = "relates_to" // general relationship
)

// Node represents an entity in the dependency graph.
type Node struct {
	Workspace   string            `json:"workspace"`
	NodeID      string            `json:"node_id"`      // session:ULID, task:ULID, symbol:hash:name
	NodeType    NodeType          `json:"node_type"`    // session, task, symbol, memory, file
	Title       string            `json:"title"`        // Human-readable name
	CurrentPath string            `json:"current_path"` // For file/symbol nodes: current file path
	PageRank    float64           `json:"pagerank"`     // Pre-computed authority score
	InDegree    int               `json:"in_degree"`    // Number of incoming edges
	OutDegree   int               `json:"out_degree"`   // Number of outgoing edges
	LastSeen    time.Time         `json:"last_seen"`    // Last activity timestamp
	Metadata    map[string]string `json:"metadata"`     // Additional key-value data
	CreatedAt   time.Time         `json:"created_at"`   // When the node was first added
	UpdatedAt   time.Time         `json:"updated_at"`   // Last update time
}

// Edge represents a relationship between two nodes.
type Edge struct {
	ID        string            `json:"id"`
	Workspace string            `json:"workspace"`
	FromID    string            `json:"from_id"`
	FromType  NodeType          `json:"from_type"`
	ToID      string            `json:"to_id"`
	ToType    NodeType          `json:"to_type"`
	EdgeType  EdgeType          `json:"edge_type"`
	Weight    float64           `json:"weight"`   // Edge importance (default 1.0)
	TTLDays   *int              `json:"ttl_days"` // NULL = no expiry, e.g., 90 for session edges
	CreatedAt time.Time         `json:"created_at"`
	Metadata  map[string]string `json:"metadata"` // Additional context
}

// NodeStats provides summary statistics for graph nodes.
type NodeStats struct {
	TotalNodes   int64              `json:"total_nodes"`
	ByType       map[NodeType]int64 `json:"by_type"`
	AvgPageRank  float64            `json:"avg_pagerank"`
	MaxPageRank  float64            `json:"max_pagerank"`
	AvgInDegree  float64            `json:"avg_in_degree"`
	AvgOutDegree float64            `json:"avg_out_degree"`
}

// EdgeStats provides summary statistics for graph edges.
type EdgeStats struct {
	TotalEdges int64              `json:"total_edges"`
	ByType     map[EdgeType]int64 `json:"by_type"`
	AvgWeight  float64            `json:"avg_weight"`
}

// GraphStats combines node and edge statistics.
type GraphStats struct {
	Nodes NodeStats `json:"nodes"`
	Edges EdgeStats `json:"edges"`
	Path  string    `json:"path"`
}

// TopNodesOptions configures top nodes queries.
type TopNodesOptions struct {
	Workspace string
	NodeType  *NodeType // Filter by type (nil = all)
	Limit     int
	MinRank   float64 // Minimum PageRank threshold
}

// NeighborOptions configures neighbor queries.
type NeighborOptions struct {
	Direction string     // "in", "out", "both"
	EdgeTypes []EdgeType // Filter by edge types (nil = all)
	Limit     int
}

// Neighbor represents a node connected to another node.
type Neighbor struct {
	Node     Node `json:"node"`
	Edge     Edge `json:"edge"`
	Distance int  `json:"distance"` // Hops from source (1 for direct neighbors)
}

// PageRankOptions configures PageRank computation.
type PageRankOptions struct {
	Workspace     string
	DampingFactor float64 // Usually 0.85
	MaxIterations int     // Maximum iterations before convergence
	Tolerance     float64 // Convergence threshold
}

// PageRankResult captures the outcome of a PageRank computation.
type PageRankResult struct {
	NodesUpdated int     `json:"nodes_updated"`
	Iterations   int     `json:"iterations"`
	Converged    bool    `json:"converged"`
	FinalDelta   float64 `json:"final_delta"` // Final max change
}
