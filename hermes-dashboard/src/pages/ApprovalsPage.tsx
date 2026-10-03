import { useEffect, useState } from "react";
import { ApiError, approveRequest, denyRequest, listPendingApprovals, type ApprovalRequest } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { useProject } from "../context/ProjectContext";

export function ApprovalsPage() {
  const { token } = useAuth();
  const { projectId } = useProject();
  const [approvals, setApprovals] = useState<ApprovalRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [decidedBy, setDecidedBy] = useState("operador");
  const [busyId, setBusyId] = useState<string | null>(null);

  async function refresh() {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      setApprovals(await listPendingApprovals(token, projectId ?? undefined));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar aprovações.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    const interval = setInterval(() => void refresh(), 4000);
    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, projectId]);

  async function decide(id: string, action: "approve" | "deny") {
    if (!token) return;
    setBusyId(id);
    setError(null);
    try {
      if (action === "approve") await approveRequest(token, id, decidedBy || "operador");
      else await denyRequest(token, id, decidedBy || "operador");
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao decidir aprovação.");
    } finally {
      setBusyId(null);
    }
  }

  return (
    <div>
      <div className="page-header">
        <h2>Fila de aprovações</h2>
        <button onClick={() => void refresh()} disabled={loading}>
          {loading ? "Atualizando…" : "Atualizar"}
        </button>
      </div>

      <label style={{ maxWidth: 320 }}>
        Decidido por
        <input value={decidedBy} onChange={(event) => setDecidedBy(event.target.value)} />
      </label>

      {error && <p className="error">{error}</p>}

      {!loading && approvals.length === 0 ? (
        <div className="empty-state">Nenhuma aprovação pendente — um agente aparece aqui assim que pede uma ferramenta de risco moderado ou alto.</div>
      ) : (
        approvals.map((approval) => (
          <div className="approval-card" key={approval.id}>
            <div className="page-header" style={{ marginBottom: 6 }}>
              <strong>Aprovação pendente</strong>
              <span className="muted">expira em {new Date(approval.expiresAt).toLocaleTimeString()}</span>
            </div>
            <div className="scope">{approval.scope}</div>
            <div className="approval-actions">
              <button className="primary" onClick={() => void decide(approval.id, "approve")} disabled={busyId === approval.id}>
                Aprovar
              </button>
              <button onClick={() => void decide(approval.id, "deny")} disabled={busyId === approval.id}>
                Negar
              </button>
            </div>
          </div>
        ))
      )}
    </div>
  );
}
