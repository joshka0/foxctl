package tools_test

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"slices"
	"testing"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	coretool "github.com/jkatigb/agentctl/internal/v2/core/tool"
	"github.com/jkatigb/agentctl/internal/v2/runtime/profiles"
	"github.com/jkatigb/agentctl/internal/v2/runtime/runner"
	"github.com/jkatigb/agentctl/internal/v2/runtime/tools"
)

func TestToolCatalog_AllowsOnlyProfileTools(t *testing.T) {
	t.Parallel()

	catalog, err := tools.NewCatalog([]coretool.ToolDef{
		{Name: "fs_read_file"},
		{Name: "fs_write_file"},
		{Name: "agent_spawn"},
	}, map[coretool.ProcessProfile]profiles.ProfileSpec{
		coretool.ProfileWorker: {
			Profile:      coretool.ProfileWorker,
			AllowedTools: []string{"fs_read_file"},
		},
		coretool.ProfileOverseer: {
			Profile:      coretool.ProfileOverseer,
			AllowedTools: []string{"agent_spawn"},
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog returned error: %v", err)
	}

	workerTools := catalog.ForProfile(coretool.ProfileWorker)
	if len(workerTools) != 1 || workerTools[0].Name != "fs_read_file" {
		t.Fatalf("unexpected worker tools: %+v", workerTools)
	}

	overseerTools := catalog.ForProfile(coretool.ProfileOverseer)
	if len(overseerTools) != 1 || overseerTools[0].Name != "agent_spawn" {
		t.Fatalf("unexpected overseer tools: %+v", overseerTools)
	}
}

func TestToolCatalog_DenyByDefaultRequiresExplicitAllowProfiles(t *testing.T) {
	t.Parallel()

	catalog, err := tools.NewCatalog([]coretool.ToolDef{
		{
			Name: "sensitive_tool",
			Policy: coretool.ToolPolicy{
				DenyByDefault: true,
			},
		},
		{
			Name: "sensitive_tool_explicit",
			Policy: coretool.ToolPolicy{
				DenyByDefault: true,
				AllowProfiles: []coretool.ProcessProfile{coretool.ProfileWorker},
			},
		},
	}, map[coretool.ProcessProfile]profiles.ProfileSpec{
		coretool.ProfileWorker: {
			Profile:      coretool.ProfileWorker,
			AllowedTools: []string{"sensitive_tool", "sensitive_tool_explicit"},
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog returned error: %v", err)
	}

	workerTools := catalog.ForProfile(coretool.ProfileWorker)
	if len(workerTools) != 1 {
		t.Fatalf("expected 1 tool after deny-by-default filtering, got %+v", workerTools)
	}
	if workerTools[0].Name != "sensitive_tool_explicit" {
		t.Fatalf("unexpected allowed tool %q", workerTools[0].Name)
	}
}

func TestToolExecutor_ArgBindingRejectsInvalidSchema(t *testing.T) {
	t.Parallel()

	catalog := mustCatalog(t, []coretool.ToolDef{
		{
			Name:       "fs_read_file",
			Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		},
	}, nil)

	exec := tools.NewExecutor(catalog, coretool.ProfileWorker, fakeDelegate{})
	_, err := exec.Execute(context.Background(), "fs_read_file", json.RawMessage(`{"path":123}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
	assertErrKind(t, err, v2errors.ErrValidation)
}

func TestToolExecutor_PassesThroughSuccessPayload(t *testing.T) {
	t.Parallel()

	catalog := mustCatalog(t, []coretool.ToolDef{
		{
			Name:       "fs_read_file",
			Parameters: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		},
	}, nil)

	delegate := fakeDelegate{
		result: runner.ToolResult{
			Output: "payload",
		},
	}
	exec := tools.NewExecutor(catalog, coretool.ProfileWorker, delegate)

	got, err := exec.Execute(context.Background(), "fs_read_file", json.RawMessage(`{"path":"README.md"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got.Status != "ok" {
		t.Fatalf("Status=%q want ok", got.Status)
	}
	if got.Output != "payload" {
		t.Fatalf("Output=%q want payload", got.Output)
	}
}

func TestToolExecutor_SlashAliasDelegatesCanonicalName(t *testing.T) {
	t.Parallel()

	catalog := mustCatalog(t, []coretool.ToolDef{
		{
			Name:       "code/search",
			Parameters: json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}}}`),
		},
	}, map[coretool.ProcessProfile]profiles.ProfileSpec{
		coretool.ProfileWorker: {
			Profile:      coretool.ProfileWorker,
			AllowedTools: []string{"code/search"},
		},
	})

	delegate := &capturingDelegate{
		result: runner.ToolResult{Output: "ok"},
	}
	exec := tools.NewExecutor(catalog, coretool.ProfileWorker, delegate)

	_, err := exec.Execute(context.Background(), "code/search", json.RawMessage(`{"query":"runtime"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if delegate.lastName != "code_search" {
		t.Fatalf("delegate tool name=%q want code_search", delegate.lastName)
	}
}

func TestToolExecutor_DotAliasDelegatesCanonicalName(t *testing.T) {
	t.Parallel()

	catalog := mustCatalog(t, []coretool.ToolDef{
		{
			Name:       "code/search",
			Parameters: json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}}}`),
		},
	}, map[coretool.ProcessProfile]profiles.ProfileSpec{
		coretool.ProfileWorker: {
			Profile:      coretool.ProfileWorker,
			AllowedTools: []string{"code.search"},
		},
	})

	delegate := &capturingDelegate{
		result: runner.ToolResult{Output: "ok"},
	}
	exec := tools.NewExecutor(catalog, coretool.ProfileWorker, delegate)

	_, err := exec.Execute(context.Background(), "code.search", json.RawMessage(`{"query":"runtime"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if delegate.lastName != "code_search" {
		t.Fatalf("delegate tool name=%q want code_search", delegate.lastName)
	}
}

func TestToolExecutor_MapsToolFailureToErrToolFailed(t *testing.T) {
	t.Parallel()

	catalog := mustCatalog(t, []coretool.ToolDef{
		{Name: "fs_read_file"},
	}, nil)

	exec := tools.NewExecutor(catalog, coretool.ProfileWorker, fakeDelegate{
		err: stderrors.New("tool failed"),
	})
	_, err := exec.Execute(context.Background(), "fs_read_file", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected tool failure")
	}
	assertErrKind(t, err, v2errors.ErrToolFailed)
}

func TestToolCatalog_DuplicateCanonicalNameRejected(t *testing.T) {
	t.Parallel()

	_, err := tools.NewCatalog([]coretool.ToolDef{
		{Name: "code/search"},
		{Name: "code_search"},
	}, map[coretool.ProcessProfile]profiles.ProfileSpec{
		coretool.ProfileWorker: {
			Profile:      coretool.ProfileWorker,
			AllowedTools: []string{"code/search"},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate canonical name error")
	}
}

func TestToolExecutor_TodoSlashAliasDelegatesCanonicalName(t *testing.T) {
	t.Parallel()

	catalog := mustCatalog(t, []coretool.ToolDef{
		{
			Name:       "todo/query",
			Parameters: json.RawMessage(`{"type":"object","required":["status"],"properties":{"status":{"type":"string"}}}`),
		},
	}, map[coretool.ProcessProfile]profiles.ProfileSpec{
		coretool.ProfileCompanion: {
			Profile:      coretool.ProfileCompanion,
			AllowedTools: []string{"todo/query"},
		},
	})

	delegate := &capturingDelegate{result: runner.ToolResult{Output: "ok"}}
	exec := tools.NewExecutor(catalog, coretool.ProfileCompanion, delegate)

	_, err := exec.Execute(context.Background(), "todo/query", json.RawMessage(`{"status":"pending"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if delegate.lastName != "todo_query" {
		t.Fatalf("delegate tool name=%q want todo_query", delegate.lastName)
	}
}

func TestToolExecutor_DefaultSpecsAllowCompanionTodoQuery(t *testing.T) {
	t.Parallel()

	catalog, err := tools.NewCatalog([]coretool.ToolDef{
		{
			Name:       "todo/query",
			Parameters: json.RawMessage(`{"type":"object","required":["status"],"properties":{"status":{"type":"string"}}}`),
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewCatalog returned error: %v", err)
	}

	delegate := &capturingDelegate{result: runner.ToolResult{Output: "ok"}}
	exec := tools.NewExecutor(catalog, coretool.ProfileCompanion, delegate)

	_, err = exec.Execute(context.Background(), "todo/query", json.RawMessage(`{"status":"pending"}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if delegate.lastName != "todo_query" {
		t.Fatalf("delegate tool name=%q want todo_query", delegate.lastName)
	}
}

func TestExecutor_UnknownToolReturnsPolicyViolation(t *testing.T) {
	t.Parallel()

	catalog := mustCatalog(t, []coretool.ToolDef{
		{Name: "fs_read_file"},
	}, nil)

	exec := tools.NewExecutor(catalog, coretool.ProfileWorker, fakeDelegate{})
	_, err := exec.Execute(context.Background(), "agent_spawn", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected policy violation for unknown/disallowed tool")
	}
	assertErrKind(t, err, v2errors.ErrPolicyViolation)
}

func mustCatalog(t *testing.T, defs []coretool.ToolDef, spec map[coretool.ProcessProfile]profiles.ProfileSpec) *tools.Catalog {
	t.Helper()
	if spec == nil {
		spec = map[coretool.ProcessProfile]profiles.ProfileSpec{
			coretool.ProfileWorker: {
				Profile:      coretool.ProfileWorker,
				AllowedTools: toolNames(defs),
			},
		}
	}
	c, err := tools.NewCatalog(defs, spec)
	if err != nil {
		t.Fatalf("NewCatalog returned error: %v", err)
	}
	return c
}

func toolNames(defs []coretool.ToolDef) []string {
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.Name)
	}
	slices.Sort(out)
	return out
}

func assertErrKind(t *testing.T, err error, kind v2errors.ErrorKind) {
	t.Helper()
	var verr *v2errors.V2Error
	if !stderrors.As(err, &verr) {
		t.Fatalf("expected V2Error, got %T", err)
	}
	if verr.Kind != kind {
		t.Fatalf("error kind=%q want %q", verr.Kind, kind)
	}
}

type fakeDelegate struct {
	result runner.ToolResult
	err    error
}

func (f fakeDelegate) Execute(_ context.Context, _ string, _ json.RawMessage) (runner.ToolResult, error) {
	if f.err != nil {
		return runner.ToolResult{}, f.err
	}
	return f.result, nil
}

type capturingDelegate struct {
	lastName string
	result   runner.ToolResult
	err      error
}

func (d *capturingDelegate) Execute(_ context.Context, name string, _ json.RawMessage) (runner.ToolResult, error) {
	d.lastName = name
	if d.err != nil {
		return runner.ToolResult{}, d.err
	}
	return d.result, nil
}
