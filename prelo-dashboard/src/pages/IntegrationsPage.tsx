import { useEffect, useState, type CSSProperties } from "react";
import {
  ApiError,
  deleteWebhook,
  getAutoReplyStatus,
  getWhatsAppStatus,
  listIntegrations,
  revokeApiKey,
  sendWebhookTest,
  setAutoReply,
  type ApiKeySummary,
  type ChannelStatus,
  type ProjectIntegrations,
  type WebhookSummary,
} from "../api/client";
import { useAuth } from "../auth/AuthContext";
import { useProject } from "../context/ProjectContext";
import { IntegrationModal } from "../components/IntegrationModal";

function statusBadge(status: string) {
  return <span className={`badge badge-${status.toLowerCase()}`}>{status}</span>;
}

function newsDate(iso: string): string {
  return new Date(iso).toLocaleDateString("pt-BR", { day: "2-digit", month: "short", year: "numeric" }).replace(/\./g, "");
}

const SCOPE_LABEL: Record<string, string> = {
  "tasks:create": "cria tasks",
  "tasks:read": "lê tasks",
  "tasks:execute": "executa tasks",
  "observability:read": "vê turnos",
};

function ApiKeyClipping({ apiKey, now, index, onRevoke }: { apiKey: ApiKeySummary; now: number; index: number; onRevoke: () => void }) {
  const expired = !!apiKey.expiresAt && new Date(apiKey.expiresAt).getTime() < now;
  return (
    <article className="clipping" style={{ "--i": index } as CSSProperties}>
      <span className="clipping-kicker">Chave de API</span>
      <h3 className="clipping-headline">{apiKey.name}</h3>
      <span className="clipping-dateline">Cadastrada em {newsDate(apiKey.createdAt)}</span>
      <div className="clipping-body">
        <p className="clipping-mono">{apiKey.displayPrefix}…</p>
        <p>Pode: {apiKey.scopes.map((s) => SCOPE_LABEL[s] ?? s).join(", ")}.</p>
        <p>
          {apiKey.expiresAt ? `${expired ? "Expirou" : "Expira"} em ${newsDate(apiKey.expiresAt)}` : "Não expira"}
          {" · "}
          {apiKey.lastUsedAt ? `último uso em ${new Date(apiKey.lastUsedAt).toLocaleString("pt-BR")}` : "nunca usada"}
        </p>
      </div>
      <div className="clipping-footer">
        <span className={`clipping-stamp ${expired ? "stamp-bad" : "stamp-ok"}`}>{expired ? "Expirada" : "Ativa"}</span>
        <button type="button" onClick={onRevoke}>Revogar</button>
      </div>
    </article>
  );
}

const DELIVERY_STAMP: Record<string, { label: string; tone: string }> = {
  DELIVERED: { label: "Entregue", tone: "stamp-ok" },
  PENDING: { label: "Na fila", tone: "stamp-wait" },
  SENDING: { label: "Enviando", tone: "stamp-wait" },
  RETRY: { label: "Tentando de novo", tone: "stamp-wait" },
  DEAD: { label: "Falhou", tone: "stamp-bad" },
};

function WebhookClipping({ webhook, index, onTest, onDelete }: { webhook: WebhookSummary; index: number; onTest: () => void; onDelete: () => void }) {
  const last = webhook.lastDelivery;
  const stamp = last ? DELIVERY_STAMP[last.status] : { label: "Sem envios", tone: "stamp-wait" };
  return (
    <article className="clipping" style={{ "--i": index } as CSSProperties}>
      <span className="clipping-kicker">Webhook de saída</span>
      <h3 className="clipping-headline">{webhook.name}</h3>
      <span className="clipping-dateline">Cadastrado em {newsDate(webhook.createdAt)}</span>
      <div className="clipping-body">
        <p className="clipping-mono">{webhook.url}</p>
        <p>Avisa em: {webhook.events.join(", ")}.</p>
        <p>
          {last
            ? `Último envio (${last.event}) em ${new Date(last.createdAt).toLocaleString("pt-BR")}${last.statusCode ? ` · HTTP ${last.statusCode}` : ""}${last.error && last.status !== "DELIVERED" ? ` · ${last.error}` : ""}`
            : "Nada enviado ainda."}
        </p>
      </div>
      <div className="clipping-footer">
        <span className={`clipping-stamp ${stamp.tone}`}>{stamp.label}</span>
        <div style={{ display: "flex", gap: 8 }}>
          <button type="button" onClick={onTest}>Enviar teste</button>
          <button type="button" onClick={onDelete}>Remover</button>
        </div>
      </div>
    </article>
  );
}

export function IntegrationsPage() {
  const { token } = useAuth();
  const { projectId, projectName } = useProject();
  const [channels, setChannels] = useState<ChannelStatus[] | null>(null);
  const [autoReplyEnabled, setAutoReplyEnabled] = useState<boolean | null>(null);
  const [integrations, setIntegrations] = useState<ProjectIntegrations | null>(null);
  const [integrationsError, setIntegrationsError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [toggling, setToggling] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [loadedAt, setLoadedAt] = useState(0);

  async function refreshIntegrations() {
    if (!token || !projectId) return;
    try {
      setIntegrations(await listIntegrations(token, projectId));
      setLoadedAt(Date.now());
      setIntegrationsError(null);
    } catch (err) {
      setIntegrationsError(err instanceof ApiError ? err.message : "Falha ao carregar as integrações do projeto.");
    }
  }

  async function refresh() {
    if (!token) return;
    setLoading(true);
    setError(null);
    try {
      const [whatsapp, autoReply] = await Promise.all([getWhatsAppStatus(token), getAutoReplyStatus(token)]);
      setChannels(whatsapp.channels);
      setAutoReplyEnabled(autoReply.effectiveEnabled);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar o status do WhatsApp.");
    } finally {
      setLoading(false);
    }
    await refreshIntegrations();
  }

  useEffect(() => {
    void refresh();
    const interval = setInterval(() => void refresh(), 15000);
    return () => clearInterval(interval);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, projectId]);

  async function handleToggleAutoReply(next: boolean) {
    if (!token) return;
    setToggling(true);
    setError(null);
    try {
      const status = await setAutoReply(token, next);
      setAutoReplyEnabled(status.effectiveEnabled);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao alterar o auto-reply.");
    } finally {
      setToggling(false);
    }
  }

  async function act(action: () => Promise<void>, failure: string) {
    try {
      await action();
      await refreshIntegrations();
    } catch (err) {
      setIntegrationsError(err instanceof ApiError ? err.message : failure);
    }
  }

  // Prefer a connected channel over stale disconnected ones from past pairing attempts — a
  // single-tenant instance should have at most one live WhatsApp channel at a time, but history
  // (expired pairings, lost sessions) can leave several DISCONNECTED rows behind it.
  const whatsappChannels = channels?.filter((c) => c.channelType === "WHATSAPP") ?? [];
  const whatsapp =
    whatsappChannels.find((c) => c.status === "CONNECTED") ??
    whatsappChannels.slice().sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0] ??
    null;

  const apiKeys = integrations?.apiKeys ?? [];
  const webhooks = integrations?.webhooks ?? [];

  return (
    <div>
      <div className="page-header">
        <div>
          <h2 style={{ marginBottom: 4 }}>Integrações</h2>
          <p className="muted" style={{ maxWidth: 620 }}>
            Tudo que conecta o projeto <strong>{projectName}</strong> a outros serviços — quem chama o Prelo e quem o
            Prelo avisa.
          </p>
        </div>
        <div style={{ display: "flex", gap: 10 }}>
          <button onClick={() => void refresh()} disabled={loading}>{loading ? "Atualizando…" : "Atualizar"}</button>
          <button className="primary" onClick={() => setModalOpen(true)}>+ Cadastrar integração</button>
        </div>
      </div>

      {error && <p className="error" role="alert">{error}</p>}

      <h3 style={{ marginTop: 8 }}>Canais de mensagem</h3>
      <div className="channel-grid">
        <div className="card">
          <h3>WhatsApp</h3>
          {channels === null && loading ? (
            <p className="muted">Carregando…</p>
          ) : !whatsapp ? (
            <div className="empty-state">Nenhum canal de WhatsApp cadastrado ainda.</div>
          ) : (
            <div>
              <p>
                {statusBadge(whatsapp.status)}{" "}
                {whatsapp.status === "CONNECTED" ? (
                  <span className="muted">número {whatsapp.externalRef.split(":")[0]}</span>
                ) : (
                  <span className="muted">precisa parear de novo (escanear QR)</span>
                )}
              </p>
              <p className="muted mono" style={{ fontSize: "0.85em" }}>
                canal {whatsapp.id} · criado em {new Date(whatsapp.createdAt).toLocaleString()}
              </p>
            </div>
          )}
        </div>

        <div className="card">
          <h3>Respostas automáticas</h3>
          <p className="muted">
            Com auto-reply desligado, mensagens que chegam pelo WhatsApp são descartadas silenciosamente
            (nenhuma resposta é enviada, nada é perdido no histórico).
          </p>
          <button
            className={autoReplyEnabled ? undefined : "primary"}
            onClick={() => void handleToggleAutoReply(!autoReplyEnabled)}
            disabled={toggling}
          >
            {toggling ? "Aplicando…" : autoReplyEnabled ? "Pausar auto-reply" : "Ativar auto-reply"}
          </button>
        </div>
      </div>

      <h3 style={{ marginTop: 36 }}>Integrações deste projeto</h3>
      {integrationsError && <p className="error" role="alert">{integrationsError}</p>}

      {integrations && apiKeys.length === 0 && webhooks.length === 0 ? (
        <div className="empty-state">
          Nenhuma integração neste projeto ainda. Use <strong>+ Cadastrar integração</strong> para gerar uma chave de API
          ou avisar um serviço seu por webhook.
        </div>
      ) : (
        <>
          {apiKeys.length > 0 && (
            <section className="clipping-section">
              <h4 className="clipping-section-title">Apps que chamam o Prelo</h4>
              <div className="clipping-grid">
                {apiKeys.map((k, index) => (
                  <ApiKeyClipping
                    key={k.id}
                    index={index}
                    apiKey={k}
                    now={loadedAt}
                    onRevoke={() => {
                      if (token && projectId && window.confirm(`Revogar a chave "${k.name}"? Quem a usa perde o acesso na hora.`)) {
                        void act(() => revokeApiKey(token, projectId, k.id), "Falha ao revogar a chave.");
                      }
                    }}
                  />
                ))}
              </div>
            </section>
          )}
          {webhooks.length > 0 && (
            <section className="clipping-section">
              <h4 className="clipping-section-title">Webhooks de saída</h4>
              <div className="clipping-grid">
                {webhooks.map((w, index) => (
                  <WebhookClipping
                    key={w.id}
                    index={apiKeys.length + index}
                    webhook={w}
                    onTest={() => {
                      if (token && projectId) void act(() => sendWebhookTest(token, projectId, w.id), "Falha ao enviar o teste.");
                    }}
                    onDelete={() => {
                      if (token && projectId && window.confirm(`Remover o webhook "${w.name}"?`)) {
                        void act(() => deleteWebhook(token, projectId, w.id), "Falha ao remover o webhook.");
                      }
                    }}
                  />
                ))}
              </div>
            </section>
          )}
        </>
      )}

      {modalOpen && token && projectId && (
        <IntegrationModal
          token={token}
          projectId={projectId}
          projectName={projectName ?? ""}
          onClose={() => setModalOpen(false)}
          onCreated={() => void refreshIntegrations()}
        />
      )}
    </div>
  );
}
