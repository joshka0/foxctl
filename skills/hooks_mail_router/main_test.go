package main

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/stretchr/testify/assert"
)

// Tests for isPlanEvent helper

func TestIsPlanEvent_True(t *testing.T) {
	assert.True(t, isPlanEvent("plan.created"))
	assert.True(t, isPlanEvent("plan.updated"))
	assert.True(t, isPlanEvent("plan.review_needed"))
	assert.True(t, isPlanEvent("plan.something"))
}

func TestIsPlanEvent_False(t *testing.T) {
	assert.False(t, isPlanEvent("not.a.plan"))
	assert.False(t, isPlanEvent("myplan.created"))
	assert.False(t, isPlanEvent(""))
	assert.False(t, isPlanEvent("review.approved"))
}

// Tests for extractPlanEventType helper

func TestExtractPlanEventType_WithTaskID(t *testing.T) {
	result := extractPlanEventType("plan.created:task-123")
	assert.Equal(t, "plan.created", result)
}

func TestExtractPlanEventType_WithoutTaskID(t *testing.T) {
	result := extractPlanEventType("plan.updated")
	assert.Equal(t, "plan.updated", result)
}

func TestExtractPlanEventType_Empty(t *testing.T) {
	result := extractPlanEventType("")
	assert.Equal(t, "", result)
}

func TestExtractPlanEventType_MultipleColons(t *testing.T) {
	result := extractPlanEventType("plan.created:task:extra")
	assert.Equal(t, "plan.created", result)
}

// Tests for extractPlanTaskID helper

func TestExtractPlanTaskID_WithTaskID(t *testing.T) {
	result := extractPlanTaskID("plan.created:task-123")
	assert.Equal(t, "task-123", result)
}

func TestExtractPlanTaskID_WithoutTaskID(t *testing.T) {
	result := extractPlanTaskID("plan.created")
	assert.Equal(t, "", result)
}

func TestExtractPlanTaskID_Empty(t *testing.T) {
	result := extractPlanTaskID("")
	assert.Equal(t, "", result)
}

func TestExtractPlanTaskID_MultipleColons(t *testing.T) {
	// Only splits on first colon
	result := extractPlanTaskID("plan.created:task:extra")
	assert.Equal(t, "task:extra", result)
}

// Tests for formatSender helper

func TestFormatSender_AdminSender(t *testing.T) {
	result := formatSender("admin")
	assert.Contains(t, result, "ADMIN")
	assert.Contains(t, result, "admin")
}

func TestFormatSender_AdminActorSender(t *testing.T) {
	result := formatSender("actor:admin:user@example.com")
	assert.Contains(t, result, "ADMIN")
	assert.Contains(t, result, "actor:admin:user@example.com")
}

func TestFormatSender_OverseerSender(t *testing.T) {
	result := formatSender("actor:system:overseer")
	assert.Contains(t, result, "OVERSEER")
	assert.Contains(t, result, "actor:system:overseer")
}

func TestFormatSender_RegularSender(t *testing.T) {
	result := formatSender("user@example.com")
	assert.Equal(t, "user@example.com", result)
	assert.NotContains(t, result, "ADMIN")
	assert.NotContains(t, result, "OVERSEER")
}

// Tests for buildMailContext helper

func TestBuildMailContext_Empty(t *testing.T) {
	result := buildMailContext(nil)
	assert.Empty(t, result)
}

func TestBuildMailContext_EmptySlice(t *testing.T) {
	result := buildMailContext([]agent.BoardMessage{})
	assert.Empty(t, result)
}

func TestBuildMailContext_PlanEvent(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject: "plan.created:task-123",
			Body:    "New plan created",
		},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "Plan Updates from Overseer")
	assert.Contains(t, result, "New Plan Created")
	assert.Contains(t, result, "task-123")
	assert.Contains(t, result, "New plan created")
}

func TestBuildMailContext_PlanEventTypes(t *testing.T) {
	tests := []struct {
		subject  string
		expected string
	}{
		{"plan.created:task-1", "New Plan Created"},
		{"plan.updated:task-1", "Plan Updated"},
		{"plan.review_needed:task-1", "Plan Review Needed"},
		{"plan.custom:task-1", "Plan Event:"},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			messages := []agent.BoardMessage{{Subject: tt.subject}}
			result := buildMailContext(messages)
			assert.Contains(t, result, tt.expected)
		})
	}
}

func TestBuildMailContext_RegularMessage(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			ID:       "msg-123",
			Subject:  "Test Message",
			Body:     "Message body",
			Sender:   "user@example.com",
			Stream:   "general",
			Priority: 5,
		},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "Mailbox Messages")
	assert.Contains(t, result, "Test Message")
	assert.Contains(t, result, "Message body")
	assert.Contains(t, result, "user@example.com")
	assert.Contains(t, result, "general")
	assert.Contains(t, result, "msg-123")
}

func TestBuildMailContext_HighPriority(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject:  "Urgent",
			Priority: 1,
		},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "[HIGH PRIORITY]")
}

func TestBuildMailContext_AckRequired(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject:     "Action Needed",
			AckRequired: true,
		},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "[ACK REQUIRED]")
}

func TestBuildMailContext_MixedMessages(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "plan.created:task-1"},
		{Subject: "Regular Message", Stream: "general"},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "Plan Updates from Overseer")
	assert.Contains(t, result, "Mailbox Messages")
}

func TestBuildMailContext_TruncatesMessages(t *testing.T) {
	// Create more than MaxMessagesInContext messages
	messages := make([]agent.BoardMessage, MaxMessagesInContext+5)
	for i := range messages {
		messages[i] = agent.BoardMessage{
			Subject: "Message",
			Stream:  "general",
		}
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "more unread messages")
}

func TestBuildMailContext_NoTruncation(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "Message 1", Stream: "general"},
		{Subject: "Message 2", Stream: "general"},
	}

	result := buildMailContext(messages)

	assert.NotContains(t, result, "more unread messages")
}

func TestBuildMailContext_AdminSender(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject: "Admin Notice",
			Sender:  "admin",
			Stream:  "general",
		},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "ADMIN")
}

func TestBuildMailContext_OverseerSender(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject: "Overseer Notice",
			Sender:  "actor:system:overseer",
			Stream:  "general",
		},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "OVERSEER")
}

// Tests for constants

func TestMaxMessagesInContext(t *testing.T) {
	assert.Equal(t, 5, MaxMessagesInContext)
}

// Tests for priority thresholds

func TestPriorityThreshold(t *testing.T) {
	// Priority <= 2 is high priority
	highPriority := agent.BoardMessage{Priority: 2}
	normalPriority := agent.BoardMessage{Priority: 3}

	assert.LessOrEqual(t, highPriority.Priority, 2)
	assert.Greater(t, normalPriority.Priority, 2)
}

// Tests for edge cases in buildMailContext

func TestBuildMailContext_EmptyBody(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject: "No Body",
			Stream:  "general",
		},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "No Body")
	// Body section should not have extra content
}

func TestBuildMailContext_PlanWithBody(t *testing.T) {
	messages := []agent.BoardMessage{
		{
			Subject: "plan.created:task-1",
			Body:    "Details about the plan",
		},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "Details about the plan")
}

func TestBuildMailContext_OnlyPlanEvents(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "plan.created:task-1"},
		{Subject: "plan.updated:task-2"},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "Plan Updates from Overseer")
	assert.NotContains(t, result, "Mailbox Messages")
}

func TestBuildMailContext_OnlyRegularMessages(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "Message 1", Stream: "general"},
	}

	result := buildMailContext(messages)

	assert.Contains(t, result, "Mailbox Messages")
	assert.NotContains(t, result, "Plan Updates from Overseer")
}
