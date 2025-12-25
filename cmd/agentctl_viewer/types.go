package main

import "time"

// Envelope is the canonical output format for robot handlers.
// Matches the agentctl envelope specification.
type Envelope struct {
	Version int            `json:"version"`
	Status  string         `json:"status"`
	Command string         `json:"command"`
	Data    any            `json:"data"`
	Meta    map[string]any `json:"meta"`
	Error   *EnvelopeError `json:"error"`
}

// EnvelopeError represents an error in the envelope format.
type EnvelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// newEnvelope creates a success envelope with the given command and data.
func newEnvelope(command string, data any, meta map[string]any) Envelope {
	if meta == nil {
		meta = make(map[string]any)
	}
	meta["generated_at"] = nowUTC()
	return Envelope{
		Version: 1,
		Status:  "ok",
		Command: command,
		Data:    data,
		Meta:    meta,
		Error:   nil,
	}
}

// newErrorEnvelope creates an error envelope.
func newErrorEnvelope(command, code, message string) Envelope {
	return Envelope{
		Version: 1,
		Status:  "error",
		Command: command,
		Data:    map[string]any{},
		Meta:    map[string]any{"generated_at": nowUTC()},
		Error:   &EnvelopeError{Code: code, Message: message},
	}
}

type jobSummary struct {
	ID        string `json:"id"`
	Command   string `json:"command"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
	Error     string `json:"error,omitempty"`
}

type jobDetail struct {
	jobSummary
	ResultData any
	Stderr     string
	Artifacts  []string
}

type detailTab int

const (
	tabInfo detailTab = iota
	tabResult
	tabStderr
	tabArtifacts
)

type viewMode int

const (
	viewJobs viewMode = iota
	viewTasks
	viewInsights
	viewMailbox
	viewReservations
	viewStats
	viewBlackboard
	viewSQLite
)

// viewModeLabels maps view modes to their display labels
var viewModeLabels = map[viewMode]string{
	viewJobs:         "Jobs",
	viewTasks:        "Tasks",
	viewInsights:     "Insights",
	viewMailbox:      "Mailbox",
	viewReservations: "Reservations",
	viewStats:        "Stats",
	viewBlackboard:   "Blackboard",
	viewSQLite:       "SQLite",
}

// viewModeCount is the total number of view modes
const viewModeCount = 8

type mailboxMessage struct {
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	Kind      string `json:"kind"`
	Priority  int    `json:"priority"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type reservation struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Holder    string `json:"holder"`
	Mode      string `json:"mode"`
	ExpiresAt string `json:"expires_at"`
}

type graphNode struct {
	TaskID            string  `json:"task_id"`
	PageRank          float64 `json:"pagerank"`
	CriticalPathScore int     `json:"critical_path_score"`
	InDegree          int     `json:"in_degree"`
	OutDegree         int     `json:"out_degree"`
}

type insightsData struct {
	Nodes            []graphNode `json:"nodes"`
	Cycles           [][]string  `json:"cycles"`
	TopologicalOrder []string    `json:"topological_order"`
}

// skillInput is used to safely marshal JSON input for agentctl run commands.
type skillInput struct {
	Operation   string    `json:"operation"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	Limit       int       `json:"limit,omitempty"`
	Inbox       *inboxReq `json:"inbox,omitempty"`
}

type inboxReq struct {
	ActorID    string `json:"actor_id"`
	OnlyUnread bool   `json:"only_unread"`
	Limit      int    `json:"limit"`
}

// recommendation is used in priority output data.
type recommendation struct {
	TaskID     string  `json:"task_id"`
	Title      string  `json:"title"`
	Score      float64 `json:"score"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// jobStats holds aggregated job statistics.
type jobStats struct {
	Total     int            `json:"total"`
	ByState   map[string]int `json:"by_state"`
	ByCommand map[string]int `json:"by_command"`
	Recent    recentStats    `json:"recent"`
}

// recentStats holds recent job activity stats.
type recentStats struct {
	LastHour int `json:"last_hour"`
	LastDay  int `json:"last_day"`
}

// blackboardRecord represents a blackboard entry.
type blackboardRecord struct {
	ID      string `json:"id"`
	NS      string `json:"ns"`
	Topic   string `json:"topic"`
	TS      int64  `json:"ts"`
	TTLSec  int    `json:"ttl_sec"`
	Payload string `json:"payload"`
	CASRef  string `json:"cas_ref,omitempty"`
	Lease   *lease `json:"lease,omitempty"`
}

// lease represents a blackboard lease.
type lease struct {
	Holder string `json:"holder"`
	Until  int64  `json:"until"`
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
