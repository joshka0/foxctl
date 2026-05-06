package services

import (
	"fmt"
	"time"

	classictools "github.com/joshka0/foxctl/internal/agent/tools"
	sysconfig "github.com/joshka0/foxctl/internal/platform/config"
	toolbridge "github.com/joshka0/foxctl/internal/v2/adapters/toolbridge"
	"github.com/joshka0/foxctl/internal/v2/core/events"
	corerun "github.com/joshka0/foxctl/internal/v2/core/run"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/profiles"
	runner "github.com/joshka0/foxctl/internal/v2/runtime/runner"
	"github.com/joshka0/foxctl/internal/v2/runtime/supervisor"
	runtimetools "github.com/joshka0/foxctl/internal/v2/runtime/tools"
)

// DefaultRuntimeDependencies assembles the canonical production v2 runner with
// the real default tool catalog and bridge delegate.
type DefaultRuntimeDependencies struct {
	Profile               coretool.ProcessProfile
	ToolSpecs             map[coretool.ProcessProfile]profiles.ProfileSpec
	AppConfig             sysconfig.Config
	WorkspaceRoot         string
	WorkspaceID           string
	VaultPath             string
	IncludeExtensions     bool
	ClassicRegistry       *classictools.Registry
	EventStore            events.Appender
	EventBus              runner.EventPublisher
	Model                 runner.Model
	TurnRecorder          corerun.TurnRecorder
	TurnRequests          corerun.TurnRequestRegistry
	EffectJournal         corerun.EffectJournal
	TurnRequestStaleAfter time.Duration
	Hooks                 runner.HookRunner
	Now                   func() time.Time
	NewID                 func() string
	ObserveStage          runner.StageObserver
	OnEventError          func(error)
	StrictDurableIdentity bool
}

// DefaultLongLivedRuntimeDependencies extends DefaultRuntimeDependencies with
// optional background components for the long-lived run service.
type DefaultLongLivedRuntimeDependencies struct {
	DefaultRuntimeDependencies
	Components LongLivedRunComponents
	Observer   supervisor.Observer
}

// NewDefaultTurnRunner builds the canonical production v2 runner using the
// shared default tool catalog and toolbridge delegate.
func NewDefaultTurnRunner(deps DefaultRuntimeDependencies) (*runner.Pipeline, error) {
	profile := deps.Profile
	if profile == "" {
		profile = coretool.ProfileWorker
	}
	catalog, delegate, err := toolbridge.NewDefaultCatalogAndDelegate(deps.ToolSpecs, toolbridge.Config{
		AppConfig:         deps.AppConfig,
		WorkspaceRoot:     deps.WorkspaceRoot,
		WorkspaceID:       deps.WorkspaceID,
		VaultPath:         deps.VaultPath,
		IncludeExtensions: deps.IncludeExtensions,
		ClassicRegistry:   deps.ClassicRegistry,
	})
	if err != nil {
		return nil, fmt.Errorf("build default v2 tool executor: %w", err)
	}
	toolExec := runtimetools.NewExecutor(catalog, profile, delegate)
	strictDurableIdentity := deps.StrictDurableIdentity || deps.TurnRequests != nil || deps.EffectJournal != nil
	return runner.New(runner.Config{
		EventStore:            deps.EventStore,
		EventBus:              deps.EventBus,
		Model:                 deps.Model,
		Tools:                 catalog.ForProfile(profile),
		ToolExecutor:          toolExec,
		EffectJournal:         deps.EffectJournal,
		TurnRecorder:          deps.TurnRecorder,
		Hooks:                 deps.Hooks,
		Now:                   deps.Now,
		NewID:                 deps.NewID,
		ObserveStage:          deps.ObserveStage,
		OnEventError:          deps.OnEventError,
		StrictDurableIdentity: strictDurableIdentity,
	}), nil
}

// NewDefaultRunService assembles the canonical production v2 runner and wraps
// it in the request-scoped run service.
func NewDefaultRunService(deps DefaultRuntimeDependencies) (*RunService, error) {
	turnRunner, err := NewDefaultTurnRunner(deps)
	if err != nil {
		return nil, err
	}
	return newDefaultConfiguredRunService(turnRunner, deps), nil
}

// NewDefaultLongLivedRunService assembles the canonical production v2 runner
// and attaches optional long-lived background components.
func NewDefaultLongLivedRunService(deps DefaultLongLivedRuntimeDependencies) (*LongLivedRunService, error) {
	turnRunner, err := NewDefaultTurnRunner(deps.DefaultRuntimeDependencies)
	if err != nil {
		return nil, err
	}
	runSvc := newDefaultConfiguredRunService(turnRunner, deps.DefaultRuntimeDependencies)
	return newLongLivedRunService(runSvc, BuildLongLivedRunSpecs(deps.Components), deps.Observer), nil
}

func newDefaultConfiguredRunService(turnRunner TurnRunner, deps DefaultRuntimeDependencies) *RunService {
	if deps.TurnRequestStaleAfter == 0 {
		return NewRunServiceWithRegistry(turnRunner, deps.TurnRequests, deps.Now)
	}
	return NewRunServiceWithRegistryConfig(turnRunner, deps.TurnRequests, deps.Now, RunServiceConfig{
		TurnRequestStaleAfter: deps.TurnRequestStaleAfter,
	})
}
