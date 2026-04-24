package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// MultiToolExecutor combines small tool executors behind one engine.ToolExecutor.
type MultiToolExecutor []engine.ToolExecutor

func (e MultiToolExecutor) List() []engine.ToolDef {
	var out []engine.ToolDef
	seen := map[string]struct{}{}
	for _, exec := range e {
		if exec == nil {
			continue
		}
		for _, def := range exec.List() {
			name := strings.TrimSpace(def.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, def)
		}
	}
	return out
}

func (e MultiToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	name = strings.TrimSpace(name)
	for _, exec := range e {
		if exec == nil {
			continue
		}
		for _, def := range exec.List() {
			if strings.TrimSpace(def.Name) == name {
				return exec.Execute(ctx, name, args)
			}
		}
	}
	return "", fmt.Errorf("unknown tool %q", name)
}
