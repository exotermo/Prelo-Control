package api

import (
	"net/http"
	"testing"

	"github.com/exotermo/prelo-core/internal/application"
)

// Regression test for 2026-10-02: ChangeRole's self-lockout guard (ErrDashboardCannotChangeOwnRole)
// had no entry in mapDashboardAuthError, so it fell through to a bare 500 "internal_error" instead
// of a clear 422 — found while manually verifying the Usuários page end to end.
func TestMapDashboardAuthError_CannotChangeOwnRole(t *testing.T) {
	status, code, _, ok := mapDashboardAuthError(application.ErrDashboardCannotChangeOwnRole)
	if !ok {
		t.Fatal("expected mapDashboardAuthError to recognize ErrDashboardCannotChangeOwnRole")
	}
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", status)
	}
	if code != "cannot_change_own_role" {
		t.Fatalf("expected code cannot_change_own_role, got %q", code)
	}
}
