package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type ExecutorWorkerHandler struct {
	service          *application.ExecutorWorkerService
	projects         projectFinder
	executionEnabled bool
}

func NewExecutorWorkerHandler(service *application.ExecutorWorkerService, projects projectFinder) *ExecutorWorkerHandler {
	return &ExecutorWorkerHandler{service: service, projects: projects}
}
func (h *ExecutorWorkerHandler) SetExecutionEnabled(enabled bool) { h.executionEnabled = enabled }

type executorWorkerView struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	ProjectID     string  `json:"projectId"`
	ImageDigest   string  `json:"imageDigest"`
	Enabled       bool    `json:"enabled"`
	CreatedBy     string  `json:"createdBy"`
	CreatedAt     string  `json:"createdAt"`
	LastSeenAt    *string `json:"lastSeenAt"`
	RuntimeStatus string  `json:"runtimeStatus"`
}

func executorWorkerViewFrom(w application.ExecutorWorker, runtimeEnabled bool) executorWorkerView {
	v := executorWorkerView{ID: w.ID.String(), Name: w.Name, ProjectID: w.ProjectID.String(), ImageDigest: w.ImageDigest,
		Enabled: w.Enabled, CreatedBy: w.CreatedBy, CreatedAt: w.CreatedAt.Format(time.RFC3339), RuntimeStatus: "OFFLINE"}
	if !w.Enabled {
		v.RuntimeStatus = "REVOKED"
	} else if !runtimeEnabled {
		v.RuntimeStatus = "DISABLED"
	} else if w.LastSeenAt != nil && time.Since(*w.LastSeenAt) <= 30*time.Second {
		v.RuntimeStatus = "READY"
	}
	if w.LastSeenAt != nil {
		s := w.LastSeenAt.Format(time.RFC3339)
		v.LastSeenAt = &s
	}
	return v
}

func requireExecutorAdmin(w http.ResponseWriter, r *http.Request) (AuthContext, bool) {
	identity, ok := FromContext(r.Context())
	if !ok || identity.TokenUse != "dashboard" || !identity.HasScope("settings:manage") {
		writeAuthError(w, http.StatusForbidden, "only a human instance administrator can manage executor workers")
		return AuthContext{}, false
	}
	return identity, true
}

func (h *ExecutorWorkerHandler) Create(w http.ResponseWriter, r *http.Request) {
	identity, ok := requireExecutorAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		ProjectID   string `json:"projectId"`
		ImageDigest string `json:"imageDigest"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id, err := uuid.Parse(req.ProjectID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "projectId must be a UUID"})
		return
	}
	projectID := domain.ProjectID{Value: id}
	if _, err := h.projects.FindByID(r.Context(), projectID); err != nil {
		writeError(w, err)
		return
	}
	worker, token, err := h.service.Register(r.Context(), req.Name, projectID, req.ImageDigest, identity.Subject)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, struct {
		Worker executorWorkerView `json:"worker"`
		Token  string             `json:"token"`
	}{executorWorkerViewFrom(worker, h.executionEnabled), token})
}

func (h *ExecutorWorkerHandler) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireExecutorAdmin(w, r); !ok {
		return
	}
	workers, err := h.service.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	views := make([]executorWorkerView, 0, len(workers))
	for _, worker := range workers {
		views = append(views, executorWorkerViewFrom(worker, h.executionEnabled))
	}
	writeJSON(w, http.StatusOK, views)
}

func (h *ExecutorWorkerHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireExecutorAdmin(w, r); !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("workerId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "workerId must be a UUID"})
		return
	}
	if err := h.service.Revoke(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Heartbeat is the only machine route in this slice. It authenticates its own prw_ bearer;
// it cannot schedule, claim or execute work and never accepts project/policy changes.
func (h *ExecutorWorkerHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeAuthError(w, http.StatusUnauthorized, "invalid executor credential")
		return
	}
	worker, err := h.service.Heartbeat(r.Context(), strings.TrimPrefix(header, "Bearer "))
	if err != nil {
		if err == application.ErrExecutorWorkerUnauthorized {
			writeAuthError(w, http.StatusUnauthorized, "invalid executor credential")
		} else {
			writeError(w, err)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		WorkerID   string `json:"workerId"`
		ProjectID  string `json:"projectId"`
		CanExecute bool   `json:"canExecute"`
		Status     string `json:"status"`
	}{worker.ID.String(), worker.ProjectID.String(), h.executionEnabled, func() string {
		if h.executionEnabled {
			return "READY"
		}
		return "REGISTERED_NO_RUNTIME"
	}()})
}
