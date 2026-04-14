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
	Create          *createReq          `json:"create"`
	Update          *updateReq          `json:"update"`
	Comment         *commentReq         `json:"comment"`
	ListTransitions *listTransitionsReq `json:"list_transitions"`
	Transition      *transitionReq      `json:"transition"`
}

type getReq struct {
	Key    string   `json:"key"`
	Fields []string `json:"fields"`
	Expand []string `json:"expand"`
}

type searchReq struct {
	JQL        string   `json:"jql"`
	StartAt    int      `json:"start_at"`
	MaxResults int      `json:"max_results"`
	Fields     []string `json:"fields"`
	Expand     []string `json:"expand"`
}

type createReq struct {
	ProjectKey  string         `json:"project_key"`
	IssueType   string         `json:"issue_type"`
	Summary     string         `json:"summary"`
	Description string         `json:"description"`
	Fields      map[string]any `json:"fields"`
	Update      map[string]any `json:"update"`
}

type updateReq struct {
	Key         string         `json:"key"`
	Summary     string         `json:"summary"`
	Description string         `json:"description"`
	Fields      map[string]any `json:"fields"`
	Update      map[string]any `json:"update"`
	NotifyUsers *bool          `json:"notify_users"`
}

type commentReq struct {
	Key        string         `json:"key"`
	Body       string         `json:"body"`
	BodyADF    map[string]any `json:"body_adf"`
	Visibility map[string]any `json:"visibility"`
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
		Case("create", func() (map[string]any, error) { return createIssue(ctx, rc, client, in.Create) }).
		Case("update", func() (map[string]any, error) { return updateIssue(ctx, rc, client, in.Update) }).
		Case("comment", func() (map[string]any, error) { return addComment(ctx, rc, client, in.Comment) }).
		Case("list_transitions", func() (map[string]any, error) { return listTransitions(ctx, rc, client, in.ListTransitions) }).
		Case("transition", func() (map[string]any, error) { return transitionIssue(ctx, rc, client, in.Transition) }).
		Run()
	if err != nil {
		var invalid *oputil.InvalidOpError
		if errors.As(err, &invalid) {
			return skillerr.Arg(err.Error(), skillerr.WithHint("Use one of: comment, create, get, list_transitions, search, transition, update."))
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
	if req.MaxResults <= 0 {
		req.MaxResults = 25
	}
	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.SearchIssues(ctx, req.JQL, req.StartAt, req.MaxResults, req.Fields, req.Expand)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("search issues", err)
	}
	result["operation"] = "search"
	result["jql"] = req.JQL
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

	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		return client.UpdateIssue(ctx, req.Key, fields, req.Update, req.NotifyUsers)
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

func wrapJiraErr(action string, err error) error {
	var jiraErr *jiraclient.Error
	if errors.As(err, &jiraErr) {
		return skillerr.Runtime(fmt.Sprintf("%s failed: %s", action, jiraErr.Error()))
	}
	return skillerr.WrapRuntime(action, err)
}
