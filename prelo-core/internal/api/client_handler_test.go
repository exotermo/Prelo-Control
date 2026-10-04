package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestRequiredScope_Clients(t *testing.T) {
	cases := map[string]string{
		"GET /api/v1/clients":                 "clients:read",
		"GET /api/v1/clients/1":               "clients:read",
		"GET /api/v1/clients/1/timeline":      "clients:read",
		"POST /api/v1/clients":                "clients:manage",
		"PATCH /api/v1/clients/1":             "clients:manage",
		"POST /api/v1/clients/1/contacts":     "clients:manage",
		"DELETE /api/v1/clients/1/contacts/2": "clients:manage",
		"DELETE /api/v1/clients/1":            "clients:delete",
		"GET /api/v1/search":                  "clients:read",
		"GET /api/v1/home":                    "clients:read",
		"POST /api/v1/recent":                 "clients:read",
		"PUT /api/v1/projects/1/client":       "projects:manage",
	}
	for route, want := range cases {
		parts := strings.SplitN(route, " ", 2)
		if got := requiredScope(parts[0], parts[1]); got != want {
			t.Errorf("%s: got %q want %q", route, got, want)
		}
	}
}

// Deleting a client is ADMIN-only; editing is day-to-day work for both roles.
func TestClientScopesPerRole(t *testing.T) {
	has := func(scopes []string, s string) bool {
		for _, x := range scopes {
			if x == s {
				return true
			}
		}
		return false
	}
	for _, s := range []string{"clients:read", "clients:manage"} {
		if !has(dashboardOperatorScopes, s) || !has(dashboardAdminScopes, s) {
			t.Errorf("%s must be granted to both roles", s)
		}
	}
	if has(dashboardOperatorScopes, "clients:delete") || !has(dashboardAdminScopes, "clients:delete") {
		t.Error("clients:delete must be ADMIN-only")
	}
}

// The new routes must register without colliding with existing patterns (ServeMux panics).
func TestWorkspaceRoutesRegisterAlongsideProjects(t *testing.T) {
	mux := http.NewServeMux()
	RegisterProjectRoutes(mux, NewProjectHandler(nil, nil))
	RegisterWorkspaceRoutes(mux, NewWorkspaceHandler(nil, nil))
}
