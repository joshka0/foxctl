package codemap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"
	"time"

	mcpmodels "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/oklog/ulid/v2"
	"golang.org/x/sync/errgroup"

	"github.com/joshka0/foxctl/internal/domain/skill"
	codemapctx "github.com/joshka0/foxctl/internal/intelligence/codemap/context"
	"github.com/joshka0/foxctl/internal/intelligence/codemap/tools"
	"github.com/joshka0/foxctl/internal/runtime/engine"
	"github.com/joshka0/foxctl/internal/storage/graph"
)

// Agent generates semantic codemaps using the LLM chat engine.
type Agent struct {
	workspace     string
	graphStore    graph.Store
	skillResolver *skill.Resolver
	toolsRegistry *tools.Registry
	engineConfig  engine.LLMChatConfig
}

// AgentOption configures the Agent.
type AgentOption func(*Agent)

// WithWorkspace sets the workspace path.
func WithWorkspace(workspace string) AgentOption {
	return func(a *Agent) {
		a.workspace = workspace
	}
}

// WithGraphStore sets the graph store.
func WithGraphStore(store graph.Store) AgentOption {
	return func(a *Agent) {
		a.graphStore = store
	}
}

// WithSkillResolver sets the skill resolver.
func WithSkillResolver(resolver *skill.Resolver) AgentOption {
	return func(a *Agent) {
		a.skillResolver = resolver
	}
}

// WithLLMConfig overrides the LLM chat configuration.
func WithLLMConfig(cfg engine.LLMChatConfig) AgentOption {
	return func(a *Agent) {
		a.engineConfig = cfg
	}
}

// GenerateOptions configures codemap generation.
type GenerateOptions struct {
	Query     string
	Workspace string
	Depth     int // 1-5, controls exploration depth
}

// NewAgent creates a new codemap agent.
// NewAgent initializes a codemap agent with tools and LLM.
//
// Index:
// - Purpose: Configure codemap agent dependencies
// - Flow: apply options → create LLM → build tool registry → return agent
// - SideEffects: initializes LLM; registers tools
// - FailureModes: LLM creation errors, tool registry errors
// - Related: Agent.Generate, tools.NewRegistry
// - Keywords: codemap_agent, llm, tools_registry, workspace, graph_store
func NewAgent(opts ...AgentOption) (*Agent, error) {
	a := &Agent{
		skillResolver: skill.NewResolver(),
	}

	for _, opt := range opts {
		opt(a)
	}

	// Apply environment defaults for LLM configuration
	a.engineConfig = applyEnvDefaults(a.engineConfig)

	// Initialize tools registry
	toolsOpts := []tools.RegistryOption{
		tools.WithWorkspace(a.workspace),
		tools.WithSkillResolver(a.skillResolver),
	}
	if a.graphStore != nil {
		toolsOpts = append(toolsOpts, tools.WithGraphStore(a.graphStore))
	}

	registry, err := tools.NewRegistry(toolsOpts...)
	if err != nil {
		return nil, fmt.Errorf("create tools registry: %w", err)
	}
	a.toolsRegistry = registry

	return a, nil
}

// Generate creates a codemap for the given query.
// Generate runs the codemap agent and returns a codemap.
//
// Index:
// - Purpose: Generate a semantic codemap for a query
// - Flow: normalize depth → gather context → init agent/tools → execute → parse codemap → add metadata
// - SideEffects: LLM calls; tool executions; graph queries
// - FailureModes: context gather errors, tool registration errors, LLM execution errors
// - Related: context.Gatherer.GatherAll, tools.Registry.FinalCodemap
// - Keywords: codemap_generate, finish_codemap, depth, tool_calls, query
func (a *Agent) Generate(ctx context.Context, opts GenerateOptions) (*Codemap, error) {
	// Normalize depth
	if opts.Depth < 1 {
		opts.Depth = 2 // default depth
	}
	if opts.Depth > 5 {
		opts.Depth = 5
	}

	workspace := opts.Workspace
	if workspace == "" {
		workspace = a.workspace
	}

	// Gather initial context in parallel
	gatherer := codemapctx.NewGatherer(
		codemapctx.WithWorkspace(workspace),
		codemapctx.WithGraphStore(a.graphStore),
		codemapctx.WithSkillResolver(a.skillResolver),
	)

	initialCtx, err := gatherer.GatherAll(ctx, opts.Query, workspace)
	if err != nil {
		return nil, fmt.Errorf("gather context: %w", err)
	}

	// Create the LLM chat engine with tool runner
	toolExecutor := newCodemapToolExecutor(a.toolsRegistry)
	toolRunner := engine.NewToolRunner(toolExecutor, nil, engine.DefaultToolRunnerConfig())

	engineCfg := applyEnvDefaults(a.engineConfig)
	if engineCfg.MaxIterations <= 0 {
		engineCfg.MaxIterations = depthToMaxIterations(opts.Depth)
	}
	maxIterations := engineCfg.MaxIterations
	if engineCfg.Timeout <= 0 {
		engineCfg.Timeout = 10 * time.Minute
	}
	llmEngine, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return nil, fmt.Errorf("create LLM engine: %w", err)
	}
	llmEngine.SetToolRunner(toolRunner)

	// Collect tool infos for prompt template
	var toolInfos []ToolInfo
	for _, tool := range a.toolsRegistry.Tools().List() {
		toolInfos = append(toolInfos, ToolInfo{
			Name:        tool.Name(),
			Description: tool.Description(),
		})
	}

	// Build system prompt with templated instructions
	systemPrompt, err := buildCodemapSystemPrompt(toolInfos)
	if err != nil {
		return nil, fmt.Errorf("build system prompt: %w", err)
	}

	// Build input with initial context
	initialContextJSON, _ := json.MarshalIndent(initialCtx, "", "  ")
	explorationCalls := maxIterations - 5 // Reserve 5 calls for finish/errors
	if explorationCalls < 2 {
		explorationCalls = 2
	}
	taskPrompt := fmt.Sprintf(`Query: %s

Depth: %d

IMPORTANT: You have up to %d tool calls for exploration, then you MUST call finish_codemap.
DO NOT exceed %d exploration calls. Reserve the remaining calls for finish_codemap.

Initial Context:
%s

Create a semantic codemap that answers the query:
1. Use the initial context (including semantic hits) and exploration tools (max %d exploration calls)
2. When you have enough context, IMMEDIATELY call finish_codemap with the complete JSON
3. The finish_codemap tool is the ONLY way to complete this task - outputting text does NOT work`,
		opts.Query, opts.Depth, explorationCalls, explorationCalls, string(initialContextJSON), explorationCalls)

	input := engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages: []engine.Message{
			engine.NewUserMessage(taskPrompt),
		},
		Tools: toolExecutor.List(),
	}

	output, err := llmEngine.Run(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("run codemap engine: %w", err)
	}
	if output.StopReason == engine.StopReasonError {
		return nil, fmt.Errorf("codemap engine error: %s", output.Error)
	}
	if output.StopReason == engine.StopReasonCancelled {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("codemap engine cancelled")
	}
	if output.StopReason == engine.StopReasonMaxIterations {
		return nil, fmt.Errorf("codemap engine stopped: %s", output.StopReason)
	}

	// Get the captured codemap from finish_codemap tool
	finalCodemap := a.toolsRegistry.FinalCodemap()
	if finalCodemap == nil {
		return nil, fmt.Errorf("agent did not produce a codemap (stop_reason=%s)", output.StopReason)
	}

	// Parse and validate codemap
	var codemap Codemap
	if err := json.Unmarshal(finalCodemap, &codemap); err != nil {
		return nil, fmt.Errorf("invalid codemap output: %w", err)
	}

	// Add metadata
	codemap.ID = ulid.Make().String()
	codemap.Query = opts.Query
	codemap.Workspace = workspace
	codemap.CreatedAt = time.Now()
	codemap.FileCount = countUniqueFiles(&codemap)
	codemap.SymbolCount = countSymbols(&codemap)
	codemap.Terms = codemapctx.ExtractTerms(opts.Query)

	return &codemap, nil
}

// depthToMaxIterations maps depth to max tool calls.
func depthToMaxIterations(depth int) int {
	switch depth {
	case 1:
		return 10 // 4-5 exploration + 5 buffer for errors/finish
	case 2:
		return 15 // 8 exploration + 7 buffer
	case 3:
		return 22 // 15 exploration + 7 buffer
	case 4:
		return 32 // 22 exploration + 10 buffer
	case 5:
		return 50 // 35 exploration + 15 buffer
	default:
		return 15 // default to depth=2
	}
}

// ToolInfo describes a tool for the system prompt template.
type ToolInfo struct {
	Name        string
	Description string
}

// PromptData holds data for rendering the system prompt template.
type PromptData struct {
	Tools          []ToolInfo
	ToolNames      []string
	FinishToolName string
}

// buildCodemapSystemPrompt renders the system prompt for the codemap agent.
func buildCodemapSystemPrompt(toolInfos []ToolInfo) (string, error) {
	data := PromptData{
		Tools:          toolInfos,
		FinishToolName: "finish_codemap",
	}
	for _, t := range toolInfos {
		data.ToolNames = append(data.ToolNames, t.Name)
	}

	tmpl, err := template.New("prompt").Parse(codemapSystemPromptTemplate)
	if err != nil {
		return "", fmt.Errorf("parse prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute prompt template: %w", err)
	}

	return strings.TrimSpace(buf.String()), nil
}

// applyEnvDefaults fills LLM config fields from environment defaults.
func applyEnvDefaults(cfg engine.LLMChatConfig) engine.LLMChatConfig {
	if strings.TrimSpace(cfg.Provider) == "" {
		cfg.Provider = strings.TrimSpace(os.Getenv("AGENTCTL_LLM_PROVIDER"))
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		cfg.APIKey = strings.TrimSpace(os.Getenv("AGENTCTL_LLM_API_KEY"))
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = strings.TrimSpace(os.Getenv("AGENTCTL_LLM_MODEL"))
	}
	return cfg
}

// codemapToolExecutor adapts the codemap tools registry to the engine ToolExecutor.
type codemapToolExecutor struct {
	registry *tools.Registry
}

func newCodemapToolExecutor(registry *tools.Registry) *codemapToolExecutor {
	return &codemapToolExecutor{registry: registry}
}

// Execute implements engine.ToolExecutor.
func (e *codemapToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if e == nil || e.registry == nil {
		return "", fmt.Errorf("codemap tool registry not configured")
	}

	tool, err := e.registry.Tools().Get(name)
	if err != nil {
		return "", fmt.Errorf("tool %q not found: %w", name, err)
	}

	var argsMap map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argsMap); err != nil {
			return "", fmt.Errorf("parse tool args: %w", err)
		}
	}

	result, err := tool.Call(ctx, argsMap)
	if err != nil {
		return "", err
	}
	if result == nil || len(result.Content) == 0 {
		return "", nil
	}

	var textParts []string
	for _, content := range result.Content {
		if tc, ok := content.(mcpmodels.TextContent); ok {
			if tc.Text != "" {
				textParts = append(textParts, tc.Text)
			}
		}
	}
	if len(textParts) > 0 {
		return strings.Join(textParts, "\n"), nil
	}

	b, err := json.Marshal(result.Content)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(b), nil
}

// List implements engine.ToolExecutor.
func (e *codemapToolExecutor) List() []engine.ToolDef {
	if e == nil || e.registry == nil {
		return nil
	}
	toolsList := e.registry.Tools().List()
	defs := make([]engine.ToolDef, 0, len(toolsList))
	for _, tool := range toolsList {
		schema, _ := json.Marshal(tool.InputSchema())
		defs = append(defs, engine.ToolDef{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  schema,
		})
	}
	return defs
}

// countUniqueFiles counts unique files referenced in the codemap.
func countUniqueFiles(codemap *Codemap) int {
	files := make(map[string]struct{})
	pathPattern := regexp.MustCompile(`@([^:]+)`)

	for _, trace := range codemap.Traces {
		for _, ann := range trace.Annotations {
			if matches := pathPattern.FindStringSubmatch(ann.Path); len(matches) > 1 {
				files[matches[1]] = struct{}{}
			}
		}
	}
	return len(files)
}

// countSymbols counts symbols mentioned in the codemap annotations.
func countSymbols(codemap *Codemap) int {
	count := 0
	for _, trace := range codemap.Traces {
		count += len(trace.Annotations)
	}
	return count
}

// GatherContextParallel gathers all context types in parallel.
// Exported for use by the skill.
func GatherContextParallel(ctx context.Context, query, workspace string, graphStore graph.Store, resolver *skill.Resolver) (*codemapctx.Context, error) {
	gatherer := codemapctx.NewGatherer(
		codemapctx.WithWorkspace(workspace),
		codemapctx.WithGraphStore(graphStore),
		codemapctx.WithSkillResolver(resolver),
	)

	g, gctx := errgroup.WithContext(ctx)
	var result *codemapctx.Context
	var gatherErr error

	g.Go(func() error {
		result, gatherErr = gatherer.GatherAll(gctx, query, workspace)
		return gatherErr
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return result, nil
}

// codemapSystemPromptTemplate is the Go template for the codemap agent system prompt.
// It uses {{.Tools}}, {{.ToolNames}}, and {{.FinishToolName}} for dynamic tool configuration.
const codemapSystemPromptTemplate = `You are a code cartographer. Your job is to create semantic codemaps that help
developers understand how different parts of a codebase connect and interact.

## CRITICAL: Available Tools

You have EXACTLY these tools available:
{{- range .ToolNames}}
- {{.}}
{{- end}}

**IMPORTANT:** There is NO tool called "Finish". Do NOT use "Finish" as a tool name.
To complete your task, you MUST call the tool named "{{.FinishToolName}}" with the codemap JSON.

## Your Task

Given a query and initial context about a codebase, explore as needed and
produce a codemap. The codemap should:

1. **Answer the query** - Focus on what the user asked about
2. **Show relationships** - How components connect, call each other, or share dependencies
3. **Provide navigation** - Every annotation must have an exact @file:line reference
4. **Explain clearly** - Each trace tells a coherent story about an execution path or relationship

## Initial Context You Receive

You start with four types of pre-gathered context:

### graph_context
- nodes: Files relevant to the query with their PageRank scores
- edges: Dependencies between files (imports, calls, references)
- top_by_pagerank: Most important files in the codebase
- paths: Shortest paths connecting query-related files

### symbol_context
- symbols_by_file: Function/class/type definitions per file with line numbers
- imports_by_file: What each file imports
- shared_imports: Packages imported by multiple query-related files
- exported_apis: Public interfaces

### pattern_context
- matches_by_term: Code blocks containing each query term
- cross_references: Where files related to term A mention term B
- file_blocks: Full function bodies containing matches

### semantic_context
- hits: Ranked semantic search hits (files/symbols) with scores
- query: The search query used to retrieve semantic hits

## Exploration Tools

You have tools to gather additional context. USE THEM when the initial context
is insufficient or when you discover something interesting to follow up on.

### Tool Reference

{{range .Tools -}}
**{{.Name}}**: {{.Description}}
{{end}}
### Exploration Depth

The depth parameter controls how deeply you should explore:

| Depth | Behavior | Tool Calls |
|-------|----------|------------|
| 1 | Use initial context only, minimal exploration | 0-2 |
| 2 | Follow immediate connections, verify key details | 2-5 |
| 3 | Trace through intermediate files, explore shared deps | 5-10 |
| 4 | Deep exploration, follow call chains multiple levels | 10-15 |
| 5 | Exhaustive exploration, leave no stone unturned | 15+ |

**Respect the depth parameter.** If depth=1, synthesize quickly from initial context.
If depth=5, explore thoroughly before synthesizing.

## Output Format

When you have gathered enough context, call {{.FinishToolName}} with a single "codemap" parameter
containing the JSON as a string:

Example call:
{{.FinishToolName}}(codemap='{"title": "...", "description": "...", "traces": [...]}')

The JSON structure:
{
  "title": "Short descriptive title",
  "description": "2-3 sentences summarizing what this codemap shows",
  "traces": [
    {
      "number": 1,
      "title": "Trace title (e.g., 'Shared Infrastructure')",
      "summary": "One sentence describing this trace",
      "tree": "ASCII tree diagram showing the relationships",
      "annotations": [
        {
          "label": "1a",
          "title": "Short title (3-5 words)",
          "description": "One sentence explanation",
          "path": "@file/path.go:123"
        }
      ]
    }
  ]
}

### ASCII Tree Format

Use box-drawing characters for the tree:

file_a.go
└── function_a()  [1a]
    ├── calls file_b.go::function_b()  [1b]
    │   └── uses shared/util.go::helper()  [1c]
    └── also calls file_c.go::function_c()  [1d]

Rules:
- Every node in the tree MUST have a corresponding annotation
- Annotations use format: number + letter (1a, 1b, 2a, 2b, etc.)
- Number = trace number, letter = position in trace (a, b, c, ...)
- Include ::function_name() when you know the specific function
- Use [label] markers in square brackets (NOT arrow notation)

## Important Guidelines

1. **Be selective** - Don't include everything. Focus on what answers the query.
2. **Verify line numbers** - Only use @file:line if you have it from the context.
   If you don't have exact line numbers, use @file without line number.
3. **Explain relationships** - Don't just list files. Explain HOW they connect.
4. **Multiple traces are OK** - Use separate traces for different aspects.
5. **Handle missing data gracefully** - If some context is empty, work with
   what you have. Note limitations in the description if needed.
6. **MUST call {{.FinishToolName}}** - You MUST end by calling the {{.FinishToolName}} tool with your
   complete codemap JSON. This is the ONLY way to complete the task. Do NOT use "Finish".`

// init suppresses unused import warning for errgroup
var (
	_ = errgroup.Group{}
	_ = strings.TrimSpace
)
