package main

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestMaxMessagesInContext(t *testing.T) {
	assert.Equal(t, 10, MaxMessagesInContext)
}

func TestDefaultRecipient(t *testing.T) {
	assert.Equal(t, "overseer", DefaultRecipient)
}

// Tests for priorityToEmoji helper

func TestPriorityToEmoji_P1(t *testing.T) {
	result := priorityToEmoji(1)
	assert.Contains(t, result, "P1")
	assert.Contains(t, result, "🔴")
}

func TestPriorityToEmoji_P2(t *testing.T) {
	result := priorityToEmoji(2)
	assert.Contains(t, result, "P2")
	assert.Contains(t, result, "🟠")
}

func TestPriorityToEmoji_P3(t *testing.T) {
	result := priorityToEmoji(3)
	assert.Contains(t, result, "P3")
	assert.Contains(t, result, "🟡")
}

func TestPriorityToEmoji_P4(t *testing.T) {
	result := priorityToEmoji(4)
	assert.Contains(t, result, "P4")
	assert.Contains(t, result, "🟢")
}

func TestPriorityToEmoji_P5(t *testing.T) {
	result := priorityToEmoji(5)
	assert.Contains(t, result, "P5")
	assert.Contains(t, result, "⚪")
}

func TestPriorityToEmoji_Default(t *testing.T) {
	result := priorityToEmoji(0)
	assert.Contains(t, result, "P5") // Default is P5
	assert.Contains(t, result, "⚪")
}

func TestPriorityToEmoji_NegativePriority(t *testing.T) {
	result := priorityToEmoji(-1)
	assert.Contains(t, result, "P5") // Falls through to default
}

func TestPriorityToEmoji_HighPriority(t *testing.T) {
	result := priorityToEmoji(100)
	assert.Contains(t, result, "P5") // Falls through to default
}

// Tests for kindToLabel helper

func TestKindToLabel_Instruction(t *testing.T) {
	result := kindToLabel(agent.BoardMessageKindInstruction)
	assert.Equal(t, "Instruction", result)
}

func TestKindToLabel_Info(t *testing.T) {
	result := kindToLabel(agent.BoardMessageKindInfo)
	assert.Equal(t, "Info", result)
}

func TestKindToLabel_Alert(t *testing.T) {
	result := kindToLabel(agent.BoardMessageKindAlert)
	assert.Equal(t, "Alert", result)
}

func TestKindToLabel_ReviewRequest(t *testing.T) {
	result := kindToLabel(agent.BoardMessageKindReviewRequest)
	assert.Equal(t, "Review Request", result)
}

func TestKindToLabel_Unknown(t *testing.T) {
	result := kindToLabel(agent.BoardMessageKind("custom"))
	assert.Equal(t, "custom", result)
}

func TestKindToLabel_Empty(t *testing.T) {
	result := kindToLabel(agent.BoardMessageKind(""))
	assert.Equal(t, "", result)
}

// Tests for buildOverseerContext helper

func TestBuildOverseerContext_Empty(t *testing.T) {
	result := buildOverseerContext(nil, "overseer")
	assert.Equal(t, "", result)
}

func TestBuildOverseerContext_EmptySlice(t *testing.T) {
	result := buildOverseerContext([]agent.BoardMessage{}, "overseer")
	assert.Equal(t, "", result)
}

func TestBuildOverseerContext_SingleMessage(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			ID:       "msg-1",
			Subject:  "Test Subject",
			Body:     "Test body content",
			Sender:   "user@example.com",
			Stream:   "general",
			Priority: 3,
			Kind:     agent.BoardMessageKindInfo,
		},
	}

	result := buildOverseerContext(messages, "overseer")

	assert.Contains(t, result, "Overseer Inbox")
	assert.Contains(t, result, "1 unread")
	assert.Contains(t, result, "Test Subject")
	assert.Contains(t, result, "Test body content")
	assert.Contains(t, result, "user@example.com")
	assert.Contains(t, result, "general")
	assert.Contains(t, result, "msg-1")
}

func TestBuildOverseerContext_RecipientShown(t *testing.T) {
	messages := []agent.BoardMessage{{Subject: "Test"}}

	result := buildOverseerContext(messages, "custom-recipient")

	assert.Contains(t, result, "custom-recipient")
}

func TestBuildOverseerContext_HighPriority(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject:  "Urgent",
			Priority: 1,
		},
	}

	result := buildOverseerContext(messages, "overseer")

	assert.Contains(t, result, "P1")
	assert.Contains(t, result, "🔴")
}

func TestBuildOverseerContext_AckRequired(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject:     "Action Needed",
			AckRequired: true,
		},
	}

	result := buildOverseerContext(messages, "overseer")

	assert.Contains(t, result, "ACTION REQUIRED")
	assert.Contains(t, result, "⚠️")
}

func TestBuildOverseerContext_NoAckRequired(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject:     "Info Only",
			AckRequired: false,
		},
	}

	result := buildOverseerContext(messages, "overseer")

	assert.NotContains(t, result, "ACTION REQUIRED")
}

func TestBuildOverseerContext_EmptyBody(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject: "Subject Only",
			Body:    "",
		},
	}

	result := buildOverseerContext(messages, "overseer")

	assert.Contains(t, result, "Subject Only")
	// Body section should be empty but not cause issues
}

func TestBuildOverseerContext_KindShown(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject: "Alert Message",
			Kind:    agent.BoardMessageKindAlert,
		},
	}

	result := buildOverseerContext(messages, "overseer")

	assert.Contains(t, result, "Alert")
}

func TestBuildOverseerContext_TruncatesAtMax(t *testing.T) {
	// Create more than MaxMessagesInContext messages
	messages := make([]agent.BoardMessage, MaxMessagesInContext+5)
	for i := range messages {
		messages[i] = agent.BoardMessage{
			ID:      "msg-" + string(rune('a'+i)),
			Subject: "Message",
			Stream:  "test",
		}
	}

	result := buildOverseerContext(messages, "overseer")

	assert.Contains(t, result, "more unread messages")
	assert.Contains(t, result, "5 more") // 15 - 10 = 5 remaining
}

func TestBuildOverseerContext_NoTruncationUnderMax(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "Message 1"},
		{Subject: "Message 2"},
	}

	result := buildOverseerContext(messages, "overseer")

	assert.NotContains(t, result, "more unread")
}

func TestBuildOverseerContext_ExactlyMaxMessages(t *testing.T) {
	messages := make([]agent.BoardMessage, MaxMessagesInContext)
	for i := range messages {
		messages[i] = agent.BoardMessage{
			Subject: "Message",
		}
	}

	result := buildOverseerContext(messages, "overseer")

	// Exactly at max, no "more" message needed
	assert.NotContains(t, result, "more unread")
}

func TestBuildOverseerContext_MultipleMessages(t *testing.T) {
	messages := []agent.BoardMessage{
		{ID: "msg-1", Subject: "First Message", Priority: 1},
		{ID: "msg-2", Subject: "Second Message", Priority: 3},
		{ID: "msg-3", Subject: "Third Message", Priority: 5},
	}

	result := buildOverseerContext(messages, "overseer")

	assert.Contains(t, result, "3 unread")
	assert.Contains(t, result, "First Message")
	assert.Contains(t, result, "Second Message")
	assert.Contains(t, result, "Third Message")
	assert.Contains(t, result, "msg-1")
	assert.Contains(t, result, "msg-2")
	assert.Contains(t, result, "msg-3")
}

func TestBuildOverseerContext_FormattingStructure(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			ID:       "msg-123",
			Subject:  "Test Subject",
			Body:     "Body text",
			Sender:   "sender@test.com",
			Stream:   "work",
			Priority: 2,
			Kind:     agent.BoardMessageKindInstruction,
		},
	}

	result := buildOverseerContext(messages, "overseer")

	// Check structural elements
	assert.Contains(t, result, "## 📬 Overseer Inbox")
	assert.Contains(t, result, "###") // Message headers use ###
	assert.Contains(t, result, "**From:**")
	assert.Contains(t, result, "**Kind:**")
	assert.Contains(t, result, "_ID:")
	assert.Contains(t, result, "| Stream:")
	assert.Contains(t, result, "---") // Separator between messages
}

func TestBuildOverseerContext_AllPriorities(t *testing.T) {
	priorities := []struct {
		priority int
		expected string
	}{
		{1, "P1"},
		{2, "P2"},
		{3, "P3"},
		{4, "P4"},
		{5, "P5"},
	}

	for _, tc := range priorities {
		messages := []agent.BoardMessage{{Subject: "Test", Priority: tc.priority}}
		result := buildOverseerContext(messages, "overseer")
		assert.Contains(t, result, tc.expected)
	}
}

func TestBuildOverseerContext_AllKinds(t *testing.T) {
	kinds := []struct {
		kind     agent.BoardMessageKind
		expected string
	}{
		{agent.BoardMessageKindInstruction, "Instruction"},
		{agent.BoardMessageKindInfo, "Info"},
		{agent.BoardMessageKindAlert, "Alert"},
		{agent.BoardMessageKindReviewRequest, "Review Request"},
	}

	for _, tc := range kinds {
		messages := []agent.BoardMessage{{Subject: "Test", Kind: tc.kind}}
		result := buildOverseerContext(messages, "overseer")
		assert.Contains(t, result, tc.expected)
	}
}

// Tests for BoardMessage structure usage

func TestBoardMessage_AllFields(t *testing.T) {
	msg := agent.BoardMessage{
		ID:          "msg-full",
		Subject:     "Full Message",
		Body:        "Complete body",
		Sender:      "admin",
		Recipient:   "overseer",
		Stream:      "main",
		Priority:    1,
		Kind:        agent.BoardMessageKindInstruction,
		AckRequired: true,
	}

	assert.Equal(t, "msg-full", msg.ID)
	assert.Equal(t, "Full Message", msg.Subject)
	assert.Equal(t, "Complete body", msg.Body)
	assert.Equal(t, "admin", msg.Sender)
	assert.Equal(t, "overseer", msg.Recipient)
	assert.Equal(t, "main", msg.Stream)
	assert.Equal(t, 1, msg.Priority)
	assert.Equal(t, agent.BoardMessageKindInstruction, msg.Kind)
	assert.True(t, msg.AckRequired)
}

// Tests for edge cases

func TestBuildOverseerContext_SpecialCharactersInSubject(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "Test <script>alert('xss')</script>"},
	}

	result := buildOverseerContext(messages, "overseer")

	// Should include the subject as-is (no HTML escaping in markdown)
	assert.Contains(t, result, "<script>")
}

func TestBuildOverseerContext_LongBody(t *testing.T) {
	longBody := ""
	for i := 0; i < 1000; i++ {
		longBody += "word "
	}

	messages := []agent.BoardMessage{
		{Subject: "Long", Body: longBody},
	}

	result := buildOverseerContext(messages, "overseer")

	// Should include the full body (no truncation in this function)
	assert.Contains(t, result, "word")
}

func TestBuildOverseerContext_EmptySender(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "No Sender", Sender: ""},
	}

	result := buildOverseerContext(messages, "overseer")

	// Should still have the From section, just empty
	assert.Contains(t, result, "**From:**")
}

func TestBuildOverseerContext_BroadcastRecipient(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "Broadcast", Recipient: "*"},
	}

	result := buildOverseerContext(messages, "*")

	assert.Contains(t, result, "Messages addressed to: *")
}

// Tests for InboxFilter structure (used in run function)

func TestInboxFilter_OnlyUnread(t *testing.T) {
	filter := agent.InboxFilter{
		OnlyUnread: true,
	}

	assert.True(t, filter.OnlyUnread)
}

func TestInboxFilter_OnlyUnsurfaced(t *testing.T) {
	filter := agent.InboxFilter{
		OnlyUnsurfaced: true,
	}

	assert.True(t, filter.OnlyUnsurfaced)
}

func TestInboxFilter_AllFields(t *testing.T) {
	filter := agent.InboxFilter{
		WorkspaceID:    "ws-123",
		ActorID:        "overseer",
		OnlyUnread:     true,
		OnlyUnsurfaced: true,
		Limit:          20,
	}

	assert.Equal(t, "ws-123", filter.WorkspaceID)
	assert.Equal(t, "overseer", filter.ActorID)
	assert.True(t, filter.OnlyUnread)
	assert.True(t, filter.OnlyUnsurfaced)
	assert.Equal(t, 20, filter.Limit)
}

// Tests for BoardMessageKind constants

func TestBoardMessageKind_Constants(t *testing.T) {
	assert.Equal(t, agent.BoardMessageKind("instruction"), agent.BoardMessageKindInstruction)
	assert.Equal(t, agent.BoardMessageKind("info"), agent.BoardMessageKindInfo)
	assert.Equal(t, agent.BoardMessageKind("alert"), agent.BoardMessageKindAlert)
	assert.Equal(t, agent.BoardMessageKind("review_request"), agent.BoardMessageKindReviewRequest)
}
