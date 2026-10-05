import { useEffect, useState } from "react";
import { ApiError, getPipeline, type PipelineNode } from "../api/client";
import { PipelineTree } from "../components/PipelineTree";
import { useAuth } from "../auth/AuthContext";
import { useProject } from "../context/ProjectContext";
import { useLiveRefresh } from "../context/LiveEventsContext";

function statusBadge(status: string) {
  return <span className={`badge badge-${status.toLowerCase()}`}>{status}</span>;
}

export function PipelinePage() {
  const { token } = useAuth();
  const { projectId } = useProject();
  const [roots, setRoots] = useState<PipelineNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function refresh() {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      setRoots(await getPipeline(token, projectId ?? undefined));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar pipeline.");
    } finally {
      setLoading(false);
    }
  }

  // PR-4: instant refresh on change; polling only as a fallback (slow while the stream is live).
  const every = useLiveRefresh(["task", "execution", "approval"], () => void refresh(), undefined, { live: 30000, fallback: 2500 });
  useEffect(() => {
    void refresh();
    const interval = setInterval(() => void refresh(), every);
    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, projectId, every]);

  return (
    <div>
      <div className="page-header">
        <h2>Pipeline</h2>
        <button onClick={() => void refresh()} disabled={loading}>
          {loading ? "Atualizando…" : "Atualizar"}
        </button>
      </div>

      {error && <p className="error">{error}</p>}

      {!loading && roots.length === 0 && !error && <p className="muted">Nada em execução agora.</p>}

      {roots.map((root) => (
        <div className="card" key={root.taskId} style={{ marginBottom: 16 }}>
          <div className="page-header">
            <span className="mono">{root.description}</span>
            {statusBadge(root.pipelineStatus)}
          </div>
          {token && <PipelineTree node={root} token={token} />}
        </div>
      ))}
    </div>
  );
}
