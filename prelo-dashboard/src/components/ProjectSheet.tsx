import { DeploysPanel } from "./DeploysPanel";
import { useEffect, useState, type CSSProperties } from "react";
import {
  decodeDashboardToken,
  getProjectModel,
  listProjectFiles,
  listServers,
  listTasks,
  type ProjectSummary,
} from "../api/client";
import { ModelPanel } from "./model/ModelPanel";
import { FilesPanel } from "./files/FilesPanel";
import { ToolboxPanel } from "./ToolboxPanel";
import { ProjectSettingsPanel } from "./ProjectSettingsPanel";
import { COVER_COLORS } from "./coverColors";
import { PROVIDERS } from "./model/providers";
import type { ProjectSection } from "../router";


interface Overview {
  model: string;
  tasks: string;
  servers: string;
  files: string;
}

const EDITION = new Intl.DateTimeFormat("pt-BR", { day: "2-digit", month: "short", year: "numeric" });

/**
 * A project's "cover" inside the Projetos screen (Fase M): magazine masthead, section index and
 * the Modelo section. Entering the project (the task/servers dashboard) stays an explicit action.
 */
export function ProjectSheet({ token, project, section, onSection, onBack, onEnter, onSaved, onDeleted }: {
  token: string;
  project: ProjectSummary;
  section: ProjectSection;
  onSection: (section: ProjectSection) => void;
  onBack: () => void;
  onEnter: () => void;
  onSaved: (project: ProjectSummary) => void;
  onDeleted: () => void;
}) {
  const setSection = onSection;
  const [overview, setOverview] = useState<Overview | null>(null);
  const canManage = decodeDashboardToken(token)?.scopes.includes("projects:manage") ?? false;

  useEffect(() => {
    let cancelled = false;
    void Promise.all([
      getProjectModel(token, project.id).then((v) => {
        const c = v.usingOwn ? v.own : v.instance.configured ? v.instance : null;
        return c ? `${c.model} · ${PROVIDERS[c.provider!].label}` : "Nenhum conectado";
      }).catch(() => "—"),
      listTasks(token, project.id).then((t) => `${t.length} task${t.length === 1 ? "" : "s"}`).catch(() => "—"),
      listServers(token, project.id).then((s) => `${s.length} servidor${s.length === 1 ? "" : "es"}`).catch(() => "—"),
      listProjectFiles(token, project.id).then((f) => `${f.files.length} arquivo${f.files.length === 1 ? "" : "s"}`).catch(() => "—"),
    ]).then(([model, tasks, servers, files]) => {
      if (!cancelled) setOverview({ model, tasks, servers, files });
    });
    return () => {
      cancelled = true;
    };
  }, [token, project.id, section]);

  const clips = overview
    ? [
      { kicker: "Modelo", title: overview.model, body: "O cérebro dos agentes deste projeto.", go: () => setSection("modelo") },
      { kicker: "Tasks", title: overview.tasks, body: "Acompanhe em Tasks e no Pipeline, dentro do projeto.", go: onEnter },
      { kicker: "Servidores", title: overview.servers, body: "Saúde e containers da frota do projeto.", go: onEnter },
      { kicker: "Arquivos", title: overview.files, body: "PDFs, imagens e documentos do projeto, cifrados no disco.", go: () => setSection("arquivos") },
    ]
    : [];

  return (
    <div className="project-sheet" style={{ "--cover": COVER_COLORS[project.coverColor ?? "ink"].hex } as CSSProperties}>
      <button type="button" className="back-link" onClick={onBack}>← Todos os projetos</button>

      <section className="project-cover">
        <div className="project-cover-titles">
          <span className="clipping-kicker">Projeto · Edição de {EDITION.format(new Date()).replace(/\./g, "")}</span>
          <h2 className="project-cover-name">{project.name}</h2>
          {project.description && <span className="project-cover-deck">{project.description}</span>}
        </div>
        <div className="project-cover-side">
          <span className="clipping-mono">
            {project.memberCount} membro{project.memberCount === 1 ? "" : "s"}
            {overview ? ` · ${overview.tasks} · ${overview.servers}` : ""}
          </span>
          <button type="button" className="primary" onClick={onEnter}>Entrar no projeto →</button>
        </div>
      </section>

      <nav className="section-tabs" aria-label="Seções do projeto">
        <button type="button" className={section === "visao-geral" ? "active" : ""} onClick={() => setSection("visao-geral")}>Visão geral</button>
        <button type="button" className={section === "modelo" ? "active" : ""} onClick={() => setSection("modelo")}>Modelo</button>
        <button type="button" className={section === "ferramentas" ? "active" : ""} onClick={() => setSection("ferramentas")}>Ferramentas</button>
        <button type="button" className={section === "arquivos" ? "active" : ""} onClick={() => setSection("arquivos")}>Arquivos</button>
        <button type="button" className={section === "configuracoes" ? "active" : ""} onClick={() => setSection("configuracoes")}>Configurações</button>
      </nav>

      <div key={section} className="page-sheet forward">
        {section === "visao-geral" && <DeploysPanel token={token} projectId={project.id} />}
        {section === "visao-geral" && (
          <div className="clipping-grid overview-grid">
            {clips.map((c, index) => (
              <article key={c.kicker} className="clipping" style={{ "--i": index } as CSSProperties}>
                <span className="clipping-kicker">{c.kicker}</span>
                <h3 className="clipping-headline">{c.title}</h3>
                <div className="clipping-body"><p>{c.body}</p></div>
                {c.go && (
                  <div className="clipping-footer">
                    <span />
                    <button type="button" onClick={c.go}>{c.kicker === "Modelo" ? "Ver modelo" : c.kicker === "Arquivos" ? "Ver arquivos" : "Abrir"}</button>
                  </div>
                )}
              </article>
            ))}
          </div>
        )}
        {section === "modelo" && (
          <ModelPanel token={token} scope={{ kind: "project", projectId: project.id }} canManage={canManage} title={`para o projeto ${project.name}`} />
        )}
        {section === "ferramentas" && <ToolboxPanel token={token} projectId={project.id} canManage={canManage} />}
        {section === "arquivos" && <FilesPanel token={token} projectId={project.id} />}
        {section === "configuracoes" && (
          <ProjectSettingsPanel key={project.id} token={token} project={project} onSaved={onSaved} onDeleted={onDeleted} />
        )}
      </div>
    </div>
  );
}
