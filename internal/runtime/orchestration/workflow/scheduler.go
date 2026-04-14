package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Scheduler executes workflow steps respecting dependencies and parallelism.
type Scheduler struct {
	dag        *DAG
	executor   StepExecutor
	template   *TemplateEngine
	maxWorkers int
}

// StepExecutor defines how individual steps are executed.
type StepExecutor interface {
	Execute(ctx context.Context, step *Step, input map[string]any) (*StepResult, error)
}

// SchedulerOption configures a Scheduler.
type SchedulerOption func(*Scheduler)

// WithMaxWorkers sets the maximum parallel workers.
func WithMaxWorkers(n int) SchedulerOption {
	return func(s *Scheduler) {
		if n > 0 {
			s.maxWorkers = n
		}
	}
}

// NewScheduler creates a scheduler for the given workflow.
func NewScheduler(dag *DAG, executor StepExecutor, opts ...SchedulerOption) *Scheduler {
	s := &Scheduler{
		dag:        dag,
		executor:   executor,
		template:   NewTemplateEngine(),
		maxWorkers: 10, // Default
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Run executes the workflow and returns results.
func (s *Scheduler) Run(ctx context.Context, execCtx *ExecutionContext) (*WorkflowResult, error) {
	result := &WorkflowResult{
		Steps:     make(map[string]StepResult),
		StartTime: time.Now(),
		Status:    StepRunning,
	}

	// Track completed steps
	completed := make(map[string]bool)
	var completedMu sync.Mutex

	// Process batches in order
	for _, batch := range s.dag.Batches() {
		if err := s.executeBatch(ctx, batch, execCtx, completed, &completedMu, result); err != nil {
			result.Status = StepFailed
			result.Error = err.Error()
			result.EndTime = time.Now()
			result.DurationMs = result.EndTime.Sub(result.StartTime).Milliseconds()
			return result, err
		}
	}

	result.Status = StepCompleted
	result.EndTime = time.Now()
	result.DurationMs = result.EndTime.Sub(result.StartTime).Milliseconds()
	return result, nil
}

// executeBatch runs a batch of steps that can execute in parallel.
func (s *Scheduler) executeBatch(
	ctx context.Context,
	stepIDs []string,
	execCtx *ExecutionContext,
	completed map[string]bool,
	completedMu *sync.Mutex,
	result *WorkflowResult,
) error {
	// Filter out already completed steps
	var toRun []string
	for _, id := range stepIDs {
		completedMu.Lock()
		done := completed[id]
		completedMu.Unlock()
		if !done {
			toRun = append(toRun, id)
		}
	}

	if len(toRun) == 0 {
		return nil
	}

	// Limit parallelism
	workers := s.maxWorkers
	if len(toRun) < workers {
		workers = len(toRun)
	}

	// Create worker pool
	stepChan := make(chan string, len(toRun))
	resultChan := make(chan stepExecutionResult, len(toRun))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for stepID := range stepChan {
				execResult := s.executeStep(ctx, stepID, execCtx)
				resultChan <- execResult
			}
		}()
	}

	// Queue steps
	for _, id := range toRun {
		stepChan <- id
	}
	close(stepChan)

	// Wait for completion
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var firstErr error
	for execResult := range resultChan {
		completedMu.Lock()
		completed[execResult.stepID] = true
		result.Steps[execResult.stepID] = *execResult.result
		execCtx.Set(execResult.stepID, execResult.result)
		completedMu.Unlock()

		if execResult.err != nil && firstErr == nil {
			firstErr = execResult.err
		}
	}

	return firstErr
}

// stepExecutionResult holds the result of a step execution.
type stepExecutionResult struct {
	stepID string
	result *StepResult
	err    error
}

// executeStep runs a single step, handling conditions and loops.
func (s *Scheduler) executeStep(ctx context.Context, stepID string, execCtx *ExecutionContext) stepExecutionResult {
	step := s.dag.Step(stepID)
	if step == nil {
		return stepExecutionResult{
			stepID: stepID,
			result: &StepResult{
				StepID: stepID,
				Status: StepFailed,
				Error:  fmt.Sprintf("step not found: %s", stepID),
			},
			err: fmt.Errorf("step not found: %s", stepID),
		}
	}

	execCtx.Current = stepID

	// Evaluate condition
	if step.If != "" {
		shouldRun, err := s.template.EvaluateCondition(step.If, execCtx)
		if err != nil {
			return stepExecutionResult{
				stepID: stepID,
				result: &StepResult{
					StepID:    stepID,
					Status:    StepFailed,
					Error:     fmt.Sprintf("condition evaluation failed: %v", err),
					StartTime: time.Now(),
					EndTime:   time.Now(),
				},
				err: err,
			}
		}
		if !shouldRun {
			return stepExecutionResult{
				stepID: stepID,
				result: &StepResult{
					StepID:    stepID,
					Status:    StepSkipped,
					StartTime: time.Now(),
					EndTime:   time.Now(),
				},
			}
		}
	}

	// Handle loop
	if step.Loop != nil {
		return s.executeLoopStep(ctx, step, execCtx)
	}

	// Execute single step
	return s.executeSingleStep(ctx, step, execCtx)
}

// executeSingleStep executes a step without looping.
func (s *Scheduler) executeSingleStep(ctx context.Context, step *Step, execCtx *ExecutionContext) stepExecutionResult {
	startTime := time.Now()

	// Render input
	renderedInput, err := s.renderStepInput(step, execCtx)
	if err != nil {
		return stepExecutionResult{
			stepID: step.ID,
			result: &StepResult{
				StepID:    step.ID,
				Status:    StepFailed,
				Error:     fmt.Sprintf("input rendering failed: %v", err),
				StartTime: startTime,
				EndTime:   time.Now(),
			},
			err: err,
		}
	}

	// Execute with retries
	result, err := s.executeWithRetry(ctx, step, renderedInput)
	if err != nil {
		strategy := ParseErrorStrategy(step.OnError)
		if strategy == ErrorContinue {
			return stepExecutionResult{
				stepID: step.ID,
				result: &StepResult{
					StepID:     step.ID,
					Status:     StepFailed,
					Error:      err.Error(),
					StartTime:  startTime,
					EndTime:    time.Now(),
					DurationMs: time.Since(startTime).Milliseconds(),
				},
			}
		}
		return stepExecutionResult{
			stepID: step.ID,
			result: &StepResult{
				StepID:     step.ID,
				Status:     StepFailed,
				Error:      err.Error(),
				StartTime:  startTime,
				EndTime:    time.Now(),
				DurationMs: time.Since(startTime).Milliseconds(),
			},
			err: err,
		}
	}

	result.StartTime = startTime
	result.EndTime = time.Now()
	result.DurationMs = result.EndTime.Sub(startTime).Milliseconds()

	return stepExecutionResult{
		stepID: step.ID,
		result: result,
	}
}

// executeLoopStep executes a step for each item in a loop.
func (s *Scheduler) executeLoopStep(ctx context.Context, step *Step, execCtx *ExecutionContext) stepExecutionResult {
	startTime := time.Now()

	// Render the 'over' expression to get the array
	overValue, err := s.template.RenderString(step.Loop.Over, execCtx)
	if err != nil {
		return stepExecutionResult{
			stepID: step.ID,
			result: &StepResult{
				StepID:    step.ID,
				Status:    StepFailed,
				Error:     fmt.Sprintf("loop.over evaluation failed: %v", err),
				StartTime: startTime,
				EndTime:   time.Now(),
			},
			err: err,
		}
	}

	// Parse as array
	items, err := parseAsArray(overValue)
	if err != nil {
		return stepExecutionResult{
			stepID: step.ID,
			result: &StepResult{
				StepID:    step.ID,
				Status:    StepFailed,
				Error:     fmt.Sprintf("loop.over is not an array: %v", err),
				StartTime: startTime,
				EndTime:   time.Now(),
			},
			err: err,
		}
	}

	// Determine parallelism
	parallel := 1
	if step.Parallel != nil && *step.Parallel > 0 {
		parallel = *step.Parallel
	}

	// Execute iterations
	results := make([]StepResult, len(items))
	var firstErr error

	if parallel == 1 {
		// Sequential execution
		for i, item := range items {
			iterCtx := execCtx.Clone()
			iterCtx.SetVar(step.Loop.As, item)
			iterCtx.SetVar("index", i)

			iterResult := s.executeSingleStep(ctx, step, iterCtx)
			results[i] = *iterResult.result
			if iterResult.err != nil && firstErr == nil {
				firstErr = iterResult.err
			}
		}
	} else {
		// Parallel execution
		var wg sync.WaitGroup
		var mu sync.Mutex
		semaphore := make(chan struct{}, parallel)

		for i, item := range items {
			wg.Add(1)
			go func(idx int, itm any) {
				defer wg.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				iterCtx := execCtx.Clone()
				iterCtx.SetVar(step.Loop.As, itm)
				iterCtx.SetVar("index", idx)

				iterResult := s.executeSingleStep(ctx, step, iterCtx)

				mu.Lock()
				results[idx] = *iterResult.result
				if iterResult.err != nil && firstErr == nil {
					firstErr = iterResult.err
				}
				mu.Unlock()
			}(i, item)
		}
		wg.Wait()
	}

	// Aggregate results
	var allData []any
	status := StepCompleted
	for _, r := range results {
		allData = append(allData, r.Data)
		if r.Status == StepFailed {
			status = StepFailed
		}
	}

	result := &StepResult{
		StepID:     step.ID,
		Status:     status,
		Data:       allData,
		Iterations: results,
		StartTime:  startTime,
		EndTime:    time.Now(),
		DurationMs: time.Since(startTime).Milliseconds(),
	}

	if firstErr != nil {
		result.Error = firstErr.Error()
		strategy := ParseErrorStrategy(step.OnError)
		if strategy != ErrorContinue {
			return stepExecutionResult{stepID: step.ID, result: result, err: firstErr}
		}
	}

	return stepExecutionResult{stepID: step.ID, result: result}
}

// renderStepInput renders the step's input with template substitution.
func (s *Scheduler) renderStepInput(step *Step, execCtx *ExecutionContext) (map[string]any, error) {
	if step.Input == nil {
		return map[string]any{}, nil
	}

	rendered, err := s.template.Render(step.Input, execCtx)
	if err != nil {
		return nil, err
	}

	result, ok := rendered.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("rendered input is not a map")
	}

	return result, nil
}

// executeWithRetry executes a step with retry logic.
func (s *Scheduler) executeWithRetry(ctx context.Context, step *Step, input map[string]any) (*StepResult, error) {
	maxRetries := step.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}

	var retryDelay time.Duration
	if step.RetryDelay != "" {
		if d, err := time.ParseDuration(step.RetryDelay); err == nil {
			retryDelay = d
		}
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		result, err := s.executor.Execute(ctx, step, input)
		if err == nil {
			result.RetryCount = attempt
			return result, nil
		}

		lastErr = err

		// Only retry if strategy is retry and more attempts remain
		if ParseErrorStrategy(step.OnError) != ErrorRetry || attempt+1 >= maxRetries {
			break
		}

		// Wait before retry
		if retryDelay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryDelay):
			}
		}
	}

	return nil, lastErr
}

// parseAsArray attempts to parse a value as an array.
func parseAsArray(v any) ([]any, error) {
	switch val := v.(type) {
	case []any:
		return val, nil
	case []string:
		result := make([]any, len(val))
		for i, s := range val {
			result[i] = s
		}
		return result, nil
	case string:
		// Try to parse as JSON array
		var arr []any
		if err := jsonUnmarshal([]byte(val), &arr); err == nil {
			return arr, nil
		}
		return nil, fmt.Errorf("cannot parse string as array")
	default:
		return nil, fmt.Errorf("value is not an array: %T", v)
	}
}

// jsonUnmarshal is a simple JSON unmarshal helper.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
