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
	mux.HandleFunc("POST /api/v1/hermes/chat", chat.Chat)

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
