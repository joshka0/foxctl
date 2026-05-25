package tools_test

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"slices"
	"strings"
	"testing"
	"testing/quick"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/profiles"
	"github.com/joshka0/foxctl/internal/v2/runtime/runner"
	"github.com/joshka0/foxctl/internal/v2/runtime/tools"
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

func TestToolCatalog_InvalidToolDefinitionNameRejected(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"",
		" ",
		"___",
		"fs read file",
		"fs-read-file",
		"fs/read;file",
		"fs/read\nfile",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := tools.NewCatalog([]coretool.ToolDef{
				{Name: "fs_read_file"},
				{Name: name},
			}, map[coretool.ProcessProfile]profiles.ProfileSpec{
				coretool.ProfileWorker: {
					Profile:      coretool.ProfileWorker,
					AllowedTools: []string{"fs_read_file", name},
				},
			})
			if err == nil {
				t.Fatalf("expected invalid tool definition name %q to be rejected", name)
			}
		})
	}
}

func TestToolCatalog_InvalidAllowlistEntriesDoNotExposeTools(t *testing.T) {
	t.Parallel()

	catalog, err := tools.NewCatalog([]coretool.ToolDef{
		{Name: "fs_read_file"},
	}, map[coretool.ProcessProfile]profiles.ProfileSpec{
		coretool.ProfileWorker: {
			Profile:      coretool.ProfileWorker,
			AllowedTools: []string{"fs-read-file", "fs read file", "fs/read;file"},
		},
	})
	if err != nil {
		t.Fatalf("NewCatalog returned error: %v", err)
	}
	if got := catalog.ForProfile(coretool.ProfileWorker); len(got) != 0 {
		t.Fatalf("invalid allowlist entries exposed tools: %+v", got)
	}
	if _, ok := catalog.Resolve("fs_read_file", coretool.ProfileWorker); ok {
		t.Fatal("invalid allowlist entry resolved fs_read_file")
	}
}

func TestToolCatalogPropertyAllowedAliasesResolveCanonicalToolOnly(t *testing.T) {
	t.Parallel()

	property := func(toolSeed uint8, allowedAliasSeed uint8, resolveAliasSeed uint8) bool {
		canonical := generatedCatalogToolName(toolSeed)
		allowedAlias := catalogToolAlias(canonical, allowedAliasSeed)
		resolveAlias := catalogToolAlias(canonical, resolveAliasSeed)

		catalog, err := tools.NewCatalog([]coretool.ToolDef{
			{Name: canonical},
		}, map[coretool.ProcessProfile]profiles.ProfileSpec{
			coretool.ProfileWorker: {
				Profile:      coretool.ProfileWorker,
				AllowedTools: []string{allowedAlias},
			},
		})
		if err != nil {
			t.Logf("NewCatalog(%q, %q) error=%v", canonical, allowedAlias, err)
			return false
		}

		if _, ok := catalog.Resolve(resolveAlias, coretool.ProfileWorker); !ok {
			t.Logf("Resolve(%q) failed for catalog tool %q allowed by %q", resolveAlias, canonical, allowedAlias)
			return false
		}

		for _, listed := range catalog.ForProfile(coretool.ProfileWorker) {
			if listed.Name != canonical || strings.ContainsAny(listed.Name, "./ ") || strings.Contains(listed.Name, "__") {
				t.Logf("ForProfile listed non-canonical tool %+v", listed)
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestToolCatalogPropertyInvalidAllowlistEntriesFailClosed(t *testing.T) {
	t.Parallel()

	property := func(toolSeed uint8, badSeed uint8) bool {
		canonical := generatedCatalogToolName(toolSeed)
		invalidAlias := injectUnsupportedCatalogNameByte(canonical, badSeed)

		catalog, err := tools.NewCatalog([]coretool.ToolDef{
			{Name: canonical},
		}, map[coretool.ProcessProfile]profiles.ProfileSpec{
			coretool.ProfileWorker: {
				Profile:      coretool.ProfileWorker,
				AllowedTools: []string{invalidAlias},
			},
		})
		if err != nil {
			t.Logf("NewCatalog valid def with invalid allowlist %q error=%v", invalidAlias, err)
			return false
		}
		if len(catalog.ForProfile(coretool.ProfileWorker)) != 0 {
			t.Logf("invalid allowlist %q exposed %v", invalidAlias, catalog.ForProfile(coretool.ProfileWorker))
			return false
		}
		if _, ok := catalog.Resolve(canonical, coretool.ProfileWorker); ok {
			t.Logf("invalid allowlist %q resolved canonical tool %q", invalidAlias, canonical)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
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

func generatedCatalogToolName(seed uint8) string {
	names := []string{
		"agent_spawn",
		"code_search",
		"context_retrieve",
		"fs_read_file",
		"obsidian_related",
		"repo_index_dag_grep",
		"todo_set_active",
	}
	return names[int(seed)%len(names)]
}

func catalogToolAlias(canonical string, seed uint8) string {
	switch seed % 6 {
	case 0:
		return canonical
	case 1:
		return strings.ReplaceAll(canonical, "_", "/")
	case 2:
		return strings.ReplaceAll(canonical, "_", ".")
	case 3:
		return " " + strings.ToUpper(strings.ReplaceAll(canonical, "_", "/")) + " "
	case 4:
		return "__" + canonical + "__"
	default:
		return strings.ReplaceAll(canonical, "_", "___")
	}
}

func injectUnsupportedCatalogNameByte(canonical string, seed uint8) string {
	bad := []byte{' ', '-', ';', ':', '\n', '\t', '\'', '"', '\\', '$', '#', '@', '!', '?', '*'}
	at := len(canonical) / 2
	return canonical[:at] + string([]byte{bad[int(seed)%len(bad)]}) + canonical[at:]
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
