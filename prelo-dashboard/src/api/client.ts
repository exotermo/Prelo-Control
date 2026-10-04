// Thin client for prelo-core's public API. Fase G1: prelo-core now requires a dashboard session
// (token_use=dashboard) for every /api/v1/* route except /dashboard-auth/* itself — every call
// below carries an Authorization: Bearer header.
export const BASE_URL: string =
  (import.meta.env.VITE_PRELO_URL as string | undefined) ?? "http://127.0.0.1:8082";

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

// projectId (Fase W) is sent as X-Project-Id on every project-scoped call — absent means the
// "unassigned" bucket (prelo-core's JWTAuthMiddleware.resolveProject default), never "everything".
async function request<T>(path: string, init: RequestInit, token?: string, projectId?: string): Promise<T> {
  const headers = new Headers(init.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (projectId) headers.set("X-Project-Id", projectId);
  const response = await fetch(`${BASE_URL}${path}`, { ...init, headers });
  if (!response.ok) {
    const { code, message } = await parseErrorBody(response);
    throw new ApiError(response.status, code, message);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

function postJSON<T>(path: string, body: unknown, token?: string, projectId?: string): Promise<T> {
  return request<T>(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }, token, projectId);
}

// --- dashboard-auth (Fase G1): no token yet at this stage, except refresh/logout which use the
// HttpOnly cookie plus the X-Dashboard-Request marker header as CSRF defense. ---

async function authRequest<T>(path: string, body: unknown, marker = false): Promise<T> {
  return request<T>(path, {
    method: "POST", credentials: "include",
    headers: { "Content-Type": "application/json", ...(marker ? { "X-Dashboard-Request": "1" } : {}) },
    body: JSON.stringify(body),
  });
}

export interface LoginChallenge { challenge: string; nextStep: "TOTP_REQUIRED" | "TOTP_SETUP_REQUIRED" }
export interface TotpSetup { secret: string; otpauthUri: string }
export interface DashboardSession { accessToken: string; expiresIn: number; recoveryCodes: string[] }

export function startHumanLogin(email: string, password: string): Promise<LoginChallenge> {
  return authRequest<LoginChallenge>("/api/v1/dashboard-auth/login", { email, password });
}
export function startTotpSetup(challenge: string): Promise<TotpSetup> {
  return authRequest<TotpSetup>("/api/v1/dashboard-auth/mfa/setup", { challenge });
}
export function finishTotp(challenge: string, code: string, enrollment: boolean): Promise<DashboardSession> {
  return authRequest<DashboardSession>(`/api/v1/dashboard-auth/mfa/${enrollment ? "confirm" : "verify"}`, { challenge, code });
}
export function refreshHumanSession(): Promise<DashboardSession> {
  return authRequest<DashboardSession>("/api/v1/dashboard-auth/refresh", {}, true);
}
export function logoutHumanSession(): Promise<void> {
  return authRequest<void>("/api/v1/dashboard-auth/logout", {}, true);
}
export function activateHumanUser(token: string, password: string): Promise<void> {
  return authRequest<void>("/api/v1/dashboard-auth/activate", { token, password });
}
export function requestPasswordReset(email: string): Promise<void> {
  return authRequest<void>("/api/v1/dashboard-auth/password-reset/request", { email });
}
export function confirmPasswordReset(token: string, password: string): Promise<void> {
  return authRequest<void>("/api/v1/dashboard-auth/password-reset/confirm", { token, password });
}

// --- tasks / executions / tools / approvals (all now require the dashboard session token) ---

export interface Task {
  id: string;
  description: string;
  status: string;
  agentId: string;
  createdAt: string;
  // MANUAL = created through this page's own form (or another API caller acting as one);
  // MESSAGING = prelo-messaging-bridge created it for one inbound WhatsApp message. Found
  // 2026-10-02: without this, every WhatsApp exchange showed up here indistinguishable from a
  // task someone actually designated.
  source: "MANUAL" | "MESSAGING";
  // Fase W: the project this task belongs to, or null for the pre-Fase-W "unassigned" bucket.
  projectId: string | null;
  // Fase C2: the client it is for, and (WhatsApp) who sent it — a phone or a "…@lid" WhatsApp ID.
  clientId?: string | null;
  contactAddress?: string | null;
}

export interface ManualContextItem {
  name: string;
  content: string;
}

export function listTasks(token: string, projectId?: string): Promise<Task[]> {
  return request<Task[]>("/api/v1/tasks", { method: "GET" }, token, projectId);
}

export function getTask(token: string, id: string): Promise<Task> {
  return request<Task>(`/api/v1/tasks/${id}`, { method: "GET" }, token);
}

export function createTask(token: string, description: string, agentId?: string, context?: ManualContextItem[], projectId?: string): Promise<Task> {
  return postJSON<Task>("/api/v1/tasks", { description, agentId, context }, token, projectId);
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

export function executeTask(token: string, taskId: string): Promise<Execution> {
  return postJSON<Execution>(`/api/v1/tasks/${taskId}/execute`, undefined, token);
}

export function getExecution(token: string, taskId: string, executionId: string): Promise<Execution> {
  return request<Execution>(`/api/v1/tasks/${taskId}/executions/${executionId}`, { method: "GET" }, token);
}

// Assumes the current 1:1 Task→Execution relationship (see TaskHandler.LatestExecution's own
// doc on the Go side) — resolves without the caller needing to already know the execution id.
export function getLatestExecution(token: string, taskId: string): Promise<Execution> {
  return request<Execution>(`/api/v1/tasks/${taskId}/executions/latest`, { method: "GET" }, token);
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

export function getTurns(token: string, taskId: string, executionId: string): Promise<Turn[]> {
  return request<Turn[]>(`/api/v1/tasks/${taskId}/executions/${executionId}/turns`, { method: "GET" }, token);
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

export function getTaskTree(token: string, taskId: string): Promise<TaskTreeNode> {
  return request<TaskTreeNode>(`/api/v1/tasks/${taskId}/tree`, { method: "GET" }, token);
}

// Fase P: the Pipeline page's own tree shape — one pipelineStatus per node instead of the raw
// Task/Execution status pair TaskTreeNode carries, since the whole point of this page is "what
// is this node waiting on right now" (including AWAITING_APPROVAL/AWAITING_SUBTASK, which never
// show up in TaskTreeNode at all).
export interface PipelineNode {
  taskId: string;
  description: string;
  agentId: string;
  depth: number;
  pipelineStatus: "CREATED" | "QUEUED" | "RUNNING" | "AWAITING_APPROVAL" | "AWAITING_SUBTASK" | "COMPLETED" | "FAILED";
  children: PipelineNode[];
}

export function getPipeline(token: string, projectId?: string): Promise<PipelineNode[]> {
  return request<PipelineNode[]>("/api/v1/pipeline", { method: "GET" }, token, projectId);
}

export interface ToolDefinition {
  name: string;
  description: string;
  riskLevel: "LOW" | "MODERATE" | "HIGH";
}

export function listTools(token: string): Promise<ToolDefinition[]> {
  return request<ToolDefinition[]>("/api/v1/tools", { method: "GET" }, token);
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

export function listPendingApprovals(token: string, projectId?: string): Promise<ApprovalRequest[]> {
  return request<ApprovalRequest[]>("/api/v1/approvals", { method: "GET" }, token, projectId);
}

export function approveRequest(token: string, id: string, decidedBy: string): Promise<ApprovalRequest> {
  return postJSON<ApprovalRequest>(`/api/v1/approvals/${id}/approve`, { decidedBy }, token);
}

export function denyRequest(token: string, id: string, decidedBy: string): Promise<ApprovalRequest> {
  return postJSON<ApprovalRequest>(`/api/v1/approvals/${id}/deny`, { decidedBy }, token);
}

// --- settings (Fase G2): prelo-messaging-bridge's owner-contacts, proxied by prelo-core ---

export interface OwnerContacts {
  contacts: string[];
}

export function getOwnerContacts(token: string): Promise<OwnerContacts> {
  return request<OwnerContacts>("/api/v1/settings/owner-contacts", { method: "GET" }, token);
}

export function setOwnerContacts(token: string, contacts: string[]): Promise<OwnerContacts> {
  return request<OwnerContacts>("/api/v1/settings/owner-contacts",
    { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ contacts }) }, token);
}

// --- integrations (Fase H4): WhatsApp channel status + auto-reply toggle, proxied by prelo-core
// through prelo-messaging-bridge to messaging-core — no more checking psql/docker logs to know
// if WhatsApp is connected. ---

export interface ChannelStatus {
  id: string;
  channelType: string;
  status: string;
  externalRef: string;
  createdAt: string;
}

export interface WhatsAppStatus {
  channels: ChannelStatus[];
}

export interface AutoReplyStatus {
  staticEnabled: boolean;
  runtimeEnabled: boolean;
  effectiveEnabled: boolean;
}

export function getWhatsAppStatus(token: string): Promise<WhatsAppStatus> {
  return request<WhatsAppStatus>("/api/v1/settings/integrations/whatsapp", { method: "GET" }, token);
}

export function getAutoReplyStatus(token: string): Promise<AutoReplyStatus> {
  return request<AutoReplyStatus>("/api/v1/settings/integrations/whatsapp/auto-reply", { method: "GET" }, token);
}

export function setAutoReply(token: string, enabled: boolean): Promise<AutoReplyStatus> {
  return request<AutoReplyStatus>("/api/v1/settings/integrations/whatsapp/auto-reply",
    { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ enabled }) }, token);
}

// --- users (Fase H — RBAC): ADMIN-only, gated by the users:manage scope server-side. The
// client-side role check below is purely a UI convenience (hide controls a request would 403
// on anyway) — prelo-core is what actually enforces it. ---

export type DashboardRole = "ADMIN" | "OPERATOR";

export interface DashboardUserSummary {
  id: string;
  email: string;
  role: DashboardRole;
  activatedAt: string | null;
  totpEnabled: boolean;
  createdAt: string;
}

export function listDashboardUsers(token: string): Promise<DashboardUserSummary[]> {
  return request<DashboardUserSummary[]>("/api/v1/users", { method: "GET" }, token);
}

export function inviteDashboardUser(token: string, email: string, role: DashboardRole): Promise<void> {
  return postJSON<void>("/api/v1/users", { email, role }, token);
}

export function changeDashboardUserRole(token: string, userId: string, role: DashboardRole): Promise<DashboardUserSummary> {
  return request<DashboardUserSummary>(`/api/v1/users/${userId}/role`,
    { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ role }) }, token);
}

// Reads sub/scope straight off the JWT payload — never trusted for authorization (prelo-core
// re-checks every scope server-side), only used to decide what the UI shows (e.g. the Usuários
// nav link) without an extra round trip.
export function decodeDashboardToken(token: string): { sub: string; scopes: string[] } | null {
  try {
    const payload = JSON.parse(atob(token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/")));
    return { sub: payload.sub, scopes: Array.isArray(payload.scope) ? payload.scope : [] };
  } catch {
    return null;
  }
}

// --- servers (Fase S1): registration + on-demand SSH health check. GET/health-check need
// servers:read (ADMIN and OPERATOR); create/delete need servers:manage (ADMIN only). The private
// key is write-only — ServerSummary never carries it back. ---

export interface ServerSummary {
  id: string;
  // Fase W: the project this server belongs to, or null for the pre-Fase-W "unassigned" bucket.
  projectId: string | null;
  name: string;
  host: string;
  sshPort: number;
  sshUser: string;
  hostKeyFingerprint: string;
  lastStatus: "UNKNOWN" | "ONLINE" | "OFFLINE" | "ERROR";
  lastCheckedAt: string | null;
  lastError: string | null;
  createdAt: string;
  createdBy: string;
}

export interface ContainerStatus {
  id: string;
  name: string;
  image: string;
  status: string;
}

export interface ServerHealth {
  status: "UNKNOWN" | "ONLINE" | "OFFLINE" | "ERROR";
  uptime: string;
  memoryUsedMb: number;
  memoryTotalMb: number;
  diskUsedPercent: number;
  containers: ContainerStatus[];
  error?: string;
}

export function listServers(token: string, projectId?: string): Promise<ServerSummary[]> {
  return request<ServerSummary[]>("/api/v1/servers", { method: "GET" }, token, projectId);
}

export function createServer(
  token: string,
  payload: { name: string; host: string; sshPort: number; sshUser: string; privateKeyPem: string },
  projectId?: string,
): Promise<ServerSummary> {
  return postJSON<ServerSummary>("/api/v1/servers", payload, token, projectId);
}

export function deleteServer(token: string, id: string): Promise<void> {
  return request<void>(`/api/v1/servers/${id}`, { method: "DELETE" }, token);
}

export function checkServerHealth(token: string, id: string): Promise<ServerHealth> {
  return postJSON<ServerHealth>(`/api/v1/servers/${id}/health-check`, undefined, token);
}

// --- projects (Fase W): isolated work environments scoping Tasks/Servers/Pipeline/Approvals.
// GET needs projects:read (ADMIN and OPERATOR both carry it — List itself narrows an OPERATOR to
// only the projects they're a member of); create/delete/membership need projects:manage (ADMIN
// only, this first version). ---

export type CoverColor = "ink" | "clay" | "moss" | "ocean" | "plum" | "mustard";

export interface ProjectSummary {
  id: string;
  name: string;
  description: string | null;
  createdAt: string;
  createdBy: string;
  memberCount: number;
  // Fase PA settings.
  defaultAgentId: string | null;
  instructions: string | null;
  coverColor: CoverColor;
  // Fase C1: the client this project is for.
  clientId: string | null;
}

export interface ProjectSettingsPayload {
  name: string;
  description: string | null;
  defaultAgentId: string | null;
  instructions: string | null;
  coverColor: CoverColor;
}

export function updateProject(token: string, id: string, payload: ProjectSettingsPayload): Promise<ProjectSummary> {
  return request<ProjectSummary>(`/api/v1/projects/${id}`,
    { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) }, token);
}

export interface AgentSummary { id: string; name: string; description: string }

export function listAgents(token: string): Promise<AgentSummary[]> {
  return request<AgentSummary[]>("/api/v1/agents", { method: "GET" }, token);
}

// --- project files (Fase PA): sealed at rest by prelo-core; up to 100 MB each. ---

export interface ProjectFile {
  id: string;
  name: string;
  contentType: string;
  kind: "pdf" | "image" | "text" | "other";
  inline: boolean;
  sizeBytes: number;
  sha256: string;
  uploadedBy: string;
  createdAt: string;
}

export interface ProjectFileList { files: ProjectFile[]; totalBytes: number; maxFileBytes: number }

export function listProjectFiles(token: string, projectId: string): Promise<ProjectFileList> {
  return request<ProjectFileList>(`/api/v1/projects/${projectId}/files`, { method: "GET" }, token);
}

export function deleteProjectFile(token: string, projectId: string, fileId: string): Promise<void> {
  return request<void>(`/api/v1/projects/${projectId}/files/${fileId}`, { method: "DELETE" }, token);
}

/** Fetches a file (with the session token, which an <iframe src> can't send) as a Blob. */
export async function fetchProjectFile(token: string, projectId: string, fileId: string, download = false): Promise<Blob> {
  const response = await fetch(`${BASE_URL}/api/v1/projects/${projectId}/files/${fileId}/content${download ? "?download=1" : ""}`,
    { headers: { Authorization: `Bearer ${token}` } });
  if (!response.ok) {
    const { code, message } = await parseErrorBody(response);
    throw new ApiError(response.status, code, message);
  }
  return response.blob();
}

/** Uploads with progress (fetch has no upload progress, XMLHttpRequest does). */
export function uploadProjectFile(token: string, projectId: string, file: File, onProgress: (fraction: number) => void):
  { promise: Promise<ProjectFile>; abort: () => void } {
  const xhr = new XMLHttpRequest();
  const promise = new Promise<ProjectFile>((resolve, reject) => {
    xhr.open("POST", `${BASE_URL}/api/v1/projects/${projectId}/files`);
    xhr.setRequestHeader("Authorization", `Bearer ${token}`);
    xhr.upload.onprogress = (event) => { if (event.lengthComputable) onProgress(event.loaded / event.total); };
    xhr.onload = () => {
      let body: { code?: string; message?: string } & Partial<ProjectFile> = {};
      try { body = JSON.parse(xhr.responseText); } catch { /* empty or non-JSON */ }
      if (xhr.status >= 200 && xhr.status < 300) resolve(body as ProjectFile);
      else reject(new ApiError(xhr.status, body.code ?? null, body.message ?? `HTTP ${xhr.status}`));
    };
    xhr.onerror = () => reject(new ApiError(0, null, "falha de rede no envio"));
    xhr.onabort = () => reject(new ApiError(0, "aborted", "envio cancelado"));
    const form = new FormData();
    form.append("file", file);
    xhr.send(form);
  });
  return { promise, abort: () => xhr.abort() };
}

export interface ProjectMember {
  userId: string;
  addedAt: string;
  addedBy: string;
}

export function listProjects(token: string): Promise<ProjectSummary[]> {
  return request<ProjectSummary[]>("/api/v1/projects", { method: "GET" }, token);
}

export function createProject(token: string, name: string, description?: string): Promise<ProjectSummary> {
  return postJSON<ProjectSummary>("/api/v1/projects", { name, description }, token);
}

export function deleteProject(token: string, id: string): Promise<void> {
  return request<void>(`/api/v1/projects/${id}`, { method: "DELETE" }, token);
}

export function listProjectMembers(token: string, projectId: string): Promise<ProjectMember[]> {
  return request<ProjectMember[]>(`/api/v1/projects/${projectId}/members`, { method: "GET" }, token);
}

export function addProjectMember(token: string, projectId: string, dashboardUserId: string): Promise<void> {
  return postJSON<void>(`/api/v1/projects/${projectId}/members`, { dashboardUserId }, token);
}

export function removeProjectMember(token: string, projectId: string, userId: string): Promise<void> {
  return request<void>(`/api/v1/projects/${projectId}/members/${userId}`, { method: "DELETE" }, token);
}

// --- model connections (Fase M): provider keys live sealed in the llm-gateway vault; prelo-core
// only relays. Nothing here ever receives a key back — only hasKey / keyLast4. ---

export type ModelProvider = "anthropic" | "openai" | "openai_compatible";

export interface ModelConnection {
  configured: boolean;
  scope: "INSTANCE" | "PROJECT";
  projectId: string | null;
  provider: ModelProvider | null;
  baseUrl: string | null;
  model: string | null;
  active: boolean;
  hasKey: boolean;
  keyLast4: string | null;
  lastTestAt: string | null;
  lastTestOk: boolean | null;
  lastTestLatencyMs: number | null;
  lastTestError: string | null;
  updatedAt: string | null;
}

export interface ProjectModelView { own: ModelConnection; instance: ModelConnection; usingOwn: boolean }
export interface ModelTestResult { ok: boolean; latencyMs: number; models: string[]; error: string | null }
export interface ModelUsageDay { day: string; calls: number; inputTokens: number; outputTokens: number }
export interface ModelSavePayload { provider: ModelProvider; baseUrl?: string; model: string; apiKey?: string }

const jsonInit = (method: string, body?: unknown): RequestInit =>
  ({ method, headers: { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body) });

/** Where a model connection lives: the instance default, or one project's own. */
export type ModelScope = { kind: "instance" } | { kind: "project"; projectId: string };
const modelBase = (scope: ModelScope) =>
  scope.kind === "instance" ? "/api/v1/model-connections/instance" : `/api/v1/projects/${scope.projectId}/model`;

export function getInstanceModel(token: string): Promise<ModelConnection> {
  return request<ModelConnection>("/api/v1/model-connections/instance", { method: "GET" }, token);
}
export function getProjectModel(token: string, projectId: string): Promise<ProjectModelView> {
  return request<ProjectModelView>(`/api/v1/projects/${projectId}/model`, { method: "GET" }, token);
}
export function testModelConnection(token: string, scope: ModelScope, payload: { provider: ModelProvider; baseUrl?: string; apiKey?: string }): Promise<ModelTestResult> {
  const path = scope.kind === "instance" ? "/api/v1/model-connections/test" : `/api/v1/projects/${scope.projectId}/model/test`;
  return request<ModelTestResult>(path, jsonInit("POST", payload), token);
}
export function saveModelConnection(token: string, scope: ModelScope, payload: ModelSavePayload): Promise<ModelConnection> {
  return request<ModelConnection>(modelBase(scope), jsonInit("PUT", payload), token);
}
export function retestModelConnection(token: string, scope: ModelScope): Promise<{ connection: ModelConnection; test: ModelTestResult }> {
  return request(`${modelBase(scope)}/retest`, { method: "POST" }, token);
}
export function deleteModelConnection(token: string, scope: ModelScope): Promise<void> {
  return request<void>(modelBase(scope), { method: "DELETE" }, token);
}
export function setProjectModelActive(token: string, projectId: string, active: boolean): Promise<ModelConnection> {
  return request<ModelConnection>(`/api/v1/projects/${projectId}/model/active`, jsonInit("PUT", { active }), token);
}
export function getModelUsage(token: string, scope: ModelScope, days = 7): Promise<ModelUsageDay[]> {
  const path = scope.kind === "instance" ? "/api/v1/model-connections/usage" : `/api/v1/projects/${scope.projectId}/model/usage`;
  return request<ModelUsageDay[]>(`${path}?days=${days}`, { method: "GET" }, token);
}

// --- integrations (Fase I): per-project API keys + outbound webhooks, ADMIN only
// (integrations:manage). The raw key / signing secret come back exactly once, on creation. ---

export type ApiKeyScope = "tasks:create" | "tasks:read" | "tasks:execute" | "observability:read";
export type WebhookEvent = "task.completed" | "task.failed" | "approval.pending" | "server.offline";

export interface ApiKeySummary {
  id: string;
  name: string;
  displayPrefix: string;
  scopes: ApiKeyScope[];
  expiresAt: string | null;
  lastUsedAt: string | null;
  createdAt: string;
}

export interface WebhookDeliverySummary {
  event: string;
  status: "PENDING" | "SENDING" | "DELIVERED" | "RETRY" | "DEAD";
  statusCode: number | null;
  error: string | null;
  attempt: number;
  createdAt: string;
}

export interface WebhookSummary {
  id: string;
  name: string;
  url: string;
  events: WebhookEvent[];
  createdAt: string;
  lastDelivery: WebhookDeliverySummary | null;
}

export interface ProjectIntegrations {
  apiKeys: ApiKeySummary[];
  webhooks: WebhookSummary[];
}

export function listIntegrations(token: string, projectId: string): Promise<ProjectIntegrations> {
  return request<ProjectIntegrations>("/api/v1/integrations", { method: "GET" }, token, projectId);
}

export function createApiKey(
  token: string, projectId: string, payload: { name: string; scopes: ApiKeyScope[]; expiresInDays: number },
): Promise<{ apiKey: ApiKeySummary; rawKey: string }> {
  return postJSON("/api/v1/integrations/api-keys", payload, token, projectId);
}

export function revokeApiKey(token: string, projectId: string, id: string): Promise<void> {
  return request<void>(`/api/v1/integrations/api-keys/${id}`, { method: "DELETE" }, token, projectId);
}

export function createWebhook(
  token: string, projectId: string, payload: { name: string; url: string; events: WebhookEvent[] },
): Promise<{ webhook: WebhookSummary; secret: string }> {
  return postJSON("/api/v1/integrations/webhooks", payload, token, projectId);
}

export function deleteWebhook(token: string, projectId: string, id: string): Promise<void> {
  return request<void>(`/api/v1/integrations/webhooks/${id}`, { method: "DELETE" }, token, projectId);
}

export function sendWebhookTest(token: string, projectId: string, id: string): Promise<void> {
  return postJSON<void>(`/api/v1/integrations/webhooks/${id}/test`, undefined, token, projectId);
}

// --- Fase C1: clients (CRM), search, home (recent + pending) and the client timeline ---

export type ClientStatus = "LEAD" | "ACTIVE" | "INACTIVE" | "DISCARDED";
export type ClientStage = "NEW" | "ANALYZED" | "CONTACTED" | "REPLIED" | "QUALIFIED";
export type ContactKind = "PHONE" | "WHATSAPP" | "EMAIL";

export interface ClientContact { id: string; kind: ContactKind; value: string; isPrimary: boolean }

export interface Client {
  id: string;
  name: string;
  company: string | null;
  status: ClientStatus;
  stage: ClientStage;
  source: "MANUAL" | "OSM" | "WHATSAPP";
  address: string | null;
  city: string | null;
  website: string | null;
  notes: string | null;
  optedOutAt: string | null;
  createdAt: string;
  updatedAt: string;
  version: number;
  primaryContact?: ClientContact;
  projectCount?: number;
  contacts?: ClientContact[];
  projects?: ProjectSummary[];
}

export interface ClientPayload {
  version?: number;
  name: string;
  company?: string | null;
  status: ClientStatus;
  stage?: ClientStage;
  address?: string | null;
  city?: string | null;
  website?: string | null;
  notes?: string | null;
  contacts?: { kind: ContactKind; value: string; isPrimary?: boolean }[];
}

export function listClients(token: string, status?: ClientStatus): Promise<Client[]> {
  return request(`/api/v1/clients${status ? `?status=${status}` : ""}`, { method: "GET" }, token);
}
export function getClient(token: string, clientId: string): Promise<Client> {
  return request(`/api/v1/clients/${clientId}`, { method: "GET" }, token);
}
export function createClient(token: string, payload: ClientPayload): Promise<Client> {
  return postJSON("/api/v1/clients", payload, token);
}
export function updateClient(token: string, clientId: string, payload: ClientPayload): Promise<Client> {
  return request(`/api/v1/clients/${clientId}`, { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) }, token);
}
export function deleteClient(token: string, clientId: string): Promise<void> {
  return request(`/api/v1/clients/${clientId}`, { method: "DELETE" }, token);
}
export function addClientContact(token: string, clientId: string, contact: { kind: ContactKind; value: string; isPrimary?: boolean }): Promise<ClientContact> {
  return postJSON(`/api/v1/clients/${clientId}/contacts`, contact, token);
}
export function removeClientContact(token: string, clientId: string, contactId: string): Promise<void> {
  return request(`/api/v1/clients/${clientId}/contacts/${contactId}`, { method: "DELETE" }, token);
}
export function setProjectClient(token: string, projectId: string, clientId: string | null): Promise<void> {
  return request(`/api/v1/projects/${projectId}/client`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ clientId }) }, token);
}

export interface TimelineEntry {
  kind: "CLIENT_CREATED" | "PROJECT" | "TASK" | "FILE";
  id: string;
  title: string;
  detail: string;
  status: string;
  projectId: string | null;
  at: string;
}
export function getClientTimeline(token: string, clientId: string, before?: string): Promise<TimelineEntry[]> {
  const query = before ? `?before=${encodeURIComponent(before)}` : "";
  return request(`/api/v1/clients/${clientId}/timeline${query}`, { method: "GET" }, token);
}

export type SearchKind = "CLIENT" | "PROJECT" | "TASK" | "FILE";
export interface SearchHit {
  kind: SearchKind;
  id: string;
  title: string;
  subtitle: string;
  projectId: string | null;
  clientId: string | null;
  status: string;
  at: string;
}
export interface SearchResults { clients: SearchHit[]; projects: SearchHit[]; tasks: SearchHit[]; files: SearchHit[] }

export function search(token: string, query: string, signal?: AbortSignal): Promise<SearchResults> {
  return request(`/api/v1/search?q=${encodeURIComponent(query)}`, { method: "GET", signal }, token);
}

export type RecentKind = "CLIENT" | "PROJECT" | "TASK";
export interface RecentItem { kind: RecentKind; id: string; title: string; subtitle: string; status: string; projectId: string | null; viewedAt: string }
export interface PendingItem {
  kind: "APPROVAL" | "RUNNING" | "FAILED";
  id: string;
  taskId: string;
  title: string;
  detail: string;
  projectId: string | null;
  projectName: string | null;
  at: string;
}
export function getHome(token: string): Promise<{ recent: RecentItem[]; pending: PendingItem[] }> {
  return request("/api/v1/home", { method: "GET" }, token);
}
/** Best-effort: remembering what was opened must never break the screen that opened it. */
export function touchRecent(token: string, kind: RecentKind, id: string): void {
  void postJSON("/api/v1/recent", { kind, id }, token).catch(() => {});
}
