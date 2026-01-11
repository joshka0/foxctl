package runservice

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/execution"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/protocol"
)

// ExecuteEphemeral runs a skill without job persistence.
// This provides faster execution for hooks and other transient operations
// where job history is not needed.
//
// Cache behavior:
//   - Cache reads are still performed (for deduplication)
//   - Cache writes are skipped (ephemeral results not persisted)
//   - Trajectory capture is skipped
//   - Job store is never opened
func (e *Executor) ExecuteEphemeral(input []byte) error {
	// Step 1: Try cache for deduplication (read-only)
	if done, err := e.TryServeCache(input); err != nil {
		return err
	} else if done {
		return nil
	}

	// Step 2: Execute skill directly without job infrastructure
	skillExecutor := execution.NewRunnerExecutor()
	start := time.Now()

	// Ensure trace exists for propagation (skill binary reads AGENTCTL_TRACE_ID from env).
	// If correlation_id is set, use it as trace_id for cross-layer correlation.
	ctx := e.ctx
	if e.options.CorrelationID != "" {
		ctx = observability.WithTraceID(ctx, e.options.CorrelationID)
	}
	ctx, _ = observability.EnsureTraceID(ctx)

	// Add workspace to context so skill runners can read it via workspace.FromContext
	if e.options.Workspace != "" {
		ctx = workspace.WithContext(ctx, e.options.Workspace)
	}

	// Build extra env vars for this execution (avoids race condition with os.Setenv)
	var extraEnv []string

	// Propagate trace context to the child process so skill-side spans correlate.
	extraEnv = append(extraEnv, observability.PropagationEnv(ctx)...)

	if e.options.NoCAS {
		extraEnv = append(extraEnv, "AGENTCTL_NO_CAS=1")
	}

	result, err := skillExecutor.Execute(ctx, execution.ExecuteOptions{
		Manifest:     e.handle.Manifest,
		ArtifactPath: e.handle.ArtifactPath,
		Input:        input,
		ExtraEnv:     extraEnv,
	})
	if err != nil {
		return fmt.Errorf("ephemeral execution failed: %w", err)
	}

	duration := time.Since(start)
	_ = duration // Available for metrics/logging if needed

	// Step 3: Handle result
	if result.Error != nil {
		return e.handleEphemeralError(result)
	}

	return e.handleEphemeralSuccess(result.Stdout)
}

// handleEphemeralError processes a failed ephemeral execution.
func (e *Executor) handleEphemeralError(result *execution.Result) error {
	// Check if stdout contains an error envelope
	if len(result.Stdout) > 0 {
		var env envelope.Envelope
		if json.Unmarshal(result.Stdout, &env) == nil && env.Status == "error" {
			// Skill returned an error envelope - output it and return nil
			// The envelope already contains the error details
			if err := writeEnvelope(e.stdout, result.Stdout); err != nil {
				return err
			}
			return nil
		}
	}

	// Build error envelope from stderr/error
	errMsg := result.Error.Error()
	if len(result.Stderr) > 0 {
		errMsg = fmt.Sprintf("%s: %s", errMsg, string(result.Stderr))
	}

	env := protocol.Error(
		e.handle.Manifest.Metadata.Name,
		protocol.ErrorCodeERuntime,
		errMsg,
		nil,
		protocol.WithWorkspace(e.options.Workspace),
		protocol.WithSkillVersion(e.handle.Manifest.Metadata.Version),
	)
	env.Meta.CorrelID = e.options.CorrelationID
	env.Meta.Source = "ephemeral"

	if err := protocol.Write(e.stdout, env); err != nil {
		return fmt.Errorf("write ephemeral error envelope: %w", err)
	}
	// Return nil since we already wrote the error envelope.
	// Returning result.Error would cause double error reporting.
	return nil
}

// handleEphemeralSuccess processes a successful ephemeral execution.
func (e *Executor) handleEphemeralSuccess(stdout []byte) error {
	// Validate output is valid envelope
	var resultEnv envelope.Envelope
	if err := json.Unmarshal(stdout, &resultEnv); err != nil {
		return fmt.Errorf("invalid result envelope: %w", err)
	}
	if err := envelope.Validate(resultEnv); err != nil {
		return fmt.Errorf("envelope validation failed: %w", err)
	}

	// Annotate with ephemeral metadata
	annotated := annotateCorrelationAndJob(stdout, "", e.options.CorrelationID)
	annotated = annotateEphemeral(annotated)

	// Write output
	if err := writeEnvelope(e.stdout, annotated); err != nil {
		return err
	}

	// Note: We intentionally skip cache writes in ephemeral mode
	// This provides the -60% latency improvement by avoiding:
	// - Job store open/close (~30ms)
	// - Job preparation (~50ms)
	// - Job state updates (~40ms)
	// - Cache writes (~30ms)

	return nil
}

// annotateEphemeral adds ephemeral execution metadata to the envelope.
func annotateEphemeral(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	var env map[string]any
	if err := json.Unmarshal(data, &env); err != nil {
		return data
	}

	meta, ok := env["meta"].(map[string]any)
	if !ok {
		meta = make(map[string]any)
		env["meta"] = meta
	}
	meta["source"] = "ephemeral"
	meta["ephemeral"] = true

	result, err := json.Marshal(env)
	if err != nil {
		return data
	}
	return result
}

// IsEphemeral returns true if ephemeral mode is enabled.
func (e *Executor) IsEphemeral() bool {
	return e.options.Ephemeral
}

// Close releases any resources held by the executor.
// For ephemeral execution, this is typically a no-op since we don't open
// job stores, cache stores, or trajectory capture.
func (e *Executor) CloseEphemeral() {
	// Note: Caching is disabled, so no cache store cleanup needed.
	// We don't close jobStore or trajCapture in ephemeral mode
	// because they were never opened.
}
