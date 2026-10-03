import { useState } from "react";
import { ApiError, getLatestExecution, getTurns, type PipelineNode, type Turn } from "../api/client";
import { PipelineTimeline } from "./PipelineTimeline";

function statusBadge(status: string) {
  return <span className={`badge badge-${status.toLowerCase()}`}>{status}</span>;
}

function PipelineNodeView({ node, token }: { node: PipelineNode; token: string }) {
  const [expanded, setExpanded] = useState(false);
  const [turns, setTurns] = useState<Turn[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function toggle() {
    if (expanded) {
      setExpanded(false);
      return;
    }
    setExpanded(true);
    if (turns !== null) return;
    setLoading(true);
    setError(null);
    try {
      const execution = await getLatestExecution(token, node.taskId);
      setTurns(await getTurns(token, node.taskId, execution.executionId));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar turnos.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <li>
      <div className="tree-node" onClick={() => void toggle()} style={{ cursor: "pointer" }}>
        <span className="mono">{node.description}</span>
        {statusBadge(node.pipelineStatus)}
      </div>
      {expanded && (
        <div style={{ marginLeft: 24, marginBottom: 8 }}>
          {loading && <p className="muted">Carregando…</p>}
          {error && <p className="error">{error}</p>}
          {turns && turns.length > 0 && <PipelineTimeline turns={turns} />}
          {turns && turns.length === 0 && <p className="muted">Sem turnos ainda.</p>}
        </div>
      )}
      {node.children.length > 0 && (
        <ul>
          {node.children.map((child) => (
            <PipelineNodeView node={child} token={token} key={child.taskId} />
          ))}
        </ul>
      )}
    </li>
  );
}

export function PipelineTree({ node, token }: { node: PipelineNode; token: string }) {
  return (
    <ul className="tree">
      <PipelineNodeView node={node} token={token} />
    </ul>
  );
}
