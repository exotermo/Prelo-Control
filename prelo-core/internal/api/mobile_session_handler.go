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

// MobileSessionHandler is PR-2 (docs/integracoes/sessao-mobile.md): the Work Control app's login,
// rotating refresh and logout, plus listing/revoking devices from the web.
type MobileSessionHandler struct {
	auth     *application.DashboardAuthService
	sessions mobileSessionStore
}

type mobileSessionStore interface {
	FindByID(ctx context.Context, id uuid.UUID) (domain.MobileSession, error)
	ListActiveByUser(ctx context.Context, userID domain.DashboardUserID, now time.Time) ([]domain.MobileSession, error)
	Revoke(ctx context.Context, id uuid.UUID, reason string) error
	RevokeAllForUser(ctx context.Context, userID domain.DashboardUserID, reason string) (int, error)
}

func NewMobileSessionHandler(auth *application.DashboardAuthService, sessions mobileSessionStore) *MobileSessionHandler {
	return &MobileSessionHandler{auth: auth, sessions: sessions}
}

func RegisterMobileSessionRoutes(mux *http.ServeMux, h *MobileSessionHandler) {
	mux.HandleFunc("POST /api/v1/dashboard-auth/mobile/verify", h.Verify)
	mux.HandleFunc("POST /api/v1/dashboard-auth/mobile/refresh", h.Refresh)
	mux.HandleFunc("POST /api/v1/dashboard-auth/mobile/logout", h.Logout)
	mux.HandleFunc("GET /api/v1/me/sessions", h.ListMine)
	mux.HandleFunc("DELETE /api/v1/me/sessions/{sessionId}", h.RevokeMine)
	mux.HandleFunc("GET /api/v1/users/{userId}/sessions", h.ListForUser)
	mux.HandleFunc("DELETE /api/v1/users/{userId}/sessions", h.RevokeAllForUser)
}

type mobileTokensResponse struct {
	AccessToken      string `json:"accessToken"`
	ExpiresIn        int    `json:"expiresIn"`
	RefreshToken     string `json:"refreshToken"`
	RefreshExpiresAt string `json:"refreshExpiresAt"`
	SessionID        string `json:"sessionId"`
}

func mobileTokens(t application.MobileSessionTokens) mobileTokensResponse {
	return mobileTokensResponse{AccessToken: t.AccessToken, ExpiresIn: t.ExpiresIn, RefreshToken: t.RefreshToken,
		RefreshExpiresAt: t.RefreshExpiresAt.Format(time.RFC3339), SessionID: t.SessionID.String()}
}

func parseDeviceID(raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	return id, err == nil && id != uuid.Nil
}

func (h *MobileSessionHandler) Verify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Challenge  string `json:"challenge"`
		Code       string `json:"code"`
		DeviceID   string `json:"deviceId"`
		DeviceName string `json:"deviceName"`
		Platform   string `json:"platform"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	deviceID, ok := parseDeviceID(req.DeviceID)
	if !ok {
		writeError(w, &domain.ValidationError{Message: "deviceId must be a UUID generated once per installation"})
		return
	}
	tokens, err := h.auth.MobileVerify(r.Context(), req.Challenge, req.Code,
		application.MobileDevice{ID: deviceID, Name: req.DeviceName, Platform: strings.ToLower(strings.TrimSpace(req.Platform))})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mobileTokens(tokens))
}

func (h *MobileSessionHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
		DeviceID     string `json:"deviceId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	deviceID, _ := parseDeviceID(req.DeviceID)
	tokens, err := h.auth.MobileRefresh(r.Context(), req.RefreshToken, deviceID)
	switch {
	case errors.Is(err, application.ErrMobileRefreshReused):
		writeJSON(w, http.StatusUnauthorized, errorResponse{Code: "refresh_reused", Message: "this refresh token was already used; the session was ended for safety"})
	case err != nil:
		writeJSON(w, http.StatusUnauthorized, errorResponse{Code: "invalid_refresh", Message: "sign in again"})
	default:
		writeJSON(w, http.StatusOK, mobileTokens(tokens))
	}
}

func (h *MobileSessionHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
		DeviceID     string `json:"deviceId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	deviceID, _ := parseDeviceID(req.DeviceID)
	h.auth.MobileLogout(r.Context(), req.RefreshToken, deviceID)
	w.WriteHeader(http.StatusNoContent)
}

type mobileSessionResponse struct {
	ID         string `json:"id"`
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt"`
	ExpiresAt  string `json:"expiresAt"`
	Current    bool   `json:"current"`
}

func (h *MobileSessionHandler) list(w http.ResponseWriter, r *http.Request, userID domain.DashboardUserID, current string) {
	sessions, err := h.sessions.ListActiveByUser(r.Context(), userID, time.Now().UTC())
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]mobileSessionResponse, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, mobileSessionResponse{ID: s.ID.String(), DeviceName: s.DeviceName, Platform: s.Platform,
			CreatedAt: s.CreatedAt.Format(time.RFC3339), LastUsedAt: s.LastUsedAt.Format(time.RFC3339),
			ExpiresAt: s.IdleExpiresAt.Format(time.RFC3339), Current: s.ID.String() == current})
	}
	writeJSON(w, http.StatusOK, out)
}

func sessionUser(w http.ResponseWriter, r *http.Request) (AuthContext, domain.DashboardUserID, bool) {
	identity, ok := FromContext(r.Context())
	if !ok || identity.TokenUse != "dashboard" {
		writeAuthError(w, http.StatusForbidden, "a personal session is required")
		return AuthContext{}, domain.DashboardUserID{}, false
	}
	id, err := uuid.Parse(identity.Subject)
	if err != nil {
		writeAuthError(w, http.StatusForbidden, "a personal session is required")
		return AuthContext{}, domain.DashboardUserID{}, false
	}
	return identity, domain.DashboardUserID{Value: id}, true
}

func (h *MobileSessionHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	identity, userID, ok := sessionUser(w, r)
	if !ok {
		return
	}
	h.list(w, r, userID, identity.SessionID)
}

func (h *MobileSessionHandler) RevokeMine(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := sessionUser(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("sessionId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "sessionId must be a UUID"})
		return
	}
	session, err := h.sessions.FindByID(r.Context(), id)
	if err != nil || session.UserID != userID {
		writeJSON(w, http.StatusNotFound, errorResponse{Code: "session_not_found", Message: "session not found"})
		return
	}
	if err := h.sessions.Revoke(r.Context(), id, "revoked_by_user"); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathUser(w http.ResponseWriter, r *http.Request) (domain.DashboardUserID, bool) {
	id, err := uuid.Parse(r.PathValue("userId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "userId must be a UUID"})
		return domain.DashboardUserID{}, false
	}
	return domain.DashboardUserID{Value: id}, true
}

func (h *MobileSessionHandler) ListForUser(w http.ResponseWriter, r *http.Request) {
	if userID, ok := pathUser(w, r); ok {
		h.list(w, r, userID, "")
	}
}

func (h *MobileSessionHandler) RevokeAllForUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathUser(w, r)
	if !ok {
		return
	}
	n, err := h.sessions.RevokeAllForUser(r.Context(), userID, "revoked_by_admin")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"revoked": n})
}
