package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// UserManagementHandler is the Usuários page's whole surface — list accounts, invite a new one
// (reusing DashboardAuthService.Invite, the same machinery the bootstrap endpoint uses), and
// change a role. Every route here is gated to users:manage (ADMIN only, see
// dashboardAdminScopes) by requiredScope in auth.go, registered alongside everything else in
// router.go as an ordinary protected route — no bespoke auth like the bootstrap endpoint's
// X-Admin-Token, since an ADMIN session already proves who's asking.
type UserManagementHandler struct {
	service *application.DashboardAuthService
}

func NewUserManagementHandler(service *application.DashboardAuthService) *UserManagementHandler {
	return &UserManagementHandler{service: service}
}

func (h *UserManagementHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.service.ListUsers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]dashboardUserResponse, 0, len(users))
	for _, u := range users {
		response = append(response, dashboardUserResponseFrom(u))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *UserManagementHandler) Invite(w http.ResponseWriter, r *http.Request) {
	var req userInviteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	role := domain.DashboardRoleOperator
	if req.Role != nil && *req.Role != "" {
		role = domain.DashboardRole(*req.Role)
	}
	if err := h.service.Invite(r.Context(), req.Email, role); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *UserManagementHandler) ChangeRole(w http.ResponseWriter, r *http.Request) {
	rawID := r.PathValue("userId")
	id, err := uuid.Parse(rawID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "userId must be a valid UUID"})
		return
	}
	var req changeRoleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	identity, ok := FromContext(r.Context())
	if !ok {
		writeError(w, &domain.ValidationError{Message: "dashboard session is required"})
		return
	}
	actorID, err := uuid.Parse(identity.Subject)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "dashboard session subject is not a valid user id"})
		return
	}
	user, err := h.service.ChangeRole(r.Context(),
		domain.DashboardUserID{Value: actorID}, domain.DashboardUserID{Value: id}, domain.DashboardRole(req.Role))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dashboardUserResponseFrom(user))
}

func dashboardUserResponseFrom(u domain.DashboardUser) dashboardUserResponse {
	var activatedAt *string
	if u.ActivatedAt != nil {
		formatted := u.ActivatedAt.Format(time.RFC3339)
		activatedAt = &formatted
	}
	return dashboardUserResponse{
		ID: u.ID.String(), Email: u.Email, Role: string(u.Role),
		ActivatedAt: activatedAt, TOTPEnabled: u.TOTPEnabled, CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}
