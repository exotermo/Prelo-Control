import { useSyncExternalStore } from "react";

// Minimal History-API routing: every navigation goes through navigate(), which notifies all
// usePathname() subscribers the same way the browser's back/forward buttons do (popstate).
function subscribe(callback: () => void) {
  window.addEventListener("popstate", callback);
  return () => window.removeEventListener("popstate", callback);
}

export function usePathname(): string {
  return useSyncExternalStore(subscribe, () => window.location.pathname);
}

export function navigate(path: string, options: { replace?: boolean } = {}) {
  if (window.location.pathname === path) return;
  if (options.replace) window.history.replaceState(null, "", path);
  else window.history.pushState(null, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

export type ProjectSection = "visao-geral" | "modelo" | "ferramentas" | "arquivos" | "configuracoes";
const SECTIONS: ProjectSection[] = ["visao-geral", "modelo", "ferramentas", "arquivos", "configuracoes"];

/** Parses /projetos, /projetos/{id} and /projetos/{id}/{section}; null when outside /projetos. */
export function parseProjectsPath(path: string): { projectId: string | null; section: ProjectSection } | null {
  const parts = path.split("/").filter(Boolean);
  if (parts[0] !== "projetos") return null;
  const section: ProjectSection = SECTIONS.includes(parts[2] as ProjectSection) ? (parts[2] as ProjectSection) : "modelo";
  return { projectId: parts[1] ?? null, section };
}

export const projectPath = (projectId: string, section: ProjectSection = "modelo") => `/projetos/${projectId}/${section}`;

export type ClientSection = "visao-geral" | "linha-do-tempo" | "contatos" | "projetos";
const CLIENT_SECTIONS: ClientSection[] = ["visao-geral", "linha-do-tempo", "contatos", "projetos"];

/** Parses /clientes, /clientes/{id} and /clientes/{id}/{section}; null when outside /clientes. */
export function parseClientsPath(path: string): { clientId: string | null; section: ClientSection } | null {
  const parts = path.split("/").filter(Boolean);
  if (parts[0] !== "clientes") return null;
  const section: ClientSection = CLIENT_SECTIONS.includes(parts[2] as ClientSection) ? (parts[2] as ClientSection) : "visao-geral";
  return { clientId: parts[1] ?? null, section };
}

export const clientPath = (clientId: string, section: ClientSection = "visao-geral") => `/clientes/${clientId}/${section}`;
