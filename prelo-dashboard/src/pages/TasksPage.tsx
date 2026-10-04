import { useEffect, useState } from "react";
import {
  ApiError,
  createTask,
  executeTask,
  getLatestExecution,
  getTask,
  getTaskTree,
  getTurns,
  listTasks,
  type Execution,
  type Task,
  type TaskTreeNode,
  type Turn,
} from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { useProject } from "../context/ProjectContext";
import { TaskClientBox } from "../components/TaskClientBox";
import { PipelineTimeline } from "../components/PipelineTimeline";

function statusBadge(status: string) {
  return <span className={`badge badge-${status.toLowerCase()}`}>{status}</span>;
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
      const task = await createTask(token, description.trim(), agentId.trim() || undefined, undefined, projectId ?? undefined);
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
  const [error, setError] = useState<string | null>(null);
  const [executing, setExecuting] = useState(false);

  async function load() {
    if (!token) return;
    setError(null);
    try {
      const [taskData, treeData] = await Promise.all([getTask(token, taskId), getTaskTree(token, taskId)]);
      setTask(taskData);
      setTree(treeData);
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

  useEffect(() => {
    void load();
    const interval = setInterval(() => void load(), 2500);
    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId, token]);

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
