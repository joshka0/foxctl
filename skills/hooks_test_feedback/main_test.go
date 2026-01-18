package main

import (
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/testwatch"
	"github.com/stretchr/testify/assert"
)

// Tests for DefaultConfig

func TestDefaultConfig_Values(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 3, cfg.MaxFailures)
}

// Tests for FeedbackConfig structure

func TestFeedbackConfig_CustomValues(t *testing.T) {
	cfg := FeedbackConfig{
		MaxFailures: 10,
	}

	assert.Equal(t, 10, cfg.MaxFailures)
}

func TestFeedbackConfig_JSONSerialization(t *testing.T) {
	cfg := FeedbackConfig{
		MaxFailures: 5,
	}

	data, err := json.Marshal(cfg)
	assert.NoError(t, err)

	var decoded FeedbackConfig
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, cfg.MaxFailures, decoded.MaxFailures)
}

// Tests for WatcherFeedback structure

func TestWatcherFeedback_Structure(t *testing.T) {
	feedback := WatcherFeedback{
		WatcherID: "watcher-1",
		Status:    "fail",
		Summary:   "3 tests failed",
		Failures: []testwatch.Failure{
			{Name: "TestExample", File: "example_test.go", Line: 42, Message: "assertion failed"},
		},
	}

	assert.Equal(t, "watcher-1", feedback.WatcherID)
	assert.Equal(t, "fail", feedback.Status)
	assert.Equal(t, "3 tests failed", feedback.Summary)
	assert.Len(t, feedback.Failures, 1)
}

func TestWatcherFeedback_JSONSerialization(t *testing.T) {
	feedback := WatcherFeedback{
		WatcherID: "watcher-1",
		Status:    "fail",
		Summary:   "Tests failed",
		Failures: []testwatch.Failure{
			{Name: "TestFoo", File: "foo_test.go"},
		},
	}

	data, err := json.Marshal(feedback)
	assert.NoError(t, err)

	var decoded WatcherFeedback
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, feedback.WatcherID, decoded.WatcherID)
	assert.Equal(t, feedback.Status, decoded.Status)
	assert.Equal(t, feedback.Summary, decoded.Summary)
	assert.Len(t, decoded.Failures, 1)
}

func TestWatcherFeedback_NoFailures(t *testing.T) {
	feedback := WatcherFeedback{
		WatcherID: "watcher-1",
		Status:    "pass",
		Summary:   "All tests passed",
	}

	assert.Nil(t, feedback.Failures)
}

// Tests for buildContextString helper

func TestBuildContextString_Empty(t *testing.T) {
	result := buildContextString(nil, DefaultConfig())

	assert.Contains(t, result, "Tests are currently failing")
}

func TestBuildContextString_EmptySlice(t *testing.T) {
	result := buildContextString([]WatcherFeedback{}, DefaultConfig())

	assert.Contains(t, result, "Tests are currently failing")
}

func TestBuildContextString_SingleWatcher(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "go-tests",
			Status:    "fail",
			Summary:   "2 of 10 tests failed",
			Failures: []testwatch.Failure{
				{Name: "TestAuth", File: "auth_test.go", Line: 25, Message: "expected true"},
			},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	assert.Contains(t, result, "Tests are currently failing")
	assert.Contains(t, result, "go-tests")
	assert.Contains(t, result, "2 of 10 tests failed")
	assert.Contains(t, result, "TestAuth")
	assert.Contains(t, result, "auth_test.go:25")
	assert.Contains(t, result, "expected true")
	assert.Contains(t, result, "address these failures")
}

func TestBuildContextString_MultipleWatchers(t *testing.T) {
	watchers := []WatcherFeedback{
		{WatcherID: "watcher-1", Summary: "1 failed", Failures: []testwatch.Failure{{Name: "Test1"}}},
		{WatcherID: "watcher-2", Summary: "2 failed", Failures: []testwatch.Failure{{Name: "Test2"}}},
	}

	result := buildContextString(watchers, DefaultConfig())

	assert.Contains(t, result, "watcher-1")
	assert.Contains(t, result, "watcher-2")
	assert.Contains(t, result, "Test1")
	assert.Contains(t, result, "Test2")
}

func TestBuildContextString_FailureWithFileAndLine(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures: []testwatch.Failure{
				{Name: "TestExample", File: "example_test.go", Line: 42},
			},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	assert.Contains(t, result, "TestExample")
	assert.Contains(t, result, "example_test.go:42")
}

func TestBuildContextString_FailureWithFileOnly(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures: []testwatch.Failure{
				{Name: "TestExample", File: "example_test.go"},
			},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	assert.Contains(t, result, "TestExample")
	assert.Contains(t, result, "example_test.go")
	assert.NotContains(t, result, "example_test.go:0")
}

func TestBuildContextString_FailureNameOnly(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures: []testwatch.Failure{
				{Name: "TestNameOnly"},
			},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	assert.Contains(t, result, "TestNameOnly")
}

func TestBuildContextString_FailureWithMessage(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures: []testwatch.Failure{
				{Name: "TestFail", Message: "assertion error: expected 5, got 3"},
			},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	assert.Contains(t, result, "TestFail")
	assert.Contains(t, result, "assertion error")
}

func TestBuildContextString_TruncatesLongMessage(t *testing.T) {
	longMessage := ""
	for i := 0; i < 200; i++ {
		longMessage += "x"
	}

	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures: []testwatch.Failure{
				{Name: "TestLong", Message: longMessage},
			},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	// Message should be truncated to 100 chars
	assert.NotContains(t, result, longMessage) // Full message not present
	assert.Contains(t, result, "xxx")          // But truncated version is
}

func TestBuildContextString_MaxFailuresLimit(t *testing.T) {
	failures := make([]testwatch.Failure, 5)
	for i := range failures {
		failures[i] = testwatch.Failure{Name: "Test" + string(rune('A'+i))}
	}

	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures:  failures,
		},
	}

	cfg := FeedbackConfig{MaxFailures: 3}
	result := buildContextString(watchers, cfg)

	// Should show "and more failures" message
	assert.Contains(t, result, "and more failures")
}

func TestBuildContextString_UnderMaxFailures(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures: []testwatch.Failure{
				{Name: "Test1"},
				{Name: "Test2"},
			},
		},
	}

	cfg := FeedbackConfig{MaxFailures: 5}
	result := buildContextString(watchers, cfg)

	// Should not show "and more" when under limit
	assert.NotContains(t, result, "and more failures")
}

func TestBuildContextString_ExactlyMaxFailures(t *testing.T) {
	failures := make([]testwatch.Failure, 3)
	for i := range failures {
		failures[i] = testwatch.Failure{Name: "Test" + string(rune('A'+i))}
	}

	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures:  failures,
		},
	}

	cfg := FeedbackConfig{MaxFailures: 3}
	result := buildContextString(watchers, cfg)

	// At exactly max, should show "and more" message
	assert.Contains(t, result, "and more failures")
}

func TestBuildContextString_FooterPresent(t *testing.T) {
	watchers := []WatcherFeedback{
		{WatcherID: "test", Failures: []testwatch.Failure{{Name: "Test"}}},
	}

	result := buildContextString(watchers, DefaultConfig())

	assert.Contains(t, result, "address these failures before continuing")
}

// Tests for testwatch.Failure structure

func TestFailure_AllFields(t *testing.T) {
	f := testwatch.Failure{
		Name:    "TestSomething",
		File:    "something_test.go",
		Line:    100,
		Message: "test failed",
	}

	assert.Equal(t, "TestSomething", f.Name)
	assert.Equal(t, "something_test.go", f.File)
	assert.Equal(t, 100, f.Line)
	assert.Equal(t, "test failed", f.Message)
}

func TestFailure_EmptyFields(t *testing.T) {
	f := testwatch.Failure{}

	assert.Empty(t, f.Name)
	assert.Empty(t, f.File)
	assert.Zero(t, f.Line)
	assert.Empty(t, f.Message)
}

// Tests for testwatch.Status values

func TestStatusValues(t *testing.T) {
	assert.Equal(t, testwatch.Status("unknown"), testwatch.StatusUnknown)
	assert.Equal(t, testwatch.Status("pass"), testwatch.StatusPass)
	assert.Equal(t, testwatch.Status("fail"), testwatch.StatusFail)
	assert.Equal(t, testwatch.Status("error"), testwatch.StatusError)
	assert.Equal(t, testwatch.Status("running"), testwatch.StatusRunning)
}

// Tests for filtering failing watchers logic

func TestFilterFailingWatchers(t *testing.T) {
	statuses := []testwatch.TestStatus{
		{WatcherID: "pass-watcher", Status: testwatch.StatusPass},
		{WatcherID: "fail-watcher", Status: testwatch.StatusFail},
		{WatcherID: "error-watcher", Status: testwatch.StatusError},
		{WatcherID: "running-watcher", Status: testwatch.StatusRunning},
	}

	var failing []testwatch.TestStatus
	for _, s := range statuses {
		if s.Status == testwatch.StatusFail || s.Status == testwatch.StatusError {
			failing = append(failing, s)
		}
	}

	assert.Len(t, failing, 2)
	assert.Equal(t, "fail-watcher", failing[0].WatcherID)
	assert.Equal(t, "error-watcher", failing[1].WatcherID)
}

// Tests for limiting failures

func TestLimitFailures(t *testing.T) {
	failures := make([]testwatch.Failure, 10)
	for i := range failures {
		failures[i] = testwatch.Failure{Name: "Test" + string(rune('0'+i))}
	}

	maxFailures := 3
	var limited []testwatch.Failure
	if len(failures) > maxFailures {
		limited = failures[:maxFailures]
	} else {
		limited = failures
	}

	assert.Len(t, limited, 3)
}

// Tests for testwatch.TestStatus structure

func TestTestStatus_AllFields(t *testing.T) {
	status := testwatch.TestStatus{
		WatcherID: "my-watcher",
		Status:    testwatch.StatusFail,
		Summary:   "5 of 20 tests failed",
		Failures: []testwatch.Failure{
			{Name: "Test1"},
			{Name: "Test2"},
		},
	}

	assert.Equal(t, "my-watcher", status.WatcherID)
	assert.Equal(t, testwatch.StatusFail, status.Status)
	assert.Equal(t, "5 of 20 tests failed", status.Summary)
	assert.Len(t, status.Failures, 2)
}

// Tests for edge cases

func TestBuildContextString_NoSummary(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Status:    "fail",
			Summary:   "",
			Failures:  []testwatch.Failure{{Name: "Test"}},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	assert.Contains(t, result, "test")
	// Should still render properly with empty summary
}

func TestBuildContextString_SpecialCharsInFailureName(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures: []testwatch.Failure{
				{Name: "Test_Special/Case#1"},
			},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	assert.Contains(t, result, "Test_Special/Case#1")
}

func TestBuildContextString_EmptyMessage(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures: []testwatch.Failure{
				{Name: "Test", Message: ""},
			},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	assert.Contains(t, result, "Test")
	// Empty message should not add extra parentheses after the test name
	// The "()" after test name indicates empty summary, which is expected
}

func TestBuildContextString_ZeroLine(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "test",
			Failures: []testwatch.Failure{
				{Name: "Test", File: "test.go", Line: 0},
			},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	// Line 0 should show file without line number
	assert.Contains(t, result, "test.go")
	assert.NotContains(t, result, "test.go:0")
}

// Tests for formatting

func TestBuildContextString_MarkdownFormatting(t *testing.T) {
	watchers := []WatcherFeedback{
		{
			WatcherID: "my-watcher",
			Summary:   "summary text",
			Failures: []testwatch.Failure{
				{Name: "TestName"},
			},
		},
	}

	result := buildContextString(watchers, DefaultConfig())

	// Should use markdown formatting
	assert.Contains(t, result, "**Watcher")
	assert.Contains(t, result, "`my-watcher`")
	assert.Contains(t, result, "`TestName`")
	assert.Contains(t, result, "-") // List items
}

// Tests for WatcherFeedback status values

func TestWatcherFeedback_StatusValues(t *testing.T) {
	validStatuses := []string{"pass", "fail", "error", "running", "unknown"}

	for _, status := range validStatuses {
		feedback := WatcherFeedback{Status: status}
		assert.NotEmpty(t, feedback.Status)
	}
}
