package worker

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/queue"
)

// Start launches n goroutines, each consuming the shared Redis stream and running claimed
// jobs through processJob. Each consumer gets a distinct name so Redis Streams' consumer
// group can track them independently (mostly informational here — see ADR-013 on why we
// don't rely on the group's PEL for redelivery).
func Start(ctx context.Context, n int, stream *queue.Stream, processJob *application.ProcessJobUseCase, namePrefix string) {
	for i := 0; i < n; i++ {
		consumerName := fmt.Sprintf("%s-%d", namePrefix, i)
		go stream.Consume(ctx, consumerName, func(ctx context.Context, jobID string) error {
			id, err := uuid.Parse(jobID)
			if err != nil {
				log.Printf("worker %s: invalid job id %q: %v", consumerName, jobID, err)
				return nil
			}
			execution, err := processJob.ProcessExecution(ctx, domain.ExecutionID{Value: id})
			if err != nil {
				log.Printf("worker %s: job %s failed: %v", consumerName, jobID, err)
				return err
			}
			log.Printf("worker %s: job %s -> %s", consumerName, jobID, execution.Status)
			return nil
		})
	}
}
