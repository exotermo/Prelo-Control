package application

import (
	"time"

	"github.com/google/uuid"
)

// ValidateExecutorCapacity accepts a recent worker snapshot as scheduling telemetry only. It
// cannot authorize a job; approval and the current policy are still rechecked by the claim.
func ValidateExecutorCapacity(c ExecutorCapacity, now time.Time) error {
	if c.ProfileID == "" || c.MemoryBytes <= 0 || c.CPUQuotaMilli <= 0 || c.DiskBytes <= 0 || c.PIDs <= 0 ||
		c.AvailableSlots < 0 || c.MaximumSlots < 0 || c.MaximumSlots > MaxExecutorWorkerSlots || c.AvailableSlots > c.MaximumSlots || c.ActiveContainers < 0 ||
		c.ActiveContainers != len(c.ActiveExecutionIDs) || len(c.ActiveExecutionIDs) > MaxExecutorWorkerSlots ||
		c.ObservedAt.IsZero() || c.ObservedAt.After(now.Add(30*time.Second)) || now.Sub(c.ObservedAt) > 45*time.Second {
		return ErrInvalidExecutorCapacity
	}
	seen := make(map[uuid.UUID]struct{}, len(c.ActiveExecutionIDs))
	for _, id := range c.ActiveExecutionIDs {
		if id == uuid.Nil {
			return ErrInvalidExecutorCapacity
		}
		if _, duplicate := seen[id]; duplicate {
			return ErrInvalidExecutorCapacity
		}
		seen[id] = struct{}{}
	}
	return nil
}
