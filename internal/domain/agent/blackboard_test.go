package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBlackboardRecord_IsExpired(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name   string
		record BlackboardRecord
		want   bool
	}{
		{
			name: "no TTL - never expires",
			record: BlackboardRecord{
				TS:     now.Unix(),
				TTLSec: 0,
			},
			want: false,
		},
		{
			name: "not expired - within TTL",
			record: BlackboardRecord{
				TS:     now.Unix(),
				TTLSec: 3600, // 1 hour
			},
			want: false,
		},
		{
			name: "expired - past TTL",
			record: BlackboardRecord{
				TS:     now.Add(-2 * time.Hour).Unix(),
				TTLSec: 3600, // 1 hour TTL, but created 2 hours ago
			},
			want: true,
		},
		{
			name: "edge case - exactly at expiry boundary",
			record: BlackboardRecord{
				TS:     now.Add(-61 * time.Second).Unix(),
				TTLSec: 60,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.record.IsExpired()
			if got != tt.want {
				t.Errorf("IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlackboardRecord_IsLeased(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name   string
		record BlackboardRecord
		want   bool
	}{
		{
			name:   "no lease",
			record: BlackboardRecord{Lease: nil},
			want:   false,
		},
		{
			name: "active lease - not expired",
			record: BlackboardRecord{
				Lease: &Lease{
					Holder: "agent-123",
					Until:  now.Add(1 * time.Hour).Unix(),
				},
			},
			want: true,
		},
		{
			name: "expired lease",
			record: BlackboardRecord{
				Lease: &Lease{
					Holder: "agent-123",
					Until:  now.Add(-1 * time.Hour).Unix(),
				},
			},
			want: false,
		},
		{
			name: "lease expiring now",
			record: BlackboardRecord{
				Lease: &Lease{
					Holder: "agent-123",
					Until:  now.Unix() - 1, // Just expired
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.record.IsLeased()
			if got != tt.want {
				t.Errorf("IsLeased() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlackboardRecord_JSONSerialization(t *testing.T) {
	record := BlackboardRecord{
		ID:      "bb-123",
		NS:      "ns:project",
		Topic:   "code-review",
		TS:      1703520000,
		TTLSec:  3600,
		Payload: json.RawMessage(`{"key":"value"}`),
		CASRef:  "sha256:abc123",
		Lease: &Lease{
			Holder: "agent-456",
			Until:  1703523600,
		},
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got BlackboardRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.ID != record.ID {
		t.Errorf("ID = %q, want %q", got.ID, record.ID)
	}
	if got.NS != record.NS {
		t.Errorf("NS = %q, want %q", got.NS, record.NS)
	}
	if got.Topic != record.Topic {
		t.Errorf("Topic = %q, want %q", got.Topic, record.Topic)
	}
	if got.CASRef != record.CASRef {
		t.Errorf("CASRef = %q, want %q", got.CASRef, record.CASRef)
	}
	if got.Lease == nil {
		t.Fatal("Lease should not be nil")
	}
	if got.Lease.Holder != record.Lease.Holder {
		t.Errorf("Lease.Holder = %q, want %q", got.Lease.Holder, record.Lease.Holder)
	}
}

func TestBlackboardItem_JSONSerialization(t *testing.T) {
	item := BlackboardItem{
		TaskID:   "task-789",
		Priority: 2,
		Tags:     []string{"urgent", "review"},
		Data:     map[string]any{"description": "Test item", "count": 42},
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got BlackboardItem
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.TaskID != item.TaskID {
		t.Errorf("TaskID = %q, want %q", got.TaskID, item.TaskID)
	}
	if got.Priority != item.Priority {
		t.Errorf("Priority = %d, want %d", got.Priority, item.Priority)
	}
	if len(got.Tags) != len(item.Tags) {
		t.Errorf("Tags length = %d, want %d", len(got.Tags), len(item.Tags))
	}
}

func TestBlackboardMetadata_JSONSerialization(t *testing.T) {
	meta := BlackboardMetadata{
		Priority: 1,
		TTLSec:   7200,
		Tags:     []string{"critical", "infrastructure"},
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got BlackboardMetadata
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Priority != meta.Priority {
		t.Errorf("Priority = %d, want %d", got.Priority, meta.Priority)
	}
	if got.TTLSec != meta.TTLSec {
		t.Errorf("TTLSec = %d, want %d", got.TTLSec, meta.TTLSec)
	}
}

func TestLease_JSONSerialization(t *testing.T) {
	lease := Lease{
		Holder: "agent-abc",
		Until:  1703530000,
	}

	data, err := json.Marshal(lease)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got Lease
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Holder != lease.Holder {
		t.Errorf("Holder = %q, want %q", got.Holder, lease.Holder)
	}
	if got.Until != lease.Until {
		t.Errorf("Until = %d, want %d", got.Until, lease.Until)
	}
}

func TestBlackboardFilter_JSONSerialization(t *testing.T) {
	filter := BlackboardFilter{
		Tags:        []string{"review", "urgent"},
		PriorityMin: 2,
	}

	data, err := json.Marshal(filter)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got BlackboardFilter
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.PriorityMin != filter.PriorityMin {
		t.Errorf("PriorityMin = %d, want %d", got.PriorityMin, filter.PriorityMin)
	}
	if len(got.Tags) != len(filter.Tags) {
		t.Errorf("Tags length = %d, want %d", len(got.Tags), len(filter.Tags))
	}
}
