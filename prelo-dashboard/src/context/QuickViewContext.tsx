import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { touchRecent } from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { TaskDetail } from "../pages/TasksPage";

// Fase C1: a task opened from anywhere (search, "Continuar", Pendências, a client's timeline)
// shows in a reading overlay on top of the current screen — including WhatsApp tasks, which
// belong to no project and so have no Tasks page to land on.
interface QuickViewState {
  openTask: (taskId: string) => void;
}

const QuickViewContext = createContext<QuickViewState | null>(null);

export function QuickViewProvider({ children }: { children: ReactNode }) {
  const { token } = useAuth();
  const [taskId, setTaskId] = useState<string | null>(null);

  const openTask = useCallback((id: string) => {
    setTaskId(id);
    if (token) touchRecent(token, "TASK", id);
  }, [token]);

  useEffect(() => {
    if (!taskId) return;
    const close = () => setTaskId(null);
    const onKey = (event: KeyboardEvent) => { if (event.key === "Escape") close(); };
    window.addEventListener("keydown", onKey);
    // Following a link out of the task (e.g. to its client) leaves the overlay behind.
    window.addEventListener("popstate", close);
    return () => { window.removeEventListener("keydown", onKey); window.removeEventListener("popstate", close); };
  }, [taskId]);

  const value = useMemo(() => ({ openTask }), [openTask]);

  return (
    <QuickViewContext.Provider value={value}>
      {children}
      {taskId && token && (
        <div className="modal-overlay" onClick={() => setTaskId(null)}>
          <div className="card quick-view" role="dialog" aria-label="Detalhe da task" onClick={(e) => e.stopPropagation()}>
            <div className="quick-view-bar">
              <span className="clipping-kicker">Task</span>
              <button onClick={() => setTaskId(null)}>Fechar</button>
            </div>
            <TaskDetail taskId={taskId} onTaskChanged={() => {}} />
          </div>
        </div>
      )}
    </QuickViewContext.Provider>
  );
}

export function useQuickView(): QuickViewState {
  const ctx = useContext(QuickViewContext);
  if (!ctx) throw new Error("useQuickView must be used within QuickViewProvider");
  return ctx;
}
