package agent

import (
	"encoding/json"
	"time"
)

// BlackboardRecord represents an entry on the shared blackboard.
type BlackboardRecord struct {
	ID      string          `json:"id"`
	NS      string          `json:"ns"`
	Topic   string          `json:"topic"`
	TS      int64           `json:"ts"`
	TTLSec  int             `json:"ttl_sec"`
	Payload json.RawMessage `json:"payload"`
	CASRef  string          `json:"cas_ref,omitempty"`
	Lease   *Lease          `json:"lease,omitempty"`
}

// Lease represents temporary ownership of a blackboard item.
type Lease struct {
	Holder string `json:"holder"` // Agent ID
	Until  int64  `json:"until"`  // Unix timestamp
}

// BlackboardFilter defines search criteria for blackboard queries.
type BlackboardFilter struct {
	Tags        []string `json:"tags,omitempty"`
	PriorityMin int      `json:"priority_min,omitempty"`
}

// BlackboardItem represents the payload structure for blackboard posts.
type BlackboardItem struct {
	TaskID   string                 `json:"task_id,omitempty"`
	Priority int                    `json:"priority,omitempty"`
	Tags     []string               `json:"tags,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// BlackboardMetadata contains additional metadata for blackboard entries.
type BlackboardMetadata struct {
	Priority int      `json:"priority,omitempty"`
	TTLSec   int      `json:"ttl_sec,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// IsExpired checks if a blackboard record has exceeded its TTL.
func (r *BlackboardRecord) IsExpired() bool {
	if r.TTLSec == 0 {
		return false
	}
	expiresAt := time.Unix(r.TS, 0).Add(time.Duration(r.TTLSec) * time.Second)
	return time.Now().UTC().After(expiresAt)
}

// IsLeased checks if a blackboard record is currently leased.
func (r *BlackboardRecord) IsLeased() bool {
	if r.Lease == nil {
		return false
	}
	return time.Now().UTC().Unix() < r.Lease.Until
}
