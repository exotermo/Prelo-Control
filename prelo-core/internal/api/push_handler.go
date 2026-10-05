package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// PushHandler is G11 (docs/integracoes/push.md): the app registers its FCM token on its own session.
// Only app sessions (sid claim) — a web session has no device to push to.
type PushHandler struct {
	store   pushTokenStore
	enabled bool
}

type pushTokenStore interface {
	SetPushToken(ctx context.Context, sessionID uuid.UUID, token string, now time.Time) error
	ClearPushToken(ctx context.Context, sessionID uuid.UUID, now time.Time) error
}

func NewPushHandler(store pushTokenStore, enabled bool) *PushHandler {
	return &PushHandler{store: store, enabled: enabled}
}

func RegisterPushRoutes(mux *http.ServeMux, h *PushHandler) {
	mux.HandleFunc("PUT /api/v1/me/push-token", h.Register)
	mux.HandleFunc("DELETE /api/v1/me/push-token", h.Unregister)
}

const maxPushTokenLen = 4096

type pushStatusResponse struct {
	Registered  bool `json:"registered"`
	PushEnabled bool `json:"pushEnabled"` // false = server has no FCM credentials yet; keep the token anyway
}

func appSessionID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	identity, _, ok := sessionUser(w, r)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(identity.SessionID)
	if err != nil {
		writeJSON(w, http.StatusForbidden, errorResponse{Code: "mobile_session_required", Message: "push tokens belong to an app session"})
		return uuid.Nil, false
	}
	return id, true
}

func (h *PushHandler) Register(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := appSessionID(w, r)
	if !ok {
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" || len(token) > maxPushTokenLen || strings.ContainsAny(token, " \t\r\n") {
		writeError(w, &domain.ValidationError{Message: "token must be the FCM registration token"})
		return
	}
	if err := h.store.SetPushToken(r.Context(), sessionID, token, time.Now().UTC()); err != nil {
		if errors.Is(err, application.ErrMobileSessionNotFound) {
			writeAuthError(w, http.StatusUnauthorized, "session ended")
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pushStatusResponse{Registered: true, PushEnabled: h.enabled})
}

func (h *PushHandler) Unregister(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := appSessionID(w, r)
	if !ok {
		return
	}
	if err := h.store.ClearPushToken(r.Context(), sessionID, time.Now().UTC()); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
