package inboxflow

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
	"github.com/joshka0/foxctl/internal/storage/contextbuffer"
)

type Dependencies struct {
	StorageRoot    string
	RunSkill       lifecycle.SkillRunner
	DetectIdentity lifecycle.IdentityDetector
}

func NewDependencies(deps lifecycle.Dependencies) Dependencies {
	return Dependencies{
		StorageRoot:    deps.StorageRoot,
		RunSkill:       deps.RunSkill,
		DetectIdentity: deps.DetectIdentity,
	}
}

type InboxPayload struct {
	SessionID    string `json:"sessionID,omitempty"`
	AltSessionID string `json:"session_id,omitempty"`
	ToolName     string `json:"tool_name,omitempty"`
	ToolInput    any    `json:"tool_input,omitempty"`
}

type PreToolRequest struct {
	Workspace string
	Payload   InboxPayload
}

type PostToolRequest struct {
	Workspace string
	Payload   InboxPayload
}

type InboxResponse struct {
	Decision  string `json:"decision"`
	Context   string `json:"context,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Workspace string `json:"workspace"`
	Enqueued  bool   `json:"enqueued,omitempty"`
	Recipient string `json:"recipient,omitempty"`
}

type overseerInboxEnvelope struct {
	Data struct {
		HookOutput struct {
			Context string `json:"context"`
			Reason  string `json:"reason"`
		} `json:"hook_output"`
	} `json:"data"`
}

func ReadInboxPreTool(ctx context.Context, deps Dependencies, req PreToolRequest) (InboxResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return InboxResponse{}, fmt.Errorf("detect workspace")
	}
	response := InboxResponse{
		Decision:  "approve",
		Workspace: target,
		Recipient: defaultRecipient(),
	}
	if deps.RunSkill == nil {
		return response, nil
	}
	sessionID := resolveInboxSessionID(target, req.Payload, deps.DetectIdentity)
	response.SessionID = sessionID
	var env overseerInboxEnvelope
	if err := deps.RunSkill(ctx, "hooks/overseer_inbox", map[string]any{
		"event":          "PreToolUse",
		"workspace_root": target,
		"session_id":     sessionID,
		"tool_name":      req.Payload.ToolName,
		"tool_input":     req.Payload.ToolInput,
	}, target, &env); err != nil {
		return response, nil
	}
	response.Context = strings.TrimSpace(env.Data.HookOutput.Context)
	return response, nil
}

func ReadInboxPostTool(ctx context.Context, deps Dependencies, req PostToolRequest) (InboxResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return InboxResponse{}, fmt.Errorf("detect workspace")
	}
	response := InboxResponse{
		Decision:  "approve",
		Workspace: target,
		Recipient: defaultRecipient(),
	}
	if deps.RunSkill == nil {
		return response, nil
	}
	sessionID := resolveInboxSessionID(target, req.Payload, deps.DetectIdentity)
	response.SessionID = sessionID
	var env overseerInboxEnvelope
	if err := deps.RunSkill(ctx, "hooks/overseer_inbox", map[string]any{
		"event":          "PostToolUse",
		"workspace_root": target,
		"session_id":     sessionID,
		"tool_name":      req.Payload.ToolName,
		"tool_input":     req.Payload.ToolInput,
	}, target, &env); err != nil {
		return response, nil
	}
	contextText := strings.TrimSpace(env.Data.HookOutput.Context)
	if contextText == "" {
		return response, nil
	}
	if sessionID == "" {
		response.Context = contextText
		return response, nil
	}
	store, err := contextbuffer.Open(ctx, deps.StorageRoot)
	if err != nil {
		response.Context = contextText
		return response, nil
	}
	defer func() { _ = store.Close() }()
	if _, err := store.Enqueue(ctx, contextbuffer.EnqueueParams{
		WorkspaceID: target,
		SessionID:   sessionID,
		Source:      "Overseer Messages",
		Text:        contextText,
		Priority:    1,
	}); err == nil {
		response.Enqueued = true
	} else {
		response.Context = contextText
	}
	return response, nil
}

func resolveInboxSessionID(workspacePath string, payload InboxPayload, detector lifecycle.IdentityDetector) string {
	sessionID := strings.TrimSpace(payload.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(payload.AltSessionID)
	}
	if sessionID == "" && detector != nil {
		sessionID, _, _ = detector(workspacePath)
	}
	return sessionID
}

func defaultRecipient() string {
	recipient := strings.TrimSpace(os.Getenv("AGENTCTL_OVERSEER_RECIPIENT"))
	if recipient == "" {
		return "overseer"
	}
	return recipient
}
