import { useEffect, useState } from "react";
import { TasksPage } from "./pages/TasksPage";
import { PipelinePage } from "./pages/PipelinePage";
import { ApprovalsPage } from "./pages/ApprovalsPage";
import { IntegrationsPage } from "./pages/IntegrationsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { UsersPage } from "./pages/UsersPage";
import { ServersPage } from "./pages/ServersPage";
import { LoginPage } from "./pages/LoginPage";
import { ProjectLanding } from "./pages/ProjectLanding";
import { AuthProvider, useAuth } from "./auth/AuthContext";
import { ProjectProvider, useProject } from "./context/ProjectContext";
import { decodeDashboardToken, listProjects } from "./api/client";
import { navigate, parseProjectsPath, usePathname } from "./router";

type Route = "tasks" | "pipeline" | "approvals" | "servers" | "integrations" | "settings" | "users";

// servers:read is in every dashboard session's scopes (ADMIN and OPERATOR both get it — see
// dashboardOperatorScopes), so Servidores belongs alongside Tasks/Aprovações, not ADMIN_ROUTES.
// Same reasoning for Pipeline (Fase P): it's gated to observability:read, which both roles carry.
const BASE_ROUTES: { id: Route; label: string }[] = [
  { id: "tasks", label: "Tasks" },
  { id: "pipeline", label: "Pipeline" },
  { id: "approvals", label: "Aprovações" },
  { id: "servers", label: "Servidores" },
];
// Everything here requires the users:manage/settings:manage scope only an ADMIN session carries
// — hidden for OPERATOR sessions so the nav never points at a page every API call on it would
// 403. prelo-core enforces this regardless; this is purely so the UI doesn't dangle a dead link.
const ADMIN_ROUTES: { id: Route; label: string }[] = [
  { id: "integrations", label: "Integrações" },
  { id: "settings", label: "Configurações" },
  { id: "users", label: "Usuários" },
];

function currentRoute(routes: { id: Route }[]): Route {
  const segment = window.location.pathname.split("/")[1];
  return routes.some((route) => route.id === segment) ? (segment as Route) : "tasks";
}

function Shell() {
  const { token, logout } = useAuth();
  const { projectId, projectName, clearProject } = useProject();
  const isAdmin = !!token && (decodeDashboardToken(token)?.scopes.includes("users:manage") ?? false);
  const routes = isAdmin ? [...BASE_ROUTES, ...ADMIN_ROUTES] : BASE_ROUTES;
  const [route, setRoute] = useState<Route>(() => currentRoute(routes));
  // Coming from /projetos (or any unknown path), make the address bar show the real section.
  useEffect(() => {
    if (window.location.pathname !== `/${route}`) window.history.replaceState(null, "", `/${route}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  // Page-turn direction: moving right along the nav turns the page forward, moving left turns
  // it back — like flipping ahead or back through a magazine.
  const [turn, setTurn] = useState<"forward" | "backward">("forward");
  function turnTo(next: Route, from: Route) {
    const order = routes.map((r) => r.id);
    setTurn(order.indexOf(next) >= order.indexOf(from) ? "forward" : "backward");
    setRoute(next);
  }
  useEffect(() => {
    const onPopState = () => setRoute((from) => {
      const next = currentRoute(routes);
      const order = routes.map((r) => r.id);
      setTurn(order.indexOf(next) >= order.indexOf(from) ? "forward" : "backward");
      return next;
    });
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  // A project saved in localStorage might no longer exist, or might belong to a different
  // dashboard user sharing this browser — confirm it's still in this session's own list on
  // every mount, and bounce back to ProjectLanding instead of silently scoping requests to a
  // project the current session can no longer (or never could) see.
  useEffect(() => {
    if (!token || !projectId) return;
    void listProjects(token).then((projects) => {
      if (!projects.some((p) => p.id === projectId)) {
        clearProject();
        navigate("/projetos", { replace: true });
      }
    }).catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);
  function goTo(next: Route) {
    if (next === route) return;
    window.history.pushState(null, "", `/${next}`);
    turnTo(next, route);
  }

  return (
    <div className="app-shell">
      <header className="app-topnav">
        <div className="brand">
          <span className="brand-mark">P</span>
          <h1>Prelo Control</h1>
        </div>
        <nav aria-label="Navegação principal">
          {routes.map((item) => (
            <a
              href={`/${item.id}`}
              key={item.id}
              className={route === item.id ? "nav-link active" : "nav-link"}
              onClick={(event) => {
                event.preventDefault();
                goTo(item.id);
              }}
            >
              {item.label}
            </a>
          ))}
        </nav>
        <div className="app-topnav-right">
          <button className="project-pill" onClick={() => { clearProject(); navigate("/projetos"); }} title="Trocar projeto">
            📁 {projectName} · Trocar projeto
          </button>
          <button onClick={logout}>Sair</button>
        </div>
      </header>
      <main className="app-content">
        <div key={route} className={`page-sheet ${turn}`}>
        {route === "tasks" && <TasksPage />}
        {route === "pipeline" && <PipelinePage />}
        {route === "approvals" && <ApprovalsPage />}
        {route === "servers" && <ServersPage />}
        {route === "integrations" && <IntegrationsPage />}
        {route === "settings" && <SettingsPage />}
        {route === "users" && <UsersPage />}
        </div>
      </main>
    </div>
  );
}

function Gate() {
  const { token, restoring } = useAuth();
  const { projectId } = useProject();
  const path = usePathname();
  // /projetos/** is its own screen (project list and each project's cover), reachable with or
  // without a project selected; everything else is the in-project dashboard.
  const inProjects = parseProjectsPath(path) !== null;
  useEffect(() => {
    if (!restoring && token && !projectId && !inProjects) navigate("/projetos", { replace: true });
  }, [restoring, token, projectId, inProjects]);
  if (restoring) return null;
  if (!token) return <LoginPage />;
  if (!projectId || inProjects) return <ProjectLanding />;
  return <Shell />;
}

function App() {
  return (
    <AuthProvider>
      <ProjectProvider>
        <Gate />
      </ProjectProvider>
    </AuthProvider>
  );
}

export default App;
