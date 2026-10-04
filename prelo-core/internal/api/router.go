package api

import "net/http"

// RegisterRoutes mounts the task and chat endpoints onto an existing mux, so the caller can
// also register cross-cutting routes (like the health check) on the same mux.
func RegisterRoutes(mux *http.ServeMux, tasks *TaskHandler, chat *ChatHandler, tools *ToolHandler, approvals *ApprovalHandler, observability *ObservabilityHandler) {
	mux.HandleFunc("POST /api/v1/tasks", tasks.Create)
	mux.HandleFunc("GET /api/v1/tasks", tasks.List)
	mux.HandleFunc("GET /api/v1/tasks/{taskId}", tasks.Get)
	mux.HandleFunc("POST /api/v1/tasks/{taskId}/execute", tasks.Execute)
	mux.HandleFunc("GET /api/v1/tasks/{taskId}/executions/{executionId}", tasks.GetExecution)
	mux.HandleFunc("GET /api/v1/tasks/{taskId}/executions/latest", tasks.LatestExecution)
	mux.HandleFunc("POST /api/v1/chat", chat.Chat)

	// Etapa 7/8 (ADR-004): tools are catalog-only and gated by PermissionPolicy on every call;
	// approvals are the only way a REQUIRE_APPROVAL call ever actually runs.
	mux.HandleFunc("GET /api/v1/tools", tools.List)
	mux.HandleFunc("POST /api/v1/executions/{executionId}/tools/{toolName}/invoke", tools.Invoke)
	mux.HandleFunc("GET /api/v1/approvals", approvals.ListPending)
	mux.HandleFunc("GET /api/v1/approvals/{approvalId}", approvals.Get)
	mux.HandleFunc("POST /api/v1/approvals/{approvalId}/approve", approvals.Approve)
	mux.HandleFunc("POST /api/v1/approvals/{approvalId}/deny", approvals.Deny)

	// Fase D: read-only trace/tree views over what Fases A-C already persist.
	mux.HandleFunc("GET /api/v1/tasks/{taskId}/executions/{executionId}/turns", observability.Turns)
	mux.HandleFunc("GET /api/v1/tasks/{taskId}/tree", observability.Tree)
}

// RegisterDashboardAuthRoutes mounts the human-login surface (Fase G1). Every path here is
// carved out of JWTAuthMiddleware by isPublicDashboardAuthPath — there is no JWT to present yet
// at login/activation/reset time, and the bootstrap invite endpoint enforces its own
// X-Admin-Token instead.
func RegisterDashboardAuthRoutes(mux *http.ServeMux, auth *DashboardAuthHandler, bootstrap *DashboardBootstrapHandler) {
	mux.HandleFunc("POST /api/v1/dashboard-auth/activate", auth.Activate)
	mux.HandleFunc("POST /api/v1/dashboard-auth/login", auth.Login)
	mux.HandleFunc("POST /api/v1/dashboard-auth/mfa/setup", auth.MfaSetup)
	mux.HandleFunc("POST /api/v1/dashboard-auth/mfa/confirm", auth.MfaConfirm)
	mux.HandleFunc("POST /api/v1/dashboard-auth/mfa/verify", auth.MfaVerify)
	mux.HandleFunc("POST /api/v1/dashboard-auth/refresh", auth.Refresh)
	mux.HandleFunc("POST /api/v1/dashboard-auth/logout", auth.Logout)
	mux.HandleFunc("POST /api/v1/dashboard-auth/password-reset/request", auth.RequestPasswordReset)
	mux.HandleFunc("POST /api/v1/dashboard-auth/password-reset/confirm", auth.ConfirmPasswordReset)
	mux.HandleFunc("POST /api/v1/admin/dashboard-invitations", bootstrap.Invite)
}

// RegisterSettingsRoutes mounts Fase G2's owner-contacts management. Protected (not carved out
// by isPublicDashboardAuthPath) — requiredScope maps both routes to settings:manage, a scope
// only an ADMIN dashboard session's token carries (see dashboardAdminScopes).
func RegisterSettingsRoutes(mux *http.ServeMux, settings *SettingsHandler) {
	mux.HandleFunc("GET /api/v1/settings/owner-contacts", settings.GetOwnerContacts)
	mux.HandleFunc("PUT /api/v1/settings/owner-contacts", settings.PutOwnerContacts)

	// Fase H4: both under /api/v1/settings/... so requiredScope's existing prefix match keeps
	// mapping them to settings:manage without touching auth.go.
	mux.HandleFunc("GET /api/v1/settings/integrations/whatsapp", settings.GetWhatsAppStatus)
	mux.HandleFunc("GET /api/v1/settings/integrations/whatsapp/auto-reply", settings.GetAutoReply)
	mux.HandleFunc("PUT /api/v1/settings/integrations/whatsapp/auto-reply", settings.PutAutoReply)
}

// RegisterUserManagementRoutes mounts the Usuários page's surface — list, invite, change role.
// Protected (not carved out by isPublicDashboardAuthPath); requiredScope maps every route here
// to users:manage, which only an ADMIN dashboard session's token carries.
func RegisterUserManagementRoutes(mux *http.ServeMux, users *UserManagementHandler) {
	mux.HandleFunc("GET /api/v1/users", users.List)
	mux.HandleFunc("POST /api/v1/users", users.Invite)
	mux.HandleFunc("PUT /api/v1/users/{userId}/role", users.ChangeRole)
}

// RegisterServerRoutes mounts Fase S1's server registration + on-demand health check. GET is
// gated to servers:read (ADMIN and OPERATOR); POST/DELETE to servers:manage (ADMIN only) — see
// requiredScope in auth.go.
func RegisterServerRoutes(mux *http.ServeMux, servers *ServerHandler) {
	mux.HandleFunc("POST /api/v1/servers", servers.Create)
	mux.HandleFunc("GET /api/v1/servers", servers.List)
	mux.HandleFunc("GET /api/v1/servers/{serverId}", servers.Get)
	mux.HandleFunc("DELETE /api/v1/servers/{serverId}", servers.Delete)
	mux.HandleFunc("POST /api/v1/servers/{serverId}/health-check", servers.CheckHealth)
}

// RegisterPipelineRoutes mounts Fase P's global "what's active right now" view — gated to
// observability:read (ADMIN and OPERATOR both already carry it) via requiredScope's prefix
// match, same as the per-task tree/turns routes.
func RegisterPipelineRoutes(mux *http.ServeMux, pipeline *PipelineHandler) {
	mux.HandleFunc("GET /api/v1/pipeline", pipeline.Get)
}

// RegisterProjectFileRoutes mounts Fase PA's sealed project files.
func RegisterProjectFileRoutes(mux *http.ServeMux, h *ProjectFileHandler) {
	mux.HandleFunc("GET /api/v1/projects/{projectId}/files", h.List)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/files", h.Upload)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/files/{fileId}/content", h.Content)
	mux.HandleFunc("DELETE /api/v1/projects/{projectId}/files/{fileId}", h.Delete)
}

// RegisterModelConnectionRoutes mounts Fase M's model-connection relay to the llm-gateway vault.
// Instance routes need settings:manage; project routes need projects:read (+ membership, checked
// in the handler) to read and projects:manage to change — see requiredScope in auth.go.
func RegisterModelConnectionRoutes(mux *http.ServeMux, h *ModelConnectionHandler) {
	mux.HandleFunc("GET /api/v1/model-connections/instance", h.GetInstance)
	mux.HandleFunc("PUT /api/v1/model-connections/instance", h.SaveInstance)
	mux.HandleFunc("POST /api/v1/model-connections/instance/retest", h.RetestInstance)
	mux.HandleFunc("DELETE /api/v1/model-connections/instance", h.DeleteInstance)
	mux.HandleFunc("POST /api/v1/model-connections/test", h.Test)
	mux.HandleFunc("GET /api/v1/model-connections/usage", h.InstanceUsage)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/model", h.GetProject)
	mux.HandleFunc("PUT /api/v1/projects/{projectId}/model", h.SaveProject)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/model/retest", h.RetestProject)
	mux.HandleFunc("PUT /api/v1/projects/{projectId}/model/active", h.SetProjectActive)
	mux.HandleFunc("DELETE /api/v1/projects/{projectId}/model", h.DeleteProject)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/model/test", h.TestForProject)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/model/usage", h.ProjectUsage)
}

// RegisterIntegrationRoutes mounts Fase I's per-project API keys and webhooks — all
// integrations:manage (ADMIN) and project-scoped through X-Project-Id, see auth.go.
func RegisterIntegrationRoutes(mux *http.ServeMux, integrations *IntegrationHandler) {
	mux.HandleFunc("GET /api/v1/integrations", integrations.List)
	mux.HandleFunc("POST /api/v1/integrations/api-keys", integrations.CreateApiKey)
	mux.HandleFunc("DELETE /api/v1/integrations/api-keys/{keyId}", integrations.RevokeApiKey)
	mux.HandleFunc("POST /api/v1/integrations/webhooks", integrations.CreateWebhook)
	mux.HandleFunc("DELETE /api/v1/integrations/webhooks/{webhookId}", integrations.DeleteWebhook)
	mux.HandleFunc("POST /api/v1/integrations/webhooks/{webhookId}/test", integrations.TestWebhook)
}

// RegisterProjectRoutes mounts Fase W's project CRUD + membership management. GET is gated to
// projects:read (ADMIN and OPERATOR both carry it); POST/DELETE and every membership route to
// projects:manage (ADMIN only, this first version) — see requiredScope in auth.go.
func RegisterProjectRoutes(mux *http.ServeMux, projects *ProjectHandler) {
	mux.HandleFunc("GET /api/v1/projects", projects.List)
	mux.HandleFunc("POST /api/v1/projects", projects.Create)
	mux.HandleFunc("DELETE /api/v1/projects/{projectId}", projects.Delete)
	mux.HandleFunc("PATCH /api/v1/projects/{projectId}", projects.Update)
	mux.HandleFunc("GET /api/v1/agents", projects.Agents)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/members", projects.ListMembers)
	mux.HandleFunc("POST /api/v1/projects/{projectId}/members", projects.AddMember)
	mux.HandleFunc("DELETE /api/v1/projects/{projectId}/members/{userId}", projects.RemoveMember)
}
