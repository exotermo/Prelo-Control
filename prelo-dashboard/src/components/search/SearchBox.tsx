import { useEffect, useMemo, useRef, useState } from "react";
import { ApiError, search, type SearchHit, type SearchResults } from "../../api/client";
import { useOpenHit } from "./useOpenHit";

const GROUPS: { key: keyof SearchResults; label: string }[] = [
  { key: "clients", label: "Clientes" },
  { key: "projects", label: "Projetos" },
  { key: "tasks", label: "Tasks e conversas" },
  { key: "files", label: "Arquivos" },
];

const KIND_MARK: Record<SearchHit["kind"], string> = { CLIENT: "CLI", PROJECT: "PRJ", TASK: "TSK", FILE: "ARQ" };

/**
 * Fase C1 search: types → debounced query → results grouped by kind, keyboard navigable
 * (↑/↓, Enter). Used inline at the top of /projetos and inside the Ctrl+K palette.
 */
export function SearchBox({ token, autoFocus, onOpened, placeholder }: {
  token: string;
  autoFocus?: boolean;
  onOpened?: () => void;
  placeholder?: string;
}) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResults | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [active, setActive] = useState(0);
  const openHit = useOpenHit(token);
  const listRef = useRef<HTMLDivElement>(null);

  const trimmed = query.trim();
  useEffect(() => {
    if (trimmed.length < 2) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      setLoading(true);
      search(token, trimmed, controller.signal)
        .then((r) => { setResults(r); setActive(0); setError(null); })
        .catch((err) => {
          if (controller.signal.aborted) return;
          setError(err instanceof ApiError ? err.message : "Falha na busca.");
        })
        .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    }, 200);
    return () => { controller.abort(); clearTimeout(timer); };
  }, [token, trimmed]);

  // Results of an older, longer query are hidden (not cleared) once the input is too short.
  const shown = trimmed.length >= 2 ? results : null;
  const flat = useMemo(() => (shown ? GROUPS.flatMap((g) => shown[g.key]) : []), [shown]);
  // Index of each group's first hit in `flat`, for keyboard navigation.
  const offsets = useMemo(
    () => GROUPS.map((_, g) => GROUPS.slice(0, g).reduce((sum, prev) => sum + (shown ? shown[prev.key].length : 0), 0)),
    [shown],
  );

  function open(hit: SearchHit) {
    openHit(hit);
    setQuery("");
    onOpened?.();
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (!flat.length) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const next = (active + (event.key === "ArrowDown" ? 1 : flat.length - 1)) % flat.length;
      setActive(next);
      listRef.current?.querySelector(`[data-index="${next}"]`)?.scrollIntoView({ block: "nearest" });
    } else if (event.key === "Enter") {
      event.preventDefault();
      open(flat[active]);
    }
  }

  return (
    <div className="search-box">
      <div className="search-input-row">
        <svg className="search-icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
          <circle cx="10.5" cy="10.5" r="6.5" fill="none" stroke="currentColor" strokeWidth="2.2" />
          <path d="M15.5 15.5 21 21" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />
        </svg>
        <input
          className="search-input"
          value={query}
          autoFocus={autoFocus}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
          placeholder={placeholder ?? "Buscar clientes, projetos, conversas, arquivos…"}
          aria-label="Buscar"
          role="combobox"
          aria-expanded={!!shown}
          aria-controls="search-results"
          aria-activedescendant={flat.length ? `search-hit-${active}` : undefined}
        />
        {loading && <span className="muted search-hint">buscando…</span>}
        {!loading && !query && <kbd className="search-kbd">Ctrl K</kbd>}
      </div>
      {error && shown && <p className="error">{error}</p>}
      {shown && (
        <div className="search-results" id="search-results" role="listbox" ref={listRef}>
          {flat.length === 0 && <p className="muted search-empty">Nada encontrado para “{trimmed}”.</p>}
          {GROUPS.map((group, g) => shown[group.key].length > 0 && (
            <div key={group.key} className="search-group">
              <div className="search-group-label">{group.label}</div>
              {shown[group.key].map((hit, h) => {
                const i = offsets[g] + h;
                return (
                  <button
                    key={`${hit.kind}-${hit.id}`}
                    id={`search-hit-${i}`}
                    data-index={i}
                    role="option"
                    aria-selected={i === active}
                    className={i === active ? "search-hit active" : "search-hit"}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => open(hit)}
                  >
                    <span className="search-hit-mark">{KIND_MARK[hit.kind]}</span>
                    <span className="search-hit-text">
                      <span className="search-hit-title">{hit.title}</span>
                      {hit.subtitle && <span className="search-hit-sub">{hit.subtitle}</span>}
                    </span>
                    {hit.status && <span className={`badge badge-${hit.status.toLowerCase()}`}>{hit.status.toLowerCase()}</span>}
                  </button>
                );
              })}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
