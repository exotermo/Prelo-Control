package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakePushStore struct {
	tokens map[uuid.UUID]string
}

func (f *fakePushStore) SetPushToken(_ context.Context, id uuid.UUID, token string, _ time.Time) error {
	f.tokens[id] = token
	return nil
}
func (f *fakePushStore) ClearPushToken(_ context.Context, id uuid.UUID, _ time.Time) error {
	delete(f.tokens, id)
	return nil
}

func TestPushTokenOnlyForAppSessions(t *testing.T) {
	store := &fakePushStore{tokens: map[uuid.UUID]string{}}
	mux := http.NewServeMux()
	RegisterPushRoutes(mux, NewPushHandler(store, false))
	user := uuid.New().String()
	sid := uuid.New()

	web := withIdentity(httptest.NewRequest(http.MethodPut, "/api/v1/me/push-token", strings.NewReader(`{"token":"abc"}`)),
		AuthContext{Subject: user, TokenUse: "dashboard"})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, web)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "mobile_session_required") {
		t.Fatalf("web session: %d %s", rec.Code, rec.Body)
	}

	app := AuthContext{Subject: user, TokenUse: "dashboard", SessionID: sid.String()}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, withIdentity(httptest.NewRequest(http.MethodPut, "/api/v1/me/push-token", strings.NewReader(`{"token":"bad token"}`)), app))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("token with spaces: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, withIdentity(httptest.NewRequest(http.MethodPut, "/api/v1/me/push-token", strings.NewReader(`{"token":"fcm-123"}`)), app))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pushEnabled":false`) || store.tokens[sid] != "fcm-123" {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, withIdentity(httptest.NewRequest(http.MethodDelete, "/api/v1/me/push-token", nil), app))
	if rec.Code != http.StatusNoContent || store.tokens[sid] != "" {
		t.Fatalf("unregister: %d", rec.Code)
	}
}
