import { useState, type FormEvent } from "react";
import {
  ApiError,
  saveModelConnection,
  testModelConnection,
  type ModelConnection,
  type ModelProvider,
  type ModelScope,
} from "../../api/client";
import { PROVIDERS } from "./providers";

type Step = 1 | 2 | 3;

export function ConnectModelModal({
  token, scope, title, current, onClose, onSaved,
}: {
  token: string;
  scope: ModelScope;
  title: string;
  current: ModelConnection | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [step, setStepState] = useState<Step>(1);
  const [leaf, setLeaf] = useState<"forward" | "backward">("forward");
  const [provider, setProvider] = useState<ModelProvider>(current?.provider ?? "anthropic");
  const [baseUrl, setBaseUrl] = useState(current?.provider === "openai_compatible" ? current.baseUrl ?? "" : "");
  const [apiKey, setApiKey] = useState("");
  const [models, setModels] = useState<string[]>([]);
  const [model, setModel] = useState("");
  const [manualModel, setManualModel] = useState("");
  const [latency, setLatency] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // A key already in the vault for this same provider can be kept: the gateway re-tests with it.
  const canKeepKey = !!current?.configured && current.hasKey && current.provider === provider;

  function setStep(next: Step) {
    setLeaf(next >= step ? "forward" : "backward");
    setStepState(next);
    setError(null);
  }

  async function runTest(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const keepKey = canKeepKey && !apiKey.trim();
      const result = keepKey
        ? null
        : await testModelConnection(token, scope, {
          provider, baseUrl: provider === "openai_compatible" ? baseUrl.trim() : undefined, apiKey: apiKey.trim() || undefined,
        });
      if (result && !result.ok) {
        setError(result.error ?? "Não foi possível conectar.");
        return;
      }
      const list = result?.models ?? [];
      setModels(list);
      setLatency(result?.latencyMs ?? null);
      setModel(list.includes(current?.model ?? "") ? current!.model! : list[0] ?? "");
      setStep(3);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Não foi possível testar a chave.");
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    const chosen = models.length > 0 ? model : manualModel.trim();
    if (!chosen) return;
    setBusy(true);
    setError(null);
    try {
      await saveModelConnection(token, scope, {
        provider, model: chosen,
        baseUrl: provider === "openai_compatible" ? baseUrl.trim() : undefined,
        apiKey: apiKey.trim() || undefined,
      });
      setApiKey("");
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Não foi possível salvar.");
    } finally {
      setBusy(false);
    }
  }

  const meta = PROVIDERS[provider];
  const keyRequired = provider !== "openai_compatible" && !canKeepKey;

  return (
    <div className="modal-overlay" onClick={onClose}>
      <form className="wizard" role="dialog" aria-modal="true" aria-label="Conectar modelo"
        onClick={(e) => e.stopPropagation()} onSubmit={(e) => void runTest(e)}>
        <div className="wizard-header">
          <div>
            <h2>Conectar modelo</h2>
            <span className="muted">{title}</span>
          </div>
          <div className="wizard-steps">
            <span className={step === 1 ? "active" : ""}>1 · Provedor</span>
            <span className={step === 2 ? "active" : ""}>2 · Chave</span>
            <span className={step === 3 ? "active" : ""}>3 · Modelo</span>
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
              <strong className="wizard-question">Qual provedor?</strong>
              <div className="type-grid">
                {(Object.keys(PROVIDERS) as ModelProvider[]).map((id) => (
                  <button key={id} type="button" className={`type-tile${provider === id ? " selected" : ""}`} onClick={() => setProvider(id)}>
                    <span className="provider-mono">{PROVIDERS[id].mono}</span>
                    <strong>{PROVIDERS[id].label}</strong>
                    <span>{PROVIDERS[id].description}</span>
                  </button>
                ))}
              </div>
            </>
          )}

          {step === 2 && (
            <>
              {provider === "openai_compatible" && (
                <label>
                  Endereço do serviço (base URL)
                  <input className="mono" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)}
                    placeholder="https://openrouter.ai/api/v1" required autoFocus />
                </label>
              )}
              <label>
                Chave de API{canKeepKey ? " (deixe vazio para manter a atual)" : provider === "openai_compatible" ? " (opcional em serviços locais)" : ""}
                <input className="mono" type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)}
                  placeholder={canKeepKey ? `•••• ${current?.keyLast4 ?? ""} guardada no cofre` : meta.keyHint}
                  required={keyRequired} autoFocus={provider !== "openai_compatible"} />
              </label>
              <div className="wizard-note">
                A chave vai direto para o cofre do <code>llm-gateway</code> e nunca mais aparece inteira. Antes de salvar, o Prelo testa a
                chave e busca a lista de modelos dela.
              </div>
            </>
          )}

          {step === 3 && (
            <>
              <div className="wizard-success">
                <span className="success-dot">
                  <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke" style={{ strokeWidth: 3 }}><path d="M5 12l5 5 9-10" /></svg>
                </span>
                {latency !== null ? `Chave válida — ${meta.label} respondeu em ${latency} ms` : "Usando a chave já guardada no cofre"}
              </div>
              {models.length > 0 ? (
                <div className="model-list">
                  {models.map((m) => (
                    <button key={m} type="button" className={`type-tile model-option${model === m ? " selected" : ""}`} onClick={() => setModel(m)}>
                      <strong className="mono">{m}</strong>
                    </button>
                  ))}
                </div>
              ) : (
                <label>
                  Modelo
                  <input className="mono" value={manualModel} onChange={(e) => setManualModel(e.target.value)}
                    placeholder={current?.model ?? "nome do modelo"} required autoFocus />
                </label>
              )}
            </>
          )}

          {error && <p className="error" role="alert">{error}</p>}
        </div>

        <div className="wizard-footer">
          {step === 1 ? <button type="button" onClick={onClose}>Cancelar</button> : <button type="button" onClick={() => setStep((step - 1) as Step)}>Voltar</button>}
          {step === 1 && <button type="button" className="primary" onClick={() => setStep(2)}>Continuar</button>}
          {step === 2 && <button type="submit" className="primary" disabled={busy}>{busy ? "Testando…" : "Testar chave e listar modelos"}</button>}
          {step === 3 && (
            <button type="button" className="primary" disabled={busy || !(models.length > 0 ? model : manualModel.trim())} onClick={() => void save()}>
              {busy ? "Testando e guardando…" : "Salvar no cofre"}
            </button>
          )}
        </div>
      </form>
    </div>
  );
}
