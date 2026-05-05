//nolint:forbidigo // log.Printf used for flow debug logging; TODO: migrate to observability.Emit
package flow

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// ---------------------------------------------------------------------------
// Evaluator: per-edge goroutine
// ---------------------------------------------------------------------------

// evaluatorConfig holds the configuration for a single edge evaluator.
type evaluatorConfig struct {
	edge         FlowEdge
	sourceNodeID string
	targetNodeID string
	bus          *OutputBus
	executor     NodeExecutor
	targetNode   FlowNode
	condition    Condition
	pauseCh      chan struct{}       // signal to pause
	resumeCh     chan struct{}       // signal to resume
	onDeliver    func(edgeID string) // called after successful delivery (for state tracking)
}

// startEvaluator starts a goroutine that subscribes to the source node's
// output, checks the trigger, applies the transform, evaluates the condition,
// and delivers the result to the target executor.
func startEvaluator(ctx context.Context, cfg evaluatorConfig) {
	sub := cfg.bus.subscribe(cfg.sourceNodeID)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-cfg.pauseCh:
				// Wait for resume or cancellation
				select {
				case <-ctx.Done():
					return
				case <-cfg.resumeCh:
					// Resumed, continue processing
					continue
				}
			case out, ok := <-sub:
				if !ok {
					return
				}

				// Check trigger (for M2, we only support output_ready)
				if cfg.edge.Trigger != TriggerOutputReady {
					continue
				}

				// Apply transform.
				transformed, err := ApplyTransform(ctx, cfg.edge.Transform, cfg.edge.TransformConfig, out.Envelope.Data)
				if err != nil {
					// Transform error: produce error envelope and deliver to target
					errOut := NodeOutput{
						Envelope: envelope.Error("flow/transform", "EPARSE",
							fmt.Sprintf("transform %q failed: %v", cfg.edge.Transform, err), nil),
						NodeID:   cfg.sourceNodeID,
						Duration: out.Duration,
					}
					cfg.executeTarget(ctx, errOut)
					continue
				}

				// Build the output to evaluate conditions against.
				evalOut := NodeOutput{
					Envelope: envelope.OK("flow/edge", transformed),
					NodeID:   cfg.sourceNodeID,
					Duration: out.Duration,
				}
				// Preserve original status for condition evaluation.
				evalOut.Envelope.Status = out.Envelope.Status

				// Evaluate condition.
				if cfg.condition != nil && !cfg.condition.Eval(evalOut) {
					// Condition filtered this output out.
					continue
				}

				// Execute target node with transformed data.
				cfg.executeTarget(ctx, evalOut)
			}
		}
	}()
}

// executeTarget runs the target executor and publishes its output.
// If the edge has a RetryPolicy with MaxAttempts > 0, the executor is retried
// on failure up to MaxAttempts total attempts with delay_ms between retries.
func (cfg evaluatorConfig) executeTarget(ctx context.Context, input NodeOutput) {
	// Recover from panics in the executor.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flow: panic in executor for node %s: %v", cfg.targetNodeID, r)
			errOut := NodeOutput{
				Envelope: envelope.Error("flow/engine", "ERUNTIME",
					fmt.Sprintf("panic in node %s: %v", cfg.targetNodeID, r), nil),
				NodeID:   cfg.targetNodeID,
				Duration: 0,
			}
			cfg.bus.publish(cfg.targetNodeID, errOut)
		}
	}()

	var inputData any
	if input.Envelope.Data != nil {
		inputData = input.Envelope.Data
	}

	// Determine max attempts from retry policy.
	maxAttempts := 1
	if cfg.edge.RetryPolicy != nil && cfg.edge.RetryPolicy.MaxAttempts > 0 {
		maxAttempts = cfg.edge.RetryPolicy.MaxAttempts
	}

	var result NodeOutput
	var err error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Apply delay before retry (not before the first attempt).
		if attempt > 0 && cfg.edge.RetryPolicy != nil && cfg.edge.RetryPolicy.DelayMS > 0 {
			delay := time.Duration(cfg.edge.RetryPolicy.DelayMS) * time.Millisecond
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}

		result, err = cfg.executor.Execute(ctx, cfg.targetNode, inputData)
		if err == nil {
			// Success — break out of retry loop.
			break
		}

		// Log the retry attempt.
		if attempt < maxAttempts-1 {
			log.Printf("flow: edge %s: attempt %d/%d failed for node %s: %v (retrying)",
				cfg.edge.ID, attempt+1, maxAttempts, cfg.targetNodeID, err)
		} else {
			log.Printf("flow: edge %s: attempt %d/%d failed for node %s: %v (retries exhausted)",
				cfg.edge.ID, attempt+1, maxAttempts, cfg.targetNodeID, err)
		}
	}

	if err != nil {
		// All retries exhausted or no retry policy.
		result = NodeOutput{
			Envelope: envelope.Error("flow/engine", "ERUNTIME",
				fmt.Sprintf("node %s execution failed after %d attempts: %v", cfg.targetNodeID, maxAttempts, err), nil),
			NodeID:   cfg.targetNodeID,
			Duration: 0,
		}
	}

	cfg.bus.publish(cfg.targetNodeID, result)

	// Notify state tracker of successful delivery.
	if cfg.onDeliver != nil {
		cfg.onDeliver(cfg.edge.ID)
	}
}

// ---------------------------------------------------------------------------
// Source executor helper
// ---------------------------------------------------------------------------

// executeSourceWithResult runs a source node executor (nil input), publishes
// its output, and returns the result for state tracking.
func executeSourceWithResult(ctx context.Context, executor NodeExecutor, node FlowNode, bus *OutputBus) (result NodeOutput) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("flow: panic in source executor for node %s: %v", node.ID, r)
			result = NodeOutput{
				Envelope: envelope.Error("flow/engine", "ERUNTIME",
					fmt.Sprintf("panic in source node %s: %v", node.ID, r), nil),
				NodeID:   node.ID,
				Duration: 0,
			}
			bus.publish(node.ID, result)
		}
	}()

	var err error
	result, err = executor.Execute(ctx, node, nil)
	if err != nil {
		result = NodeOutput{
			Envelope: envelope.Error("flow/engine", "ERUNTIME",
				fmt.Sprintf("source node %s failed: %v", node.ID, err), nil),
			NodeID:   node.ID,
			Duration: 0,
		}
	}

	bus.publish(node.ID, result)
	return result
}
