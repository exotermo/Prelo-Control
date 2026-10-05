import { useEffect, useState } from "react";
import { ApiError, getHome, type PendingItem, type RecentItem } from "../api/client";
import { useProject } from "../context/ProjectContext";
import { useQuickView } from "../context/QuickViewContext";
import { clientPath, navigate, projectPath } from "../router";
import { useLiveRefresh } from "../context/LiveEventsContext";

const RECENT_KICKER: Record<RecentItem["kind"], string> = { CLIENT: "Cliente", PROJECT: "Projeto", TASK: "Task" };
const PENDING_LABEL: Record<PendingItem["kind"], { label: string; stamp: string }> = {
  APPROVAL: { label: "Aguardando aprovação", stamp: "stamp-wait" },
  RUNNING: { label: "Em execução", stamp: "stamp-ok" },
  FAILED: { label: "Falhou", stamp: "stamp-bad" },
};

function when(iso: string): string {
  const minutes = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  if (minutes < 1) return "agora";
  if (minutes < 60) return `há ${minutes} min`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `há ${hours} h`;
  return new Date(iso).toLocaleDateString("pt-BR", { day: "2-digit", month: "short" });
}

/**
 * Fase C1 home: "Continuar de onde parou" (what this user opened last) and "Pendências" (what is
 * waiting on a human). Refreshes every 15 s, like the other self-updating pages.
 */
export function HomePanels({ token }: { token: string }) {
  const [recent, setRecent] = useState<RecentItem[]>([]);
  const [pending, setPending] = useState<PendingItem[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { openTask } = useQuickView();
  const { selectProject } = useProject();

  // PR-4: reload on any task/approval/deploy change; polling is only the fallback.
  const [tick, setTick] = useState(0);
  const every = useLiveRefresh(["task", "approval", "action"], () => setTick((t) => t + 1), undefined, { live: 60000, fallback: 15000 });
  useEffect(() => {
    let cancelled = false;
    const load = () => getHome(token)
      .then((home) => { if (!cancelled) { setRecent(home.recent); setPending(home.pending); setError(null); } })
      .catch((err) => { if (!cancelled) setError(err instanceof ApiError ? err.message : "Falha ao carregar o início."); })
      .finally(() => { if (!cancelled) setLoaded(true); });
    void load();
    const interval = setInterval(() => void load(), every);
    return () => { cancelled = true; clearInterval(interval); };
  }, [token, every, tick]);

  function openRecent(item: RecentItem) {
    if (item.kind === "CLIENT") navigate(clientPath(item.id));
    else if (item.kind === "PROJECT") navigate(projectPath(item.id, "visao-geral"));
    else openTask(item.id);
  }

  function openPending(item: PendingItem) {
    // An approval inside a project is decided on that project's Aprovações page; everything
    // else (and approvals of unassigned WhatsApp tasks) opens the task itself.
    if (item.kind === "APPROVAL" && item.projectId && item.projectName) {
      selectProject(item.projectId, item.projectName);
      navigate("/approvals");
    } else {
      openTask(item.taskId);
    }
  }

  if (!loaded) return null;
  return (
    <div className="home-panels">
      {error && <p className="error">{error}</p>}
      <section className="clipping-section">
        <h3 className="clipping-section-title">Continuar de onde parou</h3>
        {recent.length === 0 ? (
          <p className="muted">Os clientes, projetos e tasks que você abrir aparecem aqui.</p>
        ) : (
          <div className="recent-strip">
            {recent.map((item) => (
              <button key={`${item.kind}-${item.id}`} className="recent-card" onClick={() => openRecent(item)}>
                <span className="clipping-kicker">{RECENT_KICKER[item.kind]}</span>
                <span className="recent-title">{item.title}</span>
                {item.subtitle && <span className="recent-sub">{item.subtitle}</span>}
                <span className="recent-when">{when(item.viewedAt)}</span>
              </button>
            ))}
          </div>
        )}
      </section>

      <section className="clipping-section">
        <h3 className="clipping-section-title">
          Pendências {pending.length > 0 && <span className="pending-count">{pending.length}</span>}
        </h3>
        {pending.length === 0 ? (
          <p className="muted">Nada esperando por você agora.</p>
        ) : (
          <ul className="pending-list">
            {pending.map((item) => (
              <li key={`${item.kind}-${item.id}`}>
                <button className="pending-row" onClick={() => openPending(item)}>
                  <span className={`clipping-stamp ${PENDING_LABEL[item.kind].stamp}`}>{PENDING_LABEL[item.kind].label}</span>
                  <span className="pending-text">
                    <span className="pending-title">{item.title}</span>
                    <span className="pending-detail">{[item.projectName ?? "sem projeto", item.detail].join(" · ")}</span>
                  </span>
                  <span className="recent-when">{when(item.at)}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
