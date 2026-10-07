package api

type createTaskRequest struct {
	Description string                     `json:"description"`
	Context     []manualContextItemRequest `json:"context"`
	AgentID     *string                    `json:"agentId"`
	// Source distinguishes a human/dashboard-designated task from one prelo-messaging-bridge
	// creates for an inbound WhatsApp message — see domain.TaskSource. Omitted/empty defaults to
	// "MANUAL", so the dashboard's existing "Nova task" form needs no change.
	Source *string `json:"source"`
	// ContactAddress (Fase C2) is the WhatsApp sender of a MESSAGING task — a phone or a
	// WhatsApp ID. Only accepted together with source MESSAGING.
	ContactAddress *string `json:"contactAddress"`
}

type manualContextItemRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type taskResponse struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	AgentID     string  `json:"agentId"`
	CreatedAt   string  `json:"createdAt"`
	Source      string  `json:"source"`
	ProjectID   *string `json:"projectId"`
	// Fase C2.
	ClientID       *string `json:"clientId"`
	ContactAddress *string `json:"contactAddress"`
}

type executionResponse struct {
	ExecutionID string  `json:"executionId"`
	TaskID      string  `json:"taskId"`
	AgentID     string  `json:"agentId"`
	Status      string  `json:"status"`
	Result      *string `json:"result"`
	Error       *string `json:"error"`
	RequestID   *string `json:"requestId"`
	Model       *string `json:"model"`
	Provider    *string `json:"provider"`
	StartedAt   *string `json:"startedAt"`
	CompletedAt *string `json:"completedAt"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type chatRequest struct {
	Model      string          `json:"model"`
	Messages   []chatMessage   `json:"messages"`
	Parameters *chatParameters `json:"parameters"`
	TaskID     string          `json:"taskId"`
	AgentID    string          `json:"agentId"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatParameters struct {
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int     `json:"maxTokens"`
}

type createServerRequest struct {
	Name          string `json:"name"`
	Host          string `json:"host"`
	SSHPort       int    `json:"sshPort"`
	SSHUser       string `json:"sshUser"`
	PrivateKeyPEM string `json:"privateKeyPem"`
}

type createProjectRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type addProjectMemberRequest struct {
	DashboardUserID string `json:"dashboardUserId"`
}

type projectResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CreatedAt   string  `json:"createdAt"`
	CreatedBy   string  `json:"createdBy"`
	MemberCount int     `json:"memberCount"`
	// Fase PA settings.
	DefaultAgentID *string `json:"defaultAgentId"`
	Instructions   *string `json:"instructions"`
	CoverColor     string  `json:"coverColor"`
	// Fase C1: the client this project is for.
	ClientID *string `json:"clientId"`
}

type updateProjectRequest struct {
	Name           string  `json:"name"`
	Description    *string `json:"description"`
	DefaultAgentID *string `json:"defaultAgentId"`
	Instructions   *string `json:"instructions"`
	CoverColor     string  `json:"coverColor"`
}

type projectMemberResponse struct {
	UserID  string `json:"userId"`
	Role    string `json:"role"`
	AddedAt string `json:"addedAt"`
	AddedBy string `json:"addedBy"`
}

// serverResponse never includes the credential, not even encrypted — hostKeyFingerprint is the
// one credential-adjacent field it carries, and it's public information by design (TOFU: the
// fingerprint is meant to be compared against, not kept secret).
type serverResponse struct {
	ID                 string  `json:"id"`
	ProjectID          *string `json:"projectId"`
	Name               string  `json:"name"`
	Host               string  `json:"host"`
	SSHPort            int     `json:"sshPort"`
	SSHUser            string  `json:"sshUser"`
	HostKeyFingerprint string  `json:"hostKeyFingerprint"`
	LastStatus         string  `json:"lastStatus"`
	LastCheckedAt      *string `json:"lastCheckedAt"`
	LastError          *string `json:"lastError"`
	CreatedAt          string  `json:"createdAt"`
	CreatedBy          string  `json:"createdBy"`
}

type containerStatusResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

type serverHealthResponse struct {
	Status          string                    `json:"status"`
	Uptime          string                    `json:"uptime"`
	MemoryUsedMB    int                       `json:"memoryUsedMb"`
	MemoryTotalMB   int                       `json:"memoryTotalMb"`
	DiskUsedPercent int                       `json:"diskUsedPercent"`
	Containers      []containerStatusResponse `json:"containers"`
	Error           string                    `json:"error,omitempty"`
}
