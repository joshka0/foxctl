package optimization

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	dspycore "github.com/XiaoConstantine/dspy-go/pkg/core"
	dspyllms "github.com/XiaoConstantine/dspy-go/pkg/llms"
	dspyoptimizers "github.com/XiaoConstantine/dspy-go/pkg/optimizers"
)

var dspyGlobalMu sync.Mutex

func (p *PromptOptimizer) optimizeInstructionDSPyGo(ctx context.Context, workspaceID, agentRole, currentPrompt string) (*OptimizationResult, error) {
	startTime := time.Now()

	currentScore, err := p.evaluate(ctx, workspaceID, agentRole, currentPrompt)
	if err != nil {
		return nil, fmt.Errorf("prompt optimizer: evaluate current: %w", err)
	}

	candidates, optimizedPrompt, optimizedScore, err := p.runDSPyGoPromptSearch(ctx, workspaceID, agentRole, currentPrompt, p.config.BreadthCandidates)
	if err != nil {
		return nil, err
	}

	result := &OptimizationResult{
		OriginalPrompt:  currentPrompt,
		OriginalScore:   currentScore,
		OptimizedPrompt: currentPrompt,
		OptimizedScore:  currentScore,
		Candidates:      candidates,
		Mode:            p.config.Mode,
		Duration:        time.Since(startTime),
	}
	if strings.TrimSpace(optimizedPrompt) != "" && optimizedScore > currentScore+p.config.MinImprovement {
		result.OptimizedPrompt = optimizedPrompt
		result.OptimizedScore = optimizedScore
	}
	if result.OriginalScore > 0 {
		result.Improvement = (result.OptimizedScore - result.OriginalScore) / result.OriginalScore
	}
	return result, nil
}

func (p *PromptOptimizer) proposeCandidatesDSPyGo(ctx context.Context, workspaceID, agentRole, basePrompt string, count int) ([]ScoredPrompt, error) {
	if strings.TrimSpace(basePrompt) == "" {
		return nil, fmt.Errorf("prompt optimizer: base prompt is required")
	}
	if count <= 0 {
		count = p.config.BreadthCandidates
	}
	candidates, _, _, err := p.runDSPyGoPromptSearch(ctx, workspaceID, agentRole, basePrompt, count)
	if err != nil {
		return nil, err
	}
	if len(candidates) > count {
		candidates = candidates[:count]
	}
	return candidates, nil
}

func (p *PromptOptimizer) runDSPyGoPromptSearch(ctx context.Context, workspaceID, agentRole, basePrompt string, candidateLimit int) ([]ScoredPrompt, string, float64, error) {
	if strings.TrimSpace(basePrompt) == "" {
		return nil, "", 0, fmt.Errorf("prompt optimizer: base prompt is required")
	}

	restore, err := p.configureDSPyGoLLMs()
	if err != nil {
		return nil, "", 0, err
	}
	defer restore()

	program := dspyGoPromptProgram(basePrompt)
	dataset := &dspyGoExampleDataset{examples: p.buildDSPyGoExamples(agentRole, basePrompt)}
	config := dspyoptimizers.DefaultGEPAConfig()
	config.PopulationSize = max(4, candidateLimit)
	config.MaxGenerations = max(1, p.config.DepthIterations)
	config.ReflectionFreq = 1
	config.EvaluationBatchSize = max(1, min(len(dataset.examples), max(2, candidateLimit)))
	config.ConcurrencyLevel = 1
	config.FeedbackEvaluator = p.dspyGoFeedbackEvaluator()
	config.AddFormatFailureAsFeedback = true

	gepa, err := dspyoptimizers.NewGEPA(config)
	if err != nil {
		return nil, "", 0, fmt.Errorf("prompt optimizer: create dspy-go gepa: %w", err)
	}

	metric := p.dspyGoMetric(ctx, workspaceID, agentRole)
	optimizedProgram, err := gepa.Compile(ctx, program, dataset, metric)
	if err != nil {
		return nil, "", 0, fmt.Errorf("prompt optimizer: dspy-go gepa compile: %w", err)
	}

	state := gepa.GetOptimizationState()
	seen := map[string]struct{}{}
	candidates := make([]ScoredPrompt, 0)
	appendCandidate := func(prompt string, generation int) error {
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			return nil
		}
		if _, ok := seen[prompt]; ok {
			return nil
		}
		score, err := p.evaluate(ctx, workspaceID, agentRole, prompt)
		if err != nil {
			return err
		}
		seen[prompt] = struct{}{}
		candidates = append(candidates, ScoredPrompt{
			Prompt:       prompt,
			Score:        score,
			Improvements: []string{"dspy_go_gepa"},
			Generation:   generation,
		})
		return nil
	}

	if state != nil {
		for _, population := range state.PopulationHistory {
			if population == nil {
				continue
			}
			for _, candidate := range population.Candidates {
				if candidate == nil {
					continue
				}
				if err := appendCandidate(strings.TrimSpace(candidate.Instruction), candidate.Generation); err != nil {
					return nil, "", 0, err
				}
			}
		}
	}

	bestPrompt := dspyGoProgramInstruction(optimizedProgram)
	if bestPrompt == "" && state != nil && state.BestCandidate != nil {
		bestPrompt = strings.TrimSpace(state.BestCandidate.Instruction)
	}
	if bestPrompt == "" {
		bestPrompt = basePrompt
	}
	if err := appendCandidate(bestPrompt, max(1, p.config.DepthIterations)); err != nil {
		return nil, "", 0, err
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Prompt < candidates[j].Prompt
		}
		return candidates[i].Score > candidates[j].Score
	})

	bestScore := 0.0
	if score, err := p.evaluate(ctx, workspaceID, agentRole, bestPrompt); err == nil {
		bestScore = score
	}

	return candidates, bestPrompt, bestScore, nil
}

func (p *PromptOptimizer) dspyGoMetric(ctx context.Context, workspaceID, agentRole string) dspycore.Metric {
	return func(expected, actual map[string]interface{}) float64 {
		prompt, _ := actual["prompt"].(string)
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			return 0
		}

		question, _ := expected["question"].(string)
		contextText, _ := expected["context"].(string)
		targetResponse, _ := expected["target_response"].(string)
		if strings.TrimSpace(question) != "" || strings.TrimSpace(contextText) != "" || strings.TrimSpace(targetResponse) != "" {
			eval, err := p.evaluatePromptJudgeCase(ctx, prompt, question, contextText, targetResponse)
			if err == nil {
				return eval.Result.Score
			}
		}
		score, err := p.evaluate(ctx, workspaceID, agentRole, prompt)
		if err != nil {
			return 0
		}
		return score
	}
}

func (p *PromptOptimizer) dspyGoFeedbackEvaluator() dspyoptimizers.GEPAFeedbackEvaluator {
	return dspyoptimizers.GEPAFeedbackEvaluatorFunc(func(ctx context.Context, expected, actual map[string]interface{}, info *dspyoptimizers.GEPAFeedbackContext) *dspyoptimizers.GEPAFeedback {
		prompt, _ := actual["prompt"].(string)
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			return nil
		}
		question, _ := expected["question"].(string)
		contextText, _ := expected["context"].(string)
		targetResponse, _ := expected["target_response"].(string)
		if strings.TrimSpace(question) == "" && strings.TrimSpace(contextText) == "" && strings.TrimSpace(targetResponse) == "" {
			return nil
		}

		eval, err := p.evaluatePromptJudgeCase(ctx, prompt, question, contextText, targetResponse)
		if err != nil {
			return nil
		}

		targetComponent := "prompt_optimizer"
		if info != nil && info.Candidate != nil && strings.TrimSpace(info.Candidate.ModuleName) != "" {
			targetComponent = strings.TrimSpace(info.Candidate.ModuleName)
		}
		return &dspyoptimizers.GEPAFeedback{
			Feedback:        eval.Feedback,
			TargetComponent: targetComponent,
			Metadata: map[string]interface{}{
				"judge_score":       eval.Result.Score,
				"target_similarity": eval.Result.TargetSimilarity,
				"query_similarity":  eval.Result.QuerySimilarity,
				"length_quality":    eval.Result.LengthQuality,
				"generic_penalty":   eval.Result.GenericPenalty,
			},
		}
	})
}

func (p *PromptOptimizer) configureDSPyGoLLMs() (func(), error) {
	primary := p.config.PrimaryLLM
	if primary == nil || strings.TrimSpace(primary.Model) == "" {
		return nil, fmt.Errorf("prompt optimizer: dspy-go backend requires a primary llm target")
	}

	primaryLLM, err := newDSPyGoOpenAICompatibleLLM(*primary)
	if err != nil {
		return nil, err
	}

	active := dspycore.LLM(primaryLLM)
	if fallback := p.config.FallbackLLM; fallback != nil && strings.TrimSpace(fallback.Model) != "" {
		fallbackLLM, err := newDSPyGoOpenAICompatibleLLM(*fallback)
		if err != nil {
			return nil, err
		}
		active = &dspyGoFailoverLLM{
			primary:  primaryLLM,
			fallback: fallbackLLM,
		}
	}

	dspyGlobalMu.Lock()
	prevDefault := dspycore.GetDefaultLLM()
	prevTeacher := dspycore.GetTeacherLLM()
	dspycore.SetDefaultLLM(active)
	dspycore.GlobalConfig.TeacherLLM = active

	return func() {
		defer dspyGlobalMu.Unlock()
		dspycore.SetDefaultLLM(prevDefault)
		dspycore.GlobalConfig.TeacherLLM = prevTeacher
	}, nil
}

func newDSPyGoOpenAICompatibleLLM(target PromptOptimizerLLMTarget) (*dspyllms.OpenAILLM, error) {
	return dspyllms.NewOpenAICompatible(
		target.Provider,
		dspycore.ModelID(target.Model),
		normalizeDSPyGoBaseURL(target.BaseURL),
		dspyllms.WithAPIKey(strings.TrimSpace(target.APIKey)),
	)
}

func normalizeDSPyGoBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return strings.TrimSuffix(baseURL, "/v1")
	}
	return baseURL
}

func (p *PromptOptimizer) buildDSPyGoExamples(agentRole, basePrompt string) []dspycore.Example {
	limit := max(4, min(12, p.config.BreadthCandidates*2))
	examples := make([]dspycore.Example, 0, limit)
	targetProfile := strings.TrimSpace(p.config.TargetProfile)
	if targetProfile == "" {
		targetProfile = "generic"
	}

	for _, example := range p.preferenceExamples {
		if strings.TrimSpace(example.Chosen.AgentRole) != "" && !strings.EqualFold(strings.TrimSpace(example.Chosen.AgentRole), strings.TrimSpace(agentRole)) {
			continue
		}
		if len(examples) >= limit {
			break
		}
		examples = append(examples, dspycore.Example{
			Inputs: map[string]interface{}{
				"question": example.Input.Question,
				"context":  example.Input.Context,
				"target":   targetProfile,
			},
			Outputs: map[string]interface{}{
				"question":        example.Input.Question,
				"context":         example.Input.Context,
				"target_response": example.Input.TargetResponse,
				"hint":            "prefer instructions closer to chosen prompts and farther from rejected prompts",
			},
		})
	}

	for _, example := range p.transcriptExamples {
		if len(examples) >= limit {
			break
		}
		examples = append(examples, dspycore.Example{
			Inputs: map[string]interface{}{
				"user_request": example.Input.UserRequest,
				"target":       targetProfile,
			},
			Outputs: map[string]interface{}{
				"question":        example.Input.UserRequest,
				"context":         example.Input.Context,
				"target_response": example.Output.Response,
				"hint":            "preserve user intent and avoid orchestration drift",
			},
		})
	}

	if len(examples) == 0 {
		examples = append(examples, dspycore.Example{
			Inputs: map[string]interface{}{
				"role":   agentRole,
				"prompt": basePrompt,
				"target": targetProfile,
			},
			Outputs: map[string]interface{}{
				"question":        "",
				"context":         "",
				"target_response": "",
				"hint":            "improve prompt quality while staying faithful to the task and target runtime profile",
			},
		})
	}

	return examples
}

type dspyGoExampleDataset struct {
	examples []dspycore.Example
	index    int
}

func (d *dspyGoExampleDataset) Next() (dspycore.Example, bool) {
	if d.index >= len(d.examples) {
		return dspycore.Example{}, false
	}
	example := d.examples[d.index]
	d.index++
	return example, true
}

func (d *dspyGoExampleDataset) Reset() {
	d.index = 0
}

func dspyGoPromptProgram(prompt string) dspycore.Program {
	module := &dspyGoPromptModule{
		BaseModule: *dspycore.NewModule(
			dspycore.NewSignature(
				[]dspycore.InputField{{Field: dspycore.NewField("task")}},
				[]dspycore.OutputField{{Field: dspycore.NewField("prompt")}},
			).WithInstruction(strings.TrimSpace(prompt)),
		),
	}
	module.ModuleType = "PromptCarrier"
	module.DisplayName = "prompt_optimizer"

	return dspycore.NewProgramWithForwardFactory(
		map[string]dspycore.Module{"prompt_optimizer": module},
		func(modules map[string]dspycore.Module) func(context.Context, map[string]interface{}) (map[string]interface{}, error) {
			return func(ctx context.Context, inputs map[string]interface{}) (map[string]interface{}, error) {
				return modules["prompt_optimizer"].Process(ctx, inputs)
			}
		},
	)
}

func dspyGoProgramInstruction(program dspycore.Program) string {
	if module, ok := program.Modules["prompt_optimizer"]; ok && module != nil {
		return strings.TrimSpace(module.GetSignature().Instruction)
	}
	keys := make([]string, 0, len(program.Modules))
	for key := range program.Modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		module := program.Modules[key]
		if module == nil {
			continue
		}
		if instruction := strings.TrimSpace(module.GetSignature().Instruction); instruction != "" {
			return instruction
		}
	}
	return ""
}

type dspyGoPromptModule struct {
	dspycore.BaseModule
}

func (m *dspyGoPromptModule) Process(ctx context.Context, inputs map[string]any, opts ...dspycore.Option) (map[string]any, error) {
	return map[string]any{
		"prompt": m.GetSignature().Instruction,
	}, nil
}

func (m *dspyGoPromptModule) Clone() dspycore.Module {
	return &dspyGoPromptModule{BaseModule: *m.BaseModule.Clone().(*dspycore.BaseModule)}
}

func (m *dspyGoPromptModule) GetModuleType() string {
	return "PromptCarrier"
}

func (m *dspyGoPromptModule) GetDisplayName() string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	return "prompt_optimizer"
}

type dspyGoFailoverLLM struct {
	primary  dspycore.LLM
	fallback dspycore.LLM
}

func (l *dspyGoFailoverLLM) Generate(ctx context.Context, prompt string, options ...dspycore.GenerateOption) (*dspycore.LLMResponse, error) {
	resp, err := l.primary.Generate(ctx, prompt, options...)
	if err == nil || l.fallback == nil {
		return resp, err
	}
	if ctx != nil && ctx.Err() != nil {
		return resp, err
	}
	return l.fallback.Generate(ctx, prompt, options...)
}

func (l *dspyGoFailoverLLM) GenerateWithJSON(ctx context.Context, prompt string, options ...dspycore.GenerateOption) (map[string]interface{}, error) {
	resp, err := l.primary.GenerateWithJSON(ctx, prompt, options...)
	if err == nil || l.fallback == nil {
		return resp, err
	}
	if ctx != nil && ctx.Err() != nil {
		return resp, err
	}
	return l.fallback.GenerateWithJSON(ctx, prompt, options...)
}

func (l *dspyGoFailoverLLM) GenerateWithFunctions(ctx context.Context, prompt string, functions []map[string]interface{}, options ...dspycore.GenerateOption) (map[string]interface{}, error) {
	resp, err := l.primary.GenerateWithFunctions(ctx, prompt, functions, options...)
	if err == nil || l.fallback == nil {
		return resp, err
	}
	if ctx != nil && ctx.Err() != nil {
		return resp, err
	}
	return l.fallback.GenerateWithFunctions(ctx, prompt, functions, options...)
}

func (l *dspyGoFailoverLLM) CreateEmbedding(ctx context.Context, input string, options ...dspycore.EmbeddingOption) (*dspycore.EmbeddingResult, error) {
	resp, err := l.primary.CreateEmbedding(ctx, input, options...)
	if err == nil || l.fallback == nil {
		return resp, err
	}
	if ctx != nil && ctx.Err() != nil {
		return resp, err
	}
	return l.fallback.CreateEmbedding(ctx, input, options...)
}

func (l *dspyGoFailoverLLM) CreateEmbeddings(ctx context.Context, inputs []string, options ...dspycore.EmbeddingOption) (*dspycore.BatchEmbeddingResult, error) {
	resp, err := l.primary.CreateEmbeddings(ctx, inputs, options...)
	if err == nil || l.fallback == nil {
		return resp, err
	}
	if ctx != nil && ctx.Err() != nil {
		return resp, err
	}
	return l.fallback.CreateEmbeddings(ctx, inputs, options...)
}

func (l *dspyGoFailoverLLM) StreamGenerate(ctx context.Context, prompt string, options ...dspycore.GenerateOption) (*dspycore.StreamResponse, error) {
	resp, err := l.primary.StreamGenerate(ctx, prompt, options...)
	if err == nil || l.fallback == nil {
		return resp, err
	}
	if ctx != nil && ctx.Err() != nil {
		return resp, err
	}
	return l.fallback.StreamGenerate(ctx, prompt, options...)
}

func (l *dspyGoFailoverLLM) GenerateWithContent(ctx context.Context, content []dspycore.ContentBlock, options ...dspycore.GenerateOption) (*dspycore.LLMResponse, error) {
	resp, err := l.primary.GenerateWithContent(ctx, content, options...)
	if err == nil || l.fallback == nil {
		return resp, err
	}
	if ctx != nil && ctx.Err() != nil {
		return resp, err
	}
	return l.fallback.GenerateWithContent(ctx, content, options...)
}

func (l *dspyGoFailoverLLM) StreamGenerateWithContent(ctx context.Context, content []dspycore.ContentBlock, options ...dspycore.GenerateOption) (*dspycore.StreamResponse, error) {
	resp, err := l.primary.StreamGenerateWithContent(ctx, content, options...)
	if err == nil || l.fallback == nil {
		return resp, err
	}
	if ctx != nil && ctx.Err() != nil {
		return resp, err
	}
	return l.fallback.StreamGenerateWithContent(ctx, content, options...)
}

func (l *dspyGoFailoverLLM) ProviderName() string {
	return l.primary.ProviderName()
}

func (l *dspyGoFailoverLLM) ModelID() string {
	return l.primary.ModelID()
}

func (l *dspyGoFailoverLLM) Capabilities() []dspycore.Capability {
	return l.primary.Capabilities()
}
