package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// ProjectHandler is Fase W's whole surface — create/list/delete a project, and manage its
// membership. List/Create/Delete are gated to projects:read/projects:manage by requiredScope in
// auth.go; membership routes are all projects:manage (ADMIN only, this first version — see the
// plan's "fora de escopo").
type ProjectHandler struct {
	projects application.ProjectRepository
	members  application.ProjectMemberRepository
	agents   agentCatalog
	files    projectFilePurger
}

func NewProjectHandler(projects application.ProjectRepository, members application.ProjectMemberRepository) *ProjectHandler {
	return &ProjectHandler{projects: projects, members: members}
}

// List returns every project an ADMIN (projects:manage) may administer, or only the ones an
// OPERATOR is a member of — the two sources of truth JWTAuthMiddleware.resolveProject itself
// uses to decide project access, surfaced here as the project card grid's data.
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	identity, ok := FromContext(r.Context())
	if !ok {
		writeError(w, &domain.ValidationError{Message: "authentication required"})
		return
	}

	var projects []domain.Project
	var err error
	if identity.HasScope("projects:manage") {
		projects, err = h.projects.List(r.Context())
	} else {
		userID, parseErr := uuid.Parse(identity.Subject)
		if parseErr != nil {
			writeError(w, &domain.ValidationError{Message: "invalid session subject"})
			return
		}
		projects, err = h.members.ListProjectsForUser(r.Context(), domain.DashboardUserID{Value: userID})
	}
	if err != nil {
		writeError(w, err)
		return
	}

	response := make([]projectResponse, 0, len(projects))
	for _, p := range projects {
		memberCount := 0
		members, err := h.members.ListMembers(r.Context(), p.ID)
		if err == nil {
			memberCount = len(members)
		}
		response = append(response, projectResponseFrom(p, memberCount))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	createdBy := ""
	if identity, ok := FromContext(r.Context()); ok {
		createdBy = identity.Subject
	}
	project, err := domain.NewProject(req.Name, req.Description, createdBy)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := h.projects.Insert(r.Context(), project); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectResponseFrom(project, 0))
}

func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	if err := h.projects.SoftDelete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	if h.files != nil {
		if err := h.files.PurgeProject(r.Context(), id); err != nil {
			log.Printf("projects: purging files of deleted project %s failed: %v", id, err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProjectHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	members, err := h.members.ListMembers(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]projectMemberResponse, 0, len(members))
	for _, m := range members {
		response = append(response, projectMemberResponse{UserID: m.UserID.String(), AddedAt: m.AddedAt.Format(time.RFC3339), AddedBy: m.AddedBy})
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *ProjectHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	var req addProjectMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	userID, err := uuid.Parse(req.DashboardUserID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "dashboardUserId must be a valid UUID"})
		return
	}
	addedBy := ""
	if identity, ok := FromContext(r.Context()); ok {
		addedBy = identity.Subject
	}
	member := domain.NewProjectMember(id, domain.DashboardUserID{Value: userID}, addedBy)
	if err := h.members.Add(r.Context(), member); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProjectHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	rawUserID := r.PathValue("userId")
	userID, err := uuid.Parse(rawUserID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "userId must be a valid UUID"})
		return
	}
	if err := h.members.Remove(r.Context(), id, domain.DashboardUserID{Value: userID}); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseProjectID(w http.ResponseWriter, r *http.Request) (domain.ProjectID, bool) {
	raw := r.PathValue("projectId")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "projectId must be a valid UUID"})
		return domain.ProjectID{}, false
	}
	return domain.ProjectID{Value: id}, true
}

func projectResponseFrom(p domain.Project, memberCount int) projectResponse {
	return projectResponse{
		ID: p.ID.String(), Name: p.Name, Description: p.Description,
		CreatedAt: p.CreatedAt.Format(time.RFC3339), CreatedBy: p.CreatedBy,
		MemberCount: memberCount, DefaultAgentID: p.DefaultAgentID, Instructions: p.Instructions, CoverColor: p.CoverColor,
	}
}

// agentCatalog is the read side of the agent registry the settings screen needs.
type agentCatalog interface {
	List() []domain.AgentDefinition
	Find(agentID domain.AgentID) (domain.AgentDefinition, bool)
}

// projectFilePurger removes a deleted project's sealed files from disk (Fase PA).
type projectFilePurger interface {
	PurgeProject(ctx context.Context, projectID domain.ProjectID) error
}

// SetSettingsDependencies wires Fase PA: the agent catalog (to validate a default agent) and the
// file service (to purge a deleted project's files). Both optional.
func (h *ProjectHandler) SetSettingsDependencies(agents agentCatalog, files projectFilePurger) {
	h.agents = agents
	h.files = files
}

// Update edits a project's settings (projects:manage).
func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	var req updateProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DefaultAgentID != nil && *req.DefaultAgentID != "" {
		if h.agents == nil {
			writeError(w, &domain.ValidationError{Message: "agent catalog unavailable"})
			return
		}
		if _, found := h.agents.Find(domain.AgentID{Value: *req.DefaultAgentID}); !found {
			writeError(w, &domain.ValidationError{Message: "unknown agent: " + *req.DefaultAgentID})
			return
		}
	}
	project, err := h.projects.FindByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	updated, err := project.WithSettings(req.Name, req.Description, req.DefaultAgentID, req.Instructions, req.CoverColor)
	if err != nil {
		writeError(w, err)
		return
	}
	saved, err := h.projects.Update(r.Context(), updated)
	if err != nil {
		writeError(w, err)
		return
	}
	members, _ := h.members.ListMembers(r.Context(), saved.ID)
	writeJSON(w, http.StatusOK, projectResponseFrom(saved, len(members)))
}

// Agents lists the agent catalog (id, name, description) for pickers.
func (h *ProjectHandler) Agents(w http.ResponseWriter, r *http.Request) {
	type agentResponse struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	out := []agentResponse{}
	if h.agents != nil {
		for _, a := range h.agents.List() {
			out = append(out, agentResponse{ID: a.AgentID.String(), Name: a.Name, Description: a.Description})
		}
	}
	writeJSON(w, http.StatusOK, out)
}
