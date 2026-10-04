import { useCallback, useEffect, useRef, useState, type CSSProperties, type DragEvent } from "react";
import {
  ApiError,
  decodeDashboardToken,
  deleteProjectFile,
  fetchProjectFile,
  listProjectFiles,
  uploadProjectFile,
  type ProjectFile,
} from "../../api/client";
import { FileViewer } from "./FileViewer";
import { formatBytes, KIND_LABEL } from "./format";

interface Upload {
  key: string;
  name: string;
  progress: number;
  error: string | null;
  abort: () => void;
}

type KindFilter = "all" | ProjectFile["kind"];
const MAX_BYTES = 100 * 1024 * 1024;

export function FilesPanel({ token, projectId }: { token: string; projectId: string }) {
  const [files, setFiles] = useState<ProjectFile[]>([]);
  const [totalBytes, setTotalBytes] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [uploads, setUploads] = useState<Upload[]>([]);
  const [dragging, setDragging] = useState(false);
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState<KindFilter>("all");
  const [viewing, setViewing] = useState<ProjectFile | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const identity = decodeDashboardToken(token);
  const canManage = identity?.scopes.includes("projects:manage") ?? false;

  const load = useCallback(async () => {
    try {
      const list = await listProjectFiles(token, projectId);
      setFiles(list.files);
      setTotalBytes(list.totalBytes);
      setError(null);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao carregar arquivos.");
    } finally {
      setLoading(false);
    }
  }, [token, projectId]);

  useEffect(() => {
    void load();
  }, [load]);

  function patchUpload(key: string, patch: Partial<Upload>) {
    setUploads((list) => list.map((u) => (u.key === key ? { ...u, ...patch } : u)));
  }

  function start(selected: FileList | File[]) {
    for (const file of Array.from(selected)) {
      const key = `${file.name}-${file.size}-${Math.random()}`;
      if (file.size > MAX_BYTES) {
        setUploads((list) => [...list, { key, name: file.name, progress: 0, error: "passa do limite de 100 MB", abort: () => {} }]);
        continue;
      }
      const { promise, abort } = uploadProjectFile(token, projectId, file, (p) => patchUpload(key, { progress: p }));
      setUploads((list) => [...list, { key, name: file.name, progress: 0, error: null, abort }]);
      promise.then(() => {
        setUploads((list) => list.filter((u) => u.key !== key));
        void load();
      }).catch((err) => patchUpload(key, { error: err instanceof ApiError ? err.message : "falha no envio" }));
    }
  }

  function onDrop(event: DragEvent) {
    event.preventDefault();
    setDragging(false);
    if (event.dataTransfer.files.length) start(event.dataTransfer.files);
  }

  async function remove(file: ProjectFile) {
    if (!window.confirm(`Remover "${file.name}"? O arquivo cifrado é apagado do disco.`)) return;
    try {
      await deleteProjectFile(token, projectId, file.id);
      await load();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Falha ao remover.");
    }
  }

  const visible = files.filter((f) => (kind === "all" || f.kind === kind) && f.name.toLowerCase().includes(query.trim().toLowerCase()));

  return (
    <div className="files-panel">
      <div
        className={`dropzone${dragging ? " dragging" : ""}`}
        onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
      >
        <svg width="34" height="34" viewBox="0 0 24 24" aria-hidden="true" className="icon-stroke accent"><path d="M12 16V4M7 9l5-5 5 5M4 16v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3" /></svg>
        <strong>Solte os arquivos aqui</strong>
        <span className="muted">PDF, imagens e texto abrem aqui mesmo; o resto fica para baixar. Até 100 MB cada, cifrados no disco.</span>
        <button type="button" className="primary" onClick={() => inputRef.current?.click()}>Escolher arquivos</button>
        <input ref={inputRef} type="file" multiple hidden onChange={(e) => { if (e.target.files) start(e.target.files); e.target.value = ""; }} />
      </div>

      {uploads.length > 0 && (
        <ul className="upload-list">
          {uploads.map((u) => (
            <li key={u.key} className={u.error ? "failed" : ""}>
              <span className="clipping-mono upload-name">{u.name}</span>
              {u.error ? (
                <>
                  <span className="error">{u.error}</span>
                  <button type="button" onClick={() => setUploads((list) => list.filter((x) => x.key !== u.key))}>Ok</button>
                </>
              ) : (
                <>
                  <span className="upload-bar"><span style={{ width: `${Math.round(u.progress * 100)}%` }} /></span>
                  <span className="clipping-mono">{u.progress >= 1 ? "cifrando…" : `${Math.round(u.progress * 100)}%`}</span>
                  <button type="button" onClick={u.abort}>Cancelar</button>
                </>
              )}
            </li>
          ))}
        </ul>
      )}

      <div className="files-toolbar">
        <input className="files-search" placeholder="Buscar pelo nome" value={query} onChange={(e) => setQuery(e.target.value)} aria-label="Buscar arquivos" />
        <div className="kind-filter" role="group" aria-label="Filtrar por tipo">
          {(["all", "pdf", "image", "text", "other"] as KindFilter[]).map((k) => (
            <button key={k} type="button" className={kind === k ? "active" : ""} onClick={() => setKind(k)}>
              {k === "all" ? "Todos" : KIND_LABEL[k]}
            </button>
          ))}
        </div>
        <span className="clipping-mono files-total">{files.length} arquivo{files.length === 1 ? "" : "s"} · {formatBytes(totalBytes)}</span>
      </div>

      {error && <p className="error" role="alert">{error}</p>}
      {loading ? (
        <p className="muted">Carregando arquivos…</p>
      ) : visible.length === 0 ? (
        <div className="empty-state">{files.length === 0 ? "Nenhum arquivo neste projeto ainda." : "Nada encontrado com esse filtro."}</div>
      ) : (
        <div className="clipping-grid">
          {visible.map((file, index) => (
            <article key={file.id} className="clipping file-clipping" style={{ "--i": index } as CSSProperties}>
              <span className="clipping-kicker">Arquivo do projeto</span>
              <h3 className="clipping-headline file-name" title={file.name}>{file.name}</h3>
              <span className="clipping-dateline">Enviado em {new Date(file.createdAt).toLocaleDateString("pt-BR", { day: "2-digit", month: "short", year: "numeric" })}</span>
              <div className="clipping-body">
                <p className="clipping-mono">{formatBytes(file.sizeBytes)} · {file.inline ? "abre aqui" : "só para baixar"}</p>
              </div>
              <div className="clipping-footer">
                <span className={`clipping-stamp kind-${file.kind}`}>{KIND_LABEL[file.kind]}</span>
                <div className="file-actions">
                  <button type="button" onClick={() => setViewing(file)}>{file.inline ? "Abrir" : "Baixar"}</button>
                  {(canManage || file.uploadedBy === identity?.sub) && <button type="button" onClick={() => void remove(file)}>Remover</button>}
                </div>
              </div>
            </article>
          ))}
        </div>
      )}

      {viewing && viewing.inline && <FileViewer token={token} projectId={projectId} file={viewing} onClose={() => setViewing(null)} />}
      {viewing && !viewing.inline && <DownloadOnly token={token} projectId={projectId} file={viewing} onDone={() => setViewing(null)} />}
    </div>
  );
}

function DownloadOnly({ token, projectId, file, onDone }: { token: string; projectId: string; file: ProjectFile; onDone: () => void }) {
  useEffect(() => {
    void fetchProjectFile(token, projectId, file.id, true).then((blob) => {
      const link = document.createElement("a");
      link.href = URL.createObjectURL(blob);
      link.download = file.name;
      link.click();
      setTimeout(() => URL.revokeObjectURL(link.href), 10_000);
    }).finally(onDone);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return null;
}
