package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// WorkspaceHandler is Fase C1's HTTP surface: clients (CRM), search, the home screen (recent +
// pending) and a client's timeline. Scopes are mapped in requiredScope (clients:read,
// clients:manage, clients:delete); project visibility is applied by WorkspaceService.
type WorkspaceHandler struct {
	service *application.WorkspaceService
	clients application.ClientRepository
}

func NewWorkspaceHandler(service *application.WorkspaceService, clients application.ClientRepository) *WorkspaceHandler {
	return &WorkspaceHandler{service: service, clients: clients}
}

func RegisterWorkspaceRoutes(mux *http.ServeMux, h *WorkspaceHandler) {
	mux.HandleFunc("GET /api/v1/clients", h.ListClients)
	mux.HandleFunc("POST /api/v1/clients", h.CreateClient)
	mux.HandleFunc("GET /api/v1/clients/{clientId}", h.GetClient)
	mux.HandleFunc("PATCH /api/v1/clients/{clientId}", h.UpdateClient)
	mux.HandleFunc("DELETE /api/v1/clients/{clientId}", h.DeleteClient)
	mux.HandleFunc("POST /api/v1/clients/{clientId}/contacts", h.AddContact)
	mux.HandleFunc("DELETE /api/v1/clients/{clientId}/contacts/{contactId}", h.RemoveContact)
	mux.HandleFunc("GET /api/v1/clients/{clientId}/timeline", h.Timeline)
	mux.HandleFunc("PUT /api/v1/projects/{projectId}/client", h.SetProjectClient)
	mux.HandleFunc("GET /api/v1/search", h.Search)
	mux.HandleFunc("GET /api/v1/home", h.Home)
	mux.HandleFunc("POST /api/v1/recent", h.TouchRecent)
}

// --- request / response shapes ---

type contactRequest struct {
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	IsPrimary bool   `json:"isPrimary"`
}

type clientRequest struct {
	Version  int64            `json:"version"`
	Name     string           `json:"name"`
	Company  *string          `json:"company"`
	Status   string           `json:"status"`
	Stage    string           `json:"stage"`
	Address  *string          `json:"address"`
	City     *string          `json:"city"`
	Website  *string          `json:"website"`
	Notes    *string          `json:"notes"`
	Contacts []contactRequest `json:"contacts"`
}

func (r clientRequest) fields() domain.ClientFields {
	return domain.ClientFields{
		Name: r.Name, Company: r.Company, Status: domain.ClientStatus(r.Status), Stage: domain.ClientStage(r.Stage),
		Address: r.Address, City: r.City, Website: r.Website, Notes: r.Notes,
	}
}

type contactResponse struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	IsPrimary bool   `json:"isPrimary"`
}

type clientResponse struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Company        *string           `json:"company"`
	Status         string            `json:"status"`
	Stage          string            `json:"stage"`
	Source         string            `json:"source"`
	Address        *string           `json:"address"`
	City           *string           `json:"city"`
	Website        *string           `json:"website"`
	Notes          *string           `json:"notes"`
	OptedOutAt     *string           `json:"optedOutAt"`
	CreatedAt      string            `json:"createdAt"`
	UpdatedAt      string            `json:"updatedAt"`
	Version        int64             `json:"version"`
	PrimaryContact *contactResponse  `json:"primaryContact,omitempty"`
	ProjectCount   *int              `json:"projectCount,omitempty"`
	Contacts       []contactResponse `json:"contacts,omitempty"`
	Projects       []projectResponse `json:"projects,omitempty"`
}

func contactResponseFrom(c domain.ClientContact) contactResponse {
	return contactResponse{ID: c.ID.String(), Kind: string(c.Kind), Value: c.Value, IsPrimary: c.IsPrimary}
}

func clientResponseFrom(c domain.Client) clientResponse {
	out := clientResponse{
		ID: c.ID.String(), Name: c.Name, Company: c.Company, Status: string(c.Status), Stage: string(c.Stage),
		Source: string(c.Source), Address: c.Address, City: c.City, Website: c.Website, Notes: c.Notes,
		CreatedAt: c.CreatedAt.Format(time.RFC3339), UpdatedAt: c.UpdatedAt.Format(time.RFC3339), Version: c.Version,
	}
	if c.OptedOutAt != nil {
		s := c.OptedOutAt.Format(time.RFC3339)
		out.OptedOutAt = &s
	}
	return out
}

// --- helpers ---

func viewerFrom(w http.ResponseWriter, r *http.Request) (application.Viewer, bool) {
	identity, ok := FromContext(r.Context())
	if !ok {
		writeAuthError(w, http.StatusUnauthorized, "authentication required")
		return application.Viewer{}, false
	}
	userID, err := uuid.Parse(identity.Subject)
	if err != nil {
		writeAuthError(w, http.StatusForbidden, "a dashboard session is required")
		return application.Viewer{}, false
	}
	return application.Viewer{UserID: domain.DashboardUserID{Value: userID}, AllProjects: identity.HasScope("projects:manage")}, true
}

func parseClientID(w http.ResponseWriter, r *http.Request) (domain.ClientID, bool) {
	id, err := uuid.Parse(r.PathValue("clientId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "clientId must be a valid UUID"})
		return domain.ClientID{}, false
	}
	return domain.ClientID{Value: id}, true
}

func uuidString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

// --- clients ---

func (h *WorkspaceHandler) ListClients(w http.ResponseWriter, r *http.Request) {
	filter := application.ClientListFilter{}
	if raw := r.URL.Query().Get("status"); raw != "" {
		st := domain.ClientStatus(strings.ToUpper(raw))
		filter.Status = &st
	}
	summaries, err := h.clients.List(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]clientResponse, 0, len(summaries))
	for _, s := range summaries {
		resp := clientResponseFrom(s.Client)
		count := s.ProjectCount
		resp.ProjectCount = &count
		if s.PrimaryContact != nil {
			c := contactResponseFrom(*s.PrimaryContact)
			resp.PrimaryContact = &c
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *WorkspaceHandler) CreateClient(w http.ResponseWriter, r *http.Request) {
	viewer, ok := viewerFrom(w, r)
	if !ok {
		return
	}
	var req clientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	contacts := make([]application.ContactInput, 0, len(req.Contacts))
	for _, c := range req.Contacts {
		if strings.TrimSpace(c.Value) == "" {
			continue
		}
		contacts = append(contacts, application.ContactInput{Kind: domain.ContactKind(strings.ToUpper(c.Kind)), Value: c.Value, IsPrimary: c.IsPrimary})
	}
	client, err := h.service.CreateClient(r.Context(), req.fields(), contacts, viewer.UserID.String())
	if err != nil {
		writeError(w, err)
		return
	}
	h.writeClientDetail(w, r, viewer, client, http.StatusCreated)
}

func (h *WorkspaceHandler) GetClient(w http.ResponseWriter, r *http.Request) {
	viewer, ok := viewerFrom(w, r)
	if !ok {
		return
	}
	id, ok := parseClientID(w, r)
	if !ok {
		return
	}
	client, err := h.clients.FindByID(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	h.writeClientDetail(w, r, viewer, client, http.StatusOK)
}

func (h *WorkspaceHandler) writeClientDetail(w http.ResponseWriter, r *http.Request, viewer application.Viewer, client domain.Client, status int) {
	resp := clientResponseFrom(client)
	contacts, err := h.clients.ListContacts(r.Context(), client.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	resp.Contacts = make([]contactResponse, 0, len(contacts))
	for _, c := range contacts {
		resp.Contacts = append(resp.Contacts, contactResponseFrom(c))
	}
	projects, err := h.service.ClientProjects(r.Context(), viewer, client.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	resp.Projects = make([]projectResponse, 0, len(projects))
	for _, p := range projects {
		resp.Projects = append(resp.Projects, projectResponseFrom(p, 0))
	}
	writeJSON(w, status, resp)
}

func (h *WorkspaceHandler) UpdateClient(w http.ResponseWriter, r *http.Request) {
	viewer, ok := viewerFrom(w, r)
	if !ok {
		return
	}
	id, ok := parseClientID(w, r)
	if !ok {
		return
	}
	var req clientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	client, err := h.service.UpdateClient(r.Context(), id, req.Version, req.fields())
	if err != nil {
		writeError(w, err)
		return
	}
	h.writeClientDetail(w, r, viewer, client, http.StatusOK)
}

func (h *WorkspaceHandler) DeleteClient(w http.ResponseWriter, r *http.Request) {
	id, ok := parseClientID(w, r)
	if !ok {
		return
	}
	if err := h.clients.SoftDelete(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkspaceHandler) AddContact(w http.ResponseWriter, r *http.Request) {
	id, ok := parseClientID(w, r)
	if !ok {
		return
	}
	var req contactRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	contact, err := h.service.AddContact(r.Context(), id, application.ContactInput{Kind: domain.ContactKind(strings.ToUpper(req.Kind)), Value: req.Value, IsPrimary: req.IsPrimary})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, contactResponseFrom(contact))
}

func (h *WorkspaceHandler) RemoveContact(w http.ResponseWriter, r *http.Request) {
	id, ok := parseClientID(w, r)
	if !ok {
		return
	}
	contactID, err := uuid.Parse(r.PathValue("contactId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "contactId must be a valid UUID"})
		return
	}
	if err := h.clients.RemoveContact(r.Context(), id, contactID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetProjectClient links a project to a client (or unlinks with clientId null). projects:manage.
func (h *WorkspaceHandler) SetProjectClient(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	var req struct {
		ClientID *string `json:"clientId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var clientID *domain.ClientID
	if req.ClientID != nil && *req.ClientID != "" {
		parsed, err := uuid.Parse(*req.ClientID)
		if err != nil {
			writeError(w, &domain.ValidationError{Message: "clientId must be a valid UUID"})
			return
		}
		clientID = &domain.ClientID{Value: parsed}
	}
	if err := h.clients.SetProjectClient(r.Context(), projectID, clientID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- read models ---

type timelineResponse struct {
	Kind      string  `json:"kind"`
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Detail    string  `json:"detail"`
	Status    string  `json:"status"`
	ProjectID *string `json:"projectId"`
	At        string  `json:"at"`
}

func (h *WorkspaceHandler) Timeline(w http.ResponseWriter, r *http.Request) {
	viewer, ok := viewerFrom(w, r)
	if !ok {
		return
	}
	id, ok := parseClientID(w, r)
	if !ok {
		return
	}
	var before *time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, &domain.ValidationError{Message: "before must be an RFC 3339 timestamp"})
			return
		}
		before = &t
	}
	entries, err := h.service.Timeline(r.Context(), viewer, id, before)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]timelineResponse, 0, len(entries))
	for _, e := range entries {
		out = append(out, timelineResponse{Kind: e.Kind, ID: e.ID.String(), Title: e.Title, Detail: e.Detail, Status: e.Status,
			ProjectID: uuidString(e.ProjectID), At: e.At.Format(time.RFC3339Nano)})
	}
	writeJSON(w, http.StatusOK, out)
}

type searchHitResponse struct {
	Kind      string  `json:"kind"`
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Subtitle  string  `json:"subtitle"`
	ProjectID *string `json:"projectId"`
	ClientID  *string `json:"clientId"`
	Status    string  `json:"status"`
	At        string  `json:"at"`
}

func hitsResponse(hits []application.SearchHit) []searchHitResponse {
	out := make([]searchHitResponse, 0, len(hits))
	for _, h := range hits {
		out = append(out, searchHitResponse{Kind: h.Kind, ID: h.ID.String(), Title: h.Title, Subtitle: h.Subtitle,
			ProjectID: uuidString(h.ProjectID), ClientID: uuidString(h.ClientID), Status: h.Status, At: h.At.Format(time.RFC3339)})
	}
	return out
}

func (h *WorkspaceHandler) Search(w http.ResponseWriter, r *http.Request) {
	viewer, ok := viewerFrom(w, r)
	if !ok {
		return
	}
	var types application.SearchTypes
	for _, t := range strings.Split(r.URL.Query().Get("types"), ",") {
		switch strings.TrimSpace(strings.ToLower(t)) {
		case "clients":
			types.Clients = true
		case "projects":
			types.Projects = true
		case "tasks":
			types.Tasks = true
		case "files":
			types.Files = true
		}
	}
	results, err := h.service.Search(r.Context(), viewer, r.URL.Query().Get("q"), types)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"clients": hitsResponse(results.Clients), "projects": hitsResponse(results.Projects),
		"tasks": hitsResponse(results.Tasks), "files": hitsResponse(results.Files),
	})
}

func (h *WorkspaceHandler) Home(w http.ResponseWriter, r *http.Request) {
	viewer, ok := viewerFrom(w, r)
	if !ok {
		return
	}
	home, err := h.service.Home(r.Context(), viewer)
	if err != nil {
		writeError(w, err)
		return
	}
	type recentResponse struct {
		Kind      string  `json:"kind"`
		ID        string  `json:"id"`
		Title     string  `json:"title"`
		Subtitle  string  `json:"subtitle"`
		Status    string  `json:"status"`
		ProjectID *string `json:"projectId"`
		ViewedAt  string  `json:"viewedAt"`
	}
	type pendingResponse struct {
		Kind        string  `json:"kind"`
		ID          string  `json:"id"`
		TaskID      string  `json:"taskId"`
		Title       string  `json:"title"`
		Detail      string  `json:"detail"`
		ProjectID   *string `json:"projectId"`
		ProjectName *string `json:"projectName"`
		At          string  `json:"at"`
	}
	recent := make([]recentResponse, 0, len(home.Recent))
	for _, it := range home.Recent {
		recent = append(recent, recentResponse{Kind: it.Kind, ID: it.ID.String(), Title: it.Title, Subtitle: it.Subtitle,
			Status: it.Status, ProjectID: uuidString(it.ProjectID), ViewedAt: it.ViewedAt.Format(time.RFC3339)})
	}
	pending := make([]pendingResponse, 0, len(home.Pending))
	for _, it := range home.Pending {
		pending = append(pending, pendingResponse{Kind: it.Kind, ID: it.ID.String(), TaskID: it.TaskID.String(), Title: it.Title,
			Detail: it.Detail, ProjectID: uuidString(it.ProjectID), ProjectName: it.ProjectName, At: it.At.Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"recent": recent, "pending": pending})
}

func (h *WorkspaceHandler) TouchRecent(w http.ResponseWriter, r *http.Request) {
	viewer, ok := viewerFrom(w, r)
	if !ok {
		return
	}
	var req struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	refID, err := uuid.Parse(req.ID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "id must be a valid UUID"})
		return
	}
	if err := h.service.TouchRecent(r.Context(), viewer, strings.ToUpper(req.Kind), refID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
