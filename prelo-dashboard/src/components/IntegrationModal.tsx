import { useState, type FormEvent } from "react";
import {
  ApiError,
  BASE_URL,
  createApiKey,
  createWebhook,
  sendWebhookTest,
  type ApiKeyScope,
  type WebhookEvent,
} from "../api/client";

type Kind = "app" | "webhook";
type Step = 1 | 2 | 3;

const SCOPES: { id: ApiKeyScope; label: string; defaultOn: boolean }[] = [
  { id: "tasks:create", label: "Criar tasks", defaultOn: true },
  { id: "tasks:read", label: "Ler tasks e resultados", defaultOn: true },
  { id: "tasks:execute", label: "Executar tasks", defaultOn: false },
  { id: "observability:read", label: "Ver turnos e árvore de delegação", defaultOn: false },
];

const EVENTS: { id: WebhookEvent; label: string; defaultOn: boolean }[] = [
  { id: "task.completed", label: "Task concluída", defaultOn: true },
  { id: "task.failed", label: "Task falhou", defaultOn: true },
  { id: "approval.pending", label: "Aprovação pendente", defaultOn: true },
  { id: "server.offline", label: "Servidor ficou offline", defaultOn: false },
];

interface Created {
  kind: Kind;
  name: string;
  value: string;
  webhookId?: string;
}

function CheckIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke" style={{ strokeWidth: 3 }}>
      <path d="M5 12l5 5 9-10" />
    </svg>
  );
}

function CopyField({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="secret-field">
      <code>{value}</code>
      <button
        type="button"
        onClick={() => {
          void navigator.clipboard?.writeText(value).then(() => setCopied(true));
        }}
      >
        {copied ? "Copiado" : "Copiar"}
      </button>
    </div>
  );
}

export function IntegrationModal({
  token, projectId, projectName, onClose, onCreated,
}: {
  token: string;
  projectId: string;
  projectName: string;
  onClose: () => void;
  onCreated: () => void;
}) {
  const [step, setStepState] = useState<Step>(1);
  const [leaf, setLeaf] = useState<"forward" | "backward">("forward");
  function setStep(next: Step) {
    setLeaf(next >= step ? "forward" : "backward");
    setStepState(next);
  }
  const [kind, setKind] = useState<Kind>("app");
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<Set<ApiKeyScope>>(new Set(SCOPES.filter((s) => s.defaultOn).map((s) => s.id)));
  const [expiresInDays, setExpiresInDays] = useState(90);
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState<Set<WebhookEvent>>(new Set(EVENTS.filter((e) => e.defaultOn).map((e) => e.id)));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<Created | null>(null);
  const [testSent, setTestSent] = useState(false);

  function toggle<T>(set: Set<T>, value: T, update: (s: Set<T>) => void) {
    const next = new Set(set);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    update(next);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      if (kind === "app") {
        const res = await createApiKey(token, projectId, { name: name.trim(), scopes: [...scopes], expiresInDays });
        setCreated({ kind, name: res.apiKey.name, value: res.rawKey });
      } else {
        const res = await createWebhook(token, projectId, { name: name.trim(), url: url.trim(), events: [...events] });
        setCreated({ kind, name: res.webhook.name, value: res.secret, webhookId: res.webhook.id });
      }
      setStep(3);
      onCreated();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Não foi possível cadastrar.");
    } finally {
      setBusy(false);
    }
  }

  async function sendTest() {
    if (!created?.webhookId) return;
    setError(null);
    try {
      await sendWebhookTest(token, projectId, created.webhookId);
      setTestSent(true);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao enviar o teste.");
    }
  }

  const canSubmit =
    !!name.trim() && (kind === "app" ? scopes.size > 0 : !!url.trim() && events.size > 0);

  return (
    <div className="modal-overlay" onClick={onClose}>
      <form className="wizard" role="dialog" aria-modal="true" aria-label="Nova integração"
        onClick={(e) => e.stopPropagation()} onSubmit={(e) => void submit(e)}>
        <div className="wizard-header">
          <div>
            <h2>Nova integração</h2>
            <span className="muted">no projeto {projectName}</span>
          </div>
          <div className="wizard-steps">
            <span className={step === 1 ? "active" : ""}>1 · Tipo</span>
            <span className={step === 2 ? "active" : ""}>2 · Configurar</span>
            <span className={step === 3 ? "active" : ""}>3 · Pronto</span>
            <button type="button" className="icon-button" aria-label="Fechar" onClick={onClose}>
              <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke" style={{ strokeWidth: 2.5 }}>
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
        </div>

        <div key={step} className={`wizard-body wizard-page ${leaf}`}>
          {step === 1 && (
            <>
              <strong className="wizard-question">O que você quer conectar?</strong>
              <div className="type-grid">
                <button type="button" className={`type-tile${kind === "app" ? " selected" : ""}`} onClick={() => setKind("app")}>
                  <svg width="28" height="28" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke accent">
                    <circle cx="8" cy="15" r="4" /><path d="M11 12l9-9M17 6l3 3M15 8l2 2" />
                  </svg>
                  <strong>App que chama o Prelo</strong>
                  <span>Gera uma chave de API para outro sistema criar e acompanhar tasks.</span>
                  <small>ex.: seu SaaS, um script, n8n</small>
                </button>
                <button type="button" className={`type-tile${kind === "webhook" ? " selected" : ""}`} onClick={() => setKind("webhook")}>
                  <svg width="28" height="28" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke accent">
                    <path d="M4 12h12M12 6l6 6-6 6" /><path d="M20 4v16" />
                  </svg>
                  <strong>Webhook de saída</strong>
                  <span>O Prelo avisa uma URL sua quando algo acontece no projeto.</span>
                  <small>ex.: task concluída, aprovação pendente</small>
                </button>
                <button type="button" className="type-tile" disabled>
                  <svg width="28" height="28" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke accent">
                    <path d="M14.7 6.3a4 4 0 0 0-5.4 5.4L3 18v3h3l6.3-6.3a4 4 0 0 0 5.4-5.4l-2.6 2.6-2.4-.6-.6-2.4z" />
                  </svg>
                  <strong>Serviço para os agentes</strong>
                  <span>Conecta GitHub, Slack, Notion ou HTTP como ferramenta dos agentes, com aprovação por risco.</span>
                  <small className="soon">Em breve</small>
                </button>
              </div>
            </>
          )}

          {step === 2 && kind === "app" && (
            <>
              <label>
                Nome da aplicação
                <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Backend do SaaS" autoFocus required />
              </label>
              <fieldset className="check-group">
                <legend>O que essa chave pode fazer</legend>
                {SCOPES.map((s) => (
                  <label key={s.id} className="check-row">
                    <input type="checkbox" checked={scopes.has(s.id)} onChange={() => toggle(scopes, s.id, setScopes)} />
                    <span>{s.label} <code>{s.id}</code></span>
                  </label>
                ))}
                <label className="check-row disabled">
                  <input type="checkbox" disabled />
                  <span>Decidir aprovações — nunca concedido a apps, só a pessoas</span>
                </label>
              </fieldset>
              <label>
                Validade da chave
                <select value={expiresInDays} onChange={(e) => setExpiresInDays(Number(e.target.value))}>
                  <option value={90}>90 dias</option>
                  <option value={30}>30 dias</option>
                  <option value={365}>1 ano</option>
                  <option value={0}>Sem expiração</option>
                </select>
              </label>
              <div className="wizard-note">
                A chave fica presa ao projeto <strong>{projectName}</strong>: tudo que ela criar aparece só aqui, sem
                precisar mandar <code>X-Project-Id</code>.
              </div>
            </>
          )}

          {step === 2 && kind === "webhook" && (
            <>
              <label>
                Nome
                <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Avisar no canal do time" autoFocus required />
              </label>
              <label>
                URL de destino
                <input className="mono" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://seu-servico.com/prelo/webhook" required />
              </label>
              <fieldset className="check-group">
                <legend>Eventos que disparam</legend>
                {EVENTS.map((ev) => (
                  <label key={ev.id} className="check-row">
                    <input type="checkbox" checked={events.has(ev.id)} onChange={() => toggle(events, ev.id, setEvents)} />
                    <span>{ev.label} <code>{ev.id}</code></span>
                  </label>
                ))}
              </fieldset>
              <div className="wizard-note">
                Cada envio vai assinado (HMAC-SHA256 no header <code>X-Prelo-Signature</code>) e é reenviado com espera
                crescente se a sua URL não responder 2xx.
              </div>
            </>
          )}

          {step === 3 && created?.kind === "app" && (
            <>
              <div className="wizard-success"><span className="success-dot"><CheckIcon /></span>Chave criada para {created.name}</div>
              <CopyField value={created.value} />
              <div className="wizard-warning">Ela só aparece agora. Guarde num cofre de segredos — depois só dá pra revogar e gerar outra.</div>
              <strong className="wizard-question">Teste rápido</strong>
              <pre className="code-block">{`curl -X POST ${BASE_URL}/api/v1/tasks \\
  -H "Authorization: Bearer ${created.value}" \\
  -H "Content-Type: application/json" \\
  -d '{"description": "Resumir os tickets abertos"}'`}</pre>
            </>
          )}

          {step === 3 && created?.kind === "webhook" && (
            <>
              <div className="wizard-success"><span className="success-dot"><CheckIcon /></span>Webhook {created.name} cadastrado</div>
              <strong className="wizard-question">Segredo de assinatura</strong>
              <CopyField value={created.value} />
              <div className="wizard-warning">Ele só aparece agora — use para validar o header <code>X-Prelo-Signature</code>.</div>
              <strong className="wizard-question">Exemplo do que sua URL vai receber</strong>
              <pre className="code-block">{`POST <sua URL>
X-Prelo-Event: task.completed
X-Prelo-Timestamp: 1790960000
X-Prelo-Signature: sha256=HMAC(segredo, timestamp + "." + corpo)

{
  "event": "task.completed",
  "projectId": "${projectId}",
  "occurredAt": "…",
  "data": { "taskId": "…", "status": "COMPLETED", "result": "…" }
}`}</pre>
              <div>
                <button type="button" onClick={() => void sendTest()} disabled={testSent}>
                  {testSent ? "Evento de teste enfileirado" : "Enviar evento de teste"}
                </button>
              </div>
            </>
          )}

          {error && <p className="error" role="alert">{error}</p>}
        </div>

        <div className="wizard-footer">
          {step === 1 && <button type="button" onClick={onClose}>Cancelar</button>}
          {step === 2 && <button type="button" onClick={() => { setStep(1); setError(null); }}>Voltar</button>}
          {step === 3 && <span />}
          {step === 1 && <button type="button" className="primary" onClick={() => setStep(2)}>Continuar</button>}
          {step === 2 && (
            <button type="submit" className="primary" disabled={!canSubmit || busy}>
              {busy ? "Cadastrando…" : kind === "app" ? "Gerar chave" : "Cadastrar webhook"}
            </button>
          )}
          {step === 3 && <button type="button" className="primary" onClick={onClose}>Concluir</button>}
        </div>
      </form>
    </div>
  );
}
