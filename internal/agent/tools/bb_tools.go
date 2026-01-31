package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	errspkg "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/oklog/ulid/v2"
)

// Blackboard tool limits to prevent abuse.
const (
	maxBBTTLSeconds   = 86400 // 24 hours max TTL
	maxBBLimit        = 100   // Max records per search
	maxBBLeaseSeconds = 3600  // 1 hour max lease
)

// registerBBTools registers blackboard (topic bus) tools.
func (r *Registry) registerBBTools() error {
	// bb.post
	postTool := dstools.NewFuncTool(
		"bb.post",
		"Post a record to a blackboard topic.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"topic": {
					Type:        "string",
					Description: "Topic name",
					Required:    true,
				},
				"payload": {
					Type:        "object",
					Description: "JSON payload",
					Required:    true,
				},
				"ttl_seconds": {
					Type:        "integer",
					Description: "Time-to-live in seconds (default 3600)",
				},
				"cas_ref": {
					Type:        "string",
					Description: "Optional CAS digest for large payloads",
				},
			},
		},
		r.wrapWithTelemetry("bb.post", r.bbPost),
	)
	if err := r.tools.Register(postTool); err != nil {
		return fmt.Errorf("register bb.post: %w", err)
	}

	// bb.search
	searchTool := dstools.NewFuncTool(
		"bb.search",
		"Search records in a topic.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"topic": {
					Type:        "string",
					Description: "Topic name",
					Required:    true,
				},
				"limit": {
					Type:        "integer",
					Description: "Max records to return (default 20)",
				},
				"unleased_only": {
					Type:        "boolean",
					Description: "Only return records that are not currently leased",
				},
			},
		},
		r.wrapWithTelemetry("bb.search", r.bbSearch),
	)
	if err := r.tools.Register(searchTool); err != nil {
		return fmt.Errorf("register bb.search: %w", err)
	}

	// bb.claim
	claimTool := dstools.NewFuncTool(
		"bb.claim",
		"Claim a record with a lease.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"record_id": {
					Type:        "string",
					Description: "Record ID to claim",
					Required:    true,
				},
				"lease_seconds": {
					Type:        "integer",
					Description: "Lease duration in seconds (default 300)",
				},
			},
		},
		r.wrapWithTelemetry("bb.claim", r.bbClaim),
	)
	if err := r.tools.Register(claimTool); err != nil {
		return fmt.Errorf("register bb.claim: %w", err)
	}

	// bb.release
	releaseTool := dstools.NewFuncTool(
		"bb.release",
		"Release a claimed record.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"record_id": {
					Type:        "string",
					Description: "Record ID to release",
					Required:    true,
				},
			},
		},
		r.wrapWithTelemetry("bb.release", r.bbRelease),
	)
	if err := r.tools.Register(releaseTool); err != nil {
		return fmt.Errorf("register bb.release: %w", err)
	}

	// bb.list (alias to search)
	listTool := dstools.NewFuncTool(
		"bb.list",
		"List records in a topic (alias for bb.search).",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"topic": {
					Type:        "string",
					Description: "Topic name",
					Required:    true,
				},
				"limit": {
					Type:        "integer",
					Description: "Max records to return",
				},
			},
		},
		r.wrapWithTelemetry("bb.list", r.bbList),
	)
	if err := r.tools.Register(listTool); err != nil {
		return fmt.Errorf("register bb.list: %w", err)
	}

	// bb.watch
	watchTool := dstools.NewFuncTool(
		"bb.watch",
		"Watch for new records on a topic.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"topic": {
					Type:        "string",
					Description: "Topic name",
					Required:    true,
				},
				"since_ts": {
					Type:        "integer",
					Description: "Start timestamp (Unix epoch)",
				},
				"timeout_seconds": {
					Type:        "integer",
					Description: "Max time to wait for records (default 30)",
				},
			},
		},
		r.wrapWithTelemetry("bb.watch", r.bbWatch),
	)
	if err := r.tools.Register(watchTool); err != nil {
		return fmt.Errorf("register bb.watch: %w", err)
	}

	return nil
}

// bbPost implements bb.post.
func (r *Registry) bbPost(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openBlackboardStore == nil {
		return errorResult("blackboard store not configured"), nil
	}

	topic, ok := args["topic"].(string)
	if !ok || topic == "" {
		return errorResult("topic is required"), nil
	}

	payloadRaw, ok := args["payload"]
	if !ok {
		return errorResult("payload is required"), nil
	}

	payloadBytes, err := r.marshalPayload(payloadRaw)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal payload: %v", err)), nil
	}

	ttlSeconds := 3600
	if t, ok := args["ttl_seconds"].(float64); ok && t > 0 {
		// Clamp to max before casting to prevent bypass via large values
		if t > float64(maxBBTTLSeconds) {
			t = float64(maxBBTTLSeconds)
		}
		ttlSeconds = int(t)
	}

	casRef := ""
	if c, ok := args["cas_ref"].(string); ok {
		casRef = c
	}

	store, err := r.openBlackboardStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open blackboard store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close blackboard store") }()

	record := agent.BlackboardRecord{
		ID:      ulid.Make().String(),
		NS:      r.config.WorkspaceID,
		Topic:   topic,
		TS:      time.Now().Unix(),
		TTLSec:  ttlSeconds,
		Payload: payloadBytes,
		CASRef:  casRef,
	}

	if err := store.Post(ctx, record); err != nil {
		return errorResult(fmt.Sprintf("post record: %v", err)), nil
	}

	return successResult(map[string]any{
		"record_id": record.ID,
		"topic":     topic,
		"success":   true,
	}), nil
}

// bbSearch implements bb.search.
func (r *Registry) bbSearch(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openBlackboardStore == nil {
		return errorResult("blackboard store not configured"), nil
	}

	topic, ok := args["topic"].(string)
	if !ok || topic == "" {
		return errorResult("topic is required"), nil
	}

	limit := 20
	const maxLimit = 1000 // prevent memory exhaustion
	if l, ok := args["limit"].(float64); ok && l > 0 {
		// Clamp to max before casting to prevent bypass via large values
		if l > float64(maxBBLimit) {
			l = float64(maxBBLimit)
		}
		limit = int(l)
		if limit > maxLimit {
			limit = maxLimit
		}
	}

	unleasedOnly := false
	if u, ok := args["unleased_only"].(bool); ok {
		unleasedOnly = u
	}

	store, err := r.openBlackboardStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open blackboard store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close blackboard store") }()

	records, err := store.Search(ctx, r.config.WorkspaceID, topic, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("search records: %v", err)), nil
	}

	if unleasedOnly {
		filtered := []agent.BlackboardRecord{} // Empty slice, not nil (nil serializes to null in JSON)
		now := time.Now().Unix()
		for _, rec := range records {
			isLeased := false
			if rec.Lease != nil {
				if rec.Lease.Until > now {
					isLeased = true
				}
			}
			if !isLeased {
				filtered = append(filtered, rec)
			}
		}
		records = filtered
	}

	return successResult(map[string]any{
		"records": records,
		"count":   len(records),
	}), nil
}

// bbClaim implements bb.claim.
func (r *Registry) bbClaim(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openBlackboardStore == nil {
		return errorResult("blackboard store not configured"), nil
	}

	recordID, ok := args["record_id"].(string)
	if !ok || recordID == "" {
		return errorResult("record_id is required"), nil
	}

	leaseSeconds := 300
	const maxLeaseSeconds = 3600 // 1 hour max
	if l, ok := args["lease_seconds"].(float64); ok && l > 0 {
		// Clamp to max before casting to prevent bypass via large values
		if l > float64(maxBBLeaseSeconds) {
			l = float64(maxBBLeaseSeconds)
		}
		leaseSeconds = int(l)
		if leaseSeconds > maxLeaseSeconds {
			leaseSeconds = maxLeaseSeconds
		}
	}

	store, err := r.openBlackboardStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open blackboard store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close blackboard store") }()

	record, err := store.Claim(ctx, recordID, r.config.ActorID, time.Duration(leaseSeconds)*time.Second)
	if err != nil {
		return errorResult(fmt.Sprintf("claim failed: %v", err)), nil
	}

	leasedUntil := int64(0)
	if record.Lease != nil {
		leasedUntil = record.Lease.Until
	}

	return successResult(map[string]any{
		"record":       record,
		"leased_until": leasedUntil,
		"success":      true,
	}), nil
}

// bbRelease implements bb.release.
func (r *Registry) bbRelease(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openBlackboardStore == nil {
		return errorResult("blackboard store not configured"), nil
	}

	recordID, ok := args["record_id"].(string)
	if !ok || recordID == "" {
		return errorResult("record_id is required"), nil
	}

	store, err := r.openBlackboardStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open blackboard store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close blackboard store") }()

	if err := store.Release(ctx, recordID); err != nil {
		return errorResult(fmt.Sprintf("release failed: %v", err)), nil
	}

	return successResult(map[string]any{
		"record_id": recordID,
		"released":  true,
	}), nil
}

// bbList implements bb.list.
func (r *Registry) bbList(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	return r.bbSearch(ctx, args)
}

// bbWatch implements bb.watch.
func (r *Registry) bbWatch(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openBlackboardStore == nil {
		return errorResult("blackboard store not configured"), nil
	}

	topic, ok := args["topic"].(string)
	if !ok || topic == "" {
		return errorResult("topic is required"), nil
	}

	sinceTS := int64(0)
	if s, ok := args["since_ts"].(float64); ok {
		sinceTS = int64(s)
	}

	timeoutSeconds := 30
	if t, ok := args["timeout_seconds"].(float64); ok && t > 0 {
		timeoutSeconds = int(t)
	}

	store, err := r.openBlackboardStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open blackboard store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close blackboard store") }()

	watchCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	recordsCh, errCh := store.Watch(watchCtx, r.config.WorkspaceID, topic, sinceTS)

	collected := []agent.BlackboardRecord{} // Empty slice, not nil (nil serializes to null in JSON)
	lastTS := sinceTS

	drain := func() {
		for {
			select {
			case rec, ok := <-recordsCh:
				if !ok {
					return
				}
				collected = append(collected, rec)
				if rec.TS > lastTS {
					lastTS = rec.TS
				}
			default:
				return
			}
		}
	}

	// fallback retrieves records via store.Search when the watch channel yields nothing.
	// ORDERING ASSUMPTION: store.Search returns records ordered by ts DESC (newest first).
	// The reverse iteration (from len-1 to 0) ensures 'collected' is populated in
	// chronological order (oldest first), matching the expected output format.
	// Do NOT change store.Search ordering without updating this loop accordingly.
	fallback := func() {
		if len(collected) > 0 {
			return
		}

		// Use a short timeout for fallback search to avoid blocking indefinitely
		fallbackCtx, fallbackCancel := context.WithTimeout(ctx, 5*time.Second)
		defer fallbackCancel()

		records, err := store.Search(fallbackCtx, r.config.WorkspaceID, topic, 20)
		if err != nil {
			return
		}

		// Iterate in reverse to convert DESC order to chronological (ASC) for collected.
		// This ensures lastTS tracks the most recent timestamp correctly.
		for i := len(records) - 1; i >= 0; i-- {
			rec := records[i]
			if rec.TS <= sinceTS {
				continue
			}
			collected = append(collected, rec)
			if rec.TS > lastTS {
				lastTS = rec.TS
			}
		}
	}

	for {
		select {
		case <-watchCtx.Done():
			drain()
			fallback()
			return successResult(map[string]any{
				"records": collected,
				"last_ts": lastTS,
				"count":   len(collected),
			}), nil
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				return errorResult(fmt.Sprintf("watch error: %v", err)), nil
			}
			drain()
			fallback()
			return successResult(map[string]any{
				"records": collected,
				"last_ts": lastTS,
				"count":   len(collected),
			}), nil
		case rec, ok := <-recordsCh:
			if !ok {
				drain()
				fallback()
				return successResult(map[string]any{
					"records": collected,
					"last_ts": lastTS,
					"count":   len(collected),
				}), nil
			}
			collected = append(collected, rec)
			if rec.TS > lastTS {
				lastTS = rec.TS
			}
		}
	}
}

func (r *Registry) marshalPayload(v any) ([]byte, error) {
	return json.Marshal(v)
}
