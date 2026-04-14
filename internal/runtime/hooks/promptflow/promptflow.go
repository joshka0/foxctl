package promptflow

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
	"github.com/joshka0/foxctl/internal/runtime/hooks/sessionmode"
)

type Dependencies struct {
	RunSkill       lifecycle.SkillRunner
	DetectIdentity lifecycle.IdentityDetector
}

func NewDependencies(deps lifecycle.Dependencies) Dependencies {
	return Dependencies{
		RunSkill:       deps.RunSkill,
		DetectIdentity: deps.DetectIdentity,
	}
}

type AnchorPayload struct {
	Prompt       string `json:"prompt,omitempty"`
	SessionID    string `json:"sessionID,omitempty"`
	AltSessionID string `json:"session_id,omitempty"`
}

type AnchorRequest struct {
	Workspace string
	Payload   AnchorPayload
}

type AnchorResponse struct {
	Decision  string `json:"decision"`
	Context   string `json:"context,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Workspace string `json:"workspace"`
	AnchorSet bool   `json:"anchor_set,omitempty"`
	TodoMode  bool   `json:"todo_mode,omitempty"`
}

type sessionAnchorSetEnvelope struct {
	Data struct {
		Found   bool   `json:"found"`
		Message string `json:"message"`
	} `json:"data"`
}

var anchorPromptStripRE = regexp.MustCompile(`(?i)(/anchor|@anchor|anchor this|anchor it|anchor prompt|/todo)`)

func DetectAnchor(ctx context.Context, deps Dependencies, req AnchorRequest) (AnchorResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return AnchorResponse{}, fmt.Errorf("detect workspace")
	}
	prompt := strings.TrimSpace(req.Payload.Prompt)
	response := AnchorResponse{
		Decision:  "approve",
		Workspace: target,
	}
	if prompt == "" {
		return response, nil
	}

	hasAnchor := anchorPromptDetected(prompt)
	hasTodo := todoPromptDetected(prompt)
	if !hasAnchor && !hasTodo {
		return response, nil
	}

	sessionID := firstNonEmpty(strings.TrimSpace(req.Payload.SessionID), strings.TrimSpace(req.Payload.AltSessionID))
	if sessionID == "" && deps.DetectIdentity != nil {
		sessionID, _, _ = deps.DetectIdentity(target)
	}
	response.SessionID = sessionID

	contextParts := []string{}
	if hasAnchor {
		cleanPrompt := normalizeAnchorPrompt(prompt)
		if cleanPrompt == "" {
			contextParts = append(contextParts, "Usage: /anchor <goal>")
		} else {
			anchorPersisted := false
			if deps.RunSkill != nil {
				var env sessionAnchorSetEnvelope
				if err := deps.RunSkill(ctx, "session/anchor", map[string]any{
					"operation":   "set",
					"workspace":   target,
					"session_id":  sessionID,
					"main_prompt": cleanPrompt,
					"trigger":     "user_prompt_submit",
				}, target, &env); err == nil && sessionID != "" {
					anchorPersisted = true
				}
			}
			if sessionID != "" {
				if err := sessionmode.SetAnchor(sessionID, cleanPrompt, time.Now()); err == nil {
					anchorPersisted = true
				}
			}
			if anchorPersisted {
				response.AnchorSet = true
				contextParts = append(contextParts, buildAnchorClarifyPrompt(cleanPrompt))
			} else {
				contextParts = append(contextParts, "Anchor mode: unable to persist anchor for this session.")
			}
		}
	}

	if hasTodo {
		if sessionID != "" {
			_ = sessionmode.EnableTodo(sessionID, time.Now())
			response.TodoMode = true
			contextParts = append(contextParts, "**Todo mode**: enabled")
		} else {
			contextParts = append(contextParts, "Todo mode: missing session ID.")
		}
	}

	response.Context = strings.Join(contextParts, "\n\n")
	return response, nil
}

func anchorPromptDetected(prompt string) bool {
	lower := strings.ToLower(prompt)
	return strings.Contains(lower, "anchor this") ||
		strings.Contains(lower, "anchor it") ||
		strings.Contains(lower, "anchor prompt") ||
		strings.Contains(lower, "@anchor") ||
		strings.Contains(lower, "/anchor")
}

func todoPromptDetected(prompt string) bool {
	return strings.Contains(strings.ToLower(prompt), "/todo")
}

func normalizeAnchorPrompt(prompt string) string {
	cleaned := anchorPromptStripRE.ReplaceAllString(prompt, "")
	cleaned = strings.TrimSpace(strings.TrimLeft(cleaned, ":- "))
	return cleaned
}

func buildAnchorClarifyPrompt(goal string) string {
	return "**Anchor set**: " + goal + "\n\n" +
		"**IMPORTANT - Verify Understanding**:\n" +
		"Before proceeding, use AskUser to confirm you understood the request correctly:\n" +
		"1. Briefly restate what you think the user is asking for\n" +
		"2. List any assumptions you're making\n" +
		"3. Ask if there are parts that need clarification\n\n" +
		"If the user corrects or refines the goal, re-anchor with: `/anchor <corrected goal>`\n\n" +
		"**Stop hook**: will check for incomplete tasks before allowing stop"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
