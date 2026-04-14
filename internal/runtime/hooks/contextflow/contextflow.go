package contextflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
	"github.com/joshka0/foxctl/internal/storage/contextbuffer"
)

type Dependencies struct {
	StorageRoot    string
	DetectIdentity lifecycle.IdentityDetector
}

type DrainRequest struct {
	Workspace string
	SessionID string
	AgentID   string
	Sources   []string
	Limit     int
}

type DrainResponse struct {
	Decision   string `json:"decision"`
	Context    string `json:"context,omitempty"`
	Count      int    `json:"count,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Workspace  string `json:"workspace"`
	SourceName string `json:"source_name,omitempty"`
}

func DrainUpdaterContext(ctx context.Context, deps Dependencies, req DrainRequest) (DrainResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return DrainResponse{}, fmt.Errorf("detect workspace")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" && deps.DetectIdentity != nil {
		sessionID, _, _ = deps.DetectIdentity(target)
	}
	response := DrainResponse{
		Decision:  "approve",
		SessionID: sessionID,
		Workspace: target,
	}
	if sessionID == "" {
		return response, nil
	}
	store, err := contextbuffer.Open(ctx, deps.StorageRoot)
	if err != nil {
		return response, err
	}
	defer func() { _ = store.Close() }()
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	sources := req.Sources
	if len(sources) == 0 {
		sources = []string{"context-updater"}
	}
	result, err := store.Drain(ctx, contextbuffer.DrainParams{
		WorkspaceID:  target,
		SessionID:    sessionID,
		AgentID:      strings.TrimSpace(req.AgentID),
		Sources:      sources,
		Limit:        limit,
		MarkConsumed: true,
	})
	if err != nil {
		return response, err
	}
	response.Count = len(result.Entries)
	if strings.TrimSpace(result.Markdown) != "" && response.Count > 0 {
		response.Context = "<context-updater>\n" + strings.TrimSpace(result.Markdown) + "\n</context-updater>"
		response.SourceName = "context-updater"
	}
	return response, nil
}
