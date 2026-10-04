import { useAuth } from "../auth/AuthContext";
import { navigate, usePathname } from "../router";
import { openCommandPalette } from "./search/paletteEvents";

const LINKS = [
  { path: "/projetos", label: "Início" },
  { path: "/clientes", label: "Clientes" },
  { path: "/conta", label: "Conta" },
];

/** Header of the workspace screens (/projetos and /clientes), before entering a project. */
export function WorkspaceHeader() {
  const { logout } = useAuth();
  const path = usePathname();
  return (
    <header className="app-topnav">
      <div className="brand">
        <span className="brand-mark">P</span>
        <h1>Prelo Control</h1>
      </div>
      <nav aria-label="Navegação principal">
        {LINKS.map((link) => (
          <a
            key={link.path}
            href={link.path}
            className={path.startsWith(link.path) ? "nav-link active" : "nav-link"}
            onClick={(event) => { event.preventDefault(); navigate(link.path); }}
          >
            {link.label}
          </a>
        ))}
      </nav>
      <div className="app-topnav-right">
        <button className="search-trigger" onClick={openCommandPalette} title="Buscar (Ctrl+K)">
          Buscar <kbd className="search-kbd">Ctrl K</kbd>
        </button>
        <button onClick={logout}>Sair</button>
      </div>
    </header>
  );
}
