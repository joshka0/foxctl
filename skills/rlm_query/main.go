// Package main implements the rlm/query skill as a compact wrapper around the RLM runtime.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/protocol"
)

const (
	Command = "rlm/query"

	defaultExecutor      = "llm"
	defaultLLMProvider   = "openrouter"
	defaultLLMModel      = "poolside/laguna-xs.2:free"
	defaultLLMTimeout    = "60s"
	defaultRouteProfile  = "code_retrieval"
	defaultToolProfile   = "code-intel"
	defaultPlanMode      = "free"
	defaultMaxIterations = 4
	defaultMaxDepth      = 1
	defaultMaxSubcalls   = 2
)

// Input defines a Pi/OpenAPI-friendly RLM query request.
type Input struct {
	Prompt         string `json:"prompt,omitempty"`
	Workspace      string `json:"workspace,omitempty"`
	WorkspaceRoot  string `json:"workspace_root,omitempty"`
	Executor       string `json:"executor,omitempty"`
	LLMProvider    string `json:"llm_provider,omitempty"`
	LLMModel       string `json:"llm_model,omitempty"`
	LLMBaseURL     string `json:"llm_base_url,omitempty"`
	LLMTimeout     string `json:"llm_timeout,omitempty"`
	RouteProfile   string `json:"route_profile,omitempty"`
	ToolProfile    string `json:"tool_profile,omitempty"`
	PlanMode       string `json:"plan_mode,omitempty"`
	MaxIterations  int    `json:"max_iterations,omitempty"`
	MaxDepth       int    `json:"max_depth,omitempty"`
	MaxSubcalls    int    `json:"max_subcalls,omitempty"`
	RequireToolUse *bool  `json:"require_tool_use,omitempty"`
}

// Output is the compact RLM response shape intended for agents and Pi tools.
type Output struct {
	Answer            string         `json:"answer,omitempty"`
	Mode              string         `json:"mode,omitempty"`
	Iterations        int            `json:"iterations,omitempty"`
	RetrievedPaths    []string       `json:"retrieved_paths,omitempty"`
	EvidenceRefs      []string       `json:"evidence_refs,omitempty"`
	ToolNames         []string       `json:"tool_names,omitempty"`
	ParentTotalTokens int            `json:"parent_total_tokens,omitempty"`
	ParentToolUsage   map[string]any `json:"parent_tool_usage,omitempty"`
	ResultMetadata    map[string]any `json:"result_metadata,omitempty"`
	RouteProfile      string         `json:"route_profile,omitempty"`
	ToolProfile       string         `json:"tool_profile,omitempty"`
	PlanMode          string         `json:"plan_mode,omitempty"`
	EnvelopeCommand   string         `json:"envelope_command,omitempty"`
	SubprocessError   string         `json:"subprocess_error,omitempty"`
	StdoutLogs        []string       `json:"stdout_logs,omitempty"`
	StderrPreview     string         `json:"stderr_preview,omitempty"`
	DurationMS        int64          `json:"duration_ms,omitempty"`
}

type rlmRunner func(ctx context.Context, dir, bin string, env []string, args ...string) executil.CmdResult

func main() {
	skillmain.Main(Command, skillmain.Chain(run,
		skillmain.WithTimeout[Input](90*time.Second),
		skillmain.WithRecover[Input](),
	))
}

// run executes an RLM query and projects the verbose runtime envelope into stable fields.
//
// Index:
//
//	Purpose: expose RLM code-retrieval queries through OpenAPI-enabled foxctl skills
//	Keywords: rlm/query, RLM, retrieve_code, Pi tool, OpenAPI skill
//	Related: normalizeInput, buildRLMArgs, extractRLMEnvelope, outputFromRLMData
//	OutputFields: answer, retrieved_paths, evidence_refs, tool_names, parent_total_tokens
//
// [[domain:rlm-query-skill]]
// [[protocol:rlm-subprocess-envelope-normalization]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	out, err := executeRLMQuery(ctx, rc.Workspace, in, executil.RunWithEnv)
	if err != nil {
		return err
	}
	return skillout.Emit(rc, Command, out)
}

func executeRLMQuery(ctx context.Context, rcWorkspace string, in Input, runner rlmRunner) (*Output, error) {
	if runner == nil {
		runner = executil.RunWithEnv
	}
	workspaceRoot, err := normalizeInput(rcWorkspace, &in)
	if err != nil {
		return nil, err
	}

	args := buildRLMArgs(workspaceRoot, in)
	env := buildRLMEnv(in)
	start := time.Now()
	result := runner(ctx, workspaceRoot, executil.FoxctlBin(), env, args...)
	duration := time.Since(start)

	rlmEnv, logs, decodeErr := extractRLMEnvelope(result.Stdout)
	if decodeErr != nil {
		if result.Err != nil {
			return nil, skillerr.WrapRuntime("run rlm query", result.Err,
				skillerr.WithData("stderr", previewBytes(result.Stderr, 1000)),
				skillerr.WithData("stdout", previewBytes(result.Stdout, 1000)),
			)
		}
		return nil, skillerr.WrapParse("decode rlm envelope", decodeErr,
			skillerr.WithData("stdout", previewBytes(result.Stdout, 1000)),
		)
	}
	if rlmEnv.Status == envelope.StatusError {
		return nil, skillerr.Runtime("rlm query failed",
			skillerr.WithData("code", rlmEnv.Error.Code),
			skillerr.WithData("message", rlmEnv.Error.Message),
			skillerr.WithData("stderr", previewBytes(result.Stderr, 1000)),
		)
	}

	out, err := outputFromRLMData(rlmEnv)
	if err != nil {
		return nil, err
	}
	out.EnvelopeCommand = rlmEnv.Command
	out.StdoutLogs = logs
	out.DurationMS = duration.Milliseconds()
	if result.Err != nil {
		out.SubprocessError = result.Err.Error()
	}
	if stderr := strings.TrimSpace(string(result.Stderr)); stderr != "" {
		out.StderrPreview = skillout.TruncateStringWithSuffix(stderr, 1000, "... (truncated)")
	}
	return out, nil
}

func normalizeInput(base string, in *Input) (string, error) {
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Prompt == "" {
		return "", skillerr.Arg("prompt is required")
	}

	if in.Workspace == "" {
		in.Workspace = in.WorkspaceRoot
	}
	workspace := strings.TrimSpace(in.Workspace)
	if workspace == "" {
		return "", skillerr.Arg(
			"workspace is required",
			skillerr.WithHint("Pass workspace or workspace_root with the repo root. This prevents rlm/query from running relative to the installed skill directory."),
		)
	}
	if !filepath.IsAbs(workspace) && strings.TrimSpace(base) != "" {
		workspace = filepath.Join(base, workspace)
	}
	workspaceRoot, err := filepath.Abs(workspace)
	if err != nil {
		return "", skillerr.WrapArg("resolve workspace", err)
	}
	in.Workspace = workspaceRoot
	in.WorkspaceRoot = workspaceRoot

	in.Executor = defaultString(in.Executor, defaultExecutor)
	in.LLMProvider = defaultString(in.LLMProvider, defaultLLMProvider)
	in.LLMModel = defaultString(in.LLMModel, defaultLLMModel)
	in.LLMTimeout = defaultString(in.LLMTimeout, defaultLLMTimeout)
	in.RouteProfile = defaultString(in.RouteProfile, defaultRouteProfile)
	in.ToolProfile = defaultString(in.ToolProfile, defaultToolProfile)
	in.PlanMode = defaultString(in.PlanMode, defaultPlanMode)
	in.MaxIterations = defaultPositiveInt(in.MaxIterations, defaultMaxIterations)
	in.MaxDepth = defaultPositiveInt(in.MaxDepth, defaultMaxDepth)
	in.MaxSubcalls = defaultPositiveInt(in.MaxSubcalls, defaultMaxSubcalls)
	if in.RequireToolUse == nil {
		value := true
		in.RequireToolUse = &value
	}

	if err := validateEnum("executor", in.Executor, "inspect", "llm", "repl"); err != nil {
		return "", err
	}
	if err := validateEnum("route_profile", in.RouteProfile, "auto", "code_retrieval", "memory_recall", "mixed", "evidence_audit"); err != nil {
		return "", err
	}
	if err := validateEnum("plan_mode", in.PlanMode, "free", "staged", "lambda-retrieval"); err != nil {
		return "", err
	}
	return workspaceRoot, nil
}

func buildRLMArgs(workspaceRoot string, in Input) []string {
	args := []string{
		"rlm", "run",
		"--workspace", workspaceRoot,
		"--executor", in.Executor,
		"--route-profile", in.RouteProfile,
		"--tool-profile", in.ToolProfile,
		"--plan-mode", in.PlanMode,
		"--max-iterations", fmt.Sprintf("%d", in.MaxIterations),
		"--max-depth", fmt.Sprintf("%d", in.MaxDepth),
		"--max-subcalls", fmt.Sprintf("%d", in.MaxSubcalls),
		fmt.Sprintf("--require-tool-use=%t", *in.RequireToolUse),
	}
	if in.Executor == "llm" {
		args = append(args,
			"--llm-provider", in.LLMProvider,
			"--llm-model", in.LLMModel,
			"--llm-timeout", in.LLMTimeout,
		)
		if strings.TrimSpace(in.LLMBaseURL) != "" {
			args = append(args, "--llm-base-url", strings.TrimSpace(in.LLMBaseURL))
		}
	}
	args = append(args, "--prompt", in.Prompt)
	return args
}

func buildRLMEnv(in Input) []string {
	var env []string
	if strings.EqualFold(in.Executor, "llm") &&
		strings.EqualFold(in.LLMProvider, "openrouter") &&
		strings.TrimSpace(os.Getenv("FOXCTL_RLM_LLM_API_KEY")) == "" {
		if key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); key != "" {
			env = append(env, "FOXCTL_RLM_LLM_API_KEY="+key)
		}
	}
	return env
}

func extractRLMEnvelope(stdout []byte) (envelope.Envelope, []string, error) {
	var logs []string
	lines := bytes.Split(stdout, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("{")) {
			logs = append([]string{string(line)}, logs...)
			continue
		}
		env, err := protocol.DecodeEnvelope(line)
		if err == nil && env.Version == envelope.Version && env.Command != "" {
			return env, compactLogLines(lines[:i]), nil
		}
	}

	idx := bytes.Index(stdout, []byte(`{"version"`))
	if idx < 0 {
		return envelope.Envelope{}, logs, fmt.Errorf("no JSON envelope found")
	}
	for end := len(stdout); end > idx; end-- {
		candidate := bytes.TrimSpace(stdout[idx:end])
		if len(candidate) == 0 || candidate[len(candidate)-1] != '}' {
			continue
		}
		env, err := protocol.DecodeEnvelope(candidate)
		if err == nil && env.Version == envelope.Version && env.Command != "" {
			return env, compactLogLines(bytes.Split(stdout[:idx], []byte{'\n'})), nil
		}
	}
	return envelope.Envelope{}, logs, fmt.Errorf("no decodable JSON envelope found")
}

func outputFromRLMData(env envelope.Envelope) (*Output, error) {
	data, ok := objectMap(env.Data)
	if !ok {
		return nil, skillerr.Parse("rlm envelope data is not an object")
	}
	result, _ := objectMap(data["result"])
	metadata, _ := objectMap(result["metadata"])
	runSpec, _ := objectMap(data["run_spec"])
	toolPolicy, _ := objectMap(runSpec["tool_policy"])

	out := &Output{
		Answer:            stringAt(result, "answer"),
		Mode:              stringAt(data, "mode"),
		Iterations:        intAt(result, "iterations"),
		RetrievedPaths:    stringSliceAt(metadata, "retrieved_paths"),
		EvidenceRefs:      stringSliceAt(metadata, "evidence_refs"),
		ToolNames:         stringSliceAt(metadata, "tool_names"),
		ParentTotalTokens: intAt(metadata, "parent_total_tokens"),
		ResultMetadata:    metadata,
		RouteProfile:      stringAt(runSpec, "route_profile"),
		ToolProfile:       stringAt(toolPolicy, "profile"),
		PlanMode:          stringAt(runSpec, "plan_mode"),
	}
	if usage, ok := objectMap(metadata["parent_tool_usage"]); ok {
		out.ParentToolUsage = usage
	}
	return out, nil
}

func compactLogLines(lines [][]byte) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" || strings.HasPrefix(trimmed, "{") {
			continue
		}
		out = append(out, skillout.TruncateStringWithSuffix(trimmed, 240, "..."))
	}
	if len(out) > 20 {
		return out[len(out)-20:]
	}
	return out
}

func objectMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[any]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[fmt.Sprint(k)] = v
		}
		return out, true
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, false
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, false
		}
		return out, true
	}
}

func stringAt(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return value
}

func stringSliceAt(data map[string]any, key string) []string {
	raw, ok := data[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func intAt(data map[string]any, key string) int {
	switch value := data[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

func validateEnum(name, value string, allowed ...string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return skillerr.Argf("%s must be one of: %s", name, strings.Join(allowed, ", "))
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func defaultPositiveInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func previewBytes(data []byte, limit int) string {
	return skillout.TruncateStringWithSuffix(strings.TrimSpace(string(data)), limit, "... (truncated)")
}
