package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

// ServerHandler is Fase S1's whole surface — register/list/get/delete a server, and trigger one
// on-demand health check. Every route is gated to servers:manage (write) or servers:read (read)
// by requiredScope in auth.go, same mechanism as settings:manage/users:manage.
type ServerHandler struct {
	register    *application.RegisterServerUseCase
	checkHealth *application.CheckServerHealthUseCase
	servers     application.ServerRepository
}

func NewServerHandler(register *application.RegisterServerUseCase, checkHealth *application.CheckServerHealthUseCase, servers application.ServerRepository) *ServerHandler {
	return &ServerHandler{register: register, checkHealth: checkHealth, servers: servers}
}

func (h *ServerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createServerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	createdBy := ""
	if identity, ok := FromContext(r.Context()); ok {
		createdBy = identity.Subject
	}
	port := req.SSHPort
	if port == 0 {
		port = 22
	}
	var projectID *domain.ProjectID
	if raw := projectIdentity(r.Context()); raw != nil {
		id := domain.ProjectID{Value: *raw}
		projectID = &id
	}
	server, err := h.register.Register(r.Context(), req.Name, req.Host, port, req.SSHUser, []byte(req.PrivateKeyPEM), createdBy, projectID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, serverResponseFrom(server))
}

// List is project-scoped (Fase W) — projectIdentity(ctx) nil means the "unassigned" bucket.
func (h *ServerHandler) List(w http.ResponseWriter, r *http.Request) {
	servers, err := h.servers.ListByProject(r.Context(), projectIdentity(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	response := make([]serverResponse, 0, len(servers))
	for _, s := range servers {
		response = append(response, serverResponseFrom(s))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *ServerHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseServerID(w, r)
	if !ok {
		return
	}
	server, err := h.servers.FindByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, serverResponseFrom(server))
}

func (h *ServerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseServerID(w, r)
	if !ok {
		return
	}
	if err := h.servers.SoftDelete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ServerHandler) CheckHealth(w http.ResponseWriter, r *http.Request) {
	id, ok := parseServerID(w, r)
	if !ok {
		return
	}
	snapshot, err := h.checkHealth.Check(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, serverHealthResponseFrom(snapshot))
}

func parseServerID(w http.ResponseWriter, r *http.Request) (domain.ServerID, bool) {
	raw := r.PathValue("serverId")
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "serverId must be a valid UUID"})
		return domain.ServerID{}, false
	}
	return domain.ServerID{Value: id}, true
}

func serverResponseFrom(s domain.Server) serverResponse {
	var checkedAt *string
	if s.LastCheckedAt != nil {
		formatted := s.LastCheckedAt.Format(time.RFC3339)
		checkedAt = &formatted
	}
	resp := serverResponse{
		ID: s.ID.String(), Name: s.Name, Host: s.Host, SSHPort: s.SSHPort, SSHUser: s.SSHUser,
		HostKeyFingerprint: s.HostKeyFingerprint, LastStatus: string(s.LastStatus),
		LastCheckedAt: checkedAt, LastError: s.LastError,
		CreatedAt: s.CreatedAt.Format(time.RFC3339), CreatedBy: s.CreatedBy,
	}
	if s.ProjectID != nil {
		id := s.ProjectID.String()
		resp.ProjectID = &id
	}
	return resp
}

func serverHealthResponseFrom(snapshot application.ServerHealthSnapshot) serverHealthResponse {
	containers := make([]containerStatusResponse, 0, len(snapshot.Containers))
	for _, c := range snapshot.Containers {
		containers = append(containers, containerStatusResponse{ID: c.ID, Name: c.Name, Image: c.Image, Status: c.Status})
	}
	return serverHealthResponse{
		Status: string(snapshot.Status), Uptime: snapshot.Uptime,
		MemoryUsedMB: snapshot.MemoryUsedMB, MemoryTotalMB: snapshot.MemoryTotalMB,
		DiskUsedPercent: snapshot.DiskUsedPercent, Containers: containers, Error: snapshot.Error,
	}
}
