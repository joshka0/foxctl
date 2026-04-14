package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	jiraclient "github.com/jkatigb/agentctl/internal/interfaces/jira"
)

const command = "jira/issue"

type input struct {
	Operation       string              `json:"operation"`
	Get             *getReq             `json:"get"`
	Search          *searchReq          `json:"search"`
	Projects        *projectsReq        `json:"projects"`
	Project         *projectReq         `json:"project"`
	Create          *createReq          `json:"create"`
	Update          *updateReq          `json:"update"`
	ListComments    *listCommentsReq    `json:"list_comments"`
	Comment         *commentReq         `json:"comment"`
	ListLinkTypes   bool                `json:"list_link_types"`
	Link            *linkReq            `json:"link"`
	Unlink          *unlinkReq          `json:"unlink"`
	ListTransitions *listTransitionsReq `json:"list_transitions"`
	Transition      *transitionReq      `json:"transition"`
}

type getReq struct {
	Key    string   `json:"key"`
	Fields []string `json:"fields"`
	Expand []string `json:"expand"`
}

type searchReq struct {
	JQL           string   `json:"jql"`
	StartAt       int      `json:"start_at"`
	NextPageToken string   `json:"next_page_token"`
	MaxResults    int      `json:"max_results"`
	Fields        []string `json:"fields"`
	Expand        []string `json:"expand"`
}

type projectsReq struct {
	StartAt    int    `json:"start_at"`
	MaxResults int    `json:"max_results"`
	Query      string `json:"query"`
}

type projectReq struct {
	Key    string   `json:"key"`
	Expand []string `json:"expand"`
}

type createReq struct {
	ProjectKey        string         `json:"project_key"`
	IssueType         string         `json:"issue_type"`
	Summary           string         `json:"summary"`
	Description       string         `json:"description"`
	ParentKey         string         `json:"parent_key"`
	AssigneeAccountID string         `json:"assignee_account_id"`
	Labels            []string       `json:"labels"`
	Fields            map[string]any `json:"fields"`
	Update            map[string]any `json:"update"`
}

type updateReq struct {
	Key               string         `json:"key"`
	Summary           string         `json:"summary"`
	Description       string         `json:"description"`
	ParentKey         string         `json:"parent_key"`
	AssigneeAccountID string         `json:"assignee_account_id"`
	ClearAssignee     bool           `json:"clear_assignee"`
	SetLabels         []string       `json:"set_labels"`
	AddLabels         []string       `json:"add_labels"`
	RemoveLabels      []string       `json:"remove_labels"`
	Fields            map[string]any `json:"fields"`
	Update            map[string]any `json:"update"`
	NotifyUsers       *bool          `json:"notify_users"`
}

type listCommentsReq struct {
	Key        string `json:"key"`
	StartAt    int    `json:"start_at"`
	MaxResults int    `json:"max_results"`
}

type commentReq struct {
	Key        string         `json:"key"`
	Body       string         `json:"body"`
	BodyADF    map[string]any `json:"body_adf"`
	Visibility map[string]any `json:"visibility"`
}

type linkReq struct {
	OutwardIssueKey string         `json:"outward_issue_key"`
	InwardIssueKey  string         `json:"inward_issue_key"`
	TypeName        string         `json:"type_name"`
	Comment         string         `json:"comment"`
	CommentADF      map[string]any `json:"comment_adf"`
}

type unlinkReq struct {
	LinkID string `json:"link_id"`
}

type listTransitionsReq struct {
	Key string `json:"key"`
}

type transitionReq struct {
	Key          string         `json:"key"`
	TransitionID string         `json:"transition_id"`
	Fields       map[string]any `json:"fields"`
	Update       map[string]any `json:"update"`
	Comment      string         `json:"comment"`
	CommentADF   map[string]any `json:"comment_adf"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	in.Operation = oputil.DefaultOp(in.Operation, "search")

	client, err := jiraclient.NewClientFromEnv()
	if err != nil {
		return skillerr.Arg(
			"jira configuration is incomplete",
			skillerr.WithCause(err),
			skillerr.WithHint("Set JIRA_BASE_URL, JIRA_EMAIL, and JIRA_API_TOKEN."),
		)
	}

	data, err := oputil.NewSwitch(in.Operation).
		Case("get", func() (map[string]any, error) { return getIssue(ctx, rc, client, in.Get) }).
		Case("search", func() (map[string]any, error) { return searchIssues(ctx, rc, client, in.Search) }).
		Case("projects", func() (map[string]any, error) { return listProjects(ctx, rc, client, in.Projects) }).
		Case("project", func() (map[string]any, error) { return getProject(ctx, rc, client, in.Project) }).
		Case("create", func() (map[string]any, error) { return createIssue(ctx, rc, client, in.Create) }).
		Case("update", func() (map[string]any, error) { return updateIssue(ctx, rc, client, in.Update) }).
		Case("list_comments", func() (map[string]any, error) { return listComments(ctx, rc, client, in.ListComments) }).
		Case("comment", func() (map[string]any, error) { return addComment(ctx, rc, client, in.Comment) }).
		Case("list_link_types", func() (map[string]any, error) { return listLinkTypes(ctx, rc, client) }).
		Case("link", func() (map[string]any, error) { return createLink(ctx, rc, client, in.Link) }).
		Case("unlink", func() (map[string]any, error) { return deleteLink(ctx, rc, client, in.Unlink) }).
		Case("list_transitions", func() (map[string]any, error) { return listTransitions(ctx, rc, client, in.ListTransitions) }).
		Case("transition", func() (map[string]any, error) { return transitionIssue(ctx, rc, client, in.Transition) }).
		Run()
	if err != nil {
		var invalid *oputil.InvalidOpError
		if errors.As(err, &invalid) {
			return skillerr.Arg(err.Error(), skillerr.WithHint("Use one of: comment, create, get, link, list_comments, list_link_types, list_transitions, project, projects, search, transition, unlink, update."))
		}
		return err
	}

	return skillout.Emit(rc, command, data)
}

func getIssue(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *getReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.Key) == "" {
		return nil, skillerr.Arg("get.key is required")
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.GetIssue(ctx, req.Key, req.Fields, req.Expand)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("get issue", err)
	}
	result["operation"] = "get"
	return result, nil
}

func searchIssues(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *searchReq) (map[string]any, error) {
	if req == nil {
		req = &searchReq{}
	}
	if req.StartAt > 0 {
		return nil, skillerr.Arg("search.start_at is not supported by Jira enhanced search", skillerr.WithHint("Use search.next_page_token from a previous response instead."))
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 25
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.SearchIssues(ctx, req.JQL, req.NextPageToken, req.MaxResults, req.Fields, req.Expand)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("search issues", err)
	}
	result["operation"] = "search"
	result["jql"] = req.JQL
	return result, nil
}

func listProjects(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *projectsReq) (map[string]any, error) {
	if req == nil {
		req = &projectsReq{}
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 50
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListProjects(ctx, req.StartAt, req.MaxResults, req.Query)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("list projects", err)
	}
	result["operation"] = "projects"
	return result, nil
}

func getProject(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *projectReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.Key) == "" {
		return nil, skillerr.Arg("project.key is required")
	}
	if len(req.Expand) == 0 {
		req.Expand = []string{"issueTypes"}
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.GetProject(ctx, req.Key, req.Expand)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("get project", err)
	}
	result["operation"] = "project"
	return result, nil
}

func createIssue(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *createReq) (map[string]any, error) {
	if req == nil {
		return nil, skillerr.Arg("create options are required")
	}
	fields := buildCreateFields(req)
	if len(fields) == 0 {
		return nil, skillerr.Arg("create requires fields, or project_key + issue_type + summary")
	}

	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.CreateIssue(ctx, fields, req.Update)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("create issue", err)
	}
	result["operation"] = "create"
	return result, nil
}

func updateIssue(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *updateReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.Key) == "" {
		return nil, skillerr.Arg("update.key is required")
	}
	fields := cloneMap(req.Fields)
	if strings.TrimSpace(req.Summary) != "" {
		fields["summary"] = strings.TrimSpace(req.Summary)
	}
	if strings.TrimSpace(req.Description) != "" {
		fields["description"] = jiraclient.TextToADF(req.Description)
	}
	if strings.TrimSpace(req.ParentKey) != "" {
		fields["parent"] = map[string]any{"key": strings.TrimSpace(req.ParentKey)}
	}
	if strings.TrimSpace(req.AssigneeAccountID) != "" {
		fields["assignee"] = map[string]any{"accountId": strings.TrimSpace(req.AssigneeAccountID)}
	} else if req.ClearAssignee {
		fields["assignee"] = nil
	}
	if len(req.SetLabels) > 0 {
		fields["labels"] = cleanedStrings(req.SetLabels)
	}
	updateOps := cloneMap(req.Update)
	if labelOps := buildLabelUpdate(req.AddLabels, req.RemoveLabels); len(labelOps) > 0 {
		updateOps["labels"] = labelOps
	}

	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		return client.UpdateIssue(ctx, req.Key, fields, updateOps, req.NotifyUsers)
	})
	if err != nil {
		return nil, wrapJiraErr("update issue", err)
	}
	return map[string]any{
		"operation": "update",
		"key":       req.Key,
		"updated":   true,
	}, nil
}

func listComments(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *listCommentsReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.Key) == "" {
		return nil, skillerr.Arg("list_comments.key is required")
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 50
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListComments(ctx, req.Key, req.StartAt, req.MaxResults)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("list comments", err)
	}
	result["operation"] = "list_comments"
	result["key"] = req.Key
	return result, nil
}

func addComment(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *commentReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.Key) == "" {
		return nil, skillerr.Arg("comment.key is required")
	}
	if strings.TrimSpace(req.Body) == "" && len(req.BodyADF) == 0 {
		return nil, skillerr.Arg("comment requires body or body_adf")
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.AddComment(ctx, req.Key, req.Body, req.BodyADF, req.Visibility)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("add comment", err)
	}
	result["operation"] = "comment"
	result["key"] = req.Key
	return result, nil
}

func listLinkTypes(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client) (map[string]any, error) {
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListLinkTypes(ctx)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("list link types", err)
	}
	result["operation"] = "list_link_types"
	return result, nil
}

func createLink(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *linkReq) (map[string]any, error) {
	if req == nil {
		return nil, skillerr.Arg("link options are required")
	}
	if strings.TrimSpace(req.OutwardIssueKey) == "" || strings.TrimSpace(req.InwardIssueKey) == "" {
		return nil, skillerr.Arg("link.outward_issue_key and link.inward_issue_key are required")
	}
	if strings.TrimSpace(req.TypeName) == "" {
		return nil, skillerr.Arg("link.type_name is required")
	}
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		return client.CreateIssueLink(ctx, req.OutwardIssueKey, req.InwardIssueKey, req.TypeName, req.Comment, req.CommentADF)
	})
	if err != nil {
		return nil, wrapJiraErr("create issue link", err)
	}
	return map[string]any{
		"operation":         "link",
		"outward_issue_key": req.OutwardIssueKey,
		"inward_issue_key":  req.InwardIssueKey,
		"type_name":         req.TypeName,
		"updated":           true,
	}, nil
}

func deleteLink(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *unlinkReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.LinkID) == "" {
		return nil, skillerr.Arg("unlink.link_id is required")
	}
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		return client.DeleteIssueLink(ctx, req.LinkID)
	})
	if err != nil {
		return nil, wrapJiraErr("delete issue link", err)
	}
	return map[string]any{
		"operation": "unlink",
		"link_id":   req.LinkID,
		"updated":   true,
	}, nil
}

func listTransitions(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *listTransitionsReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.Key) == "" {
		return nil, skillerr.Arg("list_transitions.key is required")
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListTransitions(ctx, req.Key)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("list transitions", err)
	}
	result["operation"] = "list_transitions"
	result["key"] = req.Key
	return result, nil
}

func transitionIssue(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *transitionReq) (map[string]any, error) {
	if req == nil || strings.TrimSpace(req.Key) == "" {
		return nil, skillerr.Arg("transition.key is required")
	}
	if strings.TrimSpace(req.TransitionID) == "" {
		return nil, skillerr.Arg("transition.transition_id is required")
	}

	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		return client.TransitionIssue(ctx, req.Key, req.TransitionID, req.Fields, req.Update, req.Comment, req.CommentADF)
	})
	if err != nil {
		return nil, wrapJiraErr("transition issue", err)
	}
	return map[string]any{
		"operation":     "transition",
		"key":           req.Key,
		"transition_id": req.TransitionID,
		"updated":       true,
	}, nil
}

func buildCreateFields(req *createReq) map[string]any {
	fields := cloneMap(req.Fields)
	if strings.TrimSpace(req.ProjectKey) != "" {
		fields["project"] = map[string]any{"key": strings.TrimSpace(req.ProjectKey)}
	}
	if strings.TrimSpace(req.IssueType) != "" {
		fields["issuetype"] = map[string]any{"name": strings.TrimSpace(req.IssueType)}
	}
	if strings.TrimSpace(req.Summary) != "" {
		fields["summary"] = strings.TrimSpace(req.Summary)
	}
	if strings.TrimSpace(req.Description) != "" {
		fields["description"] = jiraclient.TextToADF(req.Description)
	}
	if strings.TrimSpace(req.ParentKey) != "" {
		fields["parent"] = map[string]any{"key": strings.TrimSpace(req.ParentKey)}
	}
	if strings.TrimSpace(req.AssigneeAccountID) != "" {
		fields["assignee"] = map[string]any{"accountId": strings.TrimSpace(req.AssigneeAccountID)}
	}
	if len(req.Labels) > 0 {
		fields["labels"] = cleanedStrings(req.Labels)
	}
	return fields
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cleanedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func buildLabelUpdate(add, remove []string) []map[string]any {
	ops := make([]map[string]any, 0, len(add)+len(remove))
	for _, value := range cleanedStrings(add) {
		ops = append(ops, map[string]any{"add": value})
	}
	for _, value := range cleanedStrings(remove) {
		ops = append(ops, map[string]any{"remove": value})
	}
	return ops
}

func wrapJiraErr(action string, err error) error {
	var jiraErr *jiraclient.Error
	if errors.As(err, &jiraErr) {
		return skillerr.Runtime(fmt.Sprintf("%s failed: %s", action, jiraErr.Error()))
	}
	return skillerr.WrapRuntime(action, err)
}
