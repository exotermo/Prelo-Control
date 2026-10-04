import { useEffect, useMemo, useState, type FormEvent } from "react";
import {
  ApiError,
  addClientContact,
  createClient,
  decodeDashboardToken,
  deleteClient,
  getClient,
  getClientTimeline,
  listClients,
  listProjects,
  removeClientContact,
  setProjectClient,
  touchRecent,
  updateClient,
  type Client,
  type ClientStatus,
  type ContactKind,
  type ProjectSummary,
  type TimelineEntry,
} from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { WorkspaceHeader } from "../components/WorkspaceHeader";
import { useQuickView } from "../context/QuickViewContext";
import { clientPath, navigate, parseClientsPath, projectPath, usePathname, type ClientSection } from "../router";

const STATUS_LABEL: Record<ClientStatus, string> = { ACTIVE: "Ativo", LEAD: "Lead", INACTIVE: "Inativo", DISCARDED: "Descartado" };
const STATUS_STAMP: Record<ClientStatus, string> = { ACTIVE: "stamp-ok", LEAD: "stamp-wait", INACTIVE: "stamp-wait", DISCARDED: "stamp-bad" };
const CONTACT_LABEL: Record<ContactKind, string> = { WHATSAPP: "WhatsApp", PHONE: "Telefone", EMAIL: "E-mail" };
const FILTERS: { id: ClientStatus | "ALL"; label: string }[] = [
  { id: "ALL", label: "Todos" },
  { id: "ACTIVE", label: "Ativos" },
  { id: "LEAD", label: "Leads" },
  { id: "INACTIVE", label: "Inativos" },
];
const SECTIONS: { id: ClientSection; label: string }[] = [
  { id: "visao-geral", label: "Visão geral" },
  { id: "linha-do-tempo", label: "Linha do tempo" },
  { id: "contatos", label: "Contatos" },
  { id: "projetos", label: "Projetos" },
];

const errorText = (err: unknown, fallback: string) => (err instanceof ApiError ? err.message : fallback);
const dateline = (iso: string) => new Date(iso).toLocaleDateString("pt-BR", { day: "2-digit", month: "short", year: "numeric" }).toUpperCase();

/** /clientes and /clientes/{id}/{section} (Fase C1). */
export function ClientsPage() {
  const { token } = useAuth();
  const route = parseClientsPath(usePathname());
  const openedId = route?.clientId ?? null;
  if (!token) return null;
  return (
    <div className="app-shell">
      <WorkspaceHeader />
      <main key={openedId ?? "all"} className={`app-content page-sheet ${openedId ? "forward" : "backward"}`}>
        {openedId ? (
          <ClientCover token={token} clientId={openedId} section={route?.section ?? "visao-geral"} />
        ) : (
          <ClientList token={token} />
        )}
      </main>
    </div>
  );
}

// --- list ---

function ClientList({ token }: { token: string }) {
  const [clients, setClients] = useState<Client[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<ClientStatus | "ALL">("ALL");
  const [text, setText] = useState("");
  const [creating, setCreating] = useState(false);

  async function refresh() {
    setLoading(true);
    try {
      setClients(await listClients(token));
      setError(null);
    } catch (err) {
      setError(errorText(err, "Falha ao carregar clientes."));
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  const visible = useMemo(() => {
    const norm = (s: string) => s.normalize("NFD").replace(/\p{Diacritic}/gu, "").toLowerCase();
    const q = norm(text.trim());
    return clients.filter((c) => (filter === "ALL" || c.status === filter)
      && (!q || norm([c.name, c.company, c.city, c.primaryContact?.value].filter(Boolean).join(" ")).includes(q)));
  }, [clients, filter, text]);

  return (
    <>
      <div className="page-header">
        <h2>Clientes</h2>
        <button className="primary" onClick={() => setCreating(true)}>+ Novo cliente</button>
      </div>
      <div className="client-toolbar">
        <div className="filter-chips" role="group" aria-label="Filtrar por situação">
          {FILTERS.map((f) => (
            <button key={f.id} className={filter === f.id ? "chip active" : "chip"} onClick={() => setFilter(f.id)}>
              {f.label}
              <span className="chip-count">{f.id === "ALL" ? clients.length : clients.filter((c) => c.status === f.id).length}</span>
            </button>
          ))}
        </div>
        <input className="client-filter" value={text} onChange={(e) => setText(e.target.value)} placeholder="Filtrar por nome, cidade, telefone…" aria-label="Filtrar clientes" />
      </div>
      {error && <p className="error">{error}</p>}
      {!loading && clients.length === 0 && (
        <div className="empty-sheet">
          <p className="model-empty-title">Nenhum cliente ainda</p>
          <p className="muted">Cadastre o primeiro cliente para ligar projetos, conversas e arquivos a ele.</p>
        </div>
      )}
      {!loading && clients.length > 0 && visible.length === 0 && <p className="muted">Nenhum cliente com esse filtro.</p>}
      <div className="clipping-grid client-grid">
        {visible.map((c) => (
          <button key={c.id} className="clipping client-clipping" onClick={() => { touchRecent(token, "CLIENT", c.id); navigate(clientPath(c.id)); }}>
            <span className="clipping-kicker">{c.company ?? "Cliente"}</span>
            <span className="clipping-headline">{c.name}</span>
            <span className="clipping-dateline">{[c.city, `desde ${dateline(c.createdAt)}`].filter(Boolean).join(" · ")}</span>
            <span className="clipping-body">
              {c.primaryContact ? <span className="clipping-mono">{CONTACT_LABEL[c.primaryContact.kind]}: {c.primaryContact.value}</span> : <span className="muted">sem contato</span>}
            </span>
            <span className="clipping-footer">
              <span>{c.projectCount ?? 0} projeto{c.projectCount === 1 ? "" : "s"}</span>
              <span className={`clipping-stamp ${STATUS_STAMP[c.status]}`}>{STATUS_LABEL[c.status]}</span>
            </span>
          </button>
        ))}
      </div>
      {creating && (
        <NewClientModal token={token} onClose={() => setCreating(false)}
          onCreated={(client) => { setCreating(false); touchRecent(token, "CLIENT", client.id); navigate(clientPath(client.id)); }} />
      )}
    </>
  );
}

function NewClientModal({ token, onClose, onCreated }: { token: string; onClose: () => void; onCreated: (c: Client) => void }) {
  const [name, setName] = useState("");
  const [company, setCompany] = useState("");
  const [city, setCity] = useState("");
  const [whatsapp, setWhatsapp] = useState("");
  const [email, setEmail] = useState("");
  const [website, setWebsite] = useState("");
  const [status, setStatus] = useState<ClientStatus>("ACTIVE");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(null);
    try {
      const contacts = [
        ...(whatsapp.trim() ? [{ kind: "WHATSAPP" as const, value: whatsapp.trim() }] : []),
        ...(email.trim() ? [{ kind: "EMAIL" as const, value: email.trim() }] : []),
      ];
      onCreated(await createClient(token, {
        name: name.trim(), company: company.trim() || null, city: city.trim() || null,
        website: website.trim() || null, status, contacts,
      }));
    } catch (err) {
      setError(errorText(err, "Falha ao cadastrar cliente."));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <form className="card client-form" onSubmit={submit} onClick={(e) => e.stopPropagation()}>
        <div className="page-header">
          <h3>Novo cliente</h3>
          <button type="button" onClick={onClose}>Fechar</button>
        </div>
        <label>Nome<input value={name} onChange={(e) => setName(e.target.value)} autoFocus required maxLength={200} /></label>
        <div className="form-row">
          <label>Empresa<input value={company} onChange={(e) => setCompany(e.target.value)} /></label>
          <label>Cidade<input value={city} onChange={(e) => setCity(e.target.value)} /></label>
        </div>
        <div className="form-row">
          <label>WhatsApp<input value={whatsapp} onChange={(e) => setWhatsapp(e.target.value)} placeholder="(41) 98445-0529" inputMode="tel" /></label>
          <label>E-mail<input value={email} onChange={(e) => setEmail(e.target.value)} type="email" /></label>
        </div>
        <label>Site<input value={website} onChange={(e) => setWebsite(e.target.value)} placeholder="https://" /></label>
        <label>Situação
          <select value={status} onChange={(e) => setStatus(e.target.value as ClientStatus)}>
            {(["ACTIVE", "LEAD", "INACTIVE"] as ClientStatus[]).map((s) => <option key={s} value={s}>{STATUS_LABEL[s]}</option>)}
          </select>
        </label>
        <p className="muted form-hint">Mensagens de WhatsApp desse número serão ligadas a este cliente.</p>
        {error && <p className="error">{error}</p>}
        <button type="submit" className="primary" disabled={saving || !name.trim()}>{saving ? "Salvando…" : "Cadastrar cliente"}</button>
      </form>
    </div>
  );
}

// --- cover ---

function ClientCover({ token, clientId, section }: { token: string; clientId: string; section: ClientSection }) {
  const [client, setClient] = useState<Client | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function reload() {
    try {
      setClient(await getClient(token, clientId));
      setError(null);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) navigate("/clientes", { replace: true });
      else setError(errorText(err, "Falha ao carregar cliente."));
    }
  }
  useEffect(() => {
    void reload();
    touchRecent(token, "CLIENT", clientId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, clientId]);

  if (error) return <p className="error">{error}</p>;
  if (!client) return <p className="muted">Abrindo cliente…</p>;

  return (
    <>
      <button className="back-link" onClick={() => navigate("/clientes")}>← Clientes</button>
      <div className="project-cover">
        <div className="project-cover-titles">
          <span className="clipping-kicker">{client.company ?? "Cliente"}</span>
          <h2 className="project-cover-name">{client.name}</h2>
          <span className="project-cover-deck">
            {[client.city, `cliente desde ${dateline(client.createdAt).toLowerCase()}`].filter(Boolean).join(" · ")}
          </span>
        </div>
        <div className="project-cover-side">
          <span className={`clipping-stamp ${STATUS_STAMP[client.status]}`}>{STATUS_LABEL[client.status]}</span>
          {client.contacts?.[0] && <span className="clipping-mono">{client.contacts[0].value}</span>}
        </div>
      </div>
      <nav className="section-tabs" aria-label="Seções do cliente">
        {SECTIONS.map((s) => (
          <button key={s.id} className={section === s.id ? "active" : ""} onClick={() => navigate(clientPath(client.id, s.id))}>
            {s.label}
            {s.id === "contatos" && client.contacts ? ` (${client.contacts.length})` : ""}
            {s.id === "projetos" && client.projects ? ` (${client.projects.length})` : ""}
          </button>
        ))}
      </nav>
      {section === "visao-geral" && <ClientOverview token={token} client={client} onSaved={setClient} />}
      {section === "linha-do-tempo" && <ClientTimeline token={token} clientId={client.id} />}
      {section === "contatos" && <ClientContacts token={token} client={client} onChanged={reload} />}
      {section === "projetos" && <ClientProjects token={token} client={client} onChanged={reload} />}
    </>
  );
}

function ClientOverview({ token, client, onSaved }: { token: string; client: Client; onSaved: (c: Client) => void }) {
  const isAdmin = decodeDashboardToken(token)?.scopes.includes("clients:delete") ?? false;
  const [form, setForm] = useState({
    name: client.name, company: client.company ?? "", city: client.city ?? "", address: client.address ?? "",
    website: client.website ?? "", notes: client.notes ?? "", status: client.status,
  });
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirmName, setConfirmName] = useState("");
  const set = (key: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) =>
    setForm((f) => ({ ...f, [key]: e.target.value }));

  async function save(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(null);
    setMessage(null);
    try {
      const saved = await updateClient(token, client.id, {
        version: client.version, name: form.name.trim(), company: form.company, city: form.city, address: form.address,
        website: form.website.trim() || null, notes: form.notes, status: form.status, stage: client.stage,
      });
      onSaved(saved);
      setMessage("Salvo.");
    } catch (err) {
      setError(err instanceof ApiError && err.status === 409 ? "Outra pessoa alterou este cliente — recarregue a página." : errorText(err, "Falha ao salvar."));
    } finally {
      setSaving(false);
    }
  }

  async function remove() {
    try {
      await deleteClient(token, client.id);
      navigate("/clientes");
    } catch (err) {
      setError(errorText(err, "Falha ao excluir."));
    }
  }

  return (
    <div className="client-overview">
      <form className="card client-form" onSubmit={save}>
        <label>Nome<input value={form.name} onChange={set("name")} required maxLength={200} /></label>
        <div className="form-row">
          <label>Empresa<input value={form.company} onChange={set("company")} /></label>
          <label>Situação
            <select value={form.status} onChange={set("status")}>
              {(Object.keys(STATUS_LABEL) as ClientStatus[]).map((s) => <option key={s} value={s}>{STATUS_LABEL[s]}</option>)}
            </select>
          </label>
        </div>
        <div className="form-row">
          <label>Cidade<input value={form.city} onChange={set("city")} /></label>
          <label>Endereço<input value={form.address} onChange={set("address")} /></label>
        </div>
        <label>Site<input value={form.website} onChange={set("website")} placeholder="https://" /></label>
        <label>Notas<textarea value={form.notes} onChange={set("notes")} rows={6} maxLength={10000} /></label>
        {error && <p className="error">{error}</p>}
        {message && <p className="muted">{message}</p>}
        <button type="submit" className="primary" disabled={saving || !form.name.trim()}>{saving ? "Salvando…" : "Salvar"}</button>
      </form>
      {isAdmin && (
        <section className="settings-section danger-zone">
          <h3>Zona de perigo</h3>
          <p className="muted">Excluir o cliente desliga os projetos dele (os projetos continuam existindo) e libera os contatos.</p>
          <label>Digite <strong>{client.name}</strong> para confirmar
            <input value={confirmName} onChange={(e) => setConfirmName(e.target.value)} />
          </label>
          <button className="danger" disabled={confirmName !== client.name} onClick={() => void remove()}>Excluir cliente</button>
        </section>
      )}
    </div>
  );
}

const TIMELINE_LABEL: Record<TimelineEntry["kind"], string> = { CLIENT_CREATED: "Cadastro", PROJECT: "Projeto", TASK: "Task", FILE: "Arquivo" };

function ClientTimeline({ token, clientId }: { token: string; clientId: string }) {
  const [entries, setEntries] = useState<TimelineEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { openTask } = useQuickView();

  async function load(before?: string) {
    setLoading(true);
    try {
      const page = await getClientTimeline(token, clientId, before);
      setEntries((prev) => (before ? [...prev, ...page] : page));
      setHasMore(page.length === 50);
      setError(null);
    } catch (err) {
      setError(errorText(err, "Falha ao carregar a linha do tempo."));
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, clientId]);

  function open(entry: TimelineEntry) {
    if (entry.kind === "TASK") openTask(entry.id);
    else if (entry.kind === "PROJECT") navigate(projectPath(entry.id, "visao-geral"));
    else if (entry.kind === "FILE" && entry.projectId) navigate(projectPath(entry.projectId, "arquivos"));
  }

  return (
    <div>
      {error && <p className="error">{error}</p>}
      {!loading && entries.length === 0 && <p className="muted">Nada registrado ainda.</p>}
      <ol className="timeline">
        {entries.map((e) => (
          <li key={`${e.kind}-${e.id}`} className={`timeline-item timeline-${e.kind.toLowerCase()}`}>
            <time className="timeline-date">{new Date(e.at).toLocaleString("pt-BR", { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" })}</time>
            <button className="timeline-body" onClick={() => open(e)} disabled={e.kind === "CLIENT_CREATED"}>
              <span className="clipping-kicker">{TIMELINE_LABEL[e.kind]}</span>
              <span className="timeline-title">{e.title}</span>
              {e.detail && <span className="timeline-detail">{e.detail}</span>}
            </button>
            {e.kind === "TASK" && e.status && <span className={`badge badge-${e.status.toLowerCase()}`}>{e.status.toLowerCase()}</span>}
          </li>
        ))}
      </ol>
      {hasMore && <button disabled={loading} onClick={() => void load(entries[entries.length - 1]?.at)}>{loading ? "Carregando…" : "Mais antigos"}</button>}
    </div>
  );
}

function ClientContacts({ token, client, onChanged }: { token: string; client: Client; onChanged: () => Promise<void> }) {
  const [kind, setKind] = useState<ContactKind>("WHATSAPP");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function add(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await addClientContact(token, client.id, { kind, value: value.trim(), isPrimary: (client.contacts?.length ?? 0) === 0 });
      setValue("");
      await onChanged();
    } catch (err) {
      setError(errorText(err, "Falha ao adicionar contato."));
    } finally {
      setBusy(false);
    }
  }

  async function remove(contactId: string) {
    try {
      await removeClientContact(token, client.id, contactId);
      await onChanged();
    } catch (err) {
      setError(errorText(err, "Falha ao remover contato."));
    }
  }

  return (
    <div className="client-contacts">
      <ul className="contact-list">
        {(client.contacts ?? []).map((c) => (
          <li key={c.id}>
            <span className="contact-kind">{CONTACT_LABEL[c.kind]}{c.isPrimary ? " · principal" : ""}</span>
            <span className="clipping-mono">{c.value}</span>
            <button onClick={() => void remove(c.id)}>Remover</button>
          </li>
        ))}
        {(client.contacts ?? []).length === 0 && <li className="muted">Nenhum contato.</li>}
      </ul>
      <form className="contact-add" onSubmit={add}>
        <select value={kind} onChange={(e) => setKind(e.target.value as ContactKind)} aria-label="Tipo de contato">
          {(Object.keys(CONTACT_LABEL) as ContactKind[]).map((k) => <option key={k} value={k}>{CONTACT_LABEL[k]}</option>)}
        </select>
        <input value={value} onChange={(e) => setValue(e.target.value)} placeholder={kind === "EMAIL" ? "nome@empresa.com" : "(41) 98445-0529"} aria-label="Contato" />
        <button type="submit" className="primary" disabled={busy || !value.trim()}>Adicionar</button>
      </form>
      {error && <p className="error">{error}</p>}
      <p className="muted form-hint">Telefones são guardados no formato internacional (+55…). Um número só pode pertencer a um cliente.</p>
    </div>
  );
}

function ClientProjects({ token, client, onChanged }: { token: string; client: Client; onChanged: () => Promise<void> }) {
  const canLink = decodeDashboardToken(token)?.scopes.includes("projects:manage") ?? false;
  const [all, setAll] = useState<ProjectSummary[]>([]);
  const [chosen, setChosen] = useState("");
  const [error, setError] = useState<string | null>(null);
  const linked = client.projects ?? [];

  useEffect(() => {
    if (canLink) void listProjects(token).then(setAll).catch(() => {});
  }, [token, canLink]);
  const available = all.filter((p) => p.clientId !== client.id);

  async function link(projectId: string, clientId: string | null) {
    try {
      await setProjectClient(token, projectId, clientId);
      setChosen("");
      await onChanged();
      if (canLink) setAll(await listProjects(token));
    } catch (err) {
      setError(errorText(err, "Falha ao vincular projeto."));
    }
  }

  return (
    <div>
      {linked.length === 0 && <p className="muted">Nenhum projeto ligado a este cliente.</p>}
      <div className="project-grid">
        {linked.map((p) => (
          <div key={p.id} className="project-card" onClick={() => navigate(projectPath(p.id, "visao-geral"))}>
            <span className="project-card-icon">📁</span>
            <span className="project-card-name">{p.name}</span>
            {p.description && <span className="muted">{p.description}</span>}
            {canLink && (
              <button style={{ alignSelf: "flex-start", marginTop: 8 }} onClick={(e) => { e.stopPropagation(); void link(p.id, null); }}>
                Desvincular
              </button>
            )}
          </div>
        ))}
      </div>
      {canLink && available.length > 0 && (
        <div className="contact-add" style={{ marginTop: 20 }}>
          <select value={chosen} onChange={(e) => setChosen(e.target.value)} aria-label="Projeto">
            <option value="">Ligar um projeto existente…</option>
            {available.map((p) => <option key={p.id} value={p.id}>{p.name}{p.clientId ? " (de outro cliente)" : ""}</option>)}
          </select>
          <button className="primary" disabled={!chosen} onClick={() => void link(chosen, client.id)}>Ligar</button>
        </div>
      )}
      {error && <p className="error">{error}</p>}
    </div>
  );
}
