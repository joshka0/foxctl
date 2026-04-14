package blackboard

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
)

func TestBlackboardStore(t *testing.T) {
	ctx := context.Background()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Open store
	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Test Post
	t.Run("Post", func(t *testing.T) {
		payload := map[string]any{
			"task_id":  "task-001",
			"title":    "Process webhook",
			"priority": 5,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("failed to marshal payload: %v", err)
		}

		record := agent.BlackboardRecord{
			ID:      "bb-item-001",
			NS:      "org/test",
			Topic:   "/tasks/todo",
			TS:      time.Now().Unix(),
			TTLSec:  3600,
			Payload: json.RawMessage(payloadBytes),
		}

		if err := store.Post(ctx, record); err != nil {
			t.Fatalf("failed to post record: %v", err)
		}
	})

	// Test Get
	t.Run("Get", func(t *testing.T) {
		record, err := store.Get(ctx, "bb-item-001")
		if err != nil {
			t.Fatalf("failed to get record: %v", err)
		}

		if record.ID != "bb-item-001" {
			t.Errorf("expected ID bb-item-001, got %s", record.ID)
		}
		if record.Topic != "/tasks/todo" {
			t.Errorf("expected topic /tasks/todo, got %s", record.Topic)
		}
	})

	// Test Search
	t.Run("Search", func(t *testing.T) {
		records, err := store.Search(ctx, "org/test", "/tasks/todo", 10)
		if err != nil {
			t.Fatalf("failed to search records: %v", err)
		}

		if len(records) != 1 {
			t.Errorf("expected 1 record, got %d", len(records))
		}
	})

	// Test Claim
	t.Run("Claim", func(t *testing.T) {
		record, err := store.Claim(ctx, "bb-item-001", "agent-001", 5*time.Minute)
		if err != nil {
			t.Fatalf("failed to claim record: %v", err)
		}

		if record.Lease == nil {
			t.Fatal("expected lease to be set")
		}
		if record.Lease.Holder != "agent-001" {
			t.Errorf("expected lease holder agent-001, got %s", record.Lease.Holder)
		}

		// Verify item is leased
		if !record.IsLeased() {
			t.Error("record should be leased")
		}
	})

	// Test Claim already leased
	t.Run("ClaimAlreadyLeased", func(t *testing.T) {
		_, err := store.Claim(ctx, "bb-item-001", "agent-002", 5*time.Minute)
		if err != ErrAlreadyLeased {
			t.Errorf("expected ErrAlreadyLeased, got %v", err)
		}
	})

	// Test Release
	t.Run("Release", func(t *testing.T) {
		if err := store.Release(ctx, "bb-item-001"); err != nil {
			t.Fatalf("failed to release record: %v", err)
		}

		// Verify lease is released
		record, err := store.Get(ctx, "bb-item-001")
		if err != nil {
			t.Fatalf("failed to get record: %v", err)
		}
		if record.IsLeased() {
			t.Error("record should not be leased after release")
		}
	})

	// Test ListByTopic
	t.Run("ListByTopic", func(t *testing.T) {
		// Add another record
		payload := map[string]any{
			"task_id": "task-002",
			"title":   "Send notification",
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("failed to marshal payload: %v", err)
		}

		record := agent.BlackboardRecord{
			ID:      "bb-item-002",
			NS:      "org/test",
			Topic:   "/tasks/todo",
			TS:      time.Now().Unix(),
			TTLSec:  3600,
			Payload: json.RawMessage(payloadBytes),
		}

		if err := store.Post(ctx, record); err != nil {
			t.Fatalf("failed to post second record: %v", err)
		}

		// List all items in topic
		records, err := store.ListByTopic(ctx, "org/test", "/tasks/todo", 10)
		if err != nil {
			t.Fatalf("failed to list by topic: %v", err)
		}

		if len(records) != 2 {
			t.Errorf("expected 2 records, got %d", len(records))
		}
	})

	// Test Delete
	t.Run("Delete", func(t *testing.T) {
		if err := store.Delete(ctx, "bb-item-001"); err != nil {
			t.Fatalf("failed to delete record: %v", err)
		}

		_, err := store.Get(ctx, "bb-item-001")
		if err == nil {
			t.Error("expected error getting deleted record")
		}
	})
}

func TestBlackboardRecordExpiry(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name     string
		record   agent.BlackboardRecord
		expected bool
	}{
		{
			name: "not expired - future TTL",
			record: agent.BlackboardRecord{
				TS:     now,
				TTLSec: 3600, // 1 hour from now
			},
			expected: false,
		},
		{
			name: "expired - past TTL",
			record: agent.BlackboardRecord{
				TS:     now - 7200, // 2 hours ago
				TTLSec: 3600,       // TTL was 1 hour
			},
			expected: true,
		},
		{
			name: "no TTL - never expires",
			record: agent.BlackboardRecord{
				TS:     now - 86400, // 1 day ago
				TTLSec: 0,           // no TTL
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.record.IsExpired(); got != tt.expected {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBlackboardRecordLease(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name     string
		record   agent.BlackboardRecord
		expected bool
	}{
		{
			name: "leased - future expiry",
			record: agent.BlackboardRecord{
				Lease: &agent.Lease{
					Holder: "agent-001",
					Until:  now + 300, // 5 minutes from now
				},
			},
			expected: true,
		},
		{
			name: "not leased - past expiry",
			record: agent.BlackboardRecord{
				Lease: &agent.Lease{
					Holder: "agent-001",
					Until:  now - 300, // 5 minutes ago
				},
			},
			expected: false,
		},
		{
			name: "not leased - no lease",
			record: agent.BlackboardRecord{
				Lease: nil,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.record.IsLeased(); got != tt.expected {
				t.Errorf("IsLeased() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBlackboardWatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Open store
	store, err := Open(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Start watching from 1 second ago to ensure we catch records created "now"
	fromTS := time.Now().Add(-1 * time.Second).Unix()
	recordCh, errCh := store.Watch(ctx, "org/test", "/tasks/watch", fromTS)

	// Post a record after starting watch
	go func() {
		time.Sleep(100 * time.Millisecond)
		payload := map[string]any{
			"task_id": "watch-task-001",
			"title":   "Watch test task",
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Errorf("failed to marshal payload: %v", err)
			return
		}

		record := agent.BlackboardRecord{
			ID:      "watch-item-001",
			NS:      "org/test",
			Topic:   "/tasks/watch",
			TS:      time.Now().Unix(),
			TTLSec:  3600,
			Payload: json.RawMessage(payloadBytes),
		}

		if err := store.Post(ctx, record); err != nil {
			t.Logf("failed to post record: %v", err)
		}
	}()

	// Wait for the record to be streamed
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("watch error: %v", err)
		}
	case record := <-recordCh:
		if record.ID != "watch-item-001" {
			t.Errorf("expected record ID watch-item-001, got %s", record.ID)
		}
		if record.Topic != "/tasks/watch" {
			t.Errorf("expected topic /tasks/watch, got %s", record.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for watch record")
	}
}
