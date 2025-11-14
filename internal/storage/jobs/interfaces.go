package jobs

import (
	"context"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/storage/jobs/types"
)

// Persistence abstracts the storage requirements Store relies on.
type Persistence interface {
	Close() error
	List(ctx context.Context, limit int) ([]types.Job, error)
	Get(ctx context.Context, id string) (types.Job, error)
	InsertJob(ctx context.Context, job types.Job) error
	UpdateState(ctx context.Context, id string, newState types.State, errMsg, resultPath string) error
	Delete(ctx context.Context, id string) error
	RecoverOrphanedJobs(ctx context.Context) (int64, error)
}

// SkillExecutor captures the execution behavior Store delegates to.
type SkillExecutor interface {
	RunSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) (types.Job, []byte, error)
	FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (types.Job, bool, error)
	ExecutePrepared(ctx context.Context, jobID string, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error)
}
