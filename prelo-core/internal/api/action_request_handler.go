package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// ActionRequestHandler is PR-3 (docs/integracoes/action-requests.md). The executor side
// (create/get/result) is only for a project's integration key (actions:request / actions:report);
// the read model per project is for people who can open that project.
type ActionRequestHandler struct {
	service  *application.ActionRequestService
	projects projectFinder
	members  projectMembershipChecker
}

func NewActionRequestHandler(service *application.ActionRequestService, projects projectFinder, members projectMembershipChecker) *ActionRequestHandler {
	return &ActionRequestHandler{service: service, projects: projects, members: members}
}

func RegisterActionRequestRoutes(mux *http.ServeMux, h *ActionRequestHandler) {
	mux.HandleFunc("POST /api/v1/action-requests", h.Create)
	mux.HandleFunc("GET /api/v1/action-requests/{id}", h.Get)
	mux.HandleFunc("POST /api/v1/action-requests/{id}/result", h.Result)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/actions", h.ListForProject)
}

type actionResultResponse struct {
	Status         string  `json:"status"`
	Sequence       int     `json:"sequence"`
	Message        *string `json:"message"`
	URL            *string `json:"url"`
	ArtifactDigest *string `json:"artifactDigest"`
	ReportedAt     string  `json:"reportedAt"`
}

type actionRequestResponse struct {
	ID             string                `json:"id"`
	WorkspaceID    string                `json:"workspaceId"`
	ProjectID      string                `json:"projectId"`
	Kind           string                `json:"kind"`
	Payload        json.RawMessage       `json:"payload"`
	PayloadHash    string                `json:"payloadHash"`
	Risk           string                `json:"risk"`
	Impact         string                `json:"impact"`
	RequestedBy    string                `json:"requestedBy"`
	IdempotencyKey string                `json:"idempotencyKey"`
	Status         string                `json:"status"`
	ApprovalID     string                `json:"approvalId"`
	ApprovalCode   string                `json:"approvalCode"`
	ExpiresAt      string                `json:"expiresAt"`
	DecidedAt      *string               `json:"decidedAt"`
	DecidedBy      *string               `json:"decidedBy"`
	Result         *actionResultResponse `json:"result"`
	CreatedAt      string                `json:"createdAt"`
}

func actionResponseFrom(v application.ActionView) actionRequestResponse {
	a := v.Action
	out := actionRequestResponse{
		ID: a.ID.String(), WorkspaceID: a.WorkspaceID.String(), ProjectID: a.ProjectID.String(), Kind: a.Kind,
		Payload: a.Payload, PayloadHash: a.PayloadHash, Risk: string(a.Risk), Impact: a.Impact, RequestedBy: a.RequestedBy,
		IdempotencyKey: a.IdempotencyKey, Status: string(v.Approval.EffectiveStatus(time.Now().UTC())),
		ApprovalID: v.Approval.ID.String(), ApprovalCode: v.Approval.ShortCode,
		ExpiresAt: v.Approval.ExpiresAt.UTC().Format(time.RFC3339), DecidedBy: v.Approval.DecidedBy,
		CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
	}
	if v.Approval.DecidedAt != nil {
		d := v.Approval.DecidedAt.UTC().Format(time.RFC3339)
		out.DecidedAt = &d
	}
	if r := a.Result; r != nil {
		out.Result = &actionResultResponse{Status: r.Status, Sequence: r.Sequence, Message: r.Message, URL: r.URL,
			ArtifactDigest: r.Digest, ReportedAt: r.ReportedAt.UTC().Format(time.RFC3339)}
	}
	return out
}

// keyCaller: the integration key's own project and id. Anything but a project key is refused.
func keyCaller(w http.ResponseWriter, r *http.Request) (domain.ProjectID, *uuid.UUID, bool) {
	identity, ok := FromContext(r.Context())
	if !ok || identity.TokenUse != tokenUseApiKey || identity.ProjectID == nil {
		writeAuthError(w, http.StatusForbidden, "use the project's integration key")
		return domain.ProjectID{}, nil, false
	}
	var keyID *uuid.UUID
	if id, err := uuid.Parse(strings.TrimPrefix(identity.Subject, "api_key:")); err == nil {
		keyID = &id
	}
	return domain.ProjectID{Value: *identity.ProjectID}, keyID, true
}

func parseActionID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "id must be a UUID"})
		return uuid.Nil, false
	}
	return id, true
}

func (h *ActionRequestHandler) Create(w http.ResponseWriter, r *http.Request) {
	project, keyID, ok := keyCaller(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	var req struct {
		Kind           string          `json:"kind"`
		ProjectID      string          `json:"projectId"`
		Payload        json.RawMessage `json:"payload"`
		PayloadHash    string          `json:"payloadHash"`
		Impact         string          `json:"impact"`
		RequestedBy    string          `json:"requestedBy"`
		IdempotencyKey string          `json:"idempotencyKey"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	projectID, err := uuid.Parse(req.ProjectID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "projectId must be a UUID"})
		return
	}
	view, created, err := h.service.Create(r.Context(), project, keyID, application.CreateActionInput{
		Kind: req.Kind, ProjectID: domain.ProjectID{Value: projectID}, Payload: req.Payload, PayloadHash: req.PayloadHash,
		Impact: req.Impact, RequestedBy: req.RequestedBy, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, actionResponseFrom(view))
}

func (h *ActionRequestHandler) Get(w http.ResponseWriter, r *http.Request) {
	project, _, ok := keyCaller(w, r)
	if !ok {
		return
	}
	id, ok := parseActionID(w, r)
	if !ok {
		return
	}
	view, err := h.service.Get(r.Context(), project, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, actionResponseFrom(view))
}

func (h *ActionRequestHandler) Result(w http.ResponseWriter, r *http.Request) {
	project, _, ok := keyCaller(w, r)
	if !ok {
		return
	}
	id, ok := parseActionID(w, r)
	if !ok {
		return
	}
	var req struct {
		Status         string `json:"status"`
		Message        string `json:"message"`
		URL            string `json:"url"`
		ArtifactDigest string `json:"artifactDigest"`
		ReportedAt     string `json:"reportedAt"`
		Sequence       int    `json:"sequence"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var reportedAt time.Time
	if req.ReportedAt != "" {
		t, err := time.Parse(time.RFC3339, req.ReportedAt)
		if err != nil {
			writeError(w, &domain.ValidationError{Message: "reportedAt must be RFC 3339"})
			return
		}
		reportedAt = t
	}
	view, err := h.service.ReportResult(r.Context(), project, id, application.ActionResultInput{
		Status: req.Status, Sequence: req.Sequence, Message: req.Message, URL: req.URL, Digest: req.ArtifactDigest, ReportedAt: reportedAt,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, actionResponseFrom(view))
}

func (h *ActionRequestHandler) ListForProject(w http.ResponseWriter, r *http.Request) {
	project, _, ok := requireProjectMember(w, r, h.projects, h.members)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	views, err := h.service.ListForProject(r.Context(), project.ID, r.URL.Query().Get("kind"), limit)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]actionRequestResponse, 0, len(views))
	for _, v := range views {
		out = append(out, actionResponseFrom(v))
	}
	writeJSON(w, http.StatusOK, out)
}
