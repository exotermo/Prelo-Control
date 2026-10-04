import { useEffect, useState, type FormEvent } from "react";
import {
  ApiError,
  addProjectMember,
  createProject,
  decodeDashboardToken,
  listDashboardUsers,
  listProjectMembers,
  listProjects,
  removeProjectMember,
  touchRecent,
  type DashboardUserSummary,
  type ProjectMember,
  type ProjectSummary,
} from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { ProjectSheet } from "../components/ProjectSheet";
import { navigate, parseProjectsPath, projectPath, usePathname } from "../router";
import { ModelPanel } from "../components/model/ModelPanel";
import { COVER_COLORS } from "../components/coverColors";
import { useProject } from "../context/ProjectContext";
import { WorkspaceHeader } from "../components/WorkspaceHeader";
import { HomePanels } from "../components/HomePanels";
import { SearchBox } from "../components/search/SearchBox";

function MembersModal({ project, onClose }: { project: ProjectSummary; onClose: () => void }) {
  const { token } = useAuth();
  const [members, setMembers] = useState<ProjectMember[]>([]);
  const [users, setUsers] = useState<DashboardUserSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  async function refresh() {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      const [memberList, userList] = await Promise.all([listProjectMembers(token, project.id), listDashboardUsers(token)]);
      setMembers(memberList);
      setUsers(userList);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar membros.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project.id, token]);

  async function toggle(user: DashboardUserSummary, isMember: boolean) {
    if (!token) return;
    try {
      if (isMember) await removeProjectMember(token, project.id, user.id);
      else await addProjectMember(token, project.id, user.id);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao atualizar membro.");
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="card" style={{ maxWidth: 480 }} onClick={(e) => e.stopPropagation()}>
        <div className="page-header">
          <h3>Membros — {project.name}</h3>
          <button onClick={onClose}>Fechar</button>
        </div>
        {error && <p className="error">{error}</p>}
        {loading ? (
          <p className="muted">Carregando…</p>
        ) : (
          <ul className="tree" style={{ listStyle: "none", padding: 0 }}>
            {users.map((user) => {
              const isMember = members.some((m) => m.userId === user.id);
              return (
                <li key={user.id} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "6px 0" }}>
                  <span>{user.email}</span>
                  <label style={{ display: "flex", alignItems: "center", gap: 6 }}>
                    <input type="checkbox" checked={isMember} onChange={() => void toggle(user, isMember)} />
                    Membro
                  </label>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}

export function ProjectLanding() {
  const { token } = useAuth();
  const { selectProject } = useProject();
  const [projects, setProjects] = useState<ProjectSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [managingProject, setManagingProject] = useState<ProjectSummary | null>(null);
  const route = parseProjectsPath(usePathname());
  const openedId = route?.projectId ?? null;
  const opened = openedId ? projects.find((p) => p.id === openedId) ?? null : null;

  const scopes = token ? decodeDashboardToken(token)?.scopes ?? [] : [];
  const isAdmin = scopes.includes("projects:manage");
  const canManageInstanceModel = scopes.includes("settings:manage");

  async function refresh() {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      const list = await listProjects(token);
      setProjects(list);
      // A /projetos/{id} link to a project this session can't see (removed, or not a member).
      const current = parseProjectsPath(window.location.pathname)?.projectId;
      if (current && !list.some((p) => p.id === current)) navigate("/projetos", { replace: true });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar projetos.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  // Fase C1: opening a project's cover is what "Continuar de onde parou" remembers.
  useEffect(() => {
    if (token && opened) touchRecent(token, "PROJECT", opened.id);
  }, [token, opened?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  async function handleCreate(event: FormEvent) {
    event.preventDefault();
    if (!name.trim() || !token) return;
    setCreating(true);
    setError(null);
    try {
      await createProject(token, name.trim(), description.trim() || undefined);
      setName("");
      setDescription("");
      setShowForm(false);
      await refresh();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao criar projeto.");
    } finally {
      setCreating(false);
    }
  }

  return (
    <div className="app-shell">
      <WorkspaceHeader />
      <main key={opened?.id ?? "all"} className={`app-content page-sheet ${opened ? "forward" : "backward"}`}>
      {openedId && !opened ? (
        <p className="muted">Abrindo projeto…</p>
      ) : opened && token ? (
        <ProjectSheet token={token} project={opened} section={route?.section ?? "modelo"}
          onSection={(section) => navigate(projectPath(opened.id, section))}
          onBack={() => navigate("/projetos")}
          onEnter={() => { selectProject(opened.id, opened.name); navigate("/tasks"); }}
          onSaved={(updated) => setProjects((list) => list.map((p) => (p.id === updated.id ? updated : p)))}
          onDeleted={() => { navigate("/projetos"); void refresh(); }} />
      ) : (
      <>
      {token && (
        <section className="home-search">
          <SearchBox token={token} />
        </section>
      )}
      {token && <HomePanels token={token} />}

      <div className="page-header home-projects-header">
        <h2>Projetos</h2>
        <button onClick={() => void refresh()} disabled={loading}>
          {loading ? "Atualizando…" : "Atualizar"}
        </button>
      </div>

      {error && <p className="error">{error}</p>}

      {!loading && projects.length === 0 && !isAdmin && (
        <p className="muted">Você ainda não foi adicionado a nenhum projeto — peça a um administrador.</p>
      )}

      <div className="project-grid">
        {projects.map((project, index) => (
          <div className="project-card" key={project.id} style={{ "--i": index, "--cover": COVER_COLORS[project.coverColor ?? "ink"].hex } as React.CSSProperties} onClick={() => navigate(projectPath(project.id))}>
            <span className="project-card-icon">📁</span>
            <span className="project-card-name">{project.name}</span>
            {project.description && <span className="muted">{project.description}</span>}
            <span className="muted" style={{ fontSize: 12 }}>
              {project.memberCount} membro{project.memberCount === 1 ? "" : "s"}
            </span>
            {isAdmin && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setManagingProject(project);
                }}
                style={{ marginTop: 8, alignSelf: "flex-start" }}
              >
                Gerenciar membros
              </button>
            )}
          </div>
        ))}

        {isAdmin && !showForm && (
          <button className="project-card project-card-new" onClick={() => setShowForm(true)}>
            <span className="project-card-icon">+</span>
            <span>Novo projeto</span>
          </button>
        )}

        {isAdmin && showForm && (
          <form className="project-card" onSubmit={handleCreate}>
            <label>
              Nome
              <input value={name} onChange={(e) => setName(e.target.value)} autoFocus />
            </label>
            <label>
              Descrição (opcional)
              <input value={description} onChange={(e) => setDescription(e.target.value)} />
            </label>
            <div style={{ display: "flex", gap: 8 }}>
              <button type="submit" className="primary" disabled={creating || !name.trim()}>
                {creating ? "Criando…" : "Criar"}
              </button>
              <button type="button" onClick={() => setShowForm(false)}>
                Cancelar
              </button>
            </div>
          </form>
        )}
      </div>

      {canManageInstanceModel && token && (
        <section className="instance-model">
          <div className="page-header">
            <div>
              <h2 style={{ marginBottom: 4 }}>Modelo padrão da instância</h2>
              <p className="muted">Usado por todo projeto que não tiver conexão própria.</p>
            </div>
          </div>
          <ModelPanel token={token} scope={{ kind: "instance" }} canManage title="padrão da instância" />
        </section>
      )}

      {managingProject && <MembersModal project={managingProject} onClose={() => setManagingProject(null)} />}
      </>
      )}
      </main>
    </div>
  );
}
