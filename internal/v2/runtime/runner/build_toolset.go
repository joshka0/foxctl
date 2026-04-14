package runner

import (
	"context"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
)

func (p *Pipeline) stageBuildToolset(_ context.Context, _ *executionState) *v2errors.V2Error {
	// PR-03 keeps tool setup minimal: dependency validation already happened in ResolveDependencies.
	return nil
}
