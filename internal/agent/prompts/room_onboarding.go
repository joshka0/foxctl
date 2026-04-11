package prompts

import (
	"fmt"
	"strings"
)

type RoomOnboardingOptions struct {
	RoomID      string
	WorkspaceID string
	Role        string
	RoomRole    string
}

const roomOnboardingHeader = "ROOM ONBOARDING:"

func ComposeRoomAwarePrompt(basePrompt string, opts RoomOnboardingOptions) string {
	basePrompt = strings.TrimSpace(basePrompt)
	block := strings.TrimSpace(RoomOnboardingBlock(opts))
	if block == "" {
		return basePrompt
	}
	if strings.Contains(basePrompt, roomOnboardingHeader) {
		return basePrompt
	}
	if basePrompt == "" {
		return block
	}
	return basePrompt + "\n\n" + block
}

func RoomOnboardingBlock(opts RoomOnboardingOptions) string {
	roomID := strings.TrimSpace(opts.RoomID)
	if roomID == "" {
		return ""
	}
	roleLabel := firstNonEmpty(strings.TrimSpace(opts.RoomRole), strings.TrimSpace(opts.Role), "participant")
	lines := []string{
		roomOnboardingHeader,
		fmt.Sprintf("- You are attached to room %q in workspace %q as %q.", roomID, firstNonEmpty(strings.TrimSpace(opts.WorkspaceID), "default"), roleLabel),
		"- Read the shared room skills first: `agentctl-room` and `agentctl-room-agent`.",
		fmt.Sprintf("- On entry, orient with: `agentctl room status %s`, `agentctl room inbox %s --actor <you>`, and `agentctl room task list %s`.", roomID, roomID, roomID),
		"- Use durable room actions instead of pane-only status updates: claim, touch, block, complete, send, ack, resolve.",
	}
	switch strings.TrimSpace(strings.ToLower(roleLabel)) {
	case "coordinator", "overseer":
		lines = append(lines,
			"- Because you are acting as coordinator, also read `agentctl-room-operator`.",
			"- You are responsible for routing, stale work, review closure, and final coordinator decisions.",
			fmt.Sprintf("- Coordinator controls: `agentctl room resolve %s <message-id> --mode read`, `agentctl room task assign|reassign|reclaim %s ...`, `agentctl room coordinator set %s <participant>`.", roomID, roomID, roomID),
		)
	case "reviewer", "security-review":
		lines = append(lines,
			"- Because you are acting as reviewer, also read `agentctl-room-operator`.",
			"- Review behavior: findings first, then verdict (`approved` or `blocked`), then scope and any non-blocking follow-ups.",
			"- Write review conclusions into the room timeline or task notes, not only pane chat.",
		)
	case "frontend-eng":
		lines = append(lines,
			"- Prioritize user-facing correctness, role-gating, accessibility, and keeping UI data contracts aligned with the live API.",
		)
	case "backend-eng":
		lines = append(lines,
			"- Prioritize contract safety, durable state, role enforcement, auditability, and deterministic tests before polish work.",
		)
	case "collaborator":
		lines = append(lines,
			"- Prefer explicit task ownership, concise durable updates, and early escalation over silent parallel work.",
		)
	}
	return strings.Join(lines, "\n")
}

func RoomOnboardingMessage(opts RoomOnboardingOptions) (string, string) {
	roleLabel := firstNonEmpty(strings.TrimSpace(opts.RoomRole), strings.TrimSpace(opts.Role), "participant")
	subject := fmt.Sprintf("Room onboarding: %s", roleLabel)
	body := RoomOnboardingBlock(opts)
	return subject, body
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
