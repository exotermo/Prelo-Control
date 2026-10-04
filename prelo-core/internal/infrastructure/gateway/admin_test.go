package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestAdminClient_UsesAdminScopeOnlyAndForwardsBody(t *testing.T) {
	var gotScope []any
	var gotBody, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		claims := jwt.MapClaims{}
		_, _ = jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return []byte("s3cret"), nil })
		gotScope, _ = claims["scope"].([]any)
		b, _ := io.ReadAll(r.Body)
		gotBody, gotPath = string(b), r.URL.Path
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewAdminClient(Config{BaseURL: srv.URL, JWTSecret: "s3cret", Issuer: "i", Audience: "a"})
	status, body, err := c.Do(context.Background(), http.MethodPost, "/test", []byte(`{"apiKey":"k"}`))
	if err != nil || status != 200 || string(body) != `{"ok":true}` {
		t.Fatalf("Do: %d %s %v", status, body, err)
	}
	if len(gotScope) != 1 || gotScope[0] != "llm:admin" {
		t.Fatalf("admin token must carry exactly llm:admin, got %v", gotScope)
	}
	if gotPath != "/api/v1/admin/connections/test" || gotBody != `{"apiKey":"k"}` {
		t.Fatalf("unexpected relay %s %s", gotPath, gotBody)
	}
}
