package updater

import (
	"time"
)

// Config configures the context updater worker.
type Config struct {
	// Polling configuration
	PollInterval   time.Duration // How often to check for new turns (default: 5s)
	TurnWindowSize int           // Number of recent turns to analyze (default: 5)

	// Thresholds
	DriftThreshold float32 // Topic drift threshold to trigger search (default: 0.7 = 30% change)
	ConfidenceMin  float32 // Minimum confidence to inject context (default: 0.8)

	// Short-term memory
	MemorySize int           // Number of recent injections to track (default: 50)
	MemoryTTL  time.Duration // How long to remember injections (default: 30m)

	// Rate limiting
	MaxInjectionRate int           // Max injections per window (default: 3)
	RateWindow       time.Duration // Rate limit window (default: 1m)

	// LLM configuration (for analyzer)
	LLMProvider string        // Provider override (default: auto-detect)
	LLMModel    string        // Model override (default: provider-specific)
	LLMTimeout  time.Duration // LLM request timeout (default: 10s)

	// Storage paths
	StorageRoot string // Root storage directory
	SessionsDB  string // Path to sessions.db

	// Feature flags
	Enabled bool // Whether the updater is enabled (default: true)
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		// Polling
		PollInterval:   5 * time.Second,
		TurnWindowSize: 5,

		// Thresholds
		DriftThreshold: 0.7,  // 30% drift triggers search
		ConfidenceMin:  0.8,  // Only inject high-confidence results

		// Short-term memory
		MemorySize: 50,
		MemoryTTL:  30 * time.Minute,

		// Rate limiting
		MaxInjectionRate: 3,
		RateWindow:       time.Minute,

		// LLM
		LLMTimeout: 10 * time.Second,

		// Feature flags
		Enabled: true,
	}
}

// WithPollInterval sets the polling interval.
func (c Config) WithPollInterval(d time.Duration) Config {
	c.PollInterval = d
	return c
}

// WithStorageRoot sets the storage root directory.
func (c Config) WithStorageRoot(path string) Config {
	c.StorageRoot = path
	return c
}

// WithSessionsDB sets the sessions database path.
func (c Config) WithSessionsDB(path string) Config {
	c.SessionsDB = path
	return c
}

// WithLLMProvider sets the LLM provider.
func (c Config) WithLLMProvider(provider string) Config {
	c.LLMProvider = provider
	return c
}

// WithLLMModel sets the LLM model.
func (c Config) WithLLMModel(model string) Config {
	c.LLMModel = model
	return c
}

// Validate checks the configuration for errors and applies defaults.
func (c *Config) Validate() error {
	if c.PollInterval < time.Second {
		c.PollInterval = time.Second
	}
	if c.TurnWindowSize < 1 {
		c.TurnWindowSize = 5
	}
	if c.MemorySize < 1 {
		c.MemorySize = 50
	}
	if c.MaxInjectionRate < 1 {
		c.MaxInjectionRate = 3
	}
	return nil
}
