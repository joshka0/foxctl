package kill

// Request is the canonical input for v2 kill orchestration.
type Request struct {
	RequestID string `json:"request_id,omitempty"`
	RunID     string `json:"run_id"`
}

// Response is the canonical kill output.
type Response struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}
