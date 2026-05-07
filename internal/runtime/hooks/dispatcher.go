package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/hooks/pathutil"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// Dispatcher executes hooks for events and merges their outputs.
type Dispatcher interface {
	// Dispatch runs all hooks for the given event and input.
	// Returns the merged output from all hooks.
	// If no hooks match, returns a default approve output.
	Dispatch(ctx context.Context, input Input) (Result, error)

	// DispatchAsync runs hooks in parallel (future optimization).
	// For v1, this is sequential.
	DispatchAsync(ctx context.Context, input Input) <-chan Result
}

// Result contains the merged output from all hooks plus metadata.
type Result struct {
	// Merged output from all hooks
	Output Output

	// HooksRun is the list of hook IDs that were executed
	HooksRun []string

	// HookResults contains individual results for debugging
	HookResults []HookResult

	// Blocked is true if any hook blocked
	Blocked bool

	// BlockedBy is the hook ID that blocked (first one)
	BlockedBy string

	// Duration is the total time spent in hook dispatch
	Duration time.Duration
}

// HookResult is the result from a single hook execution.
type HookResult struct {
	HookID   string        `json:"hook_id"`
	Skill    string        `json:"skill,omitempty"`
	Output   Output        `json:"output"`
	Duration time.Duration `json:"duration_ms"`
	Error    error         `json:"error,omitempty"`
	FailOpen bool          `json:"fail_open"` // true if error was handled as fail-open
}

// HookRunner executes a single hook and returns its output.
// This interface allows different execution strategies (shell, skill, in-process).
type HookRunner interface {
	// Run executes the hook with the given input.
	// Returns the hook output or an error.
	// If the hook times out, returns context.DeadlineExceeded.
	Run(ctx context.Context, hookDef HookDef, input Input) (Output, error)
}

// HookDef is a single hook definition from configuration.
type HookDef struct {
	ID       string         `yaml:"id" json:"id"`
	Enabled  bool           `yaml:"enabled" json:"enabled"`
	Event    Event          `yaml:"event" json:"event"`
	Priority int            `yaml:"priority,omitempty" json:"priority,omitempty"`
	Match    *HookMatcher   `yaml:"match,omitempty" json:"match,omitempty"`
	Run      []HookRunEntry `yaml:"run" json:"run"`
}

// HookMatcher defines optional matchers for a hook.
// All specified fields must match for the hook to run (AND logic).
// For OR logic, define separate hook entries.
type HookMatcher struct {
	// Actor/session matching
	ActorID     string `yaml:"actor_id,omitempty" json:"actor_id,omitempty"`         // regex
	MessageType string `yaml:"message_type,omitempty" json:"message_type,omitempty"` // regex
	Namespace   string `yaml:"namespace,omitempty" json:"namespace,omitempty"`       // regex on to_ns/from_ns

	// Tool matching (for PreToolUse/PostToolUse events)
	ToolName      string   `yaml:"tool_name,omitempty" json:"tool_name,omitempty"`           // regex on platform tool name (Edit, Write, Read, etc.)
	ToolCanonical string   `yaml:"tool_canonical,omitempty" json:"tool_canonical,omitempty"` // regex on canonical tool name (edit.apply_patch, fs.read, etc.)
	ToolKind      ToolKind `yaml:"tool_kind,omitempty" json:"tool_kind,omitempty"`           // read|write|exec|search|any

	// Content matching
	PromptRegex string `yaml:"prompt_regex,omitempty" json:"prompt_regex,omitempty"` // regex over input.prompt
	PathRegex   string `yaml:"path_regex,omitempty" json:"path_regex,omitempty"`     // regex over file_path extracted from tool_input
}

// HookRunEntry is a single skill to run within a hook.
type HookRunEntry struct {
	Skill     string         `yaml:"skill" json:"skill"`
	TimeoutMS int            `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	FailOpen  bool           `yaml:"fail_open" json:"fail_open"`
	Ephemeral *bool          `yaml:"ephemeral,omitempty" json:"ephemeral,omitempty"`
	Config    map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

// DefaultTimeout is the default hook execution timeout.
const DefaultTimeout = 2000 * time.Millisecond

// dispatcher is the default implementation of Dispatcher.
type dispatcher struct {
	config *Config
	runner HookRunner
}

// NewDispatcher creates a new hook dispatcher with the given config and runner.
// NewDispatcher builds a hook dispatcher with the provided config and runner.
//
// Index:
//
//	Purpose: Initialize a dispatcher for hook execution
//	Keywords: hook_dispatcher, hook_config, hook_runner, dispatch
//	Related: dispatcher.Dispatch, NewDispatcherWithRegistry
//	Flow: store config/runner → return dispatcher
//	Resources: hooks.Config, hooks.HookRunner
//	Events: none
//	OutputFields: Dispatcher
func NewDispatcher(cfg *Config, runner HookRunner) Dispatcher {
	return &dispatcher{
		config: cfg,
		runner: runner,
	}
}

// Dispatch runs all matching hooks for the event and returns the merged result.
// Dispatch runs matching hooks and merges their outputs.
//
// Index:
//
//	Purpose: Execute matching hooks and merge outputs into a decision
//	Keywords: hook_dispatch, hook_execute, timeout_ms, fail_open, OpHookExecute
//	Related: MatchesInput, Merge, HookRunner.Run
//	Flow: resolve hooks → filter matchers → run hooks with timeouts → emit events → merge outputs
//	Resources: hooks.Config, observability
//	Events: OpHookExecute
//	OutputFields: Result
//
// [[invariant:hook-timeout-enforced]]
// [[protocol:hook-dispatch]]
func (d *dispatcher) Dispatch(ctx context.Context, input Input) (Result, error) {
	start := time.Now()
	result := Result{
		Output: NewApprove("no hooks matched", nil),
	}

	// Find matching hooks
	hooks := d.config.HooksForEvent(input.Event)
	if len(hooks) == 0 {
		result.Duration = time.Since(start)
		return result, nil
	}

	// Filter by matchers
	var matchingHooks []HookDef
	for _, h := range hooks {
		if h.Enabled && MatchesInput(h, input) {
			matchingHooks = append(matchingHooks, h)
		}
	}

	if len(matchingHooks) == 0 {
		result.Duration = time.Since(start)
		return result, nil
	}

	// Execute hooks in order and collect outputs
	var outputs []Output
	for _, hook := range matchingHooks {
		result.HooksRun = append(result.HooksRun, hook.ID)

		for _, entry := range hook.Run {
			entryHook := hook
			entryHook.Run = []HookRunEntry{entry}

			// Create input with hook config
			hookInput := input
			hookInput.HookConfig = entry.Config

			// Set timeout
			timeout := time.Duration(entry.TimeoutMS) * time.Millisecond
			if timeout == 0 {
				timeout = DefaultTimeout
			}

			hookCtx, cancel := context.WithTimeout(ctx, timeout)
			hookStart := time.Now()

			output, err := d.runner.Run(hookCtx, entryHook, hookInput)
			cancel()

			hookResult := HookResult{
				HookID:   hook.ID,
				Skill:    entry.Skill,
				Output:   output,
				Duration: time.Since(hookStart),
				Error:    err,
			}

			if err != nil {
				if entry.FailOpen {
					// Fail open: treat as no-op
					hookResult.FailOpen = true
					hookResult.Output = NewNone()
					output = hookResult.Output
				} else {
					// Fail closed: treat as block with reason
					hookResult.Output = NewBlock(fmt.Sprintf("hook_failed:%s:%v", entry.Skill, err))
					output = hookResult.Output
				}
			}

			result.HookResults = append(result.HookResults, hookResult)
			outputs = append(outputs, output)

			// Emit observability event for hook execution
			hookEvent := observability.NewEvent(observability.OpHookExecute).
				WithComponent(observability.ComponentHook).
				WithSession(input.SessionID, input.ActorID).
				WithData("hook_id", hook.ID).
				WithData("hook_name", entry.Skill).
				WithData("event_type", string(input.Event)).
				WithData("tool_name", input.ToolName).
				WithData("blocked", output.Decision.IsBlocking()).
				WithData("fail_open", hookResult.FailOpen)
			if err != nil {
				observability.Emit(ctx, hookEvent.Error(err, hookResult.Duration))
			} else {
				observability.Emit(ctx, hookEvent.Success(hookResult.Duration))
			}

			// Check for early block (optimization)
			if output.Decision.IsBlocking() && !result.Blocked {
				result.Blocked = true
				result.BlockedBy = hook.ID
			}
		}
	}

	// Merge all outputs
	result.Output = Merge(outputs)
	result.Blocked = result.Output.Decision.IsBlocking()
	result.Duration = time.Since(start)

	return result, nil
}

// DispatchAsync runs hooks and returns a channel for the result.
// For v1, this is a simple wrapper around Dispatch.
func (d *dispatcher) DispatchAsync(ctx context.Context, input Input) <-chan Result {
	ch := make(chan Result, 1)
	go func() {
		result, err := d.Dispatch(ctx, input)
		if err != nil {
			result.Output = NewBlock(fmt.Sprintf("dispatch error: %v", err))
			result.Blocked = true
		}
		ch <- result
		close(ch)
	}()
	return ch
}

// MatchesInput checks if a hook's matchers match the input.
func MatchesInput(hook HookDef, input Input) bool {
	if hook.Match == nil {
		return true // No matchers = always match
	}

	m := hook.Match

	// Actor ID match (regex)
	if m.ActorID != "" {
		if !matchRegex(m.ActorID, input.ActorID) {
			return false
		}
	}

	// Tool name match (regex) - for PreToolUse/PostToolUse
	if m.ToolName != "" {
		if !matchRegex(m.ToolName, input.ToolName) {
			return false
		}
	}

	// Tool canonical match (regex) - for canonical tool names (edit.apply_patch, etc.)
	if m.ToolCanonical != "" {
		// Try tool_canonical first, fall back to tool_name if it looks canonical (contains a dot)
		canonical := input.ToolCanonical
		if canonical == "" && len(input.ToolName) > 0 {
			// If tool_name contains a dot, treat it as canonical
			for i := 0; i < len(input.ToolName); i++ {
				if input.ToolName[i] == '.' {
					canonical = input.ToolName
					break
				}
			}
		}
		if !matchRegex(m.ToolCanonical, canonical) {
			return false
		}
	}

	// Tool kind match (exact) - category-based matching
	if m.ToolKind != "" && m.ToolKind != ToolKindAny {
		// Compute tool kind if not provided in input
		inputKind := input.ToolKind
		if inputKind == "" {
			inputKind = ClassifyToolKind(input.ToolName, input.ToolCanonical)
		}
		// Match specific kind or allow "any"
		if inputKind != m.ToolKind && inputKind != ToolKindAny {
			return false
		}
	}

	// Message type match (regex) - for MessageReceived
	if m.MessageType != "" && input.MailboxMessage != nil {
		if !matchRegex(m.MessageType, input.MailboxMessage.Type) {
			return false
		}
	}

	// Namespace match (regex) - matches to_ns or from_ns
	if m.Namespace != "" && input.MailboxMessage != nil {
		if !matchRegex(m.Namespace, input.MailboxMessage.ToNS) &&
			!matchRegex(m.Namespace, input.MailboxMessage.FromNS) {
			return false
		}
	}

	// Prompt regex match
	if m.PromptRegex != "" {
		if !matchRegex(m.PromptRegex, input.Prompt) {
			return false
		}
	}

	// Path regex match - extract file_path from tool_input
	if m.PathRegex != "" {
		filePath := extractFilePath(input.ToolInput)
		if !matchRegex(m.PathRegex, filePath) {
			return false
		}
	}

	return true
}

// extractFilePath extracts the file path from tool input JSON.
// Tries common field names: file_path, path, file, current_path
func extractFilePath(toolInput json.RawMessage) string {
	return pathutil.ExtractPath(toolInput)
}

// matchRegex is a helper for regex matching.
// In v1, we use simple prefix/suffix matching for performance.
// Full regex support can be added if needed.
func matchRegex(pattern, value string) bool {
	if pattern == "" {
		return true
	}
	if value == "" {
		return false
	}

	// Simple implementation: treat as regex
	// For production, compile and cache patterns
	matched, err := regexpMatch(pattern, value)
	if err != nil {
		// Invalid pattern - treat as no match
		return false
	}
	return matched
}

// regexpCache caches compiled regex patterns for performance.
var regexpCache = struct {
	sync.RWMutex
	patterns map[string]*regexp.Regexp
}{
	patterns: make(map[string]*regexp.Regexp),
}

// regexpMatch performs regex matching with caching.
func regexpMatch(pattern, value string) (bool, error) {
	// Check cache first
	regexpCache.RLock()
	re, ok := regexpCache.patterns[pattern]
	regexpCache.RUnlock()

	if !ok {
		// Compile and cache
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return false, err
		}

		regexpCache.Lock()
		regexpCache.patterns[pattern] = re
		regexpCache.Unlock()
	}

	return re.MatchString(value), nil
}

// NoopRunner is a hook runner that always returns approve.
// Used for testing the dispatcher without actual hook execution.
type NoopRunner struct{}

func (NoopRunner) Run(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
	return NewApprove("noop", nil), nil
}

// FuncRunner wraps a function as a HookRunner.
type FuncRunner func(ctx context.Context, hookDef HookDef, input Input) (Output, error)

func (f FuncRunner) Run(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
	return f(ctx, hookDef, input)
}

// Envelope wraps hook output in the standard foxctl envelope format.
type Envelope struct {
	Version int             `json:"version"`
	Status  string          `json:"status"`
	Command string          `json:"command"`
	Data    EnvelopeData    `json:"data"`
	Meta    json.RawMessage `json:"meta,omitempty"`
}

// EnvelopeData contains the hook output in the data field.
type EnvelopeData struct {
	HookOutput Output `json:"hook_output"`
}

// ParseEnvelope extracts hook output from a standard envelope.
func ParseEnvelope(data []byte) (Output, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Output{}, fmt.Errorf("invalid envelope: %w", err)
	}

	if env.Status != "ok" {
		return Output{}, fmt.Errorf("hook returned error status: %s", env.Status)
	}

	return env.Data.HookOutput, nil
}
