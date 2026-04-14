package hookutil

import (
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
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
