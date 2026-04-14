package main

import (
	"context"
	"fmt"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/context/calibration"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
)

const command = "calibration/get"

// Input represents the skill input parameters for calibration profile retrieval.
type Input struct {
	Workspace string `json:"workspace,omitempty"`
	Format    string `json:"format,omitempty"` // "compact" (default) or "detailed"
}

// Output represents the skill output with profile data in multiple formats.
type Output struct {
	Found      bool                 `json:"found"`
	Profile    *calibration.Profile `json:"profile,omitempty"`
	Compact    string               `json:"compact,omitempty"`
	Detailed   string               `json:"detailed,omitempty"`
	HookOutput *hooks.Output        `json:"hook_output,omitempty"`
}

// main is the skill entry point for calibration/get.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates calibration profile retrieval with multiple output formats and hook integration.
//
// Index:
// - Purpose: Retrieve calibration profiles with compact/detailed formatting and hook output for context injection
// - Flow: resolve workspace → open memory store → load profile → format based on requested output → emit results
// - SideEffects: profile loading; format conversion; hook output generation; context preparation
// - FailureModes: workspace resolution failures, memory store access errors, missing profiles
// - Observability: emits profile availability, formatted outputs, and hook-compatible context data
// - Related: calibration.LoadProfile, calibration.FormatCompact, calibration.FormatDetailed, calibration.FormatForInjection
// - Keywords: calibration/get, profile_retrieval, calibration_data, context_injection, hook_integration
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Resolve workspace
	workspace := workspaceutil.Resolve(in.Workspace, "", rc.Workspace)
	if workspace == "" {
		return skillerr.Arg("workspace is required", skillerr.WithHint("Provide workspace path or run from a project directory"))
	}

	// Default format
	format := in.Format
	if format == "" {
		format = "compact"
	}

	// Open memory store
	store, err := rc.Stores.Memory(ctx)
	if err != nil {
		return skillerr.IO("open memory store", skillerr.WithCause(err))
	}

	// Load profile
	profile, err := calibration.LoadProfile(ctx, store, workspace)
	if err != nil {
		return skillerr.IO("load profile", skillerr.WithCause(err))
	}

	if profile == nil {
		return skillout.Emit(rc, command, Output{
			Found:   false,
			Compact: "",
		})
	}

	out := Output{
		Found:   true,
		Profile: profile,
	}

	switch format {
	case "compact":
		out.Compact = calibration.FormatCompact(profile)
		// Also provide hook output for injection
		out.HookOutput = &hooks.Output{
			Decision: hooks.DecisionNone,
			Context:  calibration.FormatForInjection(profile),
		}
	case "detailed":
		out.Detailed = calibration.FormatDetailed(profile)
	case "both":
		out.Compact = calibration.FormatCompact(profile)
		out.Detailed = calibration.FormatDetailed(profile)
		out.HookOutput = &hooks.Output{
			Decision: hooks.DecisionNone,
			Context:  calibration.FormatForInjection(profile),
		}
	default:
		return skillerr.Arg(fmt.Sprintf("invalid format: %s", format),
			skillerr.WithHint("Use one of: compact, detailed, both"))
	}

	return skillout.Emit(rc, command, out)
}
