package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/exotermo/hermes-app-go/internal/config"
)

func testAuth(t *testing.T) *JWTAuthMiddleware {
	t.Helper()
	middleware, err := NewJWTAuthMiddleware(config.APIAuthConfig{Enabled: true, Secret: "api-test-secret", Issuer: "messaging-core", Audience: "hermes-app-go", ClockSkewSeconds: 0})
	if err != nil {
		t.Fatal(err)
	}
	return middleware
}

func signedToken(t *testing.T, mutate func(jwt.MapClaims)) string {
	t.Helper()
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub":            "hermes-agent",
		"tenant_id":      "00000000-0000-0000-0000-000000000001",
		"integration_id": "integration-1",
		"token_use":      "integration",
		"scope":          []string{"tasks:create", "tasks:read", "chat:use"},
		"iss":            "messaging-core",
		"aud":            []string{"hermes-app-go"},
		"iat":            now.Unix(),
		"exp":            now.Add(time.Minute).Unix(),
	}
	mutate(claims)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	value, err := token.SignedString([]byte("api-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestJWTAuthMiddlewareAcceptsValidTokenAndScope(t *testing.T) {
	middleware := testAuth(t)
	handler := middleware.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := FromContext(r.Context())
		if !ok || identity.TenantID != "00000000-0000-0000-0000-000000000001" {
			t.Errorf("missing identity: %+v", identity)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(`{"description":"x"}`))
	req.Header.Set("Authorization", "Bearer "+signedToken(t, func(jwt.MapClaims) {}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestJWTAuthMiddlewareRejectsMissingAndInvalidTokens(t *testing.T) {
	middleware := testAuth(t)
	handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler must not run") }))
	for name, token := range map[string]string{"missing": "", "invalid": "Bearer not-a-token"} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
			if token != "" {
				req.Header.Set("Authorization", token)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", rec.Code)
			}
		})
	}
}

func TestJWTAuthMiddlewareRejectsWrongIssuerAudienceAndExpiry(t *testing.T) {
	middleware := testAuth(t)
	handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler must not run") }))
	cases := map[string]func(jwt.MapClaims){
		"issuer":   func(c jwt.MapClaims) { c["iss"] = "other" },
		"audience": func(c jwt.MapClaims) { c["aud"] = []string{"other"} },
		"expired":  func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
			req.Header.Set("Authorization", "Bearer "+signedToken(t, mutate))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", rec.Code)
			}
		})
	}
}

func TestJWTAuthMiddlewareRejectsMissingScope(t *testing.T) {
	middleware := testAuth(t)
	handler := middleware.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler must not run") }))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+signedToken(t, func(c jwt.MapClaims) { c["scope"] = []string{"tasks:read"} }))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRequiredScopeRouteMatrix(t *testing.T) {
	cases := map[string]string{
		"GET /api/v1/tasks/1":                         "tasks:read",
		"POST /api/v1/tasks":                          "tasks:create",
		"POST /api/v1/tasks/1/execute":                "tasks:execute",
		"POST /api/v1/hermes/chat":                    "chat:use",
		"GET /api/v1/tools":                           "tools:read",
		"POST /api/v1/executions/1/tools/echo/invoke": "tools:invoke",
		"GET /api/v1/approvals":                       "approvals:read",
		"POST /api/v1/approvals/1/approve":            "approvals:decide",
		"GET /api/v1/tasks/1/tree":                    "observability:read",
	}
	for route, expected := range cases {
		parts := strings.SplitN(route, " ", 2)
		if actual := requiredScope(parts[0], parts[1]); actual != expected {
			t.Errorf("%s: got %q, want %q", route, actual, expected)
		}
	}
}
