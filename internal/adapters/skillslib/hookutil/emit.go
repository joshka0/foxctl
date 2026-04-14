package hookutil

import (
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/runtime/hooks"
)

// EmitOutput emits a standard hook output envelope with optional extra fields.
func EmitOutput(rc *skillmain.RunContext, command string, output hooks.Output, extras map[string]any) error {
	data := map[string]any{
		"hook_output": output,
	}
	for key, value := range extras {
		data[key] = value
	}
	return skillout.Emit(rc, command, data)
}
