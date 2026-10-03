import { useEffect, useState } from "react";
import { ApiError, fetchProjectFile, type ProjectFile } from "../../api/client";
import { formatBytes } from "./format";

const MAX_TEXT_PREVIEW = 1 << 20;

/** The "reading table": shows one file decrypted on demand, from a blob: URL kept in memory only. */
export function FileViewer({ token, projectId, file, onClose }: { token: string; projectId: string; file: ProjectFile; onClose: () => void }) {
  const [url, setUrl] = useState<string | null>(null);
  const [text, setText] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [zoomed, setZoomed] = useState(false);

  useEffect(() => {
    let objectUrl: string | null = null;
    let cancelled = false;
    void fetchProjectFile(token, projectId, file.id).then(async (blob) => {
      if (cancelled) return;
      if (file.kind === "text") {
        const content = await blob.slice(0, MAX_TEXT_PREVIEW).text();
        if (!cancelled) setText(blob.size > MAX_TEXT_PREVIEW ? `${content}\n\n… (prévia limitada a 1 MB — baixe para ver tudo)` : content);
      } else {
        objectUrl = URL.createObjectURL(blob);
        setUrl(objectUrl);
      }
    }).catch((err) => {
      if (!cancelled) setError(err instanceof ApiError ? err.message : "Não foi possível abrir o arquivo.");
    });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [token, projectId, file.id, file.kind]);

  async function download() {
    try {
      const blob = await fetchProjectFile(token, projectId, file.id, true);
      const link = document.createElement("a");
      link.href = URL.createObjectURL(blob);
      link.download = file.name;
      link.click();
      setTimeout(() => URL.revokeObjectURL(link.href), 10_000);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao baixar.");
    }
  }

  return (
    <div className="modal-overlay reading-table" onClick={onClose}>
      <div className="reader" role="dialog" aria-modal="true" aria-label={file.name} onClick={(e) => e.stopPropagation()}>
        <div className="reader-header">
          <div className="reader-titles">
            <span className="clipping-kicker">{file.kind === "pdf" ? "Documento" : file.kind === "image" ? "Imagem" : "Texto"}</span>
            <strong className="reader-name">{file.name}</strong>
            <span className="clipping-mono">{formatBytes(file.sizeBytes)} · {new Date(file.createdAt).toLocaleString("pt-BR")}</span>
          </div>
          <div className="reader-actions">
            <button type="button" onClick={() => void download()}>Baixar</button>
            <button type="button" className="icon-button" aria-label="Fechar" onClick={onClose}>
              <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke" style={{ strokeWidth: 2.5 }}><path d="M6 6l12 12M18 6L6 18" /></svg>
            </button>
          </div>
        </div>
        <div className="reader-body">
          {error && <p className="error" role="alert">{error}</p>}
          {!error && !url && text === null && <p className="muted">Abrindo e decifrando…</p>}
          {url && file.kind === "pdf" && <iframe className="reader-pdf" src={url} title={file.name} />}
          {url && file.kind === "image" && (
            <button type="button" className={`reader-image${zoomed ? " zoomed" : ""}`} onClick={() => setZoomed((z) => !z)} aria-label={zoomed ? "Diminuir" : "Ampliar"}>
              <img src={url} alt={file.name} />
            </button>
          )}
          {text !== null && <pre className="reader-text">{text}</pre>}
        </div>
      </div>
    </div>
  );
}
