import { useEffect, useState } from "react";
import { ApiError, getPipeline, type PipelineNode } from "../api/client";
import { PipelineTree } from "../components/PipelineTree";
import { useAuth } from "../auth/AuthContext";
import { useProject } from "../context/ProjectContext";

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

  useEffect(() => {
    void refresh();
    const interval = setInterval(() => void refresh(), 2500);
    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, projectId]);

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
