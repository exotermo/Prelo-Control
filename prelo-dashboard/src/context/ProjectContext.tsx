import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

// Fase W: the "current project" is a per-viewer UI selection, not session state — the same
// dashboard session moves between several projects across requests (sent as X-Project-Id, see
// api/client.ts), so this never belongs in AuthContext/the JWT itself.
const STORAGE_KEY = "prelo-dashboard:current-project";

function readStoredSelection(): { id: string; name: string } | null {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as { id: string; name: string }) : null;
  } catch {
    return null; // private mode, blocked storage, etc. — just start with no project selected
  }
}
function writeStoredSelection(selection: { id: string; name: string } | null) {
  try {
    if (selection) window.localStorage.setItem(STORAGE_KEY, JSON.stringify(selection));
    else window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    /* ignore */
  }
}

interface ProjectState {
  projectId: string | null;
  projectName: string | null;
  selectProject: (id: string, name: string) => void;
  clearProject: () => void;
}

const ProjectContext = createContext<ProjectState | null>(null);

export function ProjectProvider({ children }: { children: ReactNode }) {
  const [selection, setSelection] = useState(() => readStoredSelection());

  const selectProject = useCallback((id: string, name: string) => {
    const next = { id, name };
    writeStoredSelection(next);
    setSelection(next);
  }, []);
  const clearProject = useCallback(() => {
    writeStoredSelection(null);
    setSelection(null);
  }, []);

  const value = useMemo(
    () => ({ projectId: selection?.id ?? null, projectName: selection?.name ?? null, selectProject, clearProject }),
    [selection, selectProject, clearProject],
  );

  return <ProjectContext.Provider value={value}>{children}</ProjectContext.Provider>;
}

export function useProject(): ProjectState {
  const ctx = useContext(ProjectContext);
  if (!ctx) throw new Error("useProject must be used within ProjectProvider");
  return ctx;
}
