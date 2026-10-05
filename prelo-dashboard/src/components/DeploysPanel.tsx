import { useEffect, useState } from "react";
import { useLiveRefresh } from "../context/LiveEventsContext";
import { listProjectActions, type ActionRequestView } from "../api/client";

const STATUS: Record<string, { label: string; stamp: string }> = {
  PENDING: { label: "Aguardando aprovação", stamp: "stamp-wait" },
  APPROVED: { label: "Aprovado", stamp: "stamp-ok" },
  DENIED: { label: "Negado", stamp: "stamp-bad" },
  EXPIRED: { label: "Expirou", stamp: "stamp-bad" },
};
const RESULT: Record<string, string> = {
  RUNNING: "em execução", SUCCEEDED: "concluído", FAILED: "falhou", ROLLED_BACK: "revertido", CANCELLED: "cancelado",
};

/** PR-3 (contrato G8): deploys requested by the BastionDeploy for this project, newest first. */
export function DeploysPanel({ token, projectId }: { token: string; projectId: string }) {
  const [items, setItems] = useState<ActionRequestView[] | null>(null);
  const [tick, setTick] = useState(0);
  const every = useLiveRefresh(["approval", "action"], () => setTick((t) => t + 1), (e) => e.projectId === projectId, { live: 60000, fallback: 15000 });

  useEffect(() => {
    let cancelled = false;
    const load = () => listProjectActions(token, projectId, "deploy").then((list) => { if (!cancelled) setItems(list); }).catch(() => {});
    void load();
    const timer = setInterval(() => void load(), every);
    return () => { cancelled = true; clearInterval(timer); };
  }, [token, projectId, every, tick]);

  if (!items || items.length === 0) return null;
  return (
    <section className="clipping-section deploys-panel">
      <h3 className="clipping-section-title">Deploys</h3>
      <ul className="pending-list">
        {items.map((a) => {
          const p = a.payload;
          const status = STATUS[a.status] ?? { label: a.status, stamp: "stamp-wait" };
          return (
            <li key={a.id}>
              <div className="pending-row deploy-row">
                <span className={`clipping-stamp ${status.stamp}`}>{status.label}</span>
                <span className="pending-text">
                  <span className="pending-title">{p.repository}@{p.commitSha.slice(0, 7)} → {p.environment}</span>
                  <span className="pending-detail">
                    {[p.target, a.result ? RESULT[a.result.status] ?? a.result.status : null, `código ${a.approvalCode}`, a.requestedBy].filter(Boolean).join(" · ")}
                  </span>
                </span>
                {a.result?.url ? <a href={a.result.url} target="_blank" rel="noreferrer">abrir</a> : <span className="recent-when">{new Date(a.createdAt).toLocaleString("pt-BR", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" })}</span>}
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
