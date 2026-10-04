package domain

import (
	"time"

	"github.com/google/uuid"
)

// Workspace (contratos G1): v1 is the whole Prelo instance — one row, stable id. Every external
// record that must be tied to "where" (action requests, deploys) carries this id.
type Workspace struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}
