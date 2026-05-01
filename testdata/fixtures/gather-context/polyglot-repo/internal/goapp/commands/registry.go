package commands

type Handler func() error

type Registry struct {
	handlers map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: map[string]Handler{}}
}

func (r *Registry) Add(name string, handler Handler) {
	r.handlers[name] = handler
}

func (r *Registry) Dispatch(name string) error {
	if handler := r.handlers[name]; handler != nil {
		return handler()
	}
	return nil
}
