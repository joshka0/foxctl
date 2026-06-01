package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain/lite"
	jiraclient "github.com/joshka0/foxctl/internal/interfaces/jira"
)

const command = "jira/board"

type input struct {
	Operation     string            `json:"operation"`
	List          *listReq          `json:"list"`
	Issues        *boardIssuesReq   `json:"issues"`
	Sprints       *sprintsReq       `json:"sprints"`
	Backlog       *boardIssuesReq   `json:"backlog"`
	Current       *boardIssuesReq   `json:"current"`
	MoveToSprint  *moveToSprintReq  `json:"move_to_sprint"`
	MoveToBacklog *moveToBacklogReq `json:"move_to_backlog"`
}

type listReq struct {
	StartAt        int    `json:"start_at"`
	MaxResults     int    `json:"max_results"`
	ProjectKeyOrID string `json:"project_key_or_id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
}

type boardIssuesReq struct {
	BoardID    int      `json:"board_id"`
	JQL        string   `json:"jql"`
	Fields     []string `json:"fields"`
	StartAt    int      `json:"start_at"`
	MaxResults int      `json:"max_results"`
}

type sprintsReq struct {
	BoardID    int    `json:"board_id"`
	State      string `json:"state"`
	StartAt    int    `json:"start_at"`
	MaxResults int    `json:"max_results"`
}

type moveToSprintReq struct {
	SprintID int      `json:"sprint_id"`
	Issues   []string `json:"issues"`
}

type moveToBacklogReq struct {
	Issues []string `json:"issues"`
}

func main() {
	lite.Main(command, run)
}

func run(ctx context.Context, rc *lite.RunContext, in input) error {
	in.Operation = oputil.DefaultOp(in.Operation, "list")

	client, err := jiraclient.NewClientFromEnv()
	if err != nil {
		return skillerr.Arg(
			"jira configuration is incomplete",
			skillerr.WithCause(err),
			skillerr.WithHint("Set JIRA_BASE_URL, JIRA_EMAIL, and JIRA_API_TOKEN."),
		)
	}

	data, err := oputil.NewSwitch(in.Operation).
		Case("list", func() (map[string]any, error) { return listBoards(ctx, rc, client, in.List) }).
		Case("issues", func() (map[string]any, error) { return listBoardIssues(ctx, rc, client, in.Issues) }).
		Case("sprints", func() (map[string]any, error) { return listBoardSprints(ctx, rc, client, in.Sprints) }).
		Case("backlog", func() (map[string]any, error) { return listBacklogIssues(ctx, rc, client, in.Backlog) }).
		Case("current", func() (map[string]any, error) { return listCurrentSprintIssues(ctx, rc, client, in.Current) }).
		Case("move_to_sprint", func() (map[string]any, error) { return moveIssuesToSprint(ctx, rc, client, in.MoveToSprint) }).
		Case("move_to_backlog", func() (map[string]any, error) { return moveIssuesToBacklog(ctx, rc, client, in.MoveToBacklog) }).
		Run()
	if err != nil {
		var invalid *oputil.InvalidOpError
		if errors.As(err, &invalid) {
			return skillerr.Arg(err.Error(), skillerr.WithHint("Use one of: backlog, current, issues, list, move_to_backlog, move_to_sprint, sprints."))
		}
		return err
	}

	return lite.Emit(rc, command, data)
}

func listBoards(ctx context.Context, rc *lite.RunContext, client *jiraclient.Client, req *listReq) (map[string]any, error) {
	if req == nil {
		req = &listReq{}
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 50
	}

	var result map[string]any
	err := lite.GuardCall(ctx, lite.BreakerHTTP, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListBoards(ctx, req.StartAt, req.MaxResults, req.ProjectKeyOrID, req.Name, req.Type)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("list boards", err)
	}
	result["operation"] = "list"
	return result, nil
}

func listBoardIssues(ctx context.Context, rc *lite.RunContext, client *jiraclient.Client, req *boardIssuesReq) (map[string]any, error) {
	if req == nil {
		return nil, skillerr.Arg("issues options are required", skillerr.WithHint("Provide issues.board_id."))
	}
	if req.BoardID == 0 {
		return nil, skillerr.Arg("issues.board_id is required")
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 50
	}

	var result map[string]any
	err := lite.GuardCall(ctx, lite.BreakerHTTP, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListBoardIssues(ctx, req.BoardID, req.JQL, req.Fields, req.StartAt, req.MaxResults)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("list board issues", err)
	}
	result["operation"] = "issues"
	result["board_id"] = req.BoardID
	return result, nil
}

func listBoardSprints(ctx context.Context, rc *lite.RunContext, client *jiraclient.Client, req *sprintsReq) (map[string]any, error) {
	if req == nil {
		return nil, skillerr.Arg("sprints options are required", skillerr.WithHint("Provide sprints.board_id."))
	}
	if req.BoardID == 0 {
		return nil, skillerr.Arg("sprints.board_id is required")
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 50
	}

	var result map[string]any
	err := lite.GuardCall(ctx, lite.BreakerHTTP, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListBoardSprints(ctx, req.BoardID, req.State, req.StartAt, req.MaxResults)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("list board sprints", err)
	}
	result["operation"] = "sprints"
	result["board_id"] = req.BoardID
	return result, nil
}

func listBacklogIssues(ctx context.Context, rc *lite.RunContext, client *jiraclient.Client, req *boardIssuesReq) (map[string]any, error) {
	if req == nil {
		return nil, skillerr.Arg("backlog options are required", skillerr.WithHint("Provide backlog.board_id."))
	}
	if req.BoardID == 0 {
		return nil, skillerr.Arg("backlog.board_id is required")
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 50
	}

	var result map[string]any
	err := lite.GuardCall(ctx, lite.BreakerHTTP, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListBacklogIssues(ctx, req.BoardID, req.JQL, req.Fields, req.StartAt, req.MaxResults)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("list backlog issues", err)
	}
	result["operation"] = "backlog"
	result["board_id"] = req.BoardID
	return result, nil
}

func listCurrentSprintIssues(ctx context.Context, rc *lite.RunContext, client *jiraclient.Client, req *boardIssuesReq) (map[string]any, error) {
	if req == nil {
		return nil, skillerr.Arg("current options are required", skillerr.WithHint("Provide current.board_id."))
	}
	if req.BoardID == 0 {
		return nil, skillerr.Arg("current.board_id is required")
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 50
	}

	var sprintResult map[string]any
	err := lite.GuardCall(ctx, lite.BreakerHTTP, func(ctx context.Context) error {
		var callErr error
		sprintResult, callErr = client.ListBoardSprints(ctx, req.BoardID, "active", 0, 1)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("list active sprints", err)
	}

	values, _ := sprintResult["values"].([]any)
	if len(values) == 0 {
		return map[string]any{
			"operation": "current",
			"board_id":  req.BoardID,
			"issues":    []any{},
			"sprint":    nil,
			"total":     0,
		}, nil
	}
	sprint, _ := values[0].(map[string]any)
	sprintID, _ := sprint["id"].(float64)

	var result map[string]any
	err = lite.GuardCall(ctx, lite.BreakerHTTP, func(ctx context.Context) error {
		var callErr error
		result, callErr = client.ListSprintIssues(ctx, int(sprintID), req.JQL, req.Fields, req.StartAt, req.MaxResults)
		return callErr
	})
	if err != nil {
		return nil, wrapJiraErr("list current sprint issues", err)
	}
	result["operation"] = "current"
	result["board_id"] = req.BoardID
	result["sprint"] = sprint
	return result, nil
}

func moveIssuesToSprint(ctx context.Context, rc *lite.RunContext, client *jiraclient.Client, req *moveToSprintReq) (map[string]any, error) {
	if req == nil {
		return nil, skillerr.Arg("move_to_sprint options are required")
	}
	if req.SprintID == 0 {
		return nil, skillerr.Arg("move_to_sprint.sprint_id is required")
	}
	if len(req.Issues) == 0 {
		return nil, skillerr.Arg("move_to_sprint.issues is required")
	}
	err := lite.GuardCall(ctx, lite.BreakerHTTP, func(ctx context.Context) error {
		return client.MoveIssuesToSprint(ctx, req.SprintID, req.Issues)
	})
	if err != nil {
		return nil, wrapJiraErr("move issues to sprint", err)
	}
	return map[string]any{
		"operation": "move_to_sprint",
		"sprint_id": req.SprintID,
		"issues":    req.Issues,
		"updated":   true,
	}, nil
}

func moveIssuesToBacklog(ctx context.Context, rc *lite.RunContext, client *jiraclient.Client, req *moveToBacklogReq) (map[string]any, error) {
	if req == nil || len(req.Issues) == 0 {
		return nil, skillerr.Arg("move_to_backlog.issues is required")
	}
	err := lite.GuardCall(ctx, lite.BreakerHTTP, func(ctx context.Context) error {
		return client.MoveIssuesToBacklog(ctx, req.Issues)
	})
	if err != nil {
		return nil, wrapJiraErr("move issues to backlog", err)
	}
	return map[string]any{
		"operation": "move_to_backlog",
		"issues":    req.Issues,
		"updated":   true,
	}, nil
}

func wrapJiraErr(action string, err error) error {
	var jiraErr *jiraclient.Error
	if errors.As(err, &jiraErr) {
		return skillerr.Runtime(fmt.Sprintf("%s failed: %s", action, jiraErr.Error()))
	}
	return skillerr.WrapRuntime(action, err)
}
