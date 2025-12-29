package templates

// JobSummary represents a job in the list
type JobSummary struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	Type      string `json:"type"`     // e.g. "skill", "job", "cron"
	Category  string `json:"category"` // e.g. "code", "fs", "text"
	Skill     string `json:"skill"`    // e.g. "symbols", "ls", "grep"
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
	Error     string `json:"error,omitempty"`
}

// JobDetail represents full job details
type JobDetail struct {
	JobSummary
	ResultData any      `json:"result_data,omitempty"`
	Stderr     string   `json:"stderr,omitempty"`
	Artifacts  []string `json:"artifacts,omitempty"`
}

// TaskSummary represents a task from the todo/manage skill
type TaskSummary struct {
	TaskID      string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Status      string  `json:"status,omitempty"`
	Priority    int     `json:"priority,omitempty"`
	Score       float64 `json:"score"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt string  `json:"completed_at,omitempty"`
	Notes       string  `json:"notes,omitempty"`
}

// TaskStats holds task statistics for display
type TaskStats struct {
	Total      int
	Pending    int
	InProgress int
	Completed  int
}

// JobStats holds aggregated statistics
type JobStats struct {
	Total     int            `json:"total"`
	ByState   map[string]int `json:"by_state"`
	ByCommand map[string]int `json:"by_command"`
	Recent    RecentStats    `json:"recent"`
}

// RecentStats holds recent activity
type RecentStats struct {
	LastHour int `json:"last_hour"`
	LastDay  int `json:"last_day"`
}

// InsightsData holds graph analysis data
type InsightsData struct {
	Nodes            []GraphNode `json:"nodes"`
	Cycles           [][]string  `json:"cycles"`
	TopologicalOrder []string    `json:"topological_order"`
}

// GraphNode represents a node in the task graph
type GraphNode struct {
	TaskID            string  `json:"task_id"`
	Title             string  `json:"-"` // Populated by joining with task list
	PageRank          float64 `json:"pagerank"`
	CriticalPathScore int     `json:"critical_path_score"`
	InDegree          int     `json:"in_degree"`
	OutDegree         int     `json:"out_degree"`
}

// MailboxMessage represents a message
type MailboxMessage struct {
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Kind      string `json:"kind"`
	Priority  int    `json:"priority"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// Reservation represents a file reservation
type Reservation struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Holder    string `json:"holder"`
	Mode      string `json:"mode"`
	ExpiresAt string `json:"expires_at"`
}

// BlackboardRecord represents a blackboard entry
type BlackboardRecord struct {
	ID      string `json:"id"`
	NS      string `json:"ns"`
	Topic   string `json:"topic"`
	TS      int64  `json:"ts"`
	TTLSec  int    `json:"ttl_sec"`
	Payload string `json:"payload"`
}

// SQLiteDatabase represents a database file
type SQLiteDatabase struct {
	Name         string `json:"name"`
	FriendlyName string `json:"friendly_name"`
	Path         string `json:"path"`
	Size         int64  `json:"size"`
}

// SQLiteTable represents a table in a database
type SQLiteTable struct {
	Name     string `json:"name"`
	RowCount int    `json:"row_count"`
}

// SearchResult represents a semantic search result
type SearchResult struct {
	Source      string  `json:"source"`
	ID          string  `json:"id"`
	Name        string  `json:"name,omitempty"`
	Path        string  `json:"path"`
	Similarity  float64 `json:"similarity"`
	RerankScore float64 `json:"rerank_score,omitempty"`
	FinalScore  float64 `json:"final_score"`
	Rank        int     `json:"rank"`
	SourceRank  int     `json:"source_rank"`
}

// SearchStats holds search result statistics
type SearchStats struct {
	TotalResults  int            `json:"total_results"`
	SourceCounts  map[string]int `json:"source_counts"`
	Reranked      bool           `json:"reranked"`
	EmbeddingDims int            `json:"embedding_dimensions"`
	LatencyMS     int64          `json:"latency_ms"`
}

// Workspace represents a known workspace
type Workspace struct {
	Path        string `json:"path"`
	Name        string `json:"name"`         // Last path component
	SessionCount int   `json:"session_count"`
	LastUsed    string `json:"last_used"`
}
