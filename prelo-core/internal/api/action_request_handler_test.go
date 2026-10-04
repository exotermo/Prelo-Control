package api

import (
	"net/http"
	"testing"
)

func TestRequiredScope_ActionRequests(t *testing.T) {
	cases := map[[2]string]string{
		{http.MethodPost, "/api/v1/action-requests"}:          "actions:request",
		{http.MethodGet, "/api/v1/action-requests/1"}:         "actions:request",
		{http.MethodPost, "/api/v1/action-requests/1/result"}: "actions:report",
		{http.MethodGet, "/api/v1/projects/1/actions"}:        "projects:read",
	}
	for route, want := range cases {
		if got := requiredScope(route[0], route[1]); got != want {
			t.Errorf("%v: got %q want %q", route, got, want)
		}
	}
}
