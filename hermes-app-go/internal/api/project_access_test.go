package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/config"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

type fakeProjectFinder struct {
	projects map[string]domain.Project
}

func (f fakeProjectFinder) FindByID(_ context.Context, id domain.ProjectID) (domain.Project, error) {
	p, ok := f.projects[id.String()]
	if !ok {
		return domain.Project{}, application.ErrProjectNotFound
	}
	return p, nil
}

type fakeMembershipChecker struct {
	members map[string]bool // key: projectID+"/"+userID
}

func (f fakeMembershipChecker) IsMember(_ context.Context, projectID domain.ProjectID, userID domain.DashboardUserID) (bool, error) {
	return f.members[projectID.String()+"/"+userID.String()], nil
}

// testAuthWithProjects mirrors testAuth but wires a fake project/membership checker so
// Fase W's resolveProject logic (ADMIN bypass, OPERATOR membership gate) can be exercised
// without a real database.
func testAuthWithProjects(t *testing.T, projects fakeProjectFinder, members fakeMembershipChecker) *JWTAuthMiddleware {
	t.Helper()
	middleware, err := NewJWTAuthMiddleware(config.APIAuthConfig{Enabled: true, Secret: "api-test-secret", Issuer: "messaging-core", Audience: "hermes-app-go", ClockSkewSeconds: 0}, projects, members, nil)
	if err != nil {
		t.Fatal(err)
	}
	return middleware
}

func dashboardToken(t *testing.T, subject string, scopes []string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":       subject,
		"token_use": "dashboard",
		"scope":     scopes,
		"iss":       "messaging-core",
		"aud":       []string{"hermes-app-go"},
		"iat":       0,
		"exp":       9999999999,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	value, err := token.SignedString([]byte("api-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func newProjectScopedRequest(token, projectID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if projectID != "" {
		req.Header.Set("X-Project-Id", projectID)
	}
	return req
}

func TestResolveProject_AdminBypassesMembership(t *testing.T) {
	projectID := uuid.New()
	projects := fakeProjectFinder{projects: map[string]domain.Project{projectID.String(): {ID: domain.ProjectID{Value: projectID}}}}
	members := fakeMembershipChecker{members: map[string]bool{}} // admin is NOT a member anywhere
	middleware := testAuthWithProjects(t, projects, members)

	var gotProjectID *uuid.UUID
	handler := middleware.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, _ := FromContext(r.Context())
		gotProjectID = identity.ProjectID
		w.WriteHeader(http.StatusNoContent)
	}))

	token := dashboardToken(t, uuid.New().String(), []string{"tasks:read", "projects:manage"})
	req := newProjectScopedRequest(token, projectID.String())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotProjectID == nil || *gotProjectID != projectID {
		t.Fatalf("expected resolved project %s, got %v", projectID, gotProjectID)
	}
}

func TestResolveProject_OperatorMemberAllowed(t *testing.T) {
	projectID := uuid.New()
	userID := uuid.New()
	projects := fakeProjectFinder{projects: map[string]domain.Project{projectID.String(): {ID: domain.ProjectID{Value: projectID}}}}
	members := fakeMembershipChecker{members: map[string]bool{projectID.String() + "/" + userID.String(): true}}
	middleware := testAuthWithProjects(t, projects, members)

	handler := middleware.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	token := dashboardToken(t, userID.String(), []string{"tasks:read", "projects:read"})
	req := newProjectScopedRequest(token, projectID.String())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResolveProject_OperatorNonMemberForbidden(t *testing.T) {
	projectID := uuid.New()
	userID := uuid.New()
	projects := fakeProjectFinder{projects: map[string]domain.Project{projectID.String(): {ID: domain.ProjectID{Value: projectID}}}}
	members := fakeMembershipChecker{members: map[string]bool{}} // not a member of anything
	middleware := testAuthWithProjects(t, projects, members)

	handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run")
	}))

	token := dashboardToken(t, userID.String(), []string{"tasks:read", "projects:read"})
	req := newProjectScopedRequest(token, projectID.String())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResolveProject_UnknownProjectForbidden(t *testing.T) {
	projects := fakeProjectFinder{projects: map[string]domain.Project{}}
	members := fakeMembershipChecker{members: map[string]bool{}}
	middleware := testAuthWithProjects(t, projects, members)

	handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run")
	}))

	token := dashboardToken(t, uuid.New().String(), []string{"tasks:read", "projects:read"})
	req := newProjectScopedRequest(token, uuid.New().String())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResolveProject_EmptyHeaderResolvesToUnassignedBucket(t *testing.T) {
	projects := fakeProjectFinder{projects: map[string]domain.Project{}}
	members := fakeMembershipChecker{members: map[string]bool{}}
	middleware := testAuthWithProjects(t, projects, members)

	var gotProjectID *uuid.UUID
	sawProjectIDField := false
	handler := middleware.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, _ := FromContext(r.Context())
		gotProjectID = identity.ProjectID
		sawProjectIDField = true
		w.WriteHeader(http.StatusNoContent)
	}))

	token := dashboardToken(t, uuid.New().String(), []string{"tasks:read", "projects:read"})
	req := newProjectScopedRequest(token, "")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if !sawProjectIDField || gotProjectID != nil {
		t.Fatalf("expected nil resolved project (unassigned bucket), got %v", gotProjectID)
	}
}

func TestResolveProject_InvalidUUIDBadRequest(t *testing.T) {
	projects := fakeProjectFinder{projects: map[string]domain.Project{}}
	members := fakeMembershipChecker{members: map[string]bool{}}
	middleware := testAuthWithProjects(t, projects, members)

	handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run")
	}))

	token := dashboardToken(t, uuid.New().String(), []string{"tasks:read", "projects:read"})
	req := newProjectScopedRequest(token, "not-a-uuid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
