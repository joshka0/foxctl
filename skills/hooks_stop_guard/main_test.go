package main

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/testwatch"
	"github.com/stretchr/testify/assert"
)

// Tests for defaultStopGuardConfig

func TestDefaultStopGuardConfig(t *testing.T) {
	cfg := defaultStopGuardConfig()

	assert.True(t, cfg.RequireTests)
	assert.True(t, cfg.RequireReview)
	assert.Equal(t, "review.approved", cfg.ReviewSubject)
	assert.Equal(t, 3, cfg.MaxFailures)
	assert.Equal(t, 50, cfg.MaxReviewMessages)
}

// Tests for matchesSubject helper

func TestMatchesSubject_ExactMatch(t *testing.T) {
	assert.True(t, matchesSubject("review.approved", "review.approved"))
}

func TestMatchesSubject_CaseInsensitive(t *testing.T) {
	assert.True(t, matchesSubject("Review.Approved", "review.approved"))
	assert.True(t, matchesSubject("REVIEW.APPROVED", "review.approved"))
}

func TestMatchesSubject_WithSuffix(t *testing.T) {
	assert.True(t, matchesSubject("review.approved: looks good", "review.approved"))
}

func TestMatchesSubject_WithWhitespace(t *testing.T) {
	assert.True(t, matchesSubject("  review.approved  ", "review.approved"))
}

func TestMatchesSubject_NoMatch(t *testing.T) {
	assert.False(t, matchesSubject("review.rejected", "review.approved"))
	assert.False(t, matchesSubject("partial.review.approved", "review.approved"))
}

func TestMatchesSubject_EmptyExpected(t *testing.T) {
	assert.True(t, matchesSubject("anything", ""))
	assert.True(t, matchesSubject("", ""))
}

func TestMatchesSubject_PrefixOnly(t *testing.T) {
	// Without colon suffix, should not match partial
	assert.False(t, matchesSubject("review.approved.extra", "review.approved"))
}

// Tests for asBool helper

func TestAsBool_BoolTrue(t *testing.T) {
	result, ok := asBool(true)
	assert.True(t, ok)
	assert.True(t, result)
}

func TestAsBool_BoolFalse(t *testing.T) {
	result, ok := asBool(false)
	assert.True(t, ok)
	assert.False(t, result)
}

func TestAsBool_StringTrue(t *testing.T) {
	for _, s := range []string{"true", "TRUE", "1", "yes", "YES", "y", "Y"} {
		result, ok := asBool(s)
		assert.True(t, ok, "should parse %q", s)
		assert.True(t, result, "should be true for %q", s)
	}
}

func TestAsBool_StringFalse(t *testing.T) {
	for _, s := range []string{"false", "FALSE", "0", "no", "NO", "n", "N"} {
		result, ok := asBool(s)
		assert.True(t, ok, "should parse %q", s)
		assert.False(t, result, "should be false for %q", s)
	}
}

func TestAsBool_Invalid(t *testing.T) {
	_, ok := asBool("maybe")
	assert.False(t, ok)

	_, ok = asBool(42)
	assert.False(t, ok)

	_, ok = asBool(nil)
	assert.False(t, ok)
}

// Tests for asInt helper

func TestAsInt_Int(t *testing.T) {
	result, ok := asInt(42)
	assert.True(t, ok)
	assert.Equal(t, 42, result)
}

func TestAsInt_Int64(t *testing.T) {
	result, ok := asInt(int64(42))
	assert.True(t, ok)
	assert.Equal(t, 42, result)
}

func TestAsInt_Float64(t *testing.T) {
	result, ok := asInt(float64(42.0))
	assert.True(t, ok)
	assert.Equal(t, 42, result)
}

func TestAsInt_String(t *testing.T) {
	result, ok := asInt("42")
	assert.True(t, ok)
	assert.Equal(t, 42, result)
}

func TestAsInt_InvalidString(t *testing.T) {
	_, ok := asInt("not-a-number")
	assert.False(t, ok)
}

func TestAsInt_Nil(t *testing.T) {
	_, ok := asInt(nil)
	assert.False(t, ok)
}

// Tests for applyConfig helper

func TestApplyConfig_Nil(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, nil)
	// Should not panic and keep defaults
	assert.True(t, cfg.RequireTests)
}

func TestApplyConfig_RequireTests(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{"require_tests": false})
	assert.False(t, cfg.RequireTests)
}

func TestApplyConfig_RequireReview(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{"require_review": false})
	assert.False(t, cfg.RequireReview)
}

func TestApplyConfig_ReviewSubject(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{"review_subject": "custom.subject"})
	assert.Equal(t, "custom.subject", cfg.ReviewSubject)
}

func TestApplyConfig_ReviewSender(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{"review_sender": "admin@example.com"})
	assert.Equal(t, "admin@example.com", cfg.ReviewSender)
}

func TestApplyConfig_ReviewKind(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{"review_kind": "approval"})
	assert.Equal(t, "approval", cfg.ReviewKind)
}

func TestApplyConfig_ReviewRecipient(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{"review_recipient": "actor-123"})
	assert.Equal(t, "actor-123", cfg.ReviewRecipient)
}

func TestApplyConfig_ReviewStream(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{"review_stream": "reviews"})
	assert.Equal(t, "reviews", cfg.ReviewStream)
}

func TestApplyConfig_MaxFailures(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{"max_failures": 5})
	assert.Equal(t, 5, cfg.MaxFailures)
}

func TestApplyConfig_MaxReviewMessages(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{"max_review_messages": 100})
	assert.Equal(t, 100, cfg.MaxReviewMessages)
}

func TestApplyConfig_MultipleFields(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{
		"require_tests":       false,
		"require_review":      false,
		"review_subject":      "lgtm",
		"max_failures":        10,
		"max_review_messages": 25,
	})

	assert.False(t, cfg.RequireTests)
	assert.False(t, cfg.RequireReview)
	assert.Equal(t, "lgtm", cfg.ReviewSubject)
	assert.Equal(t, 10, cfg.MaxFailures)
	assert.Equal(t, 25, cfg.MaxReviewMessages)
}

func TestApplyConfig_IgnoresInvalidValues(t *testing.T) {
	cfg := defaultStopGuardConfig()
	applyConfig(&cfg, map[string]any{
		"require_tests":       "invalid", // not a bool
		"max_failures":        "invalid", // not an int
		"max_review_messages": -1,        // must be > 0
	})

	// Should keep defaults
	assert.True(t, cfg.RequireTests)
	assert.Equal(t, 3, cfg.MaxFailures)
	assert.Equal(t, 50, cfg.MaxReviewMessages)
}

// Tests for buildTestContext helper

func TestBuildTestContext_SingleFailure(t *testing.T) {
	statuses := []testwatch.TestStatus{
		{
			WatcherID: "go-tests",
			Summary:   "1 failing",
			Failures: []testwatch.Failure{
				{Name: "TestFoo", File: "foo_test.go", Line: 42, Message: "expected true"},
			},
		},
	}

	result := buildTestContext(statuses, 3)

	assert.Contains(t, result, "go-tests")
	assert.Contains(t, result, "TestFoo")
	assert.Contains(t, result, "foo_test.go:42")
	assert.Contains(t, result, "expected true")
}

func TestBuildTestContext_TruncatesFailures(t *testing.T) {
	failures := make([]testwatch.Failure, 10)
	for i := 0; i < 10; i++ {
		failures[i] = testwatch.Failure{Name: "Test" + string(rune('A'+i))}
	}

	statuses := []testwatch.TestStatus{
		{WatcherID: "tests", Summary: "10 failing", Failures: failures},
	}

	result := buildTestContext(statuses, 3)

	assert.Contains(t, result, "TestA")
	assert.Contains(t, result, "TestB")
	assert.Contains(t, result, "TestC")
	assert.NotContains(t, result, "TestD")
}

func TestBuildTestContext_WithoutFileInfo(t *testing.T) {
	statuses := []testwatch.TestStatus{
		{
			WatcherID: "tests",
			Summary:   "1 failing",
			Failures: []testwatch.Failure{
				{Name: "TestNoFile"},
			},
		},
	}

	result := buildTestContext(statuses, 3)

	assert.Contains(t, result, "TestNoFile")
	assert.NotContains(t, result, ".go:")
}

func TestBuildTestContext_WithFileNoLine(t *testing.T) {
	statuses := []testwatch.TestStatus{
		{
			WatcherID: "tests",
			Summary:   "1 failing",
			Failures: []testwatch.Failure{
				{Name: "TestFileOnly", File: "foo_test.go"},
			},
		},
	}

	result := buildTestContext(statuses, 3)

	assert.Contains(t, result, "foo_test.go")
	assert.NotContains(t, result, ":0") // No line number
}

// Tests for hasReviewApproval helper

func TestHasReviewApproval_Match(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "review.approved", Sender: "reviewer", Kind: "approval"},
	}
	cfg := StopGuardConfig{ReviewSubject: "review.approved"}

	assert.True(t, hasReviewApproval(messages, cfg))
}

func TestHasReviewApproval_NoMatch(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "review.rejected", Sender: "reviewer"},
	}
	cfg := StopGuardConfig{ReviewSubject: "review.approved"}

	assert.False(t, hasReviewApproval(messages, cfg))
}

func TestHasReviewApproval_Empty(t *testing.T) {
	cfg := StopGuardConfig{ReviewSubject: "review.approved"}
	assert.False(t, hasReviewApproval(nil, cfg))
}

func TestHasReviewApproval_KindFilter(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "review.approved", Kind: "notification"},
	}
	cfg := StopGuardConfig{ReviewSubject: "review.approved", ReviewKind: "approval"}

	assert.False(t, hasReviewApproval(messages, cfg))
}

func TestHasReviewApproval_SenderFilter(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "review.approved", Sender: "user@example.com"},
	}
	cfg := StopGuardConfig{ReviewSubject: "review.approved", ReviewSender: "admin@example.com"}

	assert.False(t, hasReviewApproval(messages, cfg))
}

func TestHasReviewApproval_MultipleMessages(t *testing.T) {
	messages := []agent.BoardMessage{
		{Subject: "other.message"},
		{Subject: "review.approved"},
	}
	cfg := StopGuardConfig{ReviewSubject: "review.approved"}

	assert.True(t, hasReviewApproval(messages, cfg))
}

// Tests for StopGuardConfig structure

func TestStopGuardConfig_Fields(t *testing.T) {
	cfg := StopGuardConfig{
		RequireTests:      true,
		RequireReview:     true,
		ReviewSubject:     "approved",
		ReviewSender:      "admin",
		ReviewKind:        "approval",
		ReviewRecipient:   "actor-1",
		ReviewStream:      "reviews",
		MaxFailures:       5,
		MaxReviewMessages: 100,
	}

	assert.True(t, cfg.RequireTests)
	assert.True(t, cfg.RequireReview)
	assert.Equal(t, "approved", cfg.ReviewSubject)
	assert.Equal(t, "admin", cfg.ReviewSender)
	assert.Equal(t, "approval", cfg.ReviewKind)
	assert.Equal(t, "actor-1", cfg.ReviewRecipient)
	assert.Equal(t, "reviews", cfg.ReviewStream)
	assert.Equal(t, 5, cfg.MaxFailures)
	assert.Equal(t, 100, cfg.MaxReviewMessages)
}

// Tests for skill name constant

func TestSkillName(t *testing.T) {
	assert.Equal(t, "hooks/stop_guard", skillName)
}
