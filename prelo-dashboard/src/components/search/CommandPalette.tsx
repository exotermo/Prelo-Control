import { useEffect, useState } from "react";
import { SearchBox } from "./SearchBox";
import { OPEN_EVENT } from "./paletteEvents";

/** Fase C1: Ctrl+K / ⌘K from any screen opens search over the current page. */
export function CommandPalette({ token }: { token: string }) {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setOpen((v) => !v);
      } else if (event.key === "Escape") {
        setOpen(false);
      }
    };
    const onOpen = () => setOpen(true);
    window.addEventListener("keydown", onKey);
    window.addEventListener(OPEN_EVENT, onOpen);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener(OPEN_EVENT, onOpen);
    };
  }, []);

  if (!open) return null;
  return (
    <div className="modal-overlay palette-overlay" onClick={() => setOpen(false)}>
      <div className="palette" role="dialog" aria-label="Buscar" onClick={(e) => e.stopPropagation()}>
        <SearchBox token={token} autoFocus onOpened={() => setOpen(false)} />
        <p className="palette-foot muted">↑ ↓ para escolher · Enter para abrir · Esc para fechar</p>
      </div>
    </div>
  );
}
