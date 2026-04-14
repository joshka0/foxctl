package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	workerspkg "github.com/joshka0/foxctl/internal/runtime/jobs/workers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// RegisterWorkers registers all River workers used by the jobs package.
func RegisterWorkers() *river.Workers {
	workers := river.NewWorkers()

	mustRegisterWorker(workers, &workerspkg.CompressDailyWorker{Compressor: noopDailyCompressor{}})
	mustRegisterWorker(workers, &workerspkg.CompressScanWorker{
		Lister:   noopConversationLister{},
		Inserter: noopInserter{},
		Clock:    skilltest.RealClock{},
	})
	mustRegisterWorker(workers, &workerspkg.AgentHeartbeatCheckWorker{Recoverer: noopStaleAgentRecoverer{}})
	mustRegisterWorker(workers, &workerspkg.AgentIndexCleanupWorker{Cleaner: noopAgentIndexCleaner{}})
	mustRegisterWorker(workers, &workerspkg.ConversationTurnWorker{
		Processor: noopTurnProcessor{},
		Deliverer: noopReplyDeliverer{},
	})
	mustRegisterWorker(workers, &workerspkg.OAuthRefreshWorker{Refresher: noopTokenRefresher{}})
	mustRegisterWorker(workers, &workerspkg.OAuthCleanupWorker{Cleaner: noopExpiredTokenCleaner{}})

	return workers
}

// PeriodicJobs returns the periodic River job schedules.
func PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(30*time.Minute),
			func() (river.JobArgs, *river.InsertOpts) {
				return workerspkg.CompressScanArgs{}, nil
			},
			&river.PeriodicJobOpts{RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(60*time.Second),
			func() (river.JobArgs, *river.InsertOpts) {
				return workerspkg.AgentHeartbeatCheckArgs{}, nil
			},
			nil,
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(30*time.Minute),
			func() (river.JobArgs, *river.InsertOpts) {
				return workerspkg.AgentIndexCleanupArgs{}, nil
			},
			nil,
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(1*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return workerspkg.OAuthCleanupArgs{}, nil
			},
			nil,
		),
	}
}

func mustRegisterWorker[T river.JobArgs](workers *river.Workers, worker river.Worker[T]) {
	river.AddWorker(workers, worker)
}

type noopDailyCompressor struct{}

func (noopDailyCompressor) RunDayCompression(_ context.Context, _, _ string, _ bool) (bool, error) {
	return false, nil
}

type noopConversationLister struct{}

func (noopConversationLister) ListConversations(_ context.Context, _ int) ([]workerspkg.CompressionConversation, error) {
	return nil, nil
}

type noopInserter struct{}

func (noopInserter) Insert(_ context.Context, args river.JobArgs, _ *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	if args == nil {
		return nil, fmt.Errorf("jobs: args are required")
	}
	return &rivertype.JobInsertResult{}, nil
}

type noopStaleAgentRecoverer struct{}

func (noopStaleAgentRecoverer) RecoverStaleAgents(_ context.Context, _ time.Duration) (int, error) {
	return 0, nil
}

type noopAgentIndexCleaner struct{}

func (noopAgentIndexCleaner) CleanupAgentIndexes(_ context.Context) (int, error) {
	return 0, nil
}

type noopTurnProcessor struct{}

func (noopTurnProcessor) ProcessTurn(_ context.Context, _, _ string, _ json.RawMessage) (string, error) {
	return "", nil
}

type noopReplyDeliverer struct{}

func (noopReplyDeliverer) DeliverReply(_ context.Context, _, _, _ string) error {
	return nil
}

type noopTokenRefresher struct{}

func (noopTokenRefresher) RefreshToken(_ context.Context, _ string) error {
	return nil
}

type noopExpiredTokenCleaner struct{}

func (noopExpiredTokenCleaner) CleanupExpiredOAuth(_ context.Context) (int64, int64, error) {
	return 0, 0, nil
}
