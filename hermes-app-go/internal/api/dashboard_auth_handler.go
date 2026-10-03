package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

// dashboardRefreshCookie carries the long-lived refresh token — HttpOnly so client-side script
// can never read it, scoped to the auth routes only, SameSite=Lax as baseline CSRF defense.
// dashboardMarkerHeader is the second CSRF layer, mirroring messaging-core's
// X-Dashboard-Request: a cross-site form post can set cookies on this origin but cannot add a
// custom header, so any endpoint that consumes the cookie also requires this header present.
const dashboardRefreshCookie = "hermes_dashboard_refresh"
const dashboardMarkerHeader = "X-Dashboard-Request"

type DashboardAuthHandler struct {
	service      *application.DashboardAuthService
	secureCookie bool
}

func NewDashboardAuthHandler(service *application.DashboardAuthService, secureCookie bool) *DashboardAuthHandler {
	return &DashboardAuthHandler{service: service, secureCookie: secureCookie}
}

func (h *DashboardAuthHandler) Activate(w http.ResponseWriter, r *http.Request) {
	var req dashboardActivateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.service.Activate(r.Context(), req.Token, req.Password); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *DashboardAuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req dashboardLoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	challenge, err := h.service.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dashboardChallengeResponse{Challenge: challenge.Challenge, NextStep: challenge.NextStep})
}

func (h *DashboardAuthHandler) MfaSetup(w http.ResponseWriter, r *http.Request) {
	var req dashboardChallengeCodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	setup, err := h.service.SetupTotp(r.Context(), req.Challenge)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dashboardTotpSetupResponse{Secret: setup.Secret, OtpauthURI: setup.OtpauthURI})
}

func (h *DashboardAuthHandler) MfaConfirm(w http.ResponseWriter, r *http.Request) {
	var req dashboardChallengeCodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	session, err := h.service.ConfirmTotp(r.Context(), req.Challenge, req.Code)
	if err != nil {
		writeError(w, err)
		return
	}
	h.respondWithSession(w, session)
}

func (h *DashboardAuthHandler) MfaVerify(w http.ResponseWriter, r *http.Request) {
	var req dashboardChallengeCodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	session, err := h.service.VerifyTotp(r.Context(), req.Challenge, req.Code)
	if err != nil {
		writeError(w, err)
		return
	}
	h.respondWithSession(w, session)
}

func (h *DashboardAuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if !h.requireMarker(w, r) {
		return
	}
	cookie, err := r.Cookie(dashboardRefreshCookie)
	if err != nil || cookie.Value == "" {
		writeError(w, application.ErrDashboardTokenNotFound)
		return
	}
	session, err := h.service.Refresh(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, err)
		return
	}
	h.respondWithSession(w, session)
}

func (h *DashboardAuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if !h.requireMarker(w, r) {
		return
	}
	if cookie, err := r.Cookie(dashboardRefreshCookie); err == nil {
		h.service.Logout(r.Context(), cookie.Value)
	}
	h.clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *DashboardAuthHandler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req dashboardEmailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.service.RequestPasswordReset(r.Context(), req.Email); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *DashboardAuthHandler) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req dashboardActivateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.service.ResetPassword(r.Context(), req.Token, req.Password); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *DashboardAuthHandler) respondWithSession(w http.ResponseWriter, session application.DashboardSession) {
	h.setRefreshCookie(w, session.RefreshToken)
	recoveryCodes := session.RecoveryCodes
	if recoveryCodes == nil {
		recoveryCodes = []string{}
	}
	writeJSON(w, http.StatusOK, dashboardSessionResponse{AccessToken: session.AccessToken, ExpiresIn: session.ExpiresIn, RecoveryCodes: recoveryCodes})
}

func (h *DashboardAuthHandler) setRefreshCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name: dashboardRefreshCookie, Value: value, Path: "/api/v1/dashboard-auth",
		HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 60 * 60,
	})
}

func (h *DashboardAuthHandler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: dashboardRefreshCookie, Value: "", Path: "/api/v1/dashboard-auth",
		HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

func (h *DashboardAuthHandler) requireMarker(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get(dashboardMarkerHeader) == "" {
		writeError(w, &domain.ValidationError{Message: "missing " + dashboardMarkerHeader + " header"})
		return false
	}
	return true
}

// DashboardBootstrapHandler creates the very first (and any subsequent) invited user. Gated by
// a shared secret compared in constant time — there is no logged-in user to authorize this via
// the normal dashboard session, same bootstrap shape as messaging-core's
// DashboardBootstrapController / hermes-messaging-bridge's AdminController.
type DashboardBootstrapHandler struct {
	service    *application.DashboardAuthService
	adminToken string
}

func NewDashboardBootstrapHandler(service *application.DashboardAuthService, adminToken string) *DashboardBootstrapHandler {
	return &DashboardBootstrapHandler{service: service, adminToken: adminToken}
}

func (h *DashboardBootstrapHandler) Invite(w http.ResponseWriter, r *http.Request) {
	provided := r.Header.Get("X-Admin-Token")
	if h.adminToken == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(h.adminToken)) != 1 {
		writeError(w, &domain.ValidationError{Message: "invalid or missing X-Admin-Token"})
		return
	}
	var req dashboardInviteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	// Always ADMIN: this endpoint exists only to bootstrap the very first account, before any
	// session exists to invite anyone with a deliberately chosen role.
	if err := h.service.Invite(r.Context(), req.Email, domain.DashboardRoleAdmin); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid request body"})
		return false
	}
	return true
}

func mapDashboardAuthError(err error) (status int, code string, message string, ok bool) {
	switch {
	case errors.Is(err, application.ErrDashboardUserAlreadyExists):
		return http.StatusConflict, "user_already_exists", "a user with this email already exists", true
	case errors.Is(err, application.ErrDashboardUserNotFound), errors.Is(err, application.ErrDashboardTokenNotFound):
		return http.StatusUnauthorized, "invalid_or_expired_token", "this link or code is invalid or has expired", true
	case errors.Is(err, application.ErrDashboardTokenLimitExceeded):
		return http.StatusUnauthorized, "invalid_or_expired_token", "too many attempts; start over", true
	case errors.Is(err, application.ErrDashboardAccountLocked):
		return http.StatusLocked, "account_locked", "too many failed attempts; try again later", true
	case errors.Is(err, application.ErrDashboardRateLimited):
		return http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later", true
	case errors.Is(err, application.ErrDashboardInvalidCredentials):
		return http.StatusUnauthorized, "invalid_credentials", "invalid email or password", true
	case errors.Is(err, application.ErrDashboardInvalidCode):
		return http.StatusUnauthorized, "invalid_code", "invalid or expired code", true
	case errors.Is(err, application.ErrDashboardTotpAlreadyEnabled):
		return http.StatusConflict, "totp_already_enabled", "two-factor authentication is already enabled", true
	case errors.Is(err, application.ErrDashboardCannotChangeOwnRole):
		return http.StatusUnprocessableEntity, "cannot_change_own_role", "you cannot change your own role", true
	}
	return 0, "", "", false
}
