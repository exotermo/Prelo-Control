package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

// requireProjectMember resolves {projectId}, confirms the project exists and that the caller is a
// member of it — or holds projects:manage (ADMIN), the same rule X-Project-Id resolution applies.
// On failure it has already written the response.
func requireProjectMember(w http.ResponseWriter, r *http.Request, projects projectFinder, members projectMembershipChecker) (domain.Project, AuthContext, bool) {
	id, err := uuid.Parse(r.PathValue("projectId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "projectId must be a valid UUID"})
		return domain.Project{}, AuthContext{}, false
	}
	project, err := projects.FindByID(r.Context(), domain.ProjectID{Value: id})
	if err != nil {
		writeError(w, err)
		return domain.Project{}, AuthContext{}, false
	}
	identity, _ := FromContext(r.Context())
	if !identity.HasScope("projects:manage") {
		userID, err := uuid.Parse(identity.Subject)
		member := false
		if err == nil {
			member, err = members.IsMember(r.Context(), project.ID, domain.DashboardUserID{Value: userID})
		}
		if err != nil || !member {
			writeAuthError(w, http.StatusForbidden, "the token does not grant this operation")
			return domain.Project{}, AuthContext{}, false
		}
	}
	return project, identity, true
}
