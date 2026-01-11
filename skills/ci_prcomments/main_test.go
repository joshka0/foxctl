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
	tasks := buildTasksList(nil, nil, nil, false)
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

	tasks := buildTasksList(conflicting, comments, checks, false)
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

func TestBuildTasksList_ClassifiesCodeRabbitComments(t *testing.T) {
	line := 10
	comments := []Comment{
		{
			User: User{Login: "coderabbitai[bot]"},
			Body: "coderabbitai[bot] commented:\n\n_⚠️ Potential issue_ | _🔴 Critical_\n\n**Fix undocumented \"error\" conclusion and add missing \"action_required\"/\"stale\" handling**\nMore details here...\n",
			Path: "skills/ci_github_checks/main.go",
			Line: &line,
		},
	}

	tasks := buildTasksList(nil, comments, nil, false)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task for CodeRabbit comment, got %d: %#v", len(tasks), tasks)
	}
	task := tasks[0]
	if task.Kind != "review_comment" {
		t.Fatalf("expected kind review_comment, got %s", task.Kind)
	}
	if task.Source != "coderabbit" {
		t.Fatalf("expected source coderabbit, got %s", task.Source)
	}
	if task.Severity != "critical" {
		t.Fatalf("expected severity critical, got %s", task.Severity)
	}
	if !strings.Contains(task.Summary, "Fix undocumented \"error\" conclusion") {
		t.Fatalf("expected summary to reflect core fix, got %q", task.Summary)
	}
	if task.File != "skills/ci_github_checks/main.go" {
		t.Fatalf("expected file path to be propagated, got %s", task.File)
	}
	if task.Line == nil || *task.Line != line {
		t.Fatalf("expected line to be %d, got %#v", line, task.Line)
	}
}

func TestExtractAIAgentPromptFromBody(t *testing.T) {
	// Test with actual CodeRabbit format
	body := `**Actionable comments posted: 10**

<details>
<summary>🤖 Fix all issues with AI Agents</summary>

` + "```" + `
In @.github/workflows/backend.yml:
- Around line 78-82: The workflow writes "$HOME/.deno/bin" to $GITHUB_PATH but
doesn't update the current shell.

In @praze-app/app/login.tsx:
- Around line 89-100: The function processPendingInviteCode clears the pending
code before calling invitesAPI.recordConnection.
` + "```" + `

</details>

<!-- This is an auto-generated comment by CodeRabbit -->`

	got := extractAIAgentPromptFromBody(body)
	if got == "" {
		t.Fatalf("expected AI agent prompt to be extracted, got empty string")
	}
	if !strings.Contains(got, "@.github/workflows/backend.yml") {
		t.Fatalf("expected prompt to contain file reference, got:\n%s", got)
	}
	if !strings.Contains(got, "praze-app/app/login.tsx") {
		t.Fatalf("expected prompt to contain second file reference, got:\n%s", got)
	}
	if strings.Contains(got, "Actionable comments") {
		t.Fatalf("expected prompt to NOT contain prefix text, got:\n%s", got)
	}
}

func TestExtractAIAgentPromptFromBody_NoPrompt(t *testing.T) {
	body := `**Actionable comments posted: 5**

Some regular review content without the AI agent section.

<!-- This is an auto-generated comment by CodeRabbit -->`

	got := extractAIAgentPromptFromBody(body)
	if got != "" {
		t.Fatalf("expected empty string when no AI prompt present, got:\n%s", got)
	}
}

func TestExtractAIAgentPromptFromBody_RealFormat(t *testing.T) {
	// This matches the exact format from GitHub API
	body := `**Actionable comments posted: 10**

> [!NOTE]
> Due to the large number of review comments, Critical severity comments were prioritized.

<details>
<summary>🤖 Fix all issues with AI Agents</summary>

` + "```" + `
In @.github/workflows/backend.yml:
- Around line 78-82: The workflow writes "$HOME/.deno/bin" to $GITHUB_PATH but
doesn't update the current shell.

In @praze-app/app/login.tsx:
- Around line 89-100: The function processPendingInviteCode clears the pending
code before calling invitesAPI.recordConnection.
` + "```" + `

</details>

<!-- This is an auto-generated comment by CodeRabbit -->`

	got := extractAIAgentPromptFromBody(body)
	if got == "" {
		t.Fatalf("expected AI agent prompt to be extracted from real format, got empty")
	}
	if !strings.Contains(got, "workflows/backend.yml") {
		t.Errorf("expected prompt to contain workflow file, got:\n%s", got)
	}
	if !strings.Contains(got, "login.tsx") {
		t.Errorf("expected prompt to contain login file, got:\n%s", got)
	}
}

func TestExtractCodeRabbitAIAgentPrompts_Integration(t *testing.T) {
	reviews := []PRReview{
		{
			ID:   1,
			User: User{Login: "some-user"},
			Body: "Regular review without AI prompt",
		},
		{
			ID:   2,
			User: User{Login: "coderabbitai[bot]"},
			Body: `**Actionable comments posted: 3**

<details>
<summary>🤖 Fix all issues with AI Agents</summary>

` + "```" + `
In @src/main.go:
- Line 42: Fix the error handling here.
` + "```" + `

</details>`,
		},
	}

	got := extractCodeRabbitAIAgentPrompts(reviews)
	if got == "" {
		t.Fatalf("expected to extract AI prompt from CodeRabbit review")
	}
	if !strings.Contains(got, "src/main.go") {
		t.Errorf("expected prompt to contain file reference, got:\n%s", got)
	}
}

func TestExtractCodeRabbitComment_StripsMetaKeepsTasksEvenIfAddressed(t *testing.T) {
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
	if strings.Contains(got, "Reminders") || strings.Contains(got, "Prompt for AI Agents") || strings.Contains(got, "This is a long prompt") || strings.Contains(got, "Addressed in commit") {
		t.Fatalf("expected CodeRabbit meta to be stripped, got:\n%s", got)
	}
	if !strings.Contains(got, "Task 1") || !strings.Contains(got, "Actionable content here.") {
		t.Fatalf("expected task text to be preserved, got:\n%s", got)
	}
}
