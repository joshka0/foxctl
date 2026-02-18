package list

import "time"

// Request is the canonical list-runs request.
type Request struct {
	Limit   int    `json:"limit,omitempty"`
	Status  string `json:"status,omitempty"`
	Command string `json:"command,omitempty"`
	ActorID string `json:"actor_id,omitempty"`
}

// Item is one projected run record in the v2 list response.
type Item struct {
	RunID     string    `json:"run_id"`
	Status    string    `json:"status"`
	Command   string    `json:"command,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	ActorID   string    `json:"actor_id,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Response is the canonical list output.
type Response struct {
	Items []Item `json:"items"`
	Count int    `json:"count"`
}
