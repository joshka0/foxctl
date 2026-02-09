package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
)

// Store defines the graph storage interface.
type Store interface {
	Close() error

	// Node operations
	UpsertNode(ctx context.Context, node Node) error
	GetNode(ctx context.Context, workspace, nodeID string) (Node, error)
	DeleteNode(ctx context.Context, workspace, nodeID string) error
	TopNodes(ctx context.Context, opts TopNodesOptions) ([]Node, error)
	UpdatePageRank(ctx context.Context, workspace, nodeID string, pagerank float64) error
	UpdateDegrees(ctx context.Context, workspace, nodeID string, inDegree, outDegree int) error

	// Edge operations
	UpsertEdge(ctx context.Context, edge Edge) error
	GetEdge(ctx context.Context, id string) (Edge, error)
	DeleteEdge(ctx context.Context, id string) error
	GetEdgesFrom(ctx context.Context, workspace, nodeID string, edgeTypes []EdgeType) ([]Edge, error)
	GetEdgesTo(ctx context.Context, workspace, nodeID string, edgeTypes []EdgeType) ([]Edge, error)
	GetNeighbors(ctx context.Context, workspace, nodeID string, opts NeighborOptions) ([]Neighbor, error)

	// Bulk operations
	UpsertNodes(ctx context.Context, nodes []Node) error
	UpsertEdges(ctx context.Context, edges []Edge) error

	// Statistics
	Stats(ctx context.Context, workspace string) (GraphStats, error)

	// Maintenance
	CleanupExpiredEdges(ctx context.Context) (int, error)
	CleanupDanglingEdges(ctx context.Context, workspace string) (int, error)
	RecalculateDegrees(ctx context.Context, workspace string) error

	// PageRank
	GetAllEdges(ctx context.Context, workspace string) ([]Edge, error)
	GetAllNodes(ctx context.Context, workspace string) ([]Node, error)
	BulkUpdatePageRank(ctx context.Context, workspace string, ranks map[string]float64) error

	// Search and path operations (for semantic codemaps)
	SearchNodes(ctx context.Context, workspace, term string, limit int) ([]Node, error)
	GetEdgesBetween(ctx context.Context, workspace string, nodeIDs []string) ([]Edge, error)
	FindShortestPath(ctx context.Context, workspace, fromID, toID string, maxDepth int) ([][]string, error)
}

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db    *sql.DB
	path  string
	close func() error
}

// Open opens or creates a SQLite-backed graph store at dataDir/graph.db.
// It initializes a shared database connection, runs schema migrations, and
// returns a SQLiteStore whose Close method will invoke the underlying close function.
func Open(ctx context.Context, dataDir string) (*SQLiteStore, error) {
	dbPath := filepath.Join(dataDir, "graph.db")

	db, closeFn, err := dbutil.OpenStoreDB(ctx, dataDir, "GRAPH", "graph.db", func(ctx context.Context, db *sql.DB) error {
		tmp := &SQLiteStore{db: db}
		return tmp.migrate(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("open graph db: %w", err)
	}

	store := &SQLiteStore{db: db, path: dbPath, close: closeFn}
	store.repairWorkspaceIDs(ctx)

	return store, nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// migrate creates or updates the database schema.
func (s *SQLiteStore) migrate(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS graph_nodes (
		workspace TEXT NOT NULL,
		node_id TEXT NOT NULL,
		node_type TEXT NOT NULL,
		title TEXT,
		current_path TEXT,
		pagerank REAL DEFAULT 0.0,
		in_degree INTEGER DEFAULT 0,
		out_degree INTEGER DEFAULT 0,
		last_seen TEXT NOT NULL,
		metadata TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (workspace, node_id)
	);

	CREATE INDEX IF NOT EXISTS idx_graph_nodes_type ON graph_nodes(workspace, node_type);
	CREATE INDEX IF NOT EXISTS idx_graph_nodes_pagerank ON graph_nodes(workspace, pagerank DESC);
	CREATE INDEX IF NOT EXISTS idx_graph_nodes_path ON graph_nodes(workspace, current_path);

	CREATE TABLE IF NOT EXISTS graph_edges (
		id TEXT PRIMARY KEY,
		workspace TEXT NOT NULL,
		from_id TEXT NOT NULL,
		from_type TEXT NOT NULL,
		to_id TEXT NOT NULL,
		to_type TEXT NOT NULL,
		edge_type TEXT NOT NULL,
		weight REAL DEFAULT 1.0,
		created_at TEXT NOT NULL,
		ttl_days INTEGER,
		metadata TEXT,
		UNIQUE(workspace, from_id, to_id, edge_type)
	);

	CREATE INDEX IF NOT EXISTS idx_graph_edges_from ON graph_edges(workspace, from_id);
	CREATE INDEX IF NOT EXISTS idx_graph_edges_to ON graph_edges(workspace, to_id);
	CREATE INDEX IF NOT EXISTS idx_graph_edges_type ON graph_edges(workspace, edge_type);
	CREATE INDEX IF NOT EXISTS idx_graph_edges_ttl ON graph_edges(ttl_days, created_at) WHERE ttl_days IS NOT NULL;
	`

	_, err := s.db.ExecContext(ctx, schema)
	return err
}

// UpsertNode inserts or updates a node.
func (s *SQLiteStore) UpsertNode(ctx context.Context, node Node) error {
	node.Workspace = ws.CanonicalID(node.Workspace)
	now := time.Now().UTC().Format(time.RFC3339)
	if node.LastSeen.IsZero() {
		node.LastSeen = time.Now().UTC()
	}

	metadata, err := json.Marshal(node.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	query := `
	INSERT INTO graph_nodes (workspace, node_id, node_type, title, current_path, pagerank, in_degree, out_degree, last_seen, metadata, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(workspace, node_id) DO UPDATE SET
		node_type = excluded.node_type,
		title = CASE WHEN excluded.title != '' THEN excluded.title ELSE graph_nodes.title END,
		current_path = excluded.current_path,
		pagerank = CASE WHEN excluded.pagerank > 0 THEN excluded.pagerank ELSE graph_nodes.pagerank END,
		in_degree = CASE WHEN excluded.in_degree > 0 THEN excluded.in_degree ELSE graph_nodes.in_degree END,
		out_degree = CASE WHEN excluded.out_degree > 0 THEN excluded.out_degree ELSE graph_nodes.out_degree END,
		last_seen = excluded.last_seen,
		metadata = excluded.metadata,
		updated_at = excluded.updated_at
	`

	_, err = s.db.ExecContext(ctx, query,
		node.Workspace,
		node.NodeID,
		string(node.NodeType),
		node.Title,
		node.CurrentPath,
		node.PageRank,
		node.InDegree,
		node.OutDegree,
		node.LastSeen.UTC().Format(time.RFC3339),
		string(metadata),
		now,
		now,
	)
	return err
}

// GetNode retrieves a node by ID.
func (s *SQLiteStore) GetNode(ctx context.Context, workspace, nodeID string) (Node, error) {
	workspace = ws.CanonicalID(workspace)
	query := `
	SELECT workspace, node_id, node_type, title, current_path, pagerank, in_degree, out_degree, last_seen, metadata, created_at, updated_at
	FROM graph_nodes
	WHERE workspace = ? AND node_id = ?
	`

	var node Node
	var nodeType, lastSeen, metadata, createdAt, updatedAt string
	var title, currentPath sql.NullString

	err := s.db.QueryRowContext(ctx, query, workspace, nodeID).Scan(
		&node.Workspace,
		&node.NodeID,
		&nodeType,
		&title,
		&currentPath,
		&node.PageRank,
		&node.InDegree,
		&node.OutDegree,
		&lastSeen,
		&metadata,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, fmt.Errorf("node not found: %s", nodeID)
	}
	if err != nil {
		return Node{}, err
	}

	node.NodeType = NodeType(nodeType)
	if title.Valid {
		node.Title = title.String
	}
	if currentPath.Valid {
		node.CurrentPath = currentPath.String
	}
	node.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
	node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if metadata != "" {
		_ = json.Unmarshal([]byte(metadata), &node.Metadata)
	}

	return node, nil
}

// DeleteNode removes a node and its edges.
func (s *SQLiteStore) DeleteNode(ctx context.Context, workspace, nodeID string) error {
	workspace = ws.CanonicalID(workspace)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete edges first
	_, err = tx.ExecContext(ctx, "DELETE FROM graph_edges WHERE workspace = ? AND (from_id = ? OR to_id = ?)", workspace, nodeID, nodeID)
	if err != nil {
		return err
	}

	// Delete node
	_, err = tx.ExecContext(ctx, "DELETE FROM graph_nodes WHERE workspace = ? AND node_id = ?", workspace, nodeID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// TopNodes returns the top nodes by PageRank.
func (s *SQLiteStore) TopNodes(ctx context.Context, opts TopNodesOptions) ([]Node, error) {
	opts.Workspace = ws.CanonicalID(opts.Workspace)
	var args []any
	query := `
	SELECT workspace, node_id, node_type, title, current_path, pagerank, in_degree, out_degree, last_seen, metadata, created_at, updated_at
	FROM graph_nodes
	WHERE workspace = ? AND pagerank >= ?
	`
	args = append(args, opts.Workspace, opts.MinRank)

	if opts.NodeType != nil {
		query += " AND node_type = ?"
		args = append(args, string(*opts.NodeType))
	}

	query += " ORDER BY pagerank DESC"

	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanNodes(rows)
}

// UpdatePageRank updates a node's PageRank score.
func (s *SQLiteStore) UpdatePageRank(ctx context.Context, workspace, nodeID string, pagerank float64) error {
	workspace = ws.CanonicalID(workspace)
	_, err := s.db.ExecContext(ctx,
		"UPDATE graph_nodes SET pagerank = ?, updated_at = ? WHERE workspace = ? AND node_id = ?",
		pagerank, time.Now().UTC().Format(time.RFC3339), workspace, nodeID,
	)
	return err
}

// UpdateDegrees updates a node's in/out degree counts.
func (s *SQLiteStore) UpdateDegrees(ctx context.Context, workspace, nodeID string, inDegree, outDegree int) error {
	workspace = ws.CanonicalID(workspace)
	_, err := s.db.ExecContext(ctx,
		"UPDATE graph_nodes SET in_degree = ?, out_degree = ?, updated_at = ? WHERE workspace = ? AND node_id = ?",
		inDegree, outDegree, time.Now().UTC().Format(time.RFC3339), workspace, nodeID,
	)
	return err
}

// UpsertEdge inserts or updates an edge.
func (s *SQLiteStore) UpsertEdge(ctx context.Context, edge Edge) error {
	edge.Workspace = ws.CanonicalID(edge.Workspace)
	if edge.ID == "" {
		edge.ID = ulid.Make().String()
	}
	if edge.Weight == 0 {
		edge.Weight = 1.0
	}
	if edge.CreatedAt.IsZero() {
		edge.CreatedAt = time.Now().UTC()
	}

	metadata, err := json.Marshal(edge.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	// Note: On conflict, we update created_at to implement "sliding TTL" semantics.
	// This ensures edges that are repeatedly upserted don't expire based on their
	// original creation time, but rather from their last refresh time.
	query := `
	INSERT INTO graph_edges (id, workspace, from_id, from_type, to_id, to_type, edge_type, weight, created_at, ttl_days, metadata)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(workspace, from_id, to_id, edge_type) DO UPDATE SET
		weight = excluded.weight,
		ttl_days = excluded.ttl_days,
		metadata = excluded.metadata,
		created_at = excluded.created_at
	`

	_, err = s.db.ExecContext(ctx, query,
		edge.ID,
		edge.Workspace,
		edge.FromID,
		string(edge.FromType),
		edge.ToID,
		string(edge.ToType),
		string(edge.EdgeType),
		edge.Weight,
		edge.CreatedAt.UTC().Format(time.RFC3339),
		edge.TTLDays,
		string(metadata),
	)
	return err
}

// GetEdge retrieves an edge by ID.
func (s *SQLiteStore) GetEdge(ctx context.Context, id string) (Edge, error) {
	query := `
	SELECT id, workspace, from_id, from_type, to_id, to_type, edge_type, weight, created_at, ttl_days, metadata
	FROM graph_edges
	WHERE id = ?
	`

	var edge Edge
	var fromType, toType, edgeType, createdAt, metadata string
	var ttlDays sql.NullInt64

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&edge.ID,
		&edge.Workspace,
		&edge.FromID,
		&fromType,
		&edge.ToID,
		&toType,
		&edgeType,
		&edge.Weight,
		&createdAt,
		&ttlDays,
		&metadata,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Edge{}, fmt.Errorf("edge not found: %s", id)
	}
	if err != nil {
		return Edge{}, err
	}

	edge.FromType = NodeType(fromType)
	edge.ToType = NodeType(toType)
	edge.EdgeType = EdgeType(edgeType)
	edge.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if ttlDays.Valid {
		days := int(ttlDays.Int64)
		edge.TTLDays = &days
	}
	if metadata != "" {
		_ = json.Unmarshal([]byte(metadata), &edge.Metadata)
	}

	return edge, nil
}

// DeleteEdge removes an edge.
func (s *SQLiteStore) DeleteEdge(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM graph_edges WHERE id = ?", id)
	return err
}

// GetEdgesFrom returns all edges originating from a node.
func (s *SQLiteStore) GetEdgesFrom(ctx context.Context, workspace, nodeID string, edgeTypes []EdgeType) ([]Edge, error) {
	return s.getEdges(ctx, workspace, nodeID, "from_id", edgeTypes)
}

// GetEdgesTo returns all edges pointing to a node.
func (s *SQLiteStore) GetEdgesTo(ctx context.Context, workspace, nodeID string, edgeTypes []EdgeType) ([]Edge, error) {
	return s.getEdges(ctx, workspace, nodeID, "to_id", edgeTypes)
}

func (s *SQLiteStore) getEdges(ctx context.Context, workspace, nodeID, column string, edgeTypes []EdgeType) ([]Edge, error) {
	workspace = ws.CanonicalID(workspace)
	var args []any
	query := fmt.Sprintf(`
	SELECT id, workspace, from_id, from_type, to_id, to_type, edge_type, weight, created_at, ttl_days, metadata
	FROM graph_edges
	WHERE workspace = ? AND %s = ?
	`, column)
	args = append(args, workspace, nodeID)

	if len(edgeTypes) > 0 {
		placeholders := make([]string, len(edgeTypes))
		for i, et := range edgeTypes {
			placeholders[i] = "?"
			args = append(args, string(et))
		}
		query += fmt.Sprintf(" AND edge_type IN (%s)", strings.Join(placeholders, ","))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanEdges(rows)
}

// GetNeighbors returns neighboring nodes.
func (s *SQLiteStore) GetNeighbors(ctx context.Context, workspace, nodeID string, opts NeighborOptions) ([]Neighbor, error) {
	workspace = ws.CanonicalID(workspace)
	var neighbors []Neighbor

	// Get outgoing neighbors
	if opts.Direction == "out" || opts.Direction == "both" || opts.Direction == "" {
		edges, err := s.GetEdgesFrom(ctx, workspace, nodeID, opts.EdgeTypes)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			node, err := s.GetNode(ctx, workspace, edge.ToID)
			if err != nil {
				continue // Skip if node not found
			}
			neighbors = append(neighbors, Neighbor{Node: node, Edge: edge, Distance: 1})
		}
	}

	// Get incoming neighbors
	if opts.Direction == "in" || opts.Direction == "both" || opts.Direction == "" {
		edges, err := s.GetEdgesTo(ctx, workspace, nodeID, opts.EdgeTypes)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			node, err := s.GetNode(ctx, workspace, edge.FromID)
			if err != nil {
				continue // Skip if node not found
			}
			neighbors = append(neighbors, Neighbor{Node: node, Edge: edge, Distance: 1})
		}
	}

	// Apply limit
	if opts.Limit > 0 && len(neighbors) > opts.Limit {
		neighbors = neighbors[:opts.Limit]
	}

	return neighbors, nil
}

// UpsertNodes bulk inserts or updates nodes.
//
// Index:
// - Purpose: Persist node batches in a single transaction
// - Flow: begin tx → prepare statement → upsert rows → commit
// - SideEffects: database transaction; node table writes
// - FailureModes: tx errors, statement errors, execution errors
// - Related: Node, SQLiteStore.UpsertEdges
// - Keywords: graph_nodes, upsert, transaction, workspace_id
func (s *SQLiteStore) UpsertNodes(ctx context.Context, nodes []Node) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
	INSERT INTO graph_nodes (workspace, node_id, node_type, title, current_path, pagerank, in_degree, out_degree, last_seen, metadata, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(workspace, node_id) DO UPDATE SET
		node_type = excluded.node_type,
		title = CASE WHEN excluded.title != '' THEN excluded.title ELSE graph_nodes.title END,
		current_path = excluded.current_path,
		last_seen = excluded.last_seen,
		metadata = excluded.metadata,
		updated_at = excluded.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, node := range nodes {
		node.Workspace = ws.CanonicalID(node.Workspace)
		if node.LastSeen.IsZero() {
			node.LastSeen = time.Now().UTC()
		}
		metadata, _ := json.Marshal(node.Metadata)

		_, err = stmt.ExecContext(ctx,
			node.Workspace,
			node.NodeID,
			string(node.NodeType),
			node.Title,
			node.CurrentPath,
			node.PageRank,
			node.InDegree,
			node.OutDegree,
			node.LastSeen.UTC().Format(time.RFC3339),
			string(metadata),
			now,
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpsertEdges bulk inserts or updates edges.
//
// Index:
// - Purpose: Persist edge batches in a single transaction
// - Flow: begin tx → prepare statement → upsert rows → commit
// - SideEffects: database transaction; edge table writes
// - FailureModes: tx errors, statement errors, execution errors
// - Related: Edge, SQLiteStore.UpsertNodes
// - Keywords: graph_edges, upsert, transaction, workspace_id
func (s *SQLiteStore) UpsertEdges(ctx context.Context, edges []Edge) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Note: On conflict, we update created_at to implement "sliding TTL" semantics.
	// This ensures edges that are repeatedly upserted don't expire based on their
	// original creation time, but rather from their last refresh time.
	stmt, err := tx.PrepareContext(ctx, `
	INSERT INTO graph_edges (id, workspace, from_id, from_type, to_id, to_type, edge_type, weight, created_at, ttl_days, metadata)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(workspace, from_id, to_id, edge_type) DO UPDATE SET
		weight = excluded.weight,
		ttl_days = excluded.ttl_days,
		metadata = excluded.metadata,
		created_at = excluded.created_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, edge := range edges {
		edge.Workspace = ws.CanonicalID(edge.Workspace)
		if edge.ID == "" {
			edge.ID = ulid.Make().String()
		}
		if edge.Weight == 0 {
			edge.Weight = 1.0
		}
		if edge.CreatedAt.IsZero() {
			edge.CreatedAt = time.Now().UTC()
		}
		metadata, _ := json.Marshal(edge.Metadata)

		_, err = stmt.ExecContext(ctx,
			edge.ID,
			edge.Workspace,
			edge.FromID,
			string(edge.FromType),
			edge.ToID,
			string(edge.ToType),
			string(edge.EdgeType),
			edge.Weight,
			edge.CreatedAt.UTC().Format(time.RFC3339),
			edge.TTLDays,
			string(metadata),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Stats returns graph statistics.
func (s *SQLiteStore) Stats(ctx context.Context, workspace string) (GraphStats, error) {
	workspace = ws.CanonicalID(workspace)
	stats := GraphStats{
		Path: s.path,
		Nodes: NodeStats{
			ByType: make(map[NodeType]int64),
		},
		Edges: EdgeStats{
			ByType: make(map[EdgeType]int64),
		},
	}

	// Node stats
	row := s.db.QueryRowContext(ctx, `
	SELECT COUNT(*), COALESCE(AVG(pagerank), 0), COALESCE(MAX(pagerank), 0), COALESCE(AVG(in_degree), 0), COALESCE(AVG(out_degree), 0)
	FROM graph_nodes WHERE workspace = ?
	`, workspace)
	if err := row.Scan(&stats.Nodes.TotalNodes, &stats.Nodes.AvgPageRank, &stats.Nodes.MaxPageRank, &stats.Nodes.AvgInDegree, &stats.Nodes.AvgOutDegree); err != nil {
		return stats, err
	}

	// Node counts by type
	rows, err := s.db.QueryContext(ctx, "SELECT node_type, COUNT(*) FROM graph_nodes WHERE workspace = ? GROUP BY node_type", workspace)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeType string
		var count int64
		if err := rows.Scan(&nodeType, &count); err != nil {
			return stats, err
		}
		stats.Nodes.ByType[NodeType(nodeType)] = count
	}

	// Edge stats
	row = s.db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(AVG(weight), 0) FROM graph_edges WHERE workspace = ?", workspace)
	if err := row.Scan(&stats.Edges.TotalEdges, &stats.Edges.AvgWeight); err != nil {
		return stats, err
	}

	// Edge counts by type
	rows, err = s.db.QueryContext(ctx, "SELECT edge_type, COUNT(*) FROM graph_edges WHERE workspace = ? GROUP BY edge_type", workspace)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var edgeType string
		var count int64
		if err := rows.Scan(&edgeType, &count); err != nil {
			return stats, err
		}
		stats.Edges.ByType[EdgeType(edgeType)] = count
	}

	return stats, nil
}

// CleanupExpiredEdges removes edges that have exceeded their TTL.
func (s *SQLiteStore) CleanupExpiredEdges(ctx context.Context) (int, error) {
	result, err := s.db.ExecContext(ctx, `
	DELETE FROM graph_edges
	WHERE ttl_days IS NOT NULL
	  AND datetime(created_at, '+' || ttl_days || ' days') < datetime('now')
	`)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// CleanupDanglingEdges removes edges that reference non-existent nodes.
func (s *SQLiteStore) CleanupDanglingEdges(ctx context.Context, workspace string) (int, error) {
	workspace = ws.CanonicalID(workspace)
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM graph_edges
		WHERE workspace = ?
		  AND (
	    NOT EXISTS (SELECT 1 FROM graph_nodes WHERE workspace = graph_edges.workspace AND node_id = graph_edges.from_id)
	    OR NOT EXISTS (SELECT 1 FROM graph_nodes WHERE workspace = graph_edges.workspace AND node_id = graph_edges.to_id)
	  )
	`, workspace)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// RecalculateDegrees recalculates in/out degrees for all nodes.
func (s *SQLiteStore) RecalculateDegrees(ctx context.Context, workspace string) error {
	workspace = ws.CanonicalID(workspace)
	_, err := s.db.ExecContext(ctx, `
		UPDATE graph_nodes
		SET
			in_degree = (SELECT COUNT(*) FROM graph_edges WHERE workspace = graph_nodes.workspace AND to_id = graph_nodes.node_id),
		out_degree = (SELECT COUNT(*) FROM graph_edges WHERE workspace = graph_nodes.workspace AND from_id = graph_nodes.node_id),
		updated_at = ?
	WHERE workspace = ?
	`, time.Now().UTC().Format(time.RFC3339), workspace)
	return err
}

// GetAllEdges returns all edges for a workspace (for PageRank computation).
func (s *SQLiteStore) GetAllEdges(ctx context.Context, workspace string) ([]Edge, error) {
	workspace = ws.CanonicalID(workspace)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace, from_id, from_type, to_id, to_type, edge_type, weight, created_at, ttl_days, metadata
		FROM graph_edges
		WHERE workspace = ?
	`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanEdges(rows)
}

// GetAllNodes returns all nodes for a workspace.
func (s *SQLiteStore) GetAllNodes(ctx context.Context, workspace string) ([]Node, error) {
	workspace = ws.CanonicalID(workspace)
	rows, err := s.db.QueryContext(ctx, `
		SELECT workspace, node_id, node_type, title, current_path, pagerank, in_degree, out_degree, last_seen, metadata, created_at, updated_at
		FROM graph_nodes
		WHERE workspace = ?
	`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanNodes(rows)
}

// BulkUpdatePageRank updates PageRank for multiple nodes efficiently.
//
// Index:
// - Purpose: Update PageRank scores for a workspace in a single transaction
// - Flow: begin tx → prepare statement → apply rank updates → commit
// - SideEffects: database transaction; pagerank updates
// - FailureModes: tx errors, statement errors, execution errors
// - Related: SQLiteStore.UpsertNodes
// - Keywords: pagerank, bulk_update, transaction, workspace_id
func (s *SQLiteStore) BulkUpdatePageRank(ctx context.Context, workspace string, ranks map[string]float64) error {
	workspace = ws.CanonicalID(workspace)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, "UPDATE graph_nodes SET pagerank = ?, updated_at = ? WHERE workspace = ? AND node_id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for nodeID, rank := range ranks {
		if _, err := stmt.ExecContext(ctx, rank, now, workspace, nodeID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Helper functions

func (s *SQLiteStore) scanNodes(rows *sql.Rows) ([]Node, error) {
	var nodes []Node
	for rows.Next() {
		var node Node
		var nodeType, lastSeen, metadata, createdAt, updatedAt string
		var title, currentPath sql.NullString

		if err := rows.Scan(
			&node.Workspace,
			&node.NodeID,
			&nodeType,
			&title,
			&currentPath,
			&node.PageRank,
			&node.InDegree,
			&node.OutDegree,
			&lastSeen,
			&metadata,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}

		node.NodeType = NodeType(nodeType)
		if title.Valid {
			node.Title = title.String
		}
		if currentPath.Valid {
			node.CurrentPath = currentPath.String
		}
		node.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
		node.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		node.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		if metadata != "" {
			_ = json.Unmarshal([]byte(metadata), &node.Metadata)
		}

		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (s *SQLiteStore) scanEdges(rows *sql.Rows) ([]Edge, error) {
	var edges []Edge
	for rows.Next() {
		var edge Edge
		var fromType, toType, edgeType, createdAt, metadata string
		var ttlDays sql.NullInt64

		if err := rows.Scan(
			&edge.ID,
			&edge.Workspace,
			&edge.FromID,
			&fromType,
			&edge.ToID,
			&toType,
			&edgeType,
			&edge.Weight,
			&createdAt,
			&ttlDays,
			&metadata,
		); err != nil {
			return nil, err
		}

		edge.FromType = NodeType(fromType)
		edge.ToType = NodeType(toType)
		edge.EdgeType = EdgeType(edgeType)
		edge.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if ttlDays.Valid {
			days := int(ttlDays.Int64)
			edge.TTLDays = &days
		}
		if metadata != "" {
			_ = json.Unmarshal([]byte(metadata), &edge.Metadata)
		}

		edges = append(edges, edge)
	}
	return edges, rows.Err()
}

// SearchNodes finds nodes matching a term in node_id, title, or current_path.
// Results are ordered by PageRank descending.
func (s *SQLiteStore) SearchNodes(ctx context.Context, workspace, term string, limit int) ([]Node, error) {
	workspace = ws.CanonicalID(workspace)
	if limit <= 0 {
		limit = 50
	}

	// Case-insensitive search across node_id, title, and current_path
	query := `
	SELECT workspace, node_id, node_type, title, current_path, pagerank, in_degree, out_degree, last_seen, metadata, created_at, updated_at
	FROM graph_nodes
	WHERE workspace = ?
	  AND (node_id LIKE '%' || ? || '%' COLLATE NOCASE
	    OR title LIKE '%' || ? || '%' COLLATE NOCASE
	    OR current_path LIKE '%' || ? || '%' COLLATE NOCASE)
	ORDER BY pagerank DESC
	LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, workspace, term, term, term, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanNodes(rows)
}

// GetEdgesBetween returns edges where both from_id and to_id are within the given nodeIDs set.
func (s *SQLiteStore) GetEdgesBetween(ctx context.Context, workspace string, nodeIDs []string) ([]Edge, error) {
	workspace = ws.CanonicalID(workspace)
	if len(nodeIDs) == 0 {
		return nil, nil
	}

	// Build placeholders for IN clause
	placeholders := make([]string, len(nodeIDs))
	args := make([]any, 0, 1+2*len(nodeIDs))
	args = append(args, workspace)

	for i, id := range nodeIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	inClause := strings.Join(placeholders, ",")

	// Add nodeIDs again for the to_id IN clause
	for _, id := range nodeIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
	SELECT id, workspace, from_id, from_type, to_id, to_type, edge_type, weight, created_at, ttl_days, metadata
	FROM graph_edges
	WHERE workspace = ?
	  AND from_id IN (%s)
	  AND to_id IN (%s)
	`, inClause, inClause)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanEdges(rows)
}

// FindShortestPath finds the shortest path(s) between two nodes using BFS.
// Returns paths as slices of node IDs. maxDepth limits search depth (default 5).
// Returns nil if no path exists within maxDepth.
func (s *SQLiteStore) FindShortestPath(ctx context.Context, workspace, fromID, toID string, maxDepth int) ([][]string, error) {
	workspace = ws.CanonicalID(workspace)
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if fromID == toID {
		return [][]string{{fromID}}, nil
	}

	// BFS state
	type pathState struct {
		nodeID string
		path   []string
	}

	visited := make(map[string]bool)
	queue := []pathState{{nodeID: fromID, path: []string{fromID}}}
	visited[fromID] = true

	var shortestPaths [][]string
	shortestLen := 0

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Stop if we've exceeded shortest path length found
		if shortestLen > 0 && len(current.path) > shortestLen {
			break
		}

		// Stop if we've exceeded max depth
		if len(current.path) > maxDepth {
			continue
		}

		// Get neighbors (both directions)
		neighbors, err := s.GetNeighbors(ctx, workspace, current.nodeID, NeighborOptions{
			Direction: "both",
		})
		if err != nil {
			return nil, fmt.Errorf("get neighbors for %s: %w", current.nodeID, err)
		}

		for _, neighbor := range neighbors {
			neighborID := neighbor.Node.NodeID

			// Found target
			if neighborID == toID {
				newPath := make([]string, len(current.path)+1)
				copy(newPath, current.path)
				newPath[len(current.path)] = toID

				if shortestLen == 0 || len(newPath) <= shortestLen {
					shortestLen = len(newPath)
					shortestPaths = append(shortestPaths, newPath)
				}
				continue
			}

			// Continue BFS if not visited and within depth
			if !visited[neighborID] && len(current.path) < maxDepth {
				visited[neighborID] = true
				newPath := make([]string, len(current.path)+1)
				copy(newPath, current.path)
				newPath[len(current.path)] = neighborID
				queue = append(queue, pathState{nodeID: neighborID, path: newPath})
			}
		}
	}

	return shortestPaths, nil
}
