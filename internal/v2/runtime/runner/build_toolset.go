package runner

import (
	"context"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
)

func (p *Pipeline) stageBuildToolset(_ context.Context, st *executionState) *v2errors.V2Error {
	st.tools = cloneToolDefs(p.cfg.Tools)
	return nil
}

func cloneToolDefs(in []coretool.ToolDef) []coretool.ToolDef {
	if len(in) == 0 {
		return nil
	}
	out := make([]coretool.ToolDef, len(in))
	for i, def := range in {
		out[i] = def
		if len(def.Parameters) > 0 {
			out[i].Parameters = append([]byte(nil), def.Parameters...)
		}
		if len(def.Policy.AllowProfiles) > 0 {
			out[i].Policy.AllowProfiles = append([]coretool.ProcessProfile(nil), def.Policy.AllowProfiles...)
		}
	}
	return out
}
