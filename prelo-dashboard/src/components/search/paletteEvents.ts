export const OPEN_EVENT = "prelo:open-search";

/** Opens the Ctrl+K palette from a button (the header's "Buscar"). */
export function openCommandPalette() {
  window.dispatchEvent(new Event(OPEN_EVENT));
}
