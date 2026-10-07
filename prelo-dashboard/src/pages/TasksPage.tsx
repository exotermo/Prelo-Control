import { useEffect, useState } from "react";
import {
  ApiError,
  createTask,
  executeTask,
  estimateTask,
  getLatestExecution,
  getTask,
  getTaskTree,
  getTurns,
  listExecutorRequests,
  listTasks,
  type Execution,
  type Task,
  type TaskTreeNode,
  type TaskEstimate,
  type ExecutorRequestView,
  type Turn,
} from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { useProject } from "../context/ProjectContext";
import { TaskClientBox } from "../components/TaskClientBox";
import { PipelineTimeline } from "../components/PipelineTimeline";
import { useLiveRefresh } from "../context/LiveEventsContext";

function statusBadge(status: string) {
  return <span className={`badge badge-${status.toLowerCase()}`}>{status}</span>;
}

function estimateRange(range: { min: number; expected: number; max: number }, unit = "") {
  return `${range.min.toLocaleString("pt-BR")}–${range.max.toLocaleString("pt-BR")} ${unit} (esperado ${range.expected.toLocaleString("pt-BR")})`;
}

function confidenceLabel(level: string) {
  return level === "HIGH" ? "confiança alta" : level === "MEDIUM" ? "confiança média" : "confiança baixa";
}

function formatBytes(bytes: number) {
  return bytes >= 1024 ** 3 ? `${(bytes / 1024 ** 3).toFixed(1)} GiB` : `${Math.round(bytes / 1024 ** 2)} MiB`;
}

function estimateFingerprint(description: string, agentId: string, projectId?: string) {
  return `${projectId ?? "unassigned"}\u0000${agentId.trim() || "default"}\u0000${description.trim()}`;
}

function estimateStorageKey(taskId: string) { return `prelo.task-estimate.v1.${taskId}`; }

function readSavedEstimate(taskId: string): TaskEstimate | null {
  try {
    const raw = localStorage.getItem(estimateStorageKey(taskId));
    return raw ? JSON.parse(raw) as TaskEstimate : null;
  } catch { return null; }
}

export function TasksPage() {
  const { token } = useAuth();
  const { projectId } = useProject();
  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const [description, setDescription] = useState("");
  const [agentId, setAgentId] = useState("");
  const [creating, setCreating] = useState(false);
  const [showMessaging, setShowMessaging] = useState(false);
  const [estimate, setEstimate] = useState<{ fingerprint: string; value: TaskEstimate } | null>(null);
  const [estimating, setEstimating] = useState(false);
  const [estimateUnavailable, setEstimateUnavailable] = useState(false);
  const currentEstimateFingerprint = estimateFingerprint(description, agentId, projectId ?? undefined);
  const visibleEstimate = estimate?.fingerprint === currentEstimateFingerprint ? estimate.value : null;

  useEffect(() => {
    if (!token || description.trim().length < 8) {
      setEstimate(null);
      setEstimating(false);
      setEstimateUnavailable(false);
      return;
    }
    let cancelled = false;
    const fingerprint = estimateFingerprint(description, agentId, projectId ?? undefined);
    setEstimate(null);
    const timeout = window.setTimeout(() => {
      setEstimating(true);
      setEstimateUnavailable(false);
      estimateTask(token, description.trim(), agentId.trim() || undefined, projectId ?? undefined)
        .then((value) => { if (!cancelled) setEstimate({ fingerprint, value }); })
        .catch(() => { if (!cancelled) { setEstimate(null); setEstimateUnavailable(true); } })
        .finally(() => { if (!cancelled) setEstimating(false); });
    }, 450);
    return () => { cancelled = true; window.clearTimeout(timeout); };
  }, [token, description, agentId, projectId]);

  async function refresh() {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      setTasks(await listTasks(token, projectId ?? undefined));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar tasks.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, projectId]);

  async function handleCreate(event: React.FormEvent) {
    event.preventDefault();
    if (!description.trim() || !token) return;
    setCreating(true);
    setError(null);
    try {
      const estimateForSubmittedTask = visibleEstimate;
      const task = await createTask(token, description.trim(), agentId.trim() || undefined, undefined, projectId ?? undefined);
      if (estimateForSubmittedTask) {
        try { localStorage.setItem(estimateStorageKey(task.id), JSON.stringify(estimateForSubmittedTask)); } catch { /* comparison remains a best-effort UX cache */ }
      }
      setDescription("");
      setAgentId("");
      await refresh();
      setSelectedId(task.id);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao criar task.");
    } finally {
      setCreating(false);
    }
  }

  const messagingTasks = tasks.filter((t) => t.source === "MESSAGING");
  const visibleTasks = showMessaging ? tasks : tasks.filter((t) => t.source !== "MESSAGING");

  return (
    <div className="task-layout">
      <div>
        <div className="page-header">
          <h2>Tasks</h2>
          <button onClick={() => void refresh()} disabled={loading}>
            {loading ? "Atualizando…" : "Atualizar"}
          </button>
        </div>

        <form className="card" style={{ marginBottom: 20 }} onSubmit={handleCreate}>
          <h3>Nova task</h3>
          <label>
            Descrição
            <textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="O que o agente deve fazer?"
              required
            />
          </label>
          <label>
            Agent id (opcional — default &quot;general&quot;)
            <input value={agentId} onChange={(event) => setAgentId(event.target.value)} placeholder="general" />
          </label>
          <section className="task-estimate" aria-live="polite" aria-label="Estimativa da task">
            <div className="page-header"><h4>Estimativa antes de criar</h4>{estimating && <span className="muted">Calculando…</span>}</div>
            {estimateUnavailable && <p className="muted">Estimativa temporariamente indisponível. Isso não impede a criação.</p>}
            {visibleEstimate && <>
              <p className="muted">Faixas aproximadas · tipo {visibleEstimate.taskKind.toLowerCase()} · perfil {visibleEstimate.modelProfile} · contexto {visibleEstimate.contextSize}. Nenhum recurso é reservado.</p>
              <div className="task-estimate-grid">
                <article><strong>Hardware</strong>
                  {visibleEstimate.hardware.required ? <>
                    <span>{visibleEstimate.hardware.profileId}: {visibleEstimate.hardware.cpuMilli ?? 0} mCPU, {formatBytes(visibleEstimate.hardware.memoryBytes ?? 0)}, {formatBytes(visibleEstimate.hardware.temporaryBytes ?? 0)} temporários</span>
                    <span>Concorrência necessária: {estimateRange(visibleEstimate.hardware.concurrency, "container(es)")}</span>
                    {visibleEstimate.hardware.workerCapacity ? <>
                      <span>Worker: {visibleEstimate.hardware.workerCapacity.availableSlots}/{visibleEstimate.hardware.workerCapacity.maximumSlots} vagas · observada {new Date(visibleEstimate.hardware.workerCapacity.observedAt).toLocaleTimeString()}</span>
                      {visibleEstimate.hardware.workerCapacity.maximumSlots === 0
                        ? <span role="status">O worker reporta que este perfil excede sua capacidade estrutural; o pedido pode ser recusado para este host.</span>
                        : visibleEstimate.hardware.workerCapacity.availableSlots === 0
                          ? <span role="status">Sem vaga temporária. Se a operação for aprovada, ficará aguardando recursos do servidor.</span>
                          : <span>Há vaga reportada agora; a admissão será reavaliada pelo servidor no momento da execução.</span>}
                    </> : <span role="status">Sem snapshot recente do worker; capacidade não confirmada.</span>}
                  </> : <span>Container não previsto para esta classificação inicial.</span>}
                  <small>{confidenceLabel(visibleEstimate.hardware.confidence.level)} · {visibleEstimate.hardware.confidence.basis}</small>
                </article>
                <article><strong>Tokens do modelo</strong>
                  <span>Entrada: {estimateRange(visibleEstimate.tokens.input, "tokens")}</span>
                  <span>Saída: {estimateRange(visibleEstimate.tokens.output, "tokens")}</span>
                  <small>{confidenceLabel(visibleEstimate.tokens.confidence.level)} · {visibleEstimate.tokens.confidence.sampleCount} amostras · {visibleEstimate.tokens.confidence.basis}</small>
                </article>
                <article><strong>Tempo do modelo</strong>
                  <span>{estimateRange({ min: Math.ceil(visibleEstimate.modelTime.milliseconds.min / 1000), expected: Math.ceil(visibleEstimate.modelTime.milliseconds.expected / 1000), max: Math.ceil(visibleEstimate.modelTime.milliseconds.max / 1000) }, "seg")}</span>
                  <small>{confidenceLabel(visibleEstimate.modelTime.confidence.level)} · {visibleEstimate.modelTime.confidence.sampleCount} amostras</small>
                </article>
                <article><strong>Revisão e testes humanos</strong>
                  <span>{visibleEstimate.reviewAndTests.risk} · {estimateRange(visibleEstimate.reviewAndTests.minutes, "min")}</span>
                  <span>Fatores: {visibleEstimate.reviewAndTests.factors.join(", ")}</span>
                  <small>{confidenceLabel(visibleEstimate.reviewAndTests.confidence.level)} · {visibleEstimate.reviewAndTests.confidence.basis}</small>
                </article>
              </div>
            </>}
            {!visibleEstimate && !estimateUnavailable && !estimating && <p className="muted">Digite uma descrição para ver as faixas iniciais.</p>}
          </section>
          <button type="submit" className="primary" disabled={creating}>
            {creating ? "Criando…" : "Criar task"}
          </button>
        </form>

        {error && <p className="error">{error}</p>}

        {!loading && visibleTasks.length === 0 ? (
          <div className="empty-state">
            {tasks.length === 0 ? "Nenhuma task ainda. Crie uma acima." : "Nenhuma task designada — só mensagens do WhatsApp (veja abaixo)."}
          </div>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Descrição</th>
                <th>Agente</th>
                <th>Status</th>
                <th>Criada em</th>
              </tr>
            </thead>
            <tbody>
              {visibleTasks.map((task) => (
                <tr key={task.id} className="clickable" onClick={() => setSelectedId(task.id)}>
                  <td>{task.description}</td>
                  <td className="mono">{task.agentId}</td>
                  <td>{statusBadge(task.status)}</td>
                  <td className="muted">{new Date(task.createdAt).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {messagingTasks.length > 0 && (
          <button
            onClick={() => setShowMessaging((v) => !v)}
            style={{ marginTop: 14, fontSize: 13 }}
          >
            {showMessaging ? "Ocultar mensagens do WhatsApp" : `Mostrar mensagens do WhatsApp (${messagingTasks.length})`}
          </button>
        )}
      </div>

      {selectedId && <TaskDetail taskId={selectedId} onTaskChanged={refresh} />}
    </div>
  );
}

export function TaskDetail({ taskId, onTaskChanged }: { taskId: string; onTaskChanged: () => void }) {
  const { token } = useAuth();
  const [task, setTask] = useState<Task | null>(null);
  const [execution, setExecution] = useState<Execution | null>(null);
  const [turns, setTurns] = useState<Turn[]>([]);
  const [tree, setTree] = useState<TaskTreeNode | null>(null);
  const [savedEstimate, setSavedEstimate] = useState<TaskEstimate | null>(null);
  const [executorRequests, setExecutorRequests] = useState<ExecutorRequestView[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [executing, setExecuting] = useState(false);

  async function load() {
    if (!token) return;
    setError(null);
    try {
      const [taskData, treeData] = await Promise.all([getTask(token, taskId), getTaskTree(token, taskId)]);
      setTask(taskData);
      setTree(treeData);
      setSavedEstimate(readSavedEstimate(taskId));
      if (taskData.projectId) {
        void listExecutorRequests(token, taskData.projectId).then((items) => {
          setExecutorRequests(items.filter((item) => item.payload.taskId === taskId));
        }).catch(() => setExecutorRequests([]));
      } else {
        setExecutorRequests([]);
      }
      try {
        const exec = await getLatestExecution(token, taskId);
        setExecution(exec);
        setTurns(await getTurns(token, taskId, exec.executionId));
      } catch (err) {
        // No execution yet is expected right after creating a task — not an error to surface.
        if (!(err instanceof ApiError && err.status === 404)) throw err;
        setExecution(null);
        setTurns([]);
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar detalhe da task.");
    }
  }

  const every = useLiveRefresh(["task", "execution", "approval"], () => void load(),
    (e) => e.kind === "approval" || e.id === taskId || e.taskId === taskId || e.parentId === taskId, { live: 30000, fallback: 2500 });
  useEffect(() => {
    void load();
    const interval = setInterval(() => void load(), every);
    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId, token, every]);

  async function handleExecute() {
    if (!token) return;
    setExecuting(true);
    setError(null);
    try {
      await executeTask(token, taskId);
      onTaskChanged();
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao executar task.");
    } finally {
      setExecuting(false);
    }
  }

  if (!task) return null;

  const canExecute = task.status === "CREATED";
  const actualUsage = turns.reduce((totals, turn) => {
    if (turn.usage) {
      totals.input += turn.usage.inputTokens;
      totals.output += turn.usage.outputTokens;
      totals.duration += turn.usage.durationMs;
      totals.calls++;
    }
    return totals;
  }, { input: 0, output: 0, duration: 0, calls: 0 });
  const capacityBlock = executorRequests.find((item) => item.jobStatus === "UNSUPPORTED_CAPACITY");
  const capacityWait = executorRequests.find((item) => item.jobStatus === "WAITING_FOR_CAPACITY");
  const resourceFailure = executorRequests.find((item) => item.jobResult?.code === "resource_limit_exceeded");
  const approvalPending = executorRequests.find((item) => item.status === "PENDING");

  return (
    <div className="task-detail">
      <h3>
        Detalhe {statusBadge(task.status)}
      </h3>
      <p className="mono muted">{task.id}</p>
      <p>{task.description}</p>
      <TaskClientBox task={task} onLinked={() => void load()} />
      <button className="primary" onClick={() => void handleExecute()} disabled={!canExecute || executing}>
        {executing ? "Executando…" : canExecute ? "Executar" : "Já executada"}
      </button>
      {error && <p className="error">{error}</p>}

      {capacityWait && <p className="task-capacity-status" role="status"><strong>Aguardando recursos do servidor.</strong> A task permanece na fila e será reavaliada quando houver capacidade.</p>}
      {capacityBlock && <p className="task-capacity-status task-capacity-error" role="status"><strong>Esta task excede a capacidade máxima deste worker.</strong> {capacityBlock.jobResult?.message ?? "Divida a tarefa ou use um host mais capaz."}</p>}
      {resourceFailure && <p className="task-capacity-status task-capacity-error" role="status"><strong>Execução interrompida por limite de memória.</strong> Não haverá repetição automática; revise/divida a task ou selecione um host mais capaz.</p>}
      {!capacityWait && !capacityBlock && !resourceFailure && approvalPending && <p className="task-capacity-status" role="status">Aguardando aprovação administrativa para uma operação isolada.</p>}

      {execution && (
        <>
          <h3 style={{ marginTop: 24 }}>Execução {statusBadge(execution.status)}</h3>
          {(execution.provider || execution.model) && (
            <p className="mono muted">respondido por {execution.provider ?? "?"} · {execution.model ?? "?"}</p>
          )}
          {execution.result && <pre className="mono">{execution.result}</pre>}
          {execution.error && <p className="error">{execution.error}</p>}
        </>
      )}

      {savedEstimate && <section className="task-estimate task-estimate-comparison" aria-label="Estimativa e consumo real">
        <h4>Estimativa × consumo observado</h4>
        <p className="muted">A estimativa salva antes da criação é comparada com os dados medidos pelo Gateway.</p>
        <div className="task-estimate-grid">
          <article><strong>Tokens</strong>
            <span>Estimado — entrada: {estimateRange(savedEstimate.tokens.input)}; saída: {estimateRange(savedEstimate.tokens.output)}</span>
            {actualUsage.calls > 0 ? <span>Observado: entrada {actualUsage.input.toLocaleString("pt-BR")}, saída {actualUsage.output.toLocaleString("pt-BR")} tokens ({actualUsage.calls} chamadas)</span> : <span>Aguardando métricas de execução.</span>}
          </article>
          <article><strong>Tempo do modelo</strong>
            <span>Estimado: {estimateRange({ min: Math.ceil(savedEstimate.modelTime.milliseconds.min / 1000), expected: Math.ceil(savedEstimate.modelTime.milliseconds.expected / 1000), max: Math.ceil(savedEstimate.modelTime.milliseconds.max / 1000) }, "seg")}</span>
            {actualUsage.calls > 0 && <span>Observado: {(actualUsage.duration / 1000).toLocaleString("pt-BR")} seg no Gateway</span>}
          </article>
          <article><strong>Hardware</strong>
            <span>Perfil estimado: {savedEstimate.hardware.profileId ?? "sem container previsto"}; concorrência {savedEstimate.hardware.concurrency.expected}</span>
            <span>Uso real de CPU/memória/disco por execução ainda não é coletado.</span>
          </article>
          <article><strong>Revisão e testes</strong>
            <span>Estimativa: {savedEstimate.reviewAndTests.risk} · {estimateRange(savedEstimate.reviewAndTests.minutes, "min")}</span>
            <span>Tempo humano real ainda não é registrado.</span>
          </article>
        </div>
      </section>}

      {turns.length > 0 && (
        <>
          <h3 style={{ marginTop: 24 }}>Pipeline de execução</h3>
          <PipelineTimeline turns={turns} />
        </>
      )}

      {tree && (tree.children.length > 0 || tree.depth > 0) && (
        <>
          <h3 style={{ marginTop: 24 }}>Delegação</h3>
          <TreeNode node={tree} />
        </>
      )}
    </div>
  );
}

function TreeNode({ node }: { node: TaskTreeNode }) {
  return (
    <ul className="tree">
      <li>
        <div className="tree-node">
          <span className="mono">{node.description}</span>
          {statusBadge(node.status)}
          {node.executionStatus && statusBadge(node.executionStatus)}
        </div>
        {node.children.length > 0 && (
          <ul>
            {node.children.map((child) => (
              <TreeNode node={child} key={child.taskId} />
            ))}
          </ul>
        )}
      </li>
    </ul>
  );
}
