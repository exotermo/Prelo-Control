package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/config"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

type fakeApiKeyAuth struct {
	keys map[string]domain.ApiKey
}

func (f fakeApiKeyAuth) AuthenticateApiKey(_ context.Context, raw string) (domain.ApiKey, error) {
	k, ok := f.keys[raw]
	if !ok {
		return domain.ApiKey{}, application.ErrApiKeyNotFound
	}
	return k, nil
}

func apiKeyMiddleware(t *testing.T, raw string, scopes []string) (*JWTAuthMiddleware, domain.ProjectID) {
	t.Helper()
	project := domain.NewProjectID()
	auth := fakeApiKeyAuth{keys: map[string]domain.ApiKey{raw: {ID: domain.NewApiKeyID(), ProjectID: project, Scopes: scopes}}}
	m, err := NewJWTAuthMiddleware(config.APIAuthConfig{Enabled: true, Secret: "api-test-secret", Issuer: "messaging-core", Audience: "hermes-app-go"}, nil, nil, auth)
	if err != nil {
		t.Fatal(err)
	}
	return m, project
}

func serve(m *JWTAuthMiddleware, req *http.Request, next http.HandlerFunc) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	m.Handler(next).ServeHTTP(rec, req)
	return rec
}

func TestApiKey_AuthenticatesAndBindsProject(t *testing.T) {
	m, project := apiKeyMiddleware(t, "hk_live_good", []string{"tasks:create"})
	var got AuthContext
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer hk_live_good")
	rec := serve(m, req, func(w http.ResponseWriter, r *http.Request) {
		got, _ = FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got.TokenUse != "api_key" || got.ProjectID == nil || *got.ProjectID != project.Value {
		t.Fatalf("identity not bound to the key's project: %+v", got)
	}
	if _, isTenant := tenantIdentity(context.WithValue(context.Background(), authContextKey{}, got)); isTenant {
		t.Fatal("an API key must not take the tenant-scoped path")
	}
}

func TestApiKey_UnknownKeyIs401(t *testing.T) {
	m, _ := apiKeyMiddleware(t, "hk_live_good", []string{"tasks:create"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer hk_live_wrong")
	if rec := serve(m, req, func(http.ResponseWriter, *http.Request) { t.Fatal("must not run") }); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestApiKey_MissingScopeIs403(t *testing.T) {
	m, _ := apiKeyMiddleware(t, "hk_live_good", []string{"tasks:read"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer hk_live_good")
	if rec := serve(m, req, func(http.ResponseWriter, *http.Request) { t.Fatal("must not run") }); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestApiKey_OtherProjectHeaderIs403(t *testing.T) {
	m, _ := apiKeyMiddleware(t, "hk_live_good", []string{"tasks:read"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer hk_live_good")
	req.Header.Set("X-Project-Id", uuid.NewString())
	if rec := serve(m, req, func(http.ResponseWriter, *http.Request) { t.Fatal("must not run") }); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestApiKey_CannotManageIntegrationsOrDecideApprovals(t *testing.T) {
	m, _ := apiKeyMiddleware(t, "hk_live_good", []string{"tasks:create", "tasks:read", "tasks:execute", "observability:read"})
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/integrations/api-keys"},
		{http.MethodPost, "/api/v1/approvals/1/approve"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer hk_live_good")
		if rec := serve(m, req, func(http.ResponseWriter, *http.Request) { t.Fatal("must not run") }); rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s: expected 403, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestApiKey_RejectedWhenIntegrationsDisabled(t *testing.T) {
	m, err := NewJWTAuthMiddleware(config.APIAuthConfig{Enabled: true, Secret: "api-test-secret", Issuer: "messaging-core", Audience: "hermes-app-go"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer hk_live_anything")
	if rec := serve(m, req, func(http.ResponseWriter, *http.Request) { t.Fatal("must not run") }); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestTaskVisibleToCaller_ConfinesApiKeyToItsProject(t *testing.T) {
	own := domain.NewProjectID()
	other := domain.NewProjectID()
	ctx := context.WithValue(context.Background(), authContextKey{}, AuthContext{TokenUse: "api_key", ProjectID: &own.Value})
	if !taskVisibleToCaller(ctx, domain.Task{ProjectID: &own}) {
		t.Fatal("own project's task must be visible")
	}
	if taskVisibleToCaller(ctx, domain.Task{ProjectID: &other}) || taskVisibleToCaller(ctx, domain.Task{}) {
		t.Fatal("other project's / unassigned task must be invisible to an API key")
	}
}

func TestRequiredScope_IntegrationsAlwaysManage(t *testing.T) {
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		if s := requiredScope(m, "/api/v1/integrations/webhooks/x/test"); s != "integrations:manage" {
			t.Fatalf("%s: got %q", m, s)
		}
	}
}
