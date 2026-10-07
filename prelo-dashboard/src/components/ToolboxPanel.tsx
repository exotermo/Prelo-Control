import { useCallback, useEffect, useState, type CSSProperties } from "react";
import { ApiError, decideExecutorRequest, getProjectToolbox, listExecutorRequests, listExecutorWorkers, setProjectTool, type ExecutorRequestView, type ExecutorWorkerSummary, type ProjectToolbox, type ToolboxTool } from "../api/client";

const RISK: Record<ToolboxTool["riskLevel"], string> = {
  LOW: "Baixo", MODERATE: "Moderado", HIGH: "Alto",
};

const JOB_STATUS: Record<NonNullable<ExecutorRequestView["jobStatus"]>, string> = {
  READY: "Na fila", WAITING_FOR_CAPACITY: "Aguardando recursos do servidor",
  UNSUPPORTED_CAPACITY: "Não cabe neste worker", CLAIMED: "Reservado pelo worker",
  RUNNING: "Em execução", SUCCEEDED: "Concluído", FAILED: "Falhou", CANCELLED: "Cancelado",
};

export function ToolboxPanel({ token, projectId, canManage }: { token: string; projectId: string; canManage: boolean }) {
  const [data, setData] = useState<ProjectToolbox | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [workers, setWorkers] = useState<ExecutorWorkerSummary[]>([]);
  const [requests, setRequests] = useState<ExecutorRequestView[]>([]);

  const refresh = useCallback(async () => {
    try {
      setData(await getProjectToolbox(token, projectId));
      setRequests(await listExecutorRequests(token, projectId));
      setError(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar a caixa de ferramentas.");
    } finally {
      setLoading(false);
    }
  }, [token, projectId]);

  useEffect(() => {
    let active = true;
    void listExecutorRequests(token, projectId).then((items) => { if (active) setRequests(items); });
    return () => { active = false; };
  }, [token, projectId]);

  useEffect(() => {
    const timer = window.setInterval(() => { void refresh(); }, 10_000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  useEffect(() => {
    let active = true;
    void getProjectToolbox(token, projectId).then((value) => {
      if (active) setData(value);
    }).catch((err) => {
      if (active) setError(err instanceof ApiError ? err.message : "Falha ao carregar a caixa de ferramentas.");
    }).finally(() => {
      if (active) setLoading(false);
    });
    return () => { active = false; };
  }, [token, projectId]);

  useEffect(() => {
    if (!canManage) return;
    let active = true;
    void listExecutorWorkers(token).then((items) => {
      if (active) setWorkers(items.filter((worker) => worker.projectId === projectId));
    }).catch(() => { if (active) setWorkers([]); });
    return () => { active = false; };
  }, [token, projectId, canManage]);

  async function toggle(tool: ToolboxTool) {
    if (!canManage || busy) return;
    setBusy(tool.name);
    setError(null);
    try {
      await setProjectTool(token, projectId, tool, !tool.enabled);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao alterar a ferramenta.");
      if (err instanceof ApiError && err.status === 409) await refresh();
    } finally {
      setBusy(null);
    }
  }

  async function decide(item: ExecutorRequestView, approve: boolean) {
    if (busy || !item.canApprove) return;
    if (approve && !window.confirm(`Aprovar ${item.payload.operation} na task ${item.payload.taskId}? Confira operação, caminho, conteúdo e hash no recorte antes de continuar.`)) return;
    setBusy(item.id);
    try {
      await decideExecutorRequest(token, item.id, approve);
      setRequests(await listExecutorRequests(token, projectId));
      setError(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao decidir pedido.");
    } finally { setBusy(null); }
  }

  return (
    <section aria-label="Caixa de ferramentas">
      <div className="page-header">
        <div>
          <span className="clipping-kicker">Controle de capacidades · Projeto</span>
          <h2>Caixa de ferramentas</h2>
          <p className="muted">Permitir uma ferramenta deixa o modelo pedi-la. Cada ação de risco continua exigindo aprovação pela API do Prelo.</p>
        </div>
        <button type="button" onClick={() => void refresh()} disabled={loading}>Atualizar</button>
      </div>

      {error && <p className="error" role="alert">{error}</p>}
      {loading && <p className="muted">Carregando ferramentas…</p>}
      {data && (
        <>
          <article className="clipping">
            <span className="clipping-kicker">Execução isolada</span>
            <h3 className="clipping-headline">{data.workerStatus === "READY" ? "Worker conectado" : data.workerStatus === "NO_WORKER" ? "Aguardando worker" : "Executor desabilitado"}</h3>
            <div className="clipping-body">
              <p>{data.workerStatus === "READY" ? "O heartbeat está ativo. Cada operação ainda exige ferramenta habilitada e aprovação administrativa; o worker confirma a autorização atual antes de agir." : data.workerStatus === "NO_WORKER" ? "O serviço está habilitado, mas nenhum worker deste projeto enviou heartbeat nos últimos 30 segundos." : "A execução está desabilitada no Prelo. A máquina principal não é destino de execução."}</p>
            </div>
            <div className="clipping-footer"><span className="clipping-stamp stamp-wait">{data.workerStatus === "READY" ? "Conectado" : data.workerStatus === "NO_WORKER" ? "Sem worker ativo" : "Desabilitado"}</span></div>
          </article>

          <article className="clipping" aria-label="Perfil de recursos">
            <span className="clipping-kicker">Política de recursos do servidor</span>
            <h3 className="clipping-headline">{data.resourceProfiles[0]?.id ?? "Sem perfil"}</h3>
            {data.resourceProfiles[0] && <div className="clipping-body">
              <p>Memória: {Math.round(data.resourceProfiles[0].memoryBytes / 1024 / 1024)} MiB · CPU: {data.resourceProfiles[0].cpuQuotaMilli / 1000} vCPU</p>
              <p>Disco: {Math.round(data.resourceProfiles[0].diskBytes / 1024 / 1024)} MiB · PIDs: {data.resourceProfiles[0].pids}</p>
              <p>Tempo máximo: {Math.round(data.resourceProfiles[0].maxRuntimeSeconds / 60)} min · containers por execução: {data.resourceProfiles[0].maxContainersPerExecution}</p>
            </div>}
            <div className="clipping-footer"><span className="clipping-stamp stamp-wait">Perfil reportado no claim</span></div>
            <p className="muted">O servidor compara o perfil declarado pelo worker com o perfil cadastrado. O protocolo continua documentado como proposta P-5; a quota de disco não é rígida e o host ainda precisa de validação operacional.</p>
          </article>

          {canManage && workers.length > 0 && (
            <div className="clipping-grid" aria-label="Workers registrados">
              {workers.map((worker) => (
                <article key={worker.id} className="clipping">
                  <span className="clipping-kicker">Worker · {worker.enabled ? "Registrado" : "Revogado"}</span>
                  <h3 className="clipping-headline">{worker.name}</h3>
                  <div className="clipping-body">
                    <p>Imagem prevista: {worker.imageDigest.slice(0, 22)}…</p>
                    <p className="muted">Último contato: {worker.lastSeenAt ? new Date(worker.lastSeenAt).toLocaleString("pt-BR") : "nenhum"}</p>
                  </div>
                  <div className="clipping-footer"><span className="clipping-stamp stamp-wait">{worker.runtimeStatus === "READY" ? "Conectado" : worker.runtimeStatus === "REVOKED" ? "Revogado" : worker.runtimeStatus === "DISABLED" ? "Runtime desabilitado" : "Offline"}</span></div>
                </article>
              ))}
            </div>
          )}

          <h3>Pedidos de operação isolada</h3>
          {requests.length === 0 && <p className="muted">Nenhum pedido neste projeto.</p>}
          <div className="clipping-grid">
            {requests.slice(0, 20).map((item) => (
              <article key={item.id} className="clipping">
                <span className="clipping-kicker">Ação isolada · {item.status}</span>
                <h3 className="clipping-headline">{item.payload.operation}</h3>
                <div className="clipping-body">
                  <p>Task {item.payload.taskId} · Worker {item.payload.workerId}</p>
                  {item.payload.args.path && <p>Caminho: <code>{item.payload.args.path}</code></p>}
                  <p className="clipping-mono">Imagem: {item.payload.imageDigest}</p>
                  <p className="clipping-mono">Hash: {item.payloadHash}</p>
                  {item.jobStatus && <p><strong>Execução:</strong> {JOB_STATUS[item.jobStatus]}</p>}
                  {item.jobResult?.message && <p role="status">{item.jobResult.message}</p>}
                  {item.jobResult?.code === "resource_limit_exceeded" && <p role="status">O container excedeu o próprio limite de recursos; foi interrompido e não será repetido automaticamente.</p>}
                  {item.payload.args.contentBase64 && <details><summary>Conteúdo exato em base64</summary><pre>{item.payload.args.contentBase64}</pre></details>}
                  <p className="muted">Prazo: {new Date(item.expiresAt).toLocaleString("pt-BR")}</p>
                </div>
                {item.status === "PENDING" && item.canApprove && (
                  <div className="clipping-footer">
                    <button type="button" disabled={busy !== null} onClick={() => void decide(item, false)}>Negar</button>
                    <button type="button" disabled={busy !== null} onClick={() => void decide(item, true)}>Aprovar pela API</button>
                  </div>
                )}
              </article>
            ))}
          </div>

          <h3>Ferramentas cadastradas</h3>
          <div className="clipping-grid">
            {data.tools.map((tool, index) => (
              <article key={tool.name} className="clipping" style={{ "--i": index } as CSSProperties}>
                <span className="clipping-kicker">Risco {RISK[tool.riskLevel]}</span>
                <h3 className="clipping-headline">{tool.name}</h3>
                <div className="clipping-body">
                  <p>{tool.description}</p>
                  <p className="muted">Agentes: {tool.agents.length ? tool.agents.join(", ") : "nenhum com esta capacidade"}</p>
                  {tool.impact && <p className="muted">Impacto: {tool.impact}</p>}
                  {tool.resourceProfile && <p className="muted">Perfil do servidor: {tool.resourceProfile.id}</p>}
                </div>
                <div className="clipping-footer">
                  <span className={`clipping-stamp ${tool.enabled && (tool.agents.length || tool.name.startsWith("workspace_")) ? "stamp-ok" : "stamp-bad"}`}>
                    {tool.name.startsWith("workspace_") ? tool.enabled ? "Pode pedir aprovação" : "Bloqueada" : !tool.agents.length ? "Sem agente" : tool.enabled ? "Pode solicitar" : "Bloqueada"}
                  </span>
                  {canManage && (tool.agents.length > 0 || tool.name.startsWith("workspace_")) && (
                    <button type="button" onClick={() => void toggle(tool)} disabled={busy !== null}>
                      {busy === tool.name ? "Salvando…" : tool.enabled ? "Bloquear" : "Permitir pedido"}
                    </button>
                  )}
                </div>
              </article>
            ))}
          </div>
        </>
      )}
    </section>
  );
}
