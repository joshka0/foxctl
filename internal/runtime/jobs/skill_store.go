package jobs

import (
	"context"

	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/runtime/jobs/executor"
	jobstore "github.com/joshka0/foxctl/internal/storage/jobs"
	"github.com/joshka0/foxctl/internal/storage/jobs/persist"
)

type skillStoreConfig struct {
	casPath string
}

type skillStorePersistence interface {
	jobstore.Persistence
	executor.Persistence
}

// SkillStoreOption configures a runtime-backed skill job store.
type SkillStoreOption func(*skillStoreConfig)

// WithSkillStoreCASPath configures CAS artifact capture for skill job stderr.
func WithSkillStoreCASPath(path string) SkillStoreOption {
	return func(cfg *skillStoreConfig) {
		cfg.casPath = path
	}
}

// NewSkillStore combines job persistence with a runtime skill executor.
func NewSkillStore(ctx context.Context, root string, p skillStorePersistence, opts ...SkillStoreOption) *jobstore.Store {
	cfg := newSkillStoreConfig(opts...)
	exec := newSkillExecutor(ctx, root, p, cfg)
	return jobstore.New(root, p, exec)
}

// OpenSkillStore opens a job store that can execute prepared skill jobs.
func OpenSkillStore(ctx context.Context, root string, opts ...SkillStoreOption) (store *jobstore.Store, err error) {
	p, err := persist.Open(ctx, root)
	if err != nil {
		return nil, err
	}
	defer errs.CloseOnErr(p, &err)

	return NewSkillStore(ctx, root, p, opts...), nil
}

func newSkillStoreConfig(opts ...SkillStoreOption) skillStoreConfig {
	cfg := skillStoreConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func newSkillExecutor(_ context.Context, root string, p executor.Persistence, cfg skillStoreConfig) jobstore.SkillExecutor {
	opts := []executor.Option{}
	if cfg.casPath != "" {
		opts = append(opts, executor.WithCASPath(cfg.casPath))
	}
	return executor.New(root, p, opts...)
}
