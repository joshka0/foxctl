package tools

import (
	"context"
	"testing"
	"time"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBBPost_CreatesRecord(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		OpenBlackboardStore: func(ctx context.Context) (blackboard.Store, error) {
			return blackboard.Open(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	args := map[string]any{
		"topic":   "jobs",
		"payload": map[string]any{"job_id": "j1"},
	}
	result, err := r.bbPost(ctx, args)
	require.NoError(t, err)
	parsed := parseResult(t, result)
	assert.True(t, parsed["success"].(bool))

	recordID := parsed["record_id"].(string)

	store, err := blackboard.Open(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	record, err := store.Get(ctx, recordID)
	require.NoError(t, err)
	assert.Equal(t, "jobs", record.Topic)
}

func TestBBClaim_AcquiresLease(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	cfg := Config{
		WorkspaceRoot: rootDir,
		WorkspaceID:   "ws-1",
		ActorID:       "actor:1",
		OpenBlackboardStore: func(ctx context.Context) (blackboard.Store, error) {
			return blackboard.Open(ctx, rootDir)
		},
	}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	// Post via tool
	postRes, err := r.bbPost(ctx, map[string]any{
		"topic":   "jobs",
		"payload": map[string]any{"id": "j1"},
	})
	require.NoError(t, err)
	recordID := parseResult(t, postRes)["record_id"].(string)

	// Claim
	claimRes, err := r.bbClaim(ctx, map[string]any{
		"record_id":     recordID,
		"lease_seconds": 60,
	})
	require.NoError(t, err)
	parsed := parseResult(t, claimRes)
	assert.True(t, parsed["success"].(bool))

	store, err := blackboard.Open(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	record, err := store.Get(ctx, recordID)
	require.NoError(t, err)
	require.NotNil(t, record.Lease)
	assert.Equal(t, "actor:1", record.Lease.Holder)
}

func TestBBClaim_FailsIfAlreadyLeased(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	// Create stores for two actors
	cfg1 := Config{WorkspaceRoot: rootDir, WorkspaceID: "ws-1", ActorID: "actor:1", OpenBlackboardStore: func(ctx context.Context) (blackboard.Store, error) { return blackboard.Open(ctx, rootDir) }}
	r1, err := NewRegistry(cfg1, nil)
	require.NoError(t, err)

	cfg2 := Config{WorkspaceRoot: rootDir, WorkspaceID: "ws-1", ActorID: "actor:2", OpenBlackboardStore: func(ctx context.Context) (blackboard.Store, error) { return blackboard.Open(ctx, rootDir) }}
	r2, err := NewRegistry(cfg2, nil)
	require.NoError(t, err)

	postRes, err := r1.bbPost(ctx, map[string]any{"topic": "jobs", "payload": map[string]any{"id": "j1"}})
	require.NoError(t, err)
	recordID := parseResult(t, postRes)["record_id"].(string)

	// Actor 1 claims
	_, err = r1.bbClaim(ctx, map[string]any{"record_id": recordID})
	require.NoError(t, err)

	// Actor 2 tries to claim
	claimRes, err := r2.bbClaim(ctx, map[string]any{"record_id": recordID})
	require.NoError(t, err)
	// Expect tool failure in result
	require.True(t, claimRes.IsError)
	assert.Contains(t, claimRes.Content[0].(models.TextContent).Text, "claim failed")
}

func TestBBRelease_ClearsLease(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	cfg := Config{WorkspaceRoot: rootDir, WorkspaceID: "ws-1", ActorID: "actor:1", OpenBlackboardStore: func(ctx context.Context) (blackboard.Store, error) { return blackboard.Open(ctx, rootDir) }}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	postRes, err := r.bbPost(ctx, map[string]any{"topic": "jobs", "payload": map[string]any{"id": "j1"}})
	require.NoError(t, err)
	recordID := parseResult(t, postRes)["record_id"].(string)

	_, err = r.bbClaim(ctx, map[string]any{"record_id": recordID})
	require.NoError(t, err)

	// Release
	relRes, err := r.bbRelease(ctx, map[string]any{"record_id": recordID})
	require.NoError(t, err)
	parseResult(t, relRes)

	store, err := blackboard.Open(ctx, rootDir)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	record, err := store.Get(ctx, recordID)
	require.NoError(t, err)
	assert.Nil(t, record.Lease)
}

func TestBBSearch_FiltersUnleased(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	cfg := Config{WorkspaceRoot: rootDir, WorkspaceID: "ws-1", ActorID: "actor:1", OpenBlackboardStore: func(ctx context.Context) (blackboard.Store, error) { return blackboard.Open(ctx, rootDir) }}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	// Post 2 records
	_, err = r.bbPost(ctx, map[string]any{"topic": "jobs", "payload": map[string]any{"id": "j1"}})
	require.NoError(t, err)
	p2, err := r.bbPost(ctx, map[string]any{"topic": "jobs", "payload": map[string]any{"id": "j2"}})
	require.NoError(t, err)
	id2 := parseResult(t, p2)["record_id"].(string)

	// Claim 1
	_, err = r.bbClaim(ctx, map[string]any{"record_id": id2})
	require.NoError(t, err)

	// Search all
	res, err := r.bbSearch(ctx, map[string]any{"topic": "jobs"})
	require.NoError(t, err)
	all := parseResult(t, res)["records"].([]any)
	assert.Len(t, all, 2)

	// Search unleased
	res, err = r.bbSearch(ctx, map[string]any{"topic": "jobs", "unleased_only": true})
	require.NoError(t, err)
	unleased := parseResult(t, res)["records"].([]any)
	assert.Len(t, unleased, 1)
}

func TestBBWatch_ReturnsNewRecords(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()

	cfg := Config{WorkspaceRoot: rootDir, WorkspaceID: "ws-1", ActorID: "actor:1", OpenBlackboardStore: func(ctx context.Context) (blackboard.Store, error) { return blackboard.Open(ctx, rootDir) }}
	r, err := NewRegistry(cfg, nil)
	require.NoError(t, err)

	// Start watch in goroutine
	done := make(chan map[string]any)
	go func() {
		res, _ := r.bbWatch(ctx, map[string]any{"topic": "chat", "timeout_seconds": 2.0}) //nolint:errcheck
		// If timeout hits, it returns success with whatever it found
		done <- parseResult(t, res)
	}()

	time.Sleep(500 * time.Millisecond)                                       // Give watch time to start in CI environments
	_, _ = r.bbPost(ctx, map[string]any{"topic": "chat", "payload": "msg1"}) //nolint:errcheck

	res := <-done
	records, ok := res["records"].([]any)
	require.True(t, ok, "expected records to be []any, got %T", res["records"])
	assert.Len(t, records, 1)
}
