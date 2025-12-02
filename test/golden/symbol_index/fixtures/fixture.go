// Package fixture is a small test package for golden symbol index tests.
package fixture

// MaxRetries is the default retry count.
const MaxRetries = 3

// Config holds service configuration.
type Config struct {
	Host string
	Port int
}

// Service provides business logic.
type Service struct {
	config Config
}

// NewService creates a new service.
func NewService(cfg Config) *Service {
	return &Service{config: cfg}
}

// Run starts the service and calls helper functions.
func (s *Service) Run() error {
	s.validate()
	return s.start()
}

func (s *Service) validate() {
	// validation logic
}

func (s *Service) start() error {
	return nil
}

// Handler defines the request handler interface.
type Handler interface {
	Handle() error
}
