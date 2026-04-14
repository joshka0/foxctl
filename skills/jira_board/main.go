package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	jiraclient "github.com/jkatigb/agentctl/internal/interfaces/jira"
)

const command = "jira/board"

type input struct {
	Operation string          `json:"operation"`
	List      *listReq        `json:"list"`
	Issues    *boardIssuesReq `json:"issues"`
	Sprints   *sprintsReq     `json:"sprints"`
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

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
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
		Run()
	if err != nil {
		var invalid *oputil.InvalidOpError
		if errors.As(err, &invalid) {
			return skillerr.Arg(err.Error(), skillerr.WithHint("Use one of: issues, list, sprints."))
		}
		return err
	}

	return skillout.Emit(rc, command, data)
}

func listBoards(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *listReq) (map[string]any, error) {
	if req == nil {
		req = &listReq{}
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 50
	}

	var result map[string]any
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
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

func listBoardIssues(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *boardIssuesReq) (map[string]any, error) {
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
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
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

func listBoardSprints(ctx context.Context, rc *skillmain.RunContext, client *jiraclient.Client, req *sprintsReq) (map[string]any, error) {
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
	err := skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
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

func wrapJiraErr(action string, err error) error {
	var jiraErr *jiraclient.Error
	if errors.As(err, &jiraErr) {
		return skillerr.Runtime(fmt.Sprintf("%s failed: %s", action, jiraErr.Error()))
	}
	return skillerr.WrapRuntime(action, err)
}
