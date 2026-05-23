package observability

import (
	"context"

	platformobs "github.com/joshka0/foxctl/internal/platform/observability"
)

func init() {
	platformobs.SetSink(runtimeSink{})
}

type runtimeSink struct{}

func (runtimeSink) Emit(ctx context.Context, event *platformobs.Event) {
	Emit(ctx, event)
}

func (runtimeSink) EmitSync(ctx context.Context, event *platformobs.Event) error {
	return EmitSync(ctx, event)
}
