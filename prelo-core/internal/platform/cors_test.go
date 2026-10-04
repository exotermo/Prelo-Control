package platform

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORS_AllowedOrigin_GetsCredentialedHeaders(t *testing.T) {
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
		[]string{"http://127.0.0.1:5176"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5176")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// A credentialed fetch() (credentials: "include", used by refresh/logout for the session
	// cookie) is silently blocked by the browser unless Allow-Origin echoes the exact origin
	// (never "*") and Allow-Credentials is "true" — this is the regression that produced
	// "NetworkError when attempting to fetch resource" against the wildcard policy.
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:5176" {
		t.Fatalf("expected the exact origin to be echoed back, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected Allow-Credentials: true, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got == "*" {
		t.Fatal("must never combine a credentialed response with a wildcard origin")
	}
}

func TestCORS_UnknownOrigin_GetsNoOriginHeaderAtAll(t *testing.T) {
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }),
		[]string{"http://127.0.0.1:5176"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Allow-Origin header for an origin outside the allowlist, got %q", got)
	}
}

func TestCORS_PreflightRequest_AllowsTheDashboardMarkerHeader(t *testing.T) {
	called := false
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }),
		[]string{"http://127.0.0.1:5176"})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/dashboard-auth/refresh", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5176")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for the preflight, got %d", rec.Code)
	}
	if called {
		t.Fatal("the wrapped handler must not run for a preflight OPTIONS request")
	}
	allowedHeaders := rec.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowedHeaders, "X-Dashboard-Request") {
		t.Fatalf("expected X-Dashboard-Request in Allow-Headers (the CSRF marker on refresh/logout), got %q", allowedHeaders)
	}
	// Every project-scoped dashboard call (Fase W) carries X-Project-Id; without it in the
	// preflight allowlist the browser blocks them all (curl-only tests never notice).
	if !strings.Contains(allowedHeaders, "X-Project-Id") {
		t.Fatalf("expected X-Project-Id in Allow-Headers, got %q", allowedHeaders)
	}
}
