package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

type meFakes struct {
	user      domain.DashboardUser
	all       []domain.Project
	mine      []domain.Project
	workspace domain.Workspace
}

func (f meFakes) FindByID(context.Context, domain.DashboardUserID) (domain.DashboardUser, error) {
	return f.user, nil
}
func (f meFakes) List(context.Context) ([]domain.Project, error) { return f.all, nil }
func (f meFakes) ListProjectsForUser(context.Context, domain.DashboardUserID) ([]domain.Project, error) {
	return f.mine, nil
}
func (f meFakes) Current(context.Context) (domain.Workspace, error) { return f.workspace, nil }

func callMe(t *testing.T, f meFakes, identity AuthContext) (int, meResponse) {
	t.Helper()
	h := NewMeHandler(f, f, f, f)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req = req.WithContext(context.WithValue(req.Context(), authContextKey{}, identity))
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	var out meResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestMeReturnsIdentityWorkspaceAndVisibleProjects(t *testing.T) {
	userID := uuid.New()
	client := domain.NewClientID()
	a := domain.Project{ID: domain.NewProjectID(), Name: "A", ClientID: &client}
	b := domain.Project{ID: domain.NewProjectID(), Name: "B"}
	f := meFakes{
		user:      domain.DashboardUser{ID: domain.DashboardUserID{Value: userID}, Email: "op@x.com", Role: domain.DashboardRoleOperator},
		all:       []domain.Project{a, b},
		mine:      []domain.Project{a},
		workspace: domain.Workspace{ID: uuid.New(), Name: "Prelo Control", CreatedAt: time.Now()},
	}
	operator := AuthContext{Subject: userID.String(), TokenUse: "dashboard", Scopes: map[string]struct{}{"projects:read": {}, "tasks:read": {}}}
	code, me := callMe(t, f, operator)
	if code != http.StatusOK || me.Email != "op@x.com" || me.Role != "OPERATOR" || me.WorkspaceID != f.workspace.ID.String() {
		t.Fatalf("unexpected: %d %+v", code, me)
	}
	if len(me.Projects) != 1 || me.Projects[0].Name != "A" || me.Projects[0].ClientID == nil {
		t.Fatalf("operator must only see member projects: %+v", me.Projects)
	}
	if len(me.Scopes) != 2 || me.Scopes[0] != "projects:read" || me.Session.Kind != "web" {
		t.Fatalf("scopes/session: %+v %+v", me.Scopes, me.Session)
	}

	admin := operator
	admin.Scopes = map[string]struct{}{"projects:read": {}, "projects:manage": {}}
	if _, me := callMe(t, f, admin); len(me.Projects) != 2 {
		t.Fatalf("admin sees every project, got %d", len(me.Projects))
	}
}

func TestMeRefusesNonPersonalTokens(t *testing.T) {
	f := meFakes{}
	for _, identity := range []AuthContext{
		{Subject: "api_key:x", TokenUse: "api_key", Scopes: map[string]struct{}{"projects:read": {}}},
		{Subject: "prelo-messaging-bridge", TokenUse: "technical"},
	} {
		if code, _ := callMe(t, f, identity); code != http.StatusForbidden {
			t.Fatalf("%s should get 403, got %d", identity.TokenUse, code)
		}
	}
	if requiredScope(http.MethodGet, "/api/v1/me") != "projects:read" {
		t.Fatal("/me must require projects:read (both roles have it, integration keys don't)")
	}
}
