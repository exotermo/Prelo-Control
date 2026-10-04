package worker

import (
	"context"
	"log"
	"time"

	"github.com/exotermo/prelo-core/internal/application"
)

// Sweeper is the crash-recovery / poll-based-fallback loop (etapa G11): it depends only on
// Postgres state, never on Redis, and does two things each tick — (1) requeues jobs stuck
// CLAIMED/RUNNING past their lease (a worker died mid-job) into RETRY/DEAD, and (2) drives
// ProcessJobUseCase for anything currently claimable (PENDING/RETRY, available now). (2) is
// what makes this loop double as the "queue still works even if Redis is completely absent"
// fallback, not just post-restart recovery — the same code path serves both.
type Sweeper struct {
	jobs      application.ExecutionJobRepository
	process   *application.ProcessJobUseCase
	interval  time.Duration
	batchSize int
}

func NewSweeper(jobs application.ExecutionJobRepository, process *application.ProcessJobUseCase, interval time.Duration, batchSize int) *Sweeper {
	return &Sweeper{jobs: jobs, process: process, interval: interval, batchSize: batchSize}
}

func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Sweeper) tick(ctx context.Context) {
	requeued, err := s.jobs.RequeueOrphaned(ctx)
	if err != nil {
		log.Printf("sweeper: requeue orphaned jobs failed: %v", err)
	} else if len(requeued) > 0 {
		log.Printf("sweeper: requeued %d orphaned job(s)", len(requeued))
	}

	claimable, err := s.jobs.ListClaimable(ctx, s.batchSize)
	if err != nil {
		log.Printf("sweeper: list claimable jobs failed: %v", err)
		return
	}
	for _, id := range claimable {
		execution, err := s.process.ProcessExecution(ctx, id)
		if err != nil {
			log.Printf("sweeper: processing job %s failed: %v", id, err)
			continue
		}
		log.Printf("sweeper: job %s -> %s", id, execution.Status)
	}
}
