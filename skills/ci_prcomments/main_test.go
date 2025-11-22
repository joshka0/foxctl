package main

import (
	"strings"
	"testing"
)

func TestCleanGitHubLogLine_StripsPrefixAndMarkers(t *testing.T) {
	line := "2024-11-21T12:34:56.1234567Z ##[group]Run make fmt"
	got := cleanGitHubLogLine(line)
	if strings.Contains(got, "2024-11-21") {
		t.Fatalf("expected timestamp prefix to be stripped, got %q", got)
	}
	if strings.Contains(got, "##[group]") {
		t.Fatalf("expected group marker to be stripped, got %q", got)
	}
	if !strings.Contains(got, "Run make fmt") {
		t.Fatalf("expected core message to be preserved, got %q", got)
	}
}

func TestCleanCommentBody_RemovesHTMLCommentsAndNoisePrefixes(t *testing.T) {
	body := "<!-- internal state -->\n_⚠️ noisy bot prefix\nReal feedback line\n"
	got := cleanCommentBody(body)
	if strings.Contains(got, "internal state") {
		t.Fatalf("expected HTML comments to be removed, got %q", got)
	}
	if strings.Contains(got, "_⚠️ noisy bot prefix") {
		t.Fatalf("expected noisy prefix lines to be removed, got %q", got)
	}
	if !strings.Contains(got, "Real feedback line") {
		t.Fatalf("expected real content to be preserved, got %q", got)
	}
}

func TestBuildMarkdownReport_NoTasksSummaryRespectsErrorsOnly(t *testing.T) {
	mergeable := true
	pr := &PRInfo{
		Title:     "Test PR",
		Number:    1,
		User:      User{Login: "author"},
		Mergeable: &mergeable,
	}
	var comments []Comment
	var checks []CheckRun

	mdAll, summaryAll := buildMarkdownReport(pr, "owner", "repo", 1, comments, checks, nil, nil, "", false, false)
	if summaryAll.Total != 0 {
		t.Fatalf("expected no tasks, got %+v", summaryAll)
	}
	if !strings.Contains(mdAll, "No outstanding tasks") {
		t.Fatalf("expected 'No outstanding tasks' message when errorsOnly=false, got:\n%s", mdAll)
	}

	mdErrorsOnly, summaryErrors := buildMarkdownReport(pr, "owner", "repo", 1, comments, checks, nil, nil, "", false, true)
	if summaryErrors.Total != 0 {
		t.Fatalf("expected no tasks with errorsOnly=true, got %+v", summaryErrors)
	}
	if strings.Contains(mdErrorsOnly, "No outstanding tasks") {
		t.Fatalf("did not expect 'No outstanding tasks' message when errorsOnly=true, got:\n%s", mdErrorsOnly)
	}
}

func TestBuildTasksList_EmptyWhenNoIssues(t *testing.T) {
	tasks := buildTasksList(nil, nil, nil)
	if tasks == nil || len(tasks) != 0 {
		t.Fatalf("expected empty tasks list, got %#v", tasks)
	}
}

func TestBuildTasksList_IncludesMergeCiAndComments(t *testing.T) {
	line := 42
	conflicting := []string{"foo.go"}
	comments := []Comment{
		{
			User: User{Login: "reviewer"},
			Body: "Real feedback line",
			Path: "foo.go",
			Line: &line,
		},
	}
	checks := []CheckRun{
		{ID: 1, Name: "lint", Conclusion: "failure", HTMLURL: "http://example.com"},
	}

	tasks := buildTasksList(conflicting, comments, checks)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks (merge, ci, review), got %d: %#v", len(tasks), tasks)
	}
	if tasks[0].Kind != "merge_conflict" {
		t.Fatalf("expected first task kind merge_conflict, got %s", tasks[0].Kind)
	}
	if tasks[1].Kind != "ci_failure" || tasks[1].CheckName != "lint" {
		t.Fatalf("expected second task ci_failure for 'lint', got %#v", tasks[1])
	}
	if tasks[2].Kind != "review_comment" || tasks[2].CommentAuthor != "reviewer" || !strings.Contains(tasks[2].CommentBody, "Real feedback line") {
		t.Fatalf("unexpected review_comment task: %#v", tasks[2])
	}
}

func TestExtractCodeRabbitComment_DropsAddressedComments(t *testing.T) {
	body := "coderabbitai[bot] commented:\n" +
		"\n" +
		"Reminders\n" +
		"- Some global reminder\n" +
		"---\n" +
		"### Task 1: Short summary\n" +
		"Actionable content here.\n" +
		"\n" +
		"🤖 Prompt for AI Agents\n" +
		"<details>\n" +
		"<summary>🤖 Prompt for AI Agents</summary>\n" +
		"```\n" +
		"This is a long prompt we do not want.\n" +
		"```\n" +
		"</details>\n" +
		"\n" +
		"✅ Addressed in commit abc123\n" +
		"<!-- This is an auto-generated comment by CodeRabbit -->"

	got := extractCodeRabbitComment(body)
	if got != "" {
		t.Fatalf("expected addressed CodeRabbit comment to be dropped, got:\n%s", got)
	}
}
