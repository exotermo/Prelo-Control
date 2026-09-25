// Thin client for hermes-go's public API. hermes-go has no authentication today (unlike
// messaging-core's OAuth2) — this client sends no Authorization header at all. Fine for
// localhost; exposing this dashboard beyond that first needs an auth gate added to hermes-go,
// deliberately out of scope for this slice (see ADR-014's "evolução futura").
export const BASE_URL: string =
  (import.meta.env.VITE_HERMES_URL as string | undefined) ?? "http://127.0.0.1:8082";

export class ApiError extends Error {
  status: number;
  code: string | null;

  constructor(status: number, code: string | null, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function parseErrorBody(response: Response): Promise<{ code: string | null; message: string }> {
  try {
    const body = (await response.json()) as { code?: string; message?: string };
    return { code: body.code ?? null, message: body.message ?? response.statusText };
  } catch {
    return { code: null, message: response.statusText || `HTTP ${response.status}` };
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${BASE_URL}${path}`, init);
  if (!response.ok) {
    const { code, message } = await parseErrorBody(response);
    throw new ApiError(response.status, code, message);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

function postJSON<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method: "POST",
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
}

export interface Task {
  id: string;
  description: string;
  status: string;
  agentId: string;
  createdAt: string;
}

export interface ManualContextItem {
  name: string;
  content: string;
}

export function listTasks(): Promise<Task[]> {
  return request<Task[]>("/api/v1/tasks");
}

export function getTask(id: string): Promise<Task> {
  return request<Task>(`/api/v1/tasks/${id}`);
}

export function createTask(description: string, agentId?: string, context?: ManualContextItem[]): Promise<Task> {
  return postJSON<Task>("/api/v1/tasks", { description, agentId, context });
}

export interface Execution {
  executionId: string;
  taskId: string;
  agentId: string;
  status: string;
  result: string | null;
  error: string | null;
  requestId: string | null;
  model: string | null;
  provider: string | null;
  startedAt: string | null;
  completedAt: string | null;
}

export function executeTask(taskId: string): Promise<Execution> {
  return postJSON<Execution>(`/api/v1/tasks/${taskId}/execute`);
}

export function getExecution(taskId: string, executionId: string): Promise<Execution> {
  return request<Execution>(`/api/v1/tasks/${taskId}/executions/${executionId}`);
}

// Assumes the current 1:1 Task→Execution relationship (see TaskHandler.LatestExecution's own
// doc on the Go side) — resolves without the caller needing to already know the execution id.
export function getLatestExecution(taskId: string): Promise<Execution> {
  return request<Execution>(`/api/v1/tasks/${taskId}/executions/latest`);
}

export interface Turn {
  turnNumber: number;
  kind: "LLM_CALL" | "TOOL_CALL" | "SUBTASK";
  requestId: string;
  input: string;
  output: string | null;
  error: string | null;
  startedAt: string;
  completedAt: string | null;
}

export function getTurns(taskId: string, executionId: string): Promise<Turn[]> {
  return request<Turn[]>(`/api/v1/tasks/${taskId}/executions/${executionId}/turns`);
}

export interface TaskTreeNode {
  taskId: string;
  description: string;
  status: string;
  agentId: string;
  depth: number;
  executionStatus: string | null;
  children: TaskTreeNode[];
}

export function getTaskTree(taskId: string): Promise<TaskTreeNode> {
  return request<TaskTreeNode>(`/api/v1/tasks/${taskId}/tree`);
}

export interface ToolDefinition {
  name: string;
  description: string;
  riskLevel: "LOW" | "MODERATE" | "HIGH";
}

export function listTools(): Promise<ToolDefinition[]> {
  return request<ToolDefinition[]>("/api/v1/tools");
}

export interface ApprovalRequest {
  id: string;
  toolCallId: string;
  scope: string;
  status: string;
  requestedAt: string;
  expiresAt: string;
  decidedAt: string | null;
  decidedBy: string | null;
}

export function listPendingApprovals(): Promise<ApprovalRequest[]> {
  return request<ApprovalRequest[]>("/api/v1/approvals");
}

export function approveRequest(id: string, decidedBy: string): Promise<ApprovalRequest> {
  return postJSON<ApprovalRequest>(`/api/v1/approvals/${id}/approve`, { decidedBy });
}

export function denyRequest(id: string, decidedBy: string): Promise<ApprovalRequest> {
  return postJSON<ApprovalRequest>(`/api/v1/approvals/${id}/deny`, { decidedBy });
}
