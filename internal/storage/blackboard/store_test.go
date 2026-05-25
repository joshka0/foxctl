package blackboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/quick"
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

func TestBlackboardClaimAllowsExpiredLeaseTakeover(t *testing.T) {
	ctx := context.Background()
	store := openTestBlackboardStore(t)
	now := time.Now().UTC()

	record := agent.BlackboardRecord{
		ID:      "expired-lease",
		NS:      "org/test",
		Topic:   "/tasks/todo",
		TS:      now.Unix(),
		TTLSec:  3600,
		Payload: json.RawMessage(`{"task_id":"expired"}`),
		Lease: &agent.Lease{
			Holder: "agent-old",
			Until:  now.Add(-time.Minute).Unix(),
		},
	}
	if err := store.Post(ctx, record); err != nil {
		t.Fatalf("post expired lease record: %v", err)
	}

	claimed, err := store.Claim(ctx, record.ID, "agent-new", 5*time.Minute)
	if err != nil {
		t.Fatalf("claim expired lease: %v", err)
	}
	if claimed.Lease == nil {
		t.Fatal("claimed record should have a lease")
	}
	if claimed.Lease.Holder != "agent-new" {
		t.Fatalf("lease holder = %q, want %q", claimed.Lease.Holder, "agent-new")
	}
	if !claimed.IsLeased() {
		t.Fatal("new lease should be active")
	}

	persisted, err := store.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("get claimed record: %v", err)
	}
	if persisted.Lease == nil || persisted.Lease.Holder != "agent-new" {
		t.Fatalf("persisted lease = %+v, want holder agent-new", persisted.Lease)
	}
}

func TestBlackboardClaimRejectsActiveLeaseAndPreservesHolder(t *testing.T) {
	ctx := context.Background()
	store := openTestBlackboardStore(t)
	now := time.Now().UTC()

	record := agent.BlackboardRecord{
		ID:      "active-lease",
		NS:      "org/test",
		Topic:   "/tasks/todo",
		TS:      now.Unix(),
		TTLSec:  3600,
		Payload: json.RawMessage(`{"task_id":"active"}`),
		Lease: &agent.Lease{
			Holder: "agent-owner",
			Until:  now.Add(time.Hour).Unix(),
		},
	}
	if err := store.Post(ctx, record); err != nil {
		t.Fatalf("post active lease record: %v", err)
	}

	_, err := store.Claim(ctx, record.ID, "agent-contender", 5*time.Minute)
	if !errors.Is(err, ErrAlreadyLeased) {
		t.Fatalf("claim active lease error = %v, want %v", err, ErrAlreadyLeased)
	}

	persisted, err := store.Get(ctx, record.ID)
	if err != nil {
		t.Fatalf("get active lease record: %v", err)
	}
	if persisted.Lease == nil {
		t.Fatal("active lease should remain present")
	}
	if persisted.Lease.Holder != "agent-owner" {
		t.Fatalf("lease holder = %q, want %q", persisted.Lease.Holder, "agent-owner")
	}
	if !persisted.IsLeased() {
		t.Fatal("active lease should remain active")
	}
}

func TestBlackboardClaimRejectsNonPositiveLeaseDuration(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		duration time.Duration
	}{
		{name: "zero duration", duration: 0},
		{name: "negative duration", duration: -time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestBlackboardStore(t)
			record := agent.BlackboardRecord{
				ID:      "invalid-duration",
				NS:      "org/test",
				Topic:   "/tasks/todo",
				TS:      time.Now().UTC().Unix(),
				TTLSec:  3600,
				Payload: json.RawMessage(`{"task_id":"invalid-duration"}`),
			}
			if err := store.Post(ctx, record); err != nil {
				t.Fatalf("post record: %v", err)
			}

			_, err := store.Claim(ctx, record.ID, "agent-new", tt.duration)
			if !errors.Is(err, ErrInvalidLeaseDuration) {
				t.Fatalf("claim duration %s error = %v, want %v", tt.duration, err, ErrInvalidLeaseDuration)
			}

			persisted, err := store.Get(ctx, record.ID)
			if err != nil {
				t.Fatalf("get record after rejected claim: %v", err)
			}
			if persisted.Lease != nil {
				t.Fatalf("rejected claim persisted lease %+v", persisted.Lease)
			}
		})
	}
}

func TestBlackboardClaimLeaseExpiryProperty(t *testing.T) {
	ctx := context.Background()
	store := openTestBlackboardStore(t)
	caseNum := 0

	property := func(pastOffsetSeconds, futureOffsetSeconds uint16) bool {
		caseNum++
		now := time.Now().UTC()
		pastOffset := time.Duration(pastOffsetSeconds%3600+60) * time.Second
		futureOffset := time.Duration(futureOffsetSeconds%3600+60) * time.Second

		expired := agent.BlackboardRecord{
			ID:      fmt.Sprintf("expired-%d", caseNum),
			NS:      "org/test",
			Topic:   "/tasks/property",
			TS:      now.Unix(),
			TTLSec:  3600,
			Payload: json.RawMessage(`{"kind":"expired"}`),
			Lease: &agent.Lease{
				Holder: "agent-old",
				Until:  now.Add(-pastOffset).Unix(),
			},
		}
		active := agent.BlackboardRecord{
			ID:      fmt.Sprintf("active-%d", caseNum),
			NS:      "org/test",
			Topic:   "/tasks/property",
			TS:      now.Unix(),
			TTLSec:  3600,
			Payload: json.RawMessage(`{"kind":"active"}`),
			Lease: &agent.Lease{
				Holder: "agent-owner",
				Until:  now.Add(futureOffset).Unix(),
			},
		}

		if err := store.Post(ctx, expired); err != nil {
			t.Logf("post expired lease record: %v", err)
			return false
		}
		if err := store.Post(ctx, active); err != nil {
			t.Logf("post active lease record: %v", err)
			return false
		}

		claimed, err := store.Claim(ctx, expired.ID, "agent-new", time.Minute)
		if err != nil {
			t.Logf("claim expired lease: %v", err)
			return false
		}
		if claimed.Lease == nil || claimed.Lease.Holder != "agent-new" || !claimed.IsLeased() {
			t.Logf("expired lease claim produced lease %+v", claimed.Lease)
			return false
		}

		if _, err := store.Claim(ctx, active.ID, "agent-contender", time.Minute); !errors.Is(err, ErrAlreadyLeased) {
			t.Logf("claim active lease error = %v, want %v", err, ErrAlreadyLeased)
			return false
		}
		persistedActive, err := store.Get(ctx, active.ID)
		if err != nil {
			t.Logf("get active lease record: %v", err)
			return false
		}
		if persistedActive.Lease == nil || persistedActive.Lease.Holder != "agent-owner" || !persistedActive.IsLeased() {
			t.Logf("active lease after rejected claim = %+v", persistedActive.Lease)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 50}); err != nil {
		t.Fatalf("blackboard lease expiry invariant failed: %v", err)
	}
}

func TestBlackboardPostRejectsInvalidPayloadJSON(t *testing.T) {
	ctx := context.Background()
	store := openTestBlackboardStore(t)

	err := store.Post(ctx, agent.BlackboardRecord{
		ID:      "invalid-payload",
		NS:      "org/test",
		Topic:   "/tasks/todo",
		TS:      time.Now().Unix(),
		TTLSec:  3600,
		Payload: json.RawMessage(`{`),
	})
	requireBlackboardErrorContains(t, "Post", err, "payload")

	records, err := store.Search(ctx, "org/test", "/tasks/todo", 10)
	if err != nil {
		t.Fatalf("search after rejected post: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("rejected invalid payload persisted %d records", len(records))
	}
}

func TestBlackboardRejectsCorruptStoredPayloadJSON(t *testing.T) {
	ctx := context.Background()
	store := openTestBlackboardStore(t)

	record := agent.BlackboardRecord{
		ID:      "corrupt-payload",
		NS:      "org/test",
		Topic:   "/tasks/todo",
		TS:      time.Now().Unix(),
		TTLSec:  3600,
		Payload: json.RawMessage(`{"task_id":"valid"}`),
	}
	if err := store.Post(ctx, record); err != nil {
		t.Fatalf("post valid record: %v", err)
	}
	sqlStore, ok := store.(*sqlStore)
	if !ok {
		t.Fatalf("store type = %T, want *sqlStore", store)
	}
	if _, err := sqlStore.db.ExecContext(ctx, `
		UPDATE blackboard SET payload = ? WHERE id = ?
	`, "{", record.ID); err != nil {
		t.Fatalf("corrupt payload: %v", err)
	}

	_, err := store.Get(ctx, record.ID)
	requireBlackboardErrorContains(t, "Get", err, "payload")
	_, err = store.Search(ctx, "org/test", "/tasks/todo", 10)
	requireBlackboardErrorContains(t, "Search", err, "payload")
	_, err = store.ListByTopic(ctx, "org/test", "/tasks/todo", 10)
	requireBlackboardErrorContains(t, "ListByTopic", err, "payload")
	_, err = store.Claim(ctx, record.ID, "agent-new", time.Minute)
	requireBlackboardErrorContains(t, "Claim", err, "payload")
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

func openTestBlackboardStore(t *testing.T) Store {
	t.Helper()

	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open blackboard store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close blackboard store: %v", err)
		}
	})
	return store
}

func requireBlackboardErrorContains(t *testing.T, operation string, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s error = nil, want error containing %q", operation, want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want it to contain %q", operation, err, want)
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
