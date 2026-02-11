package jobs

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

const defaultMaxWorkers = 25

// Config configures the River job client.
type Config struct {
	// Pool is the pgx connection pool River uses.
	Pool *pgxpool.Pool

	// MaxWorkers configures the default queue concurrency.
	// If zero or negative, a default of 25 workers is used.
	MaxWorkers int

	// Queues configures additional queues by queue name -> max workers.
	Queues map[string]int
}

// NewClient creates a River client with registered workers.
func NewClient(cfg Config, workers *river.Workers) (*river.Client[pgx.Tx], error) {
	if cfg.Pool == nil {
		return nil, fmt.Errorf("jobs: pool is required")
	}
	if workers == nil {
		return nil, fmt.Errorf("jobs: workers registry is required")
	}

	maxWorkers := cfg.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = defaultMaxWorkers
	}

	queues := map[string]river.QueueConfig{
		river.QueueDefault: {MaxWorkers: maxWorkers},
	}
	for name, queueMaxWorkers := range cfg.Queues {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			return nil, fmt.Errorf("jobs: queue name cannot be empty")
		}
		if queueMaxWorkers <= 0 {
			return nil, fmt.Errorf("jobs: queue %q max workers must be > 0", trimmedName)
		}
		queues[trimmedName] = river.QueueConfig{MaxWorkers: queueMaxWorkers}
	}

	client, err := river.NewClient[pgx.Tx](riverpgxv5.New(cfg.Pool), &river.Config{
		Queues:  queues,
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("jobs: create river client: %w", err)
	}

	return client, nil
}
