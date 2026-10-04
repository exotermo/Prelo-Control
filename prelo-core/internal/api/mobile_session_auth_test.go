package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/config"
	"github.com/exotermo/prelo-core/internal/domain"
)

type fakeSessionChecker struct {
	sessions map[uuid.UUID]domain.MobileSession
}

func (f fakeSessionChecker) FindByID(_ context.Context, id uuid.UUID) (domain.MobileSession, error) {
	s, ok := f.sessions[id]
	if !ok {
		return domain.MobileSession{}, application.ErrMobileSessionNotFound
	}
	return s, nil
}

func appToken(t *testing.T, secret, sub, sid string) string {
	t.Helper()
	claims := jwt.MapClaims{"sub": sub, "token_use": "dashboard", "scope": []string{"projects:read"},
		"iss": "iss", "aud": "aud", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
	if sid != "" {
		claims["sid"] = sid
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// PR-2: an app access token dies with its session (logout, revocation, refresh reuse) instead of
// living out its 15 minutes; a web token (no sid) is unaffected.
func TestAppTokensAreCheckedAgainstTheirSession(t *testing.T) {
	secret := "s3cret-s3cret-s3cret-s3cret-s3cret"
	mw, err := NewJWTAuthMiddleware(config.APIAuthConfig{Enabled: true, Secret: secret, Issuer: "iss", Audience: "aud"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	user := uuid.New()
	now := time.Now().UTC()
	active, _ := domain.NewMobileSession(domain.DashboardUserID{Value: user}, uuid.New(), "a", "android", now)
	revoked, _ := domain.NewMobileSession(domain.DashboardUserID{Value: user}, uuid.New(), "b", "android", now)
	revoked.RevokedAt = &now
	otherUser, _ := domain.NewMobileSession(domain.DashboardUserID{Value: uuid.New()}, uuid.New(), "c", "android", now)
	checker := fakeSessionChecker{sessions: map[uuid.UUID]domain.MobileSession{active.ID: active, revoked.ID: revoked, otherUser.ID: otherUser}}

	ok := mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	call := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		ok.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := call(appToken(t, secret, user.String(), active.ID.String())); code != http.StatusUnauthorized {
		t.Fatalf("a session-bound token with no checker wired must be refused, got %d", code)
	}
	mw.SetMobileSessions(checker)
	cases := map[string]struct {
		token string
		want  int
	}{
		"web token (no sid)":      {appToken(t, secret, user.String(), ""), http.StatusOK},
		"active app session":      {appToken(t, secret, user.String(), active.ID.String()), http.StatusOK},
		"revoked app session":     {appToken(t, secret, user.String(), revoked.ID.String()), http.StatusUnauthorized},
		"unknown session":         {appToken(t, secret, user.String(), uuid.NewString()), http.StatusUnauthorized},
		"session of another user": {appToken(t, secret, user.String(), otherUser.ID.String()), http.StatusUnauthorized},
	}
	for name, c := range cases {
		if got := call(c.token); got != c.want {
			t.Errorf("%s: got %d want %d", name, got, c.want)
		}
	}
	for route, want := range map[string]string{
		"GET /api/v1/me/sessions":         "projects:read",
		"DELETE /api/v1/me/sessions/x":    "projects:read",
		"GET /api/v1/users/1/sessions":    "users:manage",
		"DELETE /api/v1/users/1/sessions": "users:manage",
	} {
		var method, path string
		for i := range route {
			if route[i] == ' ' {
				method, path = route[:i], route[i+1:]
				break
			}
		}
		if got := requiredScope(method, path); got != want {
			t.Errorf("%s: got %q want %q", route, got, want)
		}
	}
}
