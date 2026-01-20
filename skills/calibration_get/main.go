package main

import (
	"context"
	"fmt"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/calibration"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/sessionkit"
)

const command = "calibration/get"

// Input represents the skill input parameters.
type Input struct {
	Workspace string `json:"workspace,omitempty"`
	Format    string `json:"format,omitempty"` // "compact" (default) or "detailed"
}

// Output represents the skill output.
type Output struct {
	Found      bool                 `json:"found"`
	Profile    *calibration.Profile `json:"profile,omitempty"`
	Compact    string               `json:"compact,omitempty"`
	Detailed   string               `json:"detailed,omitempty"`
	HookOutput *hooks.Output        `json:"hook_output,omitempty"`
}

func main() {
	skillmain.Main(command, run)
}

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
	store, cleanup, err := sessionkit.OpenMemory(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open memory store", skillerr.WithCause(err))
	}
	defer cleanup()

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
