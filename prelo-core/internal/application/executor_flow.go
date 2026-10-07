package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/google/uuid"
)

var ErrExecutorRequestNotFound = errors.New("executor request not found")
var ErrExecutorConflict = errors.New("executor request conflict")
var ErrExecutorUnauthorized = errors.New("executor action unauthorized")
var ErrInvalidExecutorCapacity = errors.New("invalid executor capacity snapshot")

const MaxExecutorWorkerSlots = 64

type ExecutorArgs struct {
	Path          string `json:"path,omitempty"`
	ContentBase64 string `json:"contentBase64,omitempty"`
}
type ExecutorPayload struct {
	ProjectID   string       `json:"projectId"`
	TaskID      string       `json:"taskId"`
	ExecutionID string       `json:"executionId"`
	WorkerID    string       `json:"workerId"`
	ImageDigest string       `json:"imageDigest"`
	Operation   string       `json:"operation"`
	Args        ExecutorArgs `json:"args"`
}
type ExecutorRequest struct {
	ID            uuid.UUID
	Payload       ExecutorPayload
	PayloadHash   string
	Status        string
	RequestedBy   string
	RequestedAt   time.Time
	ExpiresAt     time.Time
	DecidedBy     *uuid.UUID
	DecidedAt     *time.Time
	StartBefore   *time.Time
	PolicyVersion int64
	Version       int64
	ToolCallID    *uuid.UUID
	ApprovalID    *uuid.UUID
	JobStatus     string
	JobResult     json.RawMessage
}
type ExecutorCapacity struct {
	ProfileID          string      `json:"profileId"`
	MemoryBytes        int64       `json:"memoryBytes"`
	CPUQuotaMilli      int64       `json:"cpuQuotaMilli"`
	DiskBytes          int64       `json:"diskBytes"`
	PIDs               int64       `json:"pids"`
	AvailableSlots     int         `json:"availableSlots"`
	MaximumSlots       int         `json:"maximumSlots"`
	ActiveContainers   int         `json:"activeContainers"`
	ActiveExecutionIDs []uuid.UUID `json:"activeExecutionIds"`
	ObservedAt         time.Time   `json:"observedAt"`
}
type ExecutorJob struct {
	ID             uuid.UUID
	Request        ExecutorRequest
	Status         string
	LeaseID        uuid.UUID
	LeaseUntil     time.Time
	Sequence       int
	Result         json.RawMessage
	TaskNotifiedAt *time.Time
}
type ExecutorRequestRepository interface {
	Insert(context.Context, ExecutorRequest) error
	Find(context.Context, uuid.UUID) (ExecutorRequest, error)
	ListByProject(context.Context, domain.ProjectID) ([]ExecutorRequest, error)
	Decide(context.Context, uuid.UUID, uuid.UUID, bool) (ExecutorRequest, error)
	Claim(context.Context, uuid.UUID, uuid.UUID, ExecutorCapacity) (*ExecutorJob, error)
	Current(context.Context, uuid.UUID, uuid.UUID) (ExecutorJob, error)
	Report(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int, string, json.RawMessage) error
	LinkToolCall(context.Context, uuid.UUID, uuid.UUID) error
	LinkApproval(context.Context, uuid.UUID, uuid.UUID) error
	FindByToolCall(context.Context, uuid.UUID) (ExecutorRequest, error)
	FindJob(context.Context, uuid.UUID) (ExecutorJob, error)
	MarkTaskNotified(context.Context, uuid.UUID) error
}
type ExecutorTaskFinder interface {
	FindByID(context.Context, domain.TaskID) (domain.Task, error)
}
type ExecutorExecutionFinder interface {
	FindByID(context.Context, domain.ExecutionID) (domain.Execution, error)
}
type ExecutorWorkerFinder interface {
	FindByID(context.Context, uuid.UUID) (ExecutorWorker, error)
}

type ExecutorFlow struct {
	requests     ExecutorRequestRepository
	tasks        ExecutorTaskFinder
	executions   ExecutorExecutionFinder
	workers      ExecutorWorkerFinder
	availability ToolAvailability
	enabled      bool
}

func NewExecutorFlow(requests ExecutorRequestRepository, tasks ExecutorTaskFinder, executions ExecutorExecutionFinder, workers ExecutorWorkerFinder) *ExecutorFlow {
	return &ExecutorFlow{requests: requests, tasks: tasks, executions: executions, workers: workers}
}
func (f *ExecutorFlow) SetToolAvailability(availability ToolAvailability) {
	f.availability = availability
}
func (f *ExecutorFlow) SetEnabled(enabled bool) { f.enabled = enabled }
func (f *ExecutorFlow) Enabled() bool           { return f.enabled }

// CreateForToolCall fixes every execution input before the generic Prelo approval is created.
// Existing generic approval remains the user's decision surface and is linked below.
func (f *ExecutorFlow) CreateForToolCall(ctx context.Context, projectID domain.ProjectID, execution domain.Execution, toolCall domain.ToolCall, operation string, args ExecutorArgs, requestedBy string) (ExecutorRequest, error) {
	if !f.enabled {
		return ExecutorRequest{}, ErrExecutorUnauthorized
	}
	worker, err := f.findReadyWorker(ctx, projectID)
	if err != nil {
		return ExecutorRequest{}, err
	}
	q, err := f.Create(ctx, projectID, execution.TaskID, execution.ID, worker.ID, operation, args, requestedBy)
	if err != nil {
		return ExecutorRequest{}, err
	}
	if err := f.requests.LinkToolCall(ctx, q.ID, toolCall.ID.Value); err != nil {
		return ExecutorRequest{}, err
	}
	q.ToolCallID = &toolCall.ID.Value
	return q, nil
}

func ExecutorArgsFromToolCall(operation, raw string) (ExecutorArgs, error) {
	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return ExecutorArgs{}, &domain.ValidationError{Message: "invalid workspace tool arguments"}
	}
	args := ExecutorArgs{Path: input.Path}
	if operation == "CREATE" {
		args.ContentBase64 = base64.StdEncoding.EncodeToString([]byte(input.Content))
	}
	return NormalizeExecutorArgs(operation, args)
}

func (f *ExecutorFlow) findReadyWorker(ctx context.Context, projectID domain.ProjectID) (ExecutorWorker, error) {
	list, ok := f.workers.(interface {
		List(context.Context) ([]ExecutorWorker, error)
	})
	if !ok {
		return ExecutorWorker{}, ErrExecutorUnauthorized
	}
	workers, err := list.List(ctx)
	if err != nil {
		return ExecutorWorker{}, err
	}
	now := time.Now().UTC()
	for _, worker := range workers {
		if worker.ProjectID == projectID && worker.Enabled && worker.LastSeenAt != nil && now.Sub(*worker.LastSeenAt) <= 30*time.Second {
			return worker, nil
		}
	}
	return ExecutorWorker{}, ErrExecutorWorkerUnauthorized
}

func (f *ExecutorFlow) LinkApproval(ctx context.Context, requestID, approvalID uuid.UUID) error {
	return f.requests.LinkApproval(ctx, requestID, approvalID)
}
func (f *ExecutorFlow) FindByToolCall(ctx context.Context, toolCallID uuid.UUID) (ExecutorRequest, error) {
	return f.requests.FindByToolCall(ctx, toolCallID)
}
func (f *ExecutorFlow) FindJob(ctx context.Context, jobID uuid.UUID) (ExecutorJob, error) {
	return f.requests.FindJob(ctx, jobID)
}

var executorToolNames = map[string]string{"START_WORKSPACE": "workspace_start", "LIST": "workspace_list", "READ": "workspace_read", "MKDIR": "workspace_mkdir", "CREATE": "workspace_create_file"}

func ExecutorToolName(operation string) (string, bool) {
	name, ok := executorToolNames[operation]
	return name, ok
}
func IsExecutorTool(name string) bool {
	for _, candidate := range executorToolNames {
		if candidate == name {
			return true
		}
	}
	return false
}

func ValidateExecutorPath(path string, allowRoot bool) error {
	if allowRoot && path == "." {
		return nil
	}
	if path == "" || len(path) > 240 || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\\x00\n\r") {
		return &domain.ValidationError{Message: "invalid workspace path"}
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return &domain.ValidationError{Message: "invalid workspace path"}
		}
	}
	return nil
}
func NormalizeExecutorArgs(operation string, args ExecutorArgs) (ExecutorArgs, error) {
	switch operation {
	case "START_WORKSPACE":
		if args.Path != "" || args.ContentBase64 != "" {
			return ExecutorArgs{}, &domain.ValidationError{Message: "start has no arguments"}
		}
		return ExecutorArgs{}, nil
	case "LIST", "READ", "MKDIR", "CREATE":
		if err := ValidateExecutorPath(args.Path, operation == "LIST"); err != nil {
			return ExecutorArgs{}, err
		}
		if operation != "CREATE" && args.ContentBase64 != "" {
			return ExecutorArgs{}, &domain.ValidationError{Message: "unexpected content"}
		}
		if operation == "CREATE" {
			data, err := base64.StdEncoding.DecodeString(args.ContentBase64)
			if err != nil || len(data) > 64<<10 {
				return ExecutorArgs{}, &domain.ValidationError{Message: "content must be base64 and at most 64 KiB"}
			}
			args.ContentBase64 = base64.StdEncoding.EncodeToString(data)
		}
		return args, nil
	default:
		return ExecutorArgs{}, &domain.ValidationError{Message: "unsupported executor operation"}
	}
}
func ExecutorPayloadHash(payload ExecutorPayload) string {
	encoded, _ := json.Marshal(payload)
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:])
}

func (f *ExecutorFlow) Create(ctx context.Context, projectID domain.ProjectID, taskID domain.TaskID, executionID domain.ExecutionID, workerID uuid.UUID, operation string, args ExecutorArgs, requestedBy string) (ExecutorRequest, error) {
	if projectID.Value == uuid.Nil || taskID.Value == uuid.Nil || executionID.Value == uuid.Nil || workerID == uuid.Nil || requestedBy == "" {
		return ExecutorRequest{}, &domain.ValidationError{Message: "missing executor request identity"}
	}
	normalized, err := NormalizeExecutorArgs(operation, args)
	if err != nil {
		return ExecutorRequest{}, err
	}
	task, err := f.tasks.FindByID(ctx, taskID)
	if err != nil {
		return ExecutorRequest{}, err
	}
	execution, err := f.executions.FindByID(ctx, executionID)
	if err != nil {
		return ExecutorRequest{}, err
	}
	worker, err := f.workers.FindByID(ctx, workerID)
	if err != nil {
		return ExecutorRequest{}, err
	}
	if task.ProjectID == nil || *task.ProjectID != projectID || execution.TaskID != taskID || !worker.Enabled || worker.ProjectID != projectID {
		return ExecutorRequest{}, ErrExecutorUnauthorized
	}
	if task.Status == domain.TaskCompleted || task.Status == domain.TaskFailed || execution.Status == domain.ExecutionCompleted || execution.Status == domain.ExecutionFailed {
		return ExecutorRequest{}, ErrExecutorConflict
	}
	if operation != "START_WORKSPACE" && execution.Status != domain.ExecutionRunning {
		return ExecutorRequest{}, ErrExecutorConflict
	}
	if f.availability == nil {
		return ExecutorRequest{}, ErrExecutorUnauthorized
	}
	toolName, _ := ExecutorToolName(operation)
	setting, err := f.availability.Get(ctx, projectID, toolName)
	if err != nil {
		return ExecutorRequest{}, err
	}
	if !setting.Enabled || setting.Version == 0 {
		return ExecutorRequest{}, ErrExecutorUnauthorized
	}
	payload := ExecutorPayload{ProjectID: projectID.String(), TaskID: taskID.String(), ExecutionID: executionID.String(), WorkerID: workerID.String(), ImageDigest: worker.ImageDigest, Operation: operation, Args: normalized}
	now := time.Now().UTC()
	req := ExecutorRequest{ID: uuid.New(), Payload: payload, PayloadHash: ExecutorPayloadHash(payload), Status: "PENDING", RequestedBy: requestedBy, RequestedAt: now, ExpiresAt: now.Add(30 * time.Minute), PolicyVersion: setting.Version}
	if err := f.requests.Insert(ctx, req); err != nil {
		return ExecutorRequest{}, err
	}
	return req, nil
}
func (f *ExecutorFlow) Find(ctx context.Context, id uuid.UUID) (ExecutorRequest, error) {
	return f.requests.Find(ctx, id)
}
func (f *ExecutorFlow) ListByProject(ctx context.Context, id domain.ProjectID) ([]ExecutorRequest, error) {
	return f.requests.ListByProject(ctx, id)
}
func (f *ExecutorFlow) Decide(ctx context.Context, id, actor uuid.UUID, approve bool) (ExecutorRequest, error) {
	return f.requests.Decide(ctx, id, actor, approve)
}
func (f *ExecutorFlow) DecideByToolCall(ctx context.Context, toolCallID, actor uuid.UUID, approve bool) (ExecutorRequest, error) {
	request, err := f.requests.FindByToolCall(ctx, toolCallID)
	if err != nil {
		return ExecutorRequest{}, err
	}
	return f.requests.Decide(ctx, request.ID, actor, approve)
}
func (f *ExecutorFlow) Claim(ctx context.Context, workerID uuid.UUID, capacity ExecutorCapacity) (*ExecutorJob, error) {
	if err := ValidateExecutorCapacity(capacity, time.Now().UTC()); err != nil {
		return nil, err
	}
	profile, ok := ExecutorResourceProfileForOperation("START_WORKSPACE")
	if !ok {
		return nil, ErrExecutorUnauthorized
	}
	if capacity.ProfileID != profile.ID || capacity.MemoryBytes != profile.MemoryBytes || capacity.CPUQuotaMilli != profile.CPUQuotaMilli || capacity.DiskBytes != profile.DiskBytes || int64(capacity.PIDs) != int64(profile.PIDs) {
		capacity.AvailableSlots = 0
		capacity.MaximumSlots = 0
	}
	return f.requests.Claim(ctx, workerID, uuid.New(), capacity)
}
func (f *ExecutorFlow) MarkTaskNotified(ctx context.Context, jobID uuid.UUID) error {
	return f.requests.MarkTaskNotified(ctx, jobID)
}
func (f *ExecutorFlow) Current(ctx context.Context, workerID, jobID uuid.UUID) (ExecutorJob, error) {
	return f.requests.Current(ctx, workerID, jobID)
}
func (f *ExecutorFlow) Report(ctx context.Context, workerID, jobID, leaseID uuid.UUID, sequence int, status string, result json.RawMessage) error {
	if sequence < 1 || len(result) > 64<<10 || (status != "RUNNING" && status != "SUCCEEDED" && status != "FAILED" && status != "CANCELLED") {
		return &domain.ValidationError{Message: "invalid executor result"}
	}
	return f.requests.Report(ctx, workerID, jobID, leaseID, sequence, status, result)
}
