package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

type recordingGateway struct {
	method, path string
	body         []byte
	status       int
	resp         string
}

func (g *recordingGateway) Do(_ context.Context, method, path string, body []byte) (int, []byte, error) {
	g.method, g.path, g.body = method, path, body
	return g.status, []byte(g.resp), nil
}

func connectionRequest(method, target, body string, identity AuthContext) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	return req.WithContext(context.WithValue(req.Context(), authContextKey{}, identity))
}

func TestModelConnection_RelaysBodyUntouchedAndReturnsGatewayResponse(t *testing.T) {
	gw := &recordingGateway{status: 200, resp: `{"configured":true,"keyLast4":"cdef"}`}
	h := NewModelConnectionHandler(gw, nil, nil)
	body := `{"provider":"anthropic","model":"m","apiKey":"sk-ant-secret"}`
	rec := httptest.NewRecorder()
	h.SaveInstance(rec, connectionRequest(http.MethodPut, "/api/v1/model-connections/instance", body, AuthContext{}))

	if gw.method != http.MethodPut || gw.path != "/instance" || string(gw.body) != body {
		t.Fatalf("relay mismatch: %s %s %s", gw.method, gw.path, gw.body)
	}
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "sk-ant-secret") {
		t.Fatalf("unexpected response %d %s", rec.Code, rec.Body.String())
	}
}

func TestModelConnection_ProjectReadRequiresMembership(t *testing.T) {
	projectID := uuid.New()
	user := uuid.New()
	projects := fakeProjectFinder{projects: map[string]domain.Project{projectID.String(): {ID: domain.ProjectID{Value: projectID}}}}
	gw := &recordingGateway{status: 200, resp: `{}`}

	notMember := NewModelConnectionHandler(gw, projects, fakeMembershipChecker{members: map[string]bool{}})
	req := connectionRequest(http.MethodGet, "/", "", AuthContext{Subject: user.String(), Scopes: map[string]struct{}{"projects:read": {}}})
	req.SetPathValue("projectId", projectID.String())
	rec := httptest.NewRecorder()
	notMember.GetProject(rec, req)
	if rec.Code != http.StatusForbidden || gw.path != "" {
		t.Fatalf("non-member must be refused before reaching the gateway, got %d (path %q)", rec.Code, gw.path)
	}

	member := NewModelConnectionHandler(gw, projects, fakeMembershipChecker{members: map[string]bool{projectID.String() + "/" + user.String(): true}})
	rec = httptest.NewRecorder()
	member.GetProject(rec, req)
	if rec.Code != 200 || gw.path != "/projects/"+projectID.String() {
		t.Fatalf("member read: %d %q", rec.Code, gw.path)
	}
}

func TestRequiredScope_ModelConnections(t *testing.T) {
	cases := map[string]string{
		"GET /api/v1/model-connections/instance": "settings:manage",
		"PUT /api/v1/model-connections/instance": "settings:manage",
		"POST /api/v1/model-connections/test":    "settings:manage",
		"GET /api/v1/projects/1/model":           "projects:read",
		"PUT /api/v1/projects/1/model":           "projects:manage",
		"POST /api/v1/projects/1/model/retest":   "projects:manage",
		"DELETE /api/v1/projects/1/model":        "projects:manage",
	}
	for route, want := range cases {
		parts := strings.SplitN(route, " ", 2)
		if got := requiredScope(parts[0], parts[1]); got != want {
			t.Errorf("%s: got %q want %q", route, got, want)
		}
	}
}
