package supervisor

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"
)

// Host owns component startup/shutdown and cancellation propagation.
type Host struct {
	specs   []Spec
	observe Observer
}

// NewHost builds a supervisor host.
func NewHost(specs []Spec, observe Observer) *Host {
	out := make([]Spec, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		if spec.Component == nil {
			continue
		}
		out = append(out, Spec{
			Name:      name,
			Component: spec.Component,
		})
	}
	return &Host{
		specs:   out,
		observe: observe,
	}
}

// Run starts all components and blocks until context cancellation or component failure.
func (h *Host) Run(ctx context.Context) error {
	if h == nil || len(h.specs) == 0 {
		return nil
	}

	group, groupCtx := errgroup.WithContext(ctx)
	for _, spec := range h.specs {
		spec := spec
		group.Go(func() error {
			h.emit(Event{Kind: EventStarting, Name: spec.Name})
			err := spec.Component.Run(groupCtx)
			switch {
			case err == nil:
				h.emit(Event{Kind: EventStopped, Name: spec.Name})
				return nil
			case stderrors.Is(err, context.Canceled), stderrors.Is(err, context.DeadlineExceeded):
				h.emit(Event{Kind: EventStopped, Name: spec.Name})
				return nil
			default:
				wrapped := fmt.Errorf("component %s: %w", spec.Name, err)
				h.emit(Event{Kind: EventFailed, Name: spec.Name, Err: wrapped})
				return wrapped
			}
		})
	}

	if err := group.Wait(); err != nil {
		return err
	}
	return nil
}

func (h *Host) emit(evt Event) {
	if h == nil || h.observe == nil {
		return
	}
	h.observe(evt)
}
