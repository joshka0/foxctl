// Package hooks defines the hook contract and execution pipeline for agentctl events.
// It resolves hook skills, runs hook executors, and merges outputs into decisions.
//
// Hooks are skills invoked at canonical events during actor execution.
// They can observe, block, mutate, or enqueue actions.
//
// The types in this package are CANONICAL and STABLE for v1.
// Changes here affect all hook skills and the dispatcher.
package hooks
