import { useEffect, useState, type FormEvent } from "react";
import {
  ApiError,
  addProjectMember,
  decodeDashboardToken,
  deleteProject,
  listAgents,
  listDashboardUsers,
  listProjectMembers,
  removeProjectMember,
  updateProject,
  type AgentSummary,
  type CoverColor,
  type DashboardUserSummary,
  type ProjectSummary,
} from "../api/client";
import { COVER_COLORS } from "./coverColors";

const MAX_INSTRUCTIONS = 4000;

export function ProjectSettingsPanel({ token, project, onSaved, onDeleted }: {
  token: string;
  project: ProjectSummary;
  onSaved: (project: ProjectSummary) => void;
  onDeleted: () => void;
}) {
  const canManage = decodeDashboardToken(token)?.scopes.includes("projects:manage") ?? false;
  const canListUsers = decodeDashboardToken(token)?.scopes.includes("users:manage") ?? false;

  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description ?? "");
  const [coverColor, setCoverColor] = useState<CoverColor>(project.coverColor ?? "ink");
  const [defaultAgentId, setDefaultAgentId] = useState(project.defaultAgentId ?? "");
  const [instructions, setInstructions] = useState(project.instructions ?? "");
  const [agents, setAgents] = useState<AgentSummary[]>([]);
  const [users, setUsers] = useState<DashboardUserSummary[]>([]);
  const [memberIds, setMemberIds] = useState<Set<string>>(new Set());
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmName, setConfirmName] = useState("");
  const [deleting, setDeleting] = useState(false);

  useEffect(() => {
    void listAgents(token).then(setAgents).catch(() => setAgents([]));
    if (canManage) {
      void listProjectMembers(token, project.id).then((m) => setMemberIds(new Set(m.map((x) => x.userId)))).catch(() => {});
    }
    if (canListUsers) void listDashboardUsers(token).then(setUsers).catch(() => {});
  }, [token, project.id, canManage, canListUsers]);

  const dirty = name !== project.name || description !== (project.description ?? "") || coverColor !== (project.coverColor ?? "ink")
    || defaultAgentId !== (project.defaultAgentId ?? "") || instructions !== (project.instructions ?? "");

  async function save(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      const updated = await updateProject(token, project.id, {
        name: name.trim(), description: description.trim() || null, coverColor,
        defaultAgentId: defaultAgentId || null, instructions: instructions.trim() || null,
      });
      onSaved(updated);
      setSaved(true);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao salvar.");
    } finally {
      setSaving(false);
    }
  }

  async function toggleMember(user: DashboardUserSummary) {
    const isMember = memberIds.has(user.id);
    try {
      if (isMember) await removeProjectMember(token, project.id, user.id);
      else await addProjectMember(token, project.id, user.id);
      setMemberIds((set) => {
        const next = new Set(set);
        if (isMember) next.delete(user.id);
        else next.add(user.id);
        return next;
      });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao atualizar membro.");
    }
  }

  async function destroy() {
    setDeleting(true);
    setError(null);
    try {
      await deleteProject(token, project.id);
      onDeleted();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao excluir o projeto.");
      setDeleting(false);
    }
  }

  return (
    <div className="settings-panel">
      {!canManage && <div className="wizard-note">Você está vendo as configurações em modo leitura — só administradores alteram.</div>}

      <form className="settings-form" onSubmit={(e) => void save(e)}>
        <section className="settings-section">
          <h3>Identidade</h3>
          <label>
            Nome
            <input value={name} onChange={(e) => setName(e.target.value)} maxLength={120} required disabled={!canManage} />
          </label>
          <label>
            Descrição (subtítulo da capa)
            <input value={description} onChange={(e) => setDescription(e.target.value)} disabled={!canManage} />
          </label>
          <fieldset className="color-picker" disabled={!canManage}>
            <legend>Cor da capa</legend>
            {(Object.keys(COVER_COLORS) as CoverColor[]).map((c) => (
              <button key={c} type="button" className={`color-swatch${coverColor === c ? " selected" : ""}`}
                style={{ background: COVER_COLORS[c].hex }} aria-label={COVER_COLORS[c].label} title={COVER_COLORS[c].label}
                aria-pressed={coverColor === c} onClick={() => setCoverColor(c)} />
            ))}
          </fieldset>
        </section>

        <section className="settings-section">
          <h3>Agente padrão</h3>
          <p className="muted">Usado pelas tasks deste projeto quando ninguém escolhe um agente.</p>
          <div className="agent-options">
            <button type="button" className={`type-tile${defaultAgentId === "" ? " selected" : ""}`} disabled={!canManage} onClick={() => setDefaultAgentId("")}>
              <strong>Padrão do Hermes</strong>
              <span>general</span>
            </button>
            {agents.map((a) => (
              <button key={a.id} type="button" className={`type-tile${defaultAgentId === a.id ? " selected" : ""}`} disabled={!canManage} onClick={() => setDefaultAgentId(a.id)}>
                <strong>{a.name || a.id}</strong>
                <span className="mono">{a.id}</span>
                {a.description && <span>{a.description}</span>}
              </button>
            ))}
          </div>
        </section>

        <section className="settings-section">
          <h3>Instruções do projeto</h3>
          <p className="muted">Todo agente deste projeto recebe este texto como contexto — tom, regras do cliente, o que evitar.</p>
          <textarea value={instructions} onChange={(e) => setInstructions(e.target.value.slice(0, MAX_INSTRUCTIONS))} rows={6}
            placeholder="Ex.: o cliente prefere respostas curtas e em português formal; nunca citar preços sem confirmação." disabled={!canManage} />
          <span className="clipping-mono char-count">{instructions.length} / {MAX_INSTRUCTIONS}</span>
        </section>

        {canManage && (
          <div className="settings-save">
            {saved && !dirty && <span className="settings-saved">Salvo.</span>}
            <button type="submit" className="primary" disabled={!dirty || saving || !name.trim()}>{saving ? "Salvando…" : "Salvar alterações"}</button>
          </div>
        )}
      </form>

      {error && <p className="error" role="alert">{error}</p>}

      {canManage && (
        <section className="settings-section">
          <h3>Membros</h3>
          <p className="muted">Quem não é administrador só vê e usa este projeto se estiver marcado aqui.</p>
          {canListUsers ? (
            <ul className="member-list">
              {users.map((u) => (
                <li key={u.id}>
                  <label className="check-row">
                    <input type="checkbox" checked={memberIds.has(u.id)} onChange={() => void toggleMember(u)} />
                    <span>{u.email} <code>{u.role}</code></span>
                  </label>
                </li>
              ))}
            </ul>
          ) : (
            <p className="muted">{memberIds.size} membro(s).</p>
          )}
        </section>
      )}

      {canManage && (
        <section className="settings-section danger-zone">
          <h3>Zona de perigo</h3>
          <p>Excluir o projeto o tira da lista de todo mundo e apaga do disco os arquivos dele. Tasks e histórico ficam no banco para auditoria.</p>
          <label>
            Digite <strong>{project.name}</strong> para confirmar
            <input value={confirmName} onChange={(e) => setConfirmName(e.target.value)} autoComplete="off" />
          </label>
          <button type="button" className="danger" disabled={confirmName !== project.name || deleting} onClick={() => void destroy()}>
            {deleting ? "Excluindo…" : "Excluir projeto"}
          </button>
        </section>
      )}
    </div>
  );
}
