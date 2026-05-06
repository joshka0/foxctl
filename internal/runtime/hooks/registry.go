package hooks

import (
	"context"
	"strings"

	"github.com/joshka0/foxctl/internal/runtime/execution"
)

// Registry manages hook runners and routes hooks to the appropriate execution strategy.
type Registry struct {
	config        *Config
	shellRunner   HookRunner
	skillRunner   HookRunner
	defaultRunner HookRunner
}

// RegistryOptions configures the registry.
type RegistryOptions struct {
	// SkillsDir is the root directory for installed skills.
	SkillsDir string

	// Executor is the skill executor. If nil, uses NewRunnerExecutor.
	Executor execution.SkillExecutor

	// Resolver is the skill resolver. If nil, uses DefaultResolver with SkillsDir.
	Resolver SkillResolver

	// ShellRunner overrides the default shell runner.
	ShellRunner HookRunner

	// SkillRunner overrides the default skill runner.
	SkillRunner HookRunner
}

// NewRegistry creates a new hook registry with the given config and options.
// NewRegistry configures hook runners and skill resolution.
//
// Index:
//   Purpose: Configure hook runners and skill resolution
//   Keywords: hook_registry, skill_resolver, shell_runner, skill_runner, executor
//   Related: NewDispatcherWithRegistry, SkillRunner, ShellRunner
//   Flow: resolve executor → resolve resolver → build shell/skill runners → return registry
//   Resources: execution.SkillExecutor, SkillResolver
//   Events: none
//   OutputFields: *Registry
func NewRegistry(cfg *Config, opts RegistryOptions) *Registry {
	// Set up executor
	executor := opts.Executor
	if executor == nil {
		executor = execution.NewRunnerExecutor()
	}

	// Set up resolver
	resolver := opts.Resolver
	if resolver == nil && opts.SkillsDir != "" {
		resolver = NewDefaultResolver(opts.SkillsDir)
	}

	// Set up shell runner
	shellRunner := opts.ShellRunner
	if shellRunner == nil {
		shellRunner = &ShellRunner{}
	}

	// Set up skill runner
	skillRunner := opts.SkillRunner
	if skillRunner == nil && resolver != nil {
		skillRunner = &SkillRunner{
			Executor: executor,
			Resolver: resolver,
		}
	}

	return &Registry{
		config:        cfg,
		shellRunner:   shellRunner,
		skillRunner:   skillRunner,
		defaultRunner: skillRunner, // Default to skill runner
	}
}

// GetRunner returns the appropriate runner for a hook definition.
func (r *Registry) GetRunner(hookDef HookDef) HookRunner {
	// Determine runner type based on skill names in the hook definition
	for _, entry := range hookDef.Run {
		if isShellHook(entry.Skill) {
			return r.shellRunner
		}
	}

	// Default to skill runner
	if r.skillRunner != nil {
		return r.skillRunner
	}
	return r.defaultRunner
}

// isShellHook determines if a skill name refers to a shell script.
func isShellHook(skillName string) bool {
	// Shell hooks have .sh extension
	if strings.HasSuffix(skillName, ".sh") {
		return true
	}
	// Or absolute paths to shell scripts
	if strings.HasPrefix(skillName, "/") && strings.HasSuffix(skillName, ".sh") {
		return true
	}
	return false
}

// Run is a convenience method that gets the appropriate runner and executes.
func (r *Registry) Run(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
	runner := r.GetRunner(hookDef)
	return runner.Run(ctx, hookDef, input)
}

// Config returns the registry's hook configuration.
func (r *Registry) Config() *Config {
	return r.config
}

// RegistryRunner adapts the Registry to the HookRunner interface.
// This allows the dispatcher to use the registry directly.
type RegistryRunner struct {
	Registry *Registry
}

// Run implements HookRunner by delegating to the registry.
func (r *RegistryRunner) Run(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
	return r.Registry.Run(ctx, hookDef, input)
}

// NewDispatcherWithRegistry creates a dispatcher that uses the registry for hook execution.
// NewDispatcherWithRegistry builds a dispatcher with a registry-backed runner.
//
// Index:
//   Purpose: Construct a dispatcher that resolves hook skills by name
//   Keywords: hook_dispatcher, registry, skill_resolver, hooks
//   Related: NewRegistry, NewDispatcher
//   Flow: build registry → wrap with RegistryRunner → create dispatcher
//   Resources: *Registry, RegistryRunner
//   Events: none
//   OutputFields: Dispatcher
func NewDispatcherWithRegistry(cfg *Config, skillsDir string) Dispatcher {
	registry := NewRegistry(cfg, RegistryOptions{
		SkillsDir: skillsDir,
	})
	return NewDispatcher(cfg, &RegistryRunner{Registry: registry})
}
