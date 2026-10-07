package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidateExecutorCapacityRejectsStaleAndInconsistentSnapshots(t *testing.T) {
	now := time.Now().UTC()
	valid := ExecutorCapacity{ProfileID: WorkspaceSmallProfileID, MemoryBytes: 256 << 20, CPUQuotaMilli: 500,
		DiskBytes: 1 << 30, PIDs: 64, AvailableSlots: 0, MaximumSlots: 1, ActiveContainers: 1,
		ActiveExecutionIDs: []uuid.UUID{uuid.New()}, ObservedAt: now}
	if err := ValidateExecutorCapacity(valid, now); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	stale := valid
	stale.ObservedAt = now.Add(-time.Minute)
	if err := ValidateExecutorCapacity(stale, now); err == nil {
		t.Fatal("stale capacity snapshot accepted")
	}
	inconsistent := valid
	inconsistent.ActiveContainers = 0
	if err := ValidateExecutorCapacity(inconsistent, now); err == nil {
		t.Fatal("active count inconsistent with execution IDs was accepted")
	}
	tooManySlots := valid
	tooManySlots.AvailableSlots = 2
	if err := ValidateExecutorCapacity(tooManySlots, now); err == nil {
		t.Fatal("available slots above maximum accepted")
	}
	tooManySlots = valid
	tooManySlots.MaximumSlots = MaxExecutorWorkerSlots + 1
	tooManySlots.AvailableSlots = tooManySlots.MaximumSlots
	if err := ValidateExecutorCapacity(tooManySlots, now); err == nil {
		t.Fatal("snapshot exceeded the server-side slot bound")
	}
}

func TestExecutorFlowFailsClosedWhenWorkerProfileDiffersFromServerProfile(t *testing.T) {
	repo := &executorRequestsFake{}
	flow := NewExecutorFlow(repo, nil, nil, nil)
	now := time.Now().UTC()
	capacity := ExecutorCapacity{ProfileID: WorkspaceSmallProfileID, MemoryBytes: 128 << 20, CPUQuotaMilli: 250,
		DiskBytes: 1 << 30, PIDs: 64, AvailableSlots: 1, MaximumSlots: 1, ObservedAt: now}
	if _, err := flow.Claim(context.Background(), uuid.New(), capacity); err != nil {
		t.Fatal(err)
	}
	if repo.claimedCapacity.AvailableSlots != 0 || repo.claimedCapacity.MaximumSlots != 0 {
		t.Fatalf("worker with smaller limits must be structurally unsupported: %+v", repo.claimedCapacity)
	}
}
