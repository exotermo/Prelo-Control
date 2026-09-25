import { useEffect, useState } from "react";
import { TasksPage } from "./pages/TasksPage";
import { ApprovalsPage } from "./pages/ApprovalsPage";

type Route = "tasks" | "approvals";

const ROUTES: { id: Route; label: string; icon: string }[] = [
  { id: "tasks", label: "Tasks", icon: "▤" },
  { id: "approvals", label: "Aprovações", icon: "✓" },
];

function currentRoute(): Route {
  const segment = window.location.pathname.split("/")[1];
  return ROUTES.some((route) => route.id === segment) ? (segment as Route) : "tasks";
}

function App() {
  const [route, setRoute] = useState<Route>(currentRoute);
  useEffect(() => {
    const onPopState = () => setRoute(currentRoute());
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);
  function navigate(next: Route) {
    window.history.pushState(null, "", `/${next}`);
    setRoute(next);
  }

  return (
    <div className="app-shell">
      <aside className="app-sidebar">
        <div className="brand">
          <span className="brand-mark">H</span>
          <div>
            <h1>Hermes</h1>
            <small>Console do agente</small>
          </div>
        </div>
        <nav aria-label="Navegação principal">
          {ROUTES.map((item) => (
            <a
              href={`/${item.id}`}
              key={item.id}
              className={route === item.id ? "nav-link active" : "nav-link"}
              onClick={(event) => {
                event.preventDefault();
                navigate(item.id);
              }}
            >
              <span aria-hidden="true">{item.icon}</span>
              {item.label}
            </a>
          ))}
        </nav>
      </aside>
      <main className="app-content">
        {route === "tasks" && <TasksPage />}
        {route === "approvals" && <ApprovalsPage />}
      </main>
    </div>
  );
}

export default App;
