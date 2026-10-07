package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/google/uuid"
)

type projectAdminChecker interface {
	IsProjectAdmin(ctx context.Context, projectID domain.ProjectID, userID domain.DashboardUserID) (bool, error)
}

type ExecutorFlowHandler struct {
	flow             *application.ExecutorFlow
	workers          *application.ExecutorWorkerService
	projects         projectFinder
	members          projectMembershipChecker
	admin            projectAdminChecker
	sessions         mobileSessionChecker
	auth             *application.DashboardAuthService
	files            *application.ProjectFileService
	executionEnabled bool
	completion       interface {
		CompleteExecutorJob(context.Context, application.ExecutorJob) error
	}
}

func NewExecutorFlowHandler(flow *application.ExecutorFlow, workers *application.ExecutorWorkerService, projects projectFinder, members projectMembershipChecker, admin projectAdminChecker) *ExecutorFlowHandler {
	return &ExecutorFlowHandler{flow: flow, workers: workers, projects: projects, members: members, admin: admin}
}
func (h *ExecutorFlowHandler) SetStepUp(sessions mobileSessionChecker, auth *application.DashboardAuthService) {
	h.sessions = sessions
	h.auth = auth
}
func (h *ExecutorFlowHandler) SetFiles(files *application.ProjectFileService) { h.files = files }
func (h *ExecutorFlowHandler) SetExecutionEnabled(enabled bool)               { h.executionEnabled = enabled }
func (h *ExecutorFlowHandler) SetCompletion(completion interface {
	CompleteExecutorJob(context.Context, application.ExecutorJob) error
}) {
	h.completion = completion
}
func (h *ExecutorFlowHandler) requireEnabled(w http.ResponseWriter) bool {
	if h.executionEnabled {
		return true
	}
	writeJSON(w, http.StatusServiceUnavailable, errorResponse{Code: "executor_disabled", Message: "isolated executor is disabled"})
	return false
}

type executorRequestView struct {
	ID          string                      `json:"id"`
	Payload     application.ExecutorPayload `json:"payload"`
	PayloadHash string                      `json:"payloadHash"`
	Status      string                      `json:"status"`
	RequestedBy string                      `json:"requestedBy"`
	RequestedAt time.Time                   `json:"requestedAt"`
	ExpiresAt   time.Time                   `json:"expiresAt"`
	DecidedBy   *uuid.UUID                  `json:"decidedBy"`
	DecidedAt   *time.Time                  `json:"decidedAt"`
	StartBefore *time.Time                  `json:"startBefore"`
	CanApprove  bool                        `json:"canApprove"`
	JobStatus   string                      `json:"jobStatus,omitempty"`
	JobResult   json.RawMessage             `json:"jobResult,omitempty"`
}

func executorRequestViewFrom(q application.ExecutorRequest) executorRequestView {
	return executorRequestView{ID: q.ID.String(), Payload: q.Payload, PayloadHash: q.PayloadHash, Status: q.Status, RequestedBy: q.RequestedBy, RequestedAt: q.RequestedAt, ExpiresAt: q.ExpiresAt, DecidedBy: q.DecidedBy, DecidedAt: q.DecidedAt, StartBefore: q.StartBefore, JobStatus: q.JobStatus, JobResult: q.JobResult}
}
func (h *ExecutorFlowHandler) Create(w http.ResponseWriter, r *http.Request) {
	project, identity, ok := requireProjectMember(w, r, h.projects, h.members)
	if !ok {
		return
	}
	if identity.TokenUse != "dashboard" {
		writeAuthError(w, http.StatusForbidden, "a human session is required")
		return
	}
	var body struct {
		TaskID      string                   `json:"taskId"`
		ExecutionID string                   `json:"executionId"`
		WorkerID    string                   `json:"workerId"`
		Operation   string                   `json:"operation"`
		Args        application.ExecutorArgs `json:"args"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	taskID, err1 := uuid.Parse(body.TaskID)
	executionID, err2 := uuid.Parse(body.ExecutionID)
	workerID, err3 := uuid.Parse(body.WorkerID)
	if err1 != nil || err2 != nil || err3 != nil {
		writeError(w, &domain.ValidationError{Message: "taskId, executionId and workerId must be UUIDs"})
		return
	}
	q, err := h.flow.Create(r.Context(), project.ID, domain.TaskID{Value: taskID}, domain.ExecutionID{Value: executionID}, workerID, body.Operation, body.Args, identity.Subject)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, executorRequestViewFrom(q))
}
func (h *ExecutorFlowHandler) List(w http.ResponseWriter, r *http.Request) {
	project, identity, ok := requireProjectMember(w, r, h.projects, h.members)
	if !ok {
		return
	}
	items, err := h.flow.ListByProject(r.Context(), project.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	views := make([]executorRequestView, 0, len(items))
	canApprove := h.canApprove(r, identity, project.ID)
	for _, q := range items {
		v := executorRequestViewFrom(q)
		v.CanApprove = canApprove
		views = append(views, v)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, views)
}
func (h *ExecutorFlowHandler) requestForHuman(w http.ResponseWriter, r *http.Request) (application.ExecutorRequest, AuthContext, bool) {
	identity, ok := FromContext(r.Context())
	if !ok || identity.TokenUse != "dashboard" {
		writeAuthError(w, http.StatusForbidden, "human session required")
		return application.ExecutorRequest{}, AuthContext{}, false
	}
	id, err := uuid.Parse(r.PathValue("requestId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid requestId"})
		return application.ExecutorRequest{}, AuthContext{}, false
	}
	q, err := h.flow.Find(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return application.ExecutorRequest{}, AuthContext{}, false
	}
	projectID, _ := uuid.Parse(q.Payload.ProjectID)
	if _, err := h.projects.FindByID(r.Context(), domain.ProjectID{Value: projectID}); err != nil {
		writeError(w, err)
		return application.ExecutorRequest{}, AuthContext{}, false
	}
	if !identity.HasScope("projects:manage") {
		userID, err := uuid.Parse(identity.Subject)
		if err != nil {
			writeAuthError(w, http.StatusForbidden, "invalid subject")
			return application.ExecutorRequest{}, AuthContext{}, false
		}
		member, err := h.members.IsMember(r.Context(), domain.ProjectID{Value: projectID}, domain.DashboardUserID{Value: userID})
		if err != nil {
			writeError(w, err)
			return application.ExecutorRequest{}, AuthContext{}, false
		}
		if !member {
			writeAuthError(w, http.StatusForbidden, "not a project member")
			return application.ExecutorRequest{}, AuthContext{}, false
		}
	}
	return q, identity, true
}
func (h *ExecutorFlowHandler) Get(w http.ResponseWriter, r *http.Request) {
	q, identity, ok := h.requestForHuman(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	v := executorRequestViewFrom(q)
	projectUUID, _ := uuid.Parse(q.Payload.ProjectID)
	v.CanApprove = h.canApprove(r, identity, domain.ProjectID{Value: projectUUID})
	writeJSON(w, http.StatusOK, v)
}
func (h *ExecutorFlowHandler) canApprove(r *http.Request, identity AuthContext, projectID domain.ProjectID) bool {
	if identity.TokenUse != "dashboard" {
		return false
	}
	if identity.HasScope("settings:manage") {
		return true
	}
	userID, err := uuid.Parse(identity.Subject)
	if err != nil {
		return false
	}
	allowed, err := h.admin.IsProjectAdmin(r.Context(), projectID, domain.DashboardUserID{Value: userID})
	return err == nil && allowed
}
func (h *ExecutorFlowHandler) Approve(w http.ResponseWriter, r *http.Request) { h.decide(w, r, true) }
func (h *ExecutorFlowHandler) Deny(w http.ResponseWriter, r *http.Request)    { h.decide(w, r, false) }
func (h *ExecutorFlowHandler) decide(w http.ResponseWriter, r *http.Request, approve bool) {
	q, identity, ok := h.requestForHuman(w, r)
	if !ok {
		return
	}
	userID, err := uuid.Parse(identity.Subject)
	if err != nil {
		writeAuthError(w, http.StatusForbidden, "invalid subject")
		return
	}
	projectID, _ := uuid.Parse(q.Payload.ProjectID)
	allowed := identity.HasScope("settings:manage")
	if !allowed {
		allowed, err = h.admin.IsProjectAdmin(r.Context(), domain.ProjectID{Value: projectID}, domain.DashboardUserID{Value: userID})
		if err != nil {
			writeError(w, err)
			return
		}
	}
	if !allowed {
		writeAuthError(w, http.StatusForbidden, "administrator required")
		return
	}
	var body struct {
		TotpCode string `json:"totpCode"`
	}
	if r.ContentLength > 0 && !decodeJSON(w, r, &body) {
		return
	}
	if approve {
		if h.sessions == nil || h.auth == nil {
			writeError(w, application.ErrStepUpRequired)
			return
		}
		if identity.SessionID == "" {
			writeError(w, application.ErrStepUpRequired)
			return
		}
		sessionID, err := uuid.Parse(identity.SessionID)
		if err != nil {
			writeError(w, application.ErrStepUpRequired)
			return
		}
		session, err := h.sessions.FindByID(r.Context(), sessionID)
		if err != nil {
			writeError(w, application.ErrStepUpRequired)
			return
		}
		if !session.RecentTotp(time.Now().UTC()) {
			if body.TotpCode == "" {
				writeError(w, application.ErrStepUpRequired)
				return
			}
			if err := h.auth.ConfirmStepUp(r.Context(), sessionID, domain.DashboardUserID{Value: userID}, body.TotpCode); err != nil {
				writeError(w, err)
				return
			}
		}
	}
	updated, err := h.flow.Decide(r.Context(), q.ID, userID, approve)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, executorRequestViewFrom(updated))
}
func (h *ExecutorFlowHandler) authenticateWorker(w http.ResponseWriter, r *http.Request) (application.ExecutorWorker, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeAuthError(w, http.StatusUnauthorized, "invalid worker credential")
		return application.ExecutorWorker{}, false
	}
	worker, err := h.workers.Authenticate(r.Context(), strings.TrimPrefix(header, "Bearer "))
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid worker credential")
		return application.ExecutorWorker{}, false
	}
	return worker, true
}

type executorJobView struct {
	JobID      string              `json:"jobId"`
	Request    executorRequestView `json:"request"`
	Status     string              `json:"status"`
	LeaseID    string              `json:"leaseId"`
	LeaseUntil time.Time           `json:"leaseUntil"`
	Sequence   int                 `json:"sequence"`
	FileID     *string             `json:"fileId,omitempty"`
}

func executorJobViewFrom(job application.ExecutorJob) executorJobView {
	v := executorJobView{JobID: job.ID.String(), Request: executorRequestViewFrom(job.Request), Status: job.Status, LeaseID: job.LeaseID.String(), LeaseUntil: job.LeaseUntil, Sequence: job.Sequence}
	if job.Request.Payload.Operation == "CREATE" {
		s := application.ExecutorFileID(job.Request.ID, job.Request.Payload.Args.Path).String()
		v.FileID = &s
	}
	return v
}
func (h *ExecutorFlowHandler) Claim(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	worker, ok := h.authenticateWorker(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var body struct {
		Capacity application.ExecutorCapacity `json:"capacity"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	job, err := h.flow.Claim(r.Context(), worker.ID, body.Capacity)
	if err != nil {
		writeError(w, err)
		return
	}
	if job == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if job.LeaseID == uuid.Nil && (job.Status == "UNSUPPORTED_CAPACITY" || terminalExecutorStatus(job.Status)) {
		if job.Request.ToolCallID != nil && h.completion == nil && job.TaskNotifiedAt == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorResponse{Code: "task_resume_unavailable", Message: "executor result is saved and task resume will be retried"})
			return
		}
		if job.Request.ToolCallID != nil && job.TaskNotifiedAt == nil {
			if err := h.completion.CompleteExecutorJob(r.Context(), *job); err != nil {
				writeError(w, err)
				return
			}
		}
		if err := h.flow.MarkTaskNotified(r.Context(), job.ID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, executorJobViewFrom(*job))
}
func (h *ExecutorFlowHandler) Current(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	worker, ok := h.authenticateWorker(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("jobId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid jobId"})
		return
	}
	job, err := h.flow.Current(r.Context(), worker.ID, id)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, executorJobViewFrom(job))
}
func (h *ExecutorFlowHandler) Result(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	worker, ok := h.authenticateWorker(w, r)
	if !ok {
		return
	}
	jobID, err := uuid.Parse(r.PathValue("jobId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid jobId"})
		return
	}
	var body struct {
		LeaseID  string          `json:"leaseId"`
		Sequence int             `json:"sequence"`
		Status   string          `json:"status"`
		Result   json.RawMessage `json:"result"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	leaseID, err := uuid.Parse(body.LeaseID)
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid leaseId"})
		return
	}
	if err := h.flow.Report(r.Context(), worker.ID, jobID, leaseID, body.Sequence, body.Status, body.Result); err != nil {
		writeError(w, err)
		return
	}
	if terminalExecutorStatus(body.Status) && h.completion != nil {
		job, err := h.flow.FindJob(r.Context(), jobID)
		if err != nil {
			writeError(w, err)
			return
		}
		if job.Request.ToolCallID != nil {
			if err := h.completion.CompleteExecutorJob(r.Context(), job); err != nil {
				writeError(w, err)
				return
			}
		}
		if err := h.flow.MarkTaskNotified(r.Context(), job.ID); err != nil {
			writeError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func terminalExecutorStatus(status string) bool {
	return status == "SUCCEEDED" || status == "FAILED" || status == "CANCELLED" || status == "UNSUPPORTED_CAPACITY"
}

func (h *ExecutorFlowHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if !h.requireEnabled(w) {
		return
	}
	worker, ok := h.authenticateWorker(w, r)
	if !ok {
		return
	}
	if h.files == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Code: "files_unavailable", Message: "project file storage is unavailable"})
		return
	}
	jobID, err := uuid.Parse(r.PathValue("jobId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid jobId"})
		return
	}
	fileID, err := uuid.Parse(r.PathValue("fileId"))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid fileId"})
		return
	}
	leaseID, err := uuid.Parse(r.Header.Get("X-Executor-Lease-Id"))
	if err != nil {
		writeAuthError(w, http.StatusForbidden, "invalid lease")
		return
	}
	job, err := h.flow.Current(r.Context(), worker.ID, jobID)
	if err != nil {
		writeError(w, err)
		return
	}
	if job.LeaseID != leaseID || job.Request.Payload.Operation != "CREATE" || application.ExecutorFileID(job.Request.ID, job.Request.Payload.Args.Path) != fileID {
		writeAuthError(w, http.StatusForbidden, "file is not authorized for this job")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (64<<10)+1)
	file, created, err := h.files.UploadFromExecutor(r.Context(), job.Request, r.Body)
	if err != nil {
		writeUploadError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, projectFileResponseFrom(file))
}
