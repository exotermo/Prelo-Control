import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { BASE_URL } from "../api/client";
import { useAuth } from "../auth/AuthContext";

// PR-4 (contratos G4): one Server-Sent Events connection per tab. Uses fetch streaming (not
// EventSource) so the token travels in the Authorization header, never in the URL. Events only say
// what changed; screens re-fetch through the normal API.
export interface LiveEvent {
  kind: "task" | "execution" | "approval" | "action" | "resync" | "ready" | "session_ended" | string;
  id?: string;
  projectId?: string | null;
  status?: string;
  taskId?: string;
  parentId?: string;
  actionRequestId?: string;
}

type Listener = (event: LiveEvent) => void;

interface LiveEventsState {
  connected: boolean;
  subscribe: (listener: Listener) => () => void;
}

const LiveEventsContext = createContext<LiveEventsState | null>(null);

export function LiveEventsProvider({ children }: { children: ReactNode }) {
  const { token } = useAuth();
  const [connected, setConnected] = useState(false);
  const listeners = useRef(new Set<Listener>());

  useEffect(() => {
    if (!token) return;
    const controller = new AbortController();
    let attempt = 0;
    let stopped = false;

    const emit = (event: LiveEvent) => listeners.current.forEach((l) => l(event));

    async function run() {
      while (!stopped) {
        try {
          const response = await fetch(`${BASE_URL}/api/v1/events/stream`, {
            headers: { Authorization: `Bearer ${token}`, Accept: "text/event-stream" },
            signal: controller.signal,
          });
          if (response.status === 401 || response.status === 403) return; // a new token will reconnect
          if (!response.ok || !response.body) throw new Error(`stream ${response.status}`);
          attempt = 0;
          setConnected(true);
          emit({ kind: "resync" }); // whatever happened while disconnected
          const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
          let buffer = "";
          for (;;) {
            const { value, done } = await reader.read();
            if (done) break;
            buffer += value;
            let cut;
            while ((cut = buffer.indexOf("\n\n")) >= 0) {
              const block = buffer.slice(0, cut);
              buffer = buffer.slice(cut + 2);
              const data = block.split("\n").filter((l) => l.startsWith("data:")).map((l) => l.slice(5).trim()).join("\n");
              const kind = block.split("\n").find((l) => l.startsWith("event:"))?.slice(6).trim();
              if (!kind || kind === "ready") continue;
              try {
                emit({ ...(data ? JSON.parse(data) : {}), kind });
              } catch {
                /* ignore malformed */
              }
            }
          }
        } catch {
          if (controller.signal.aborted) return;
        }
        setConnected(false);
        attempt += 1;
        await new Promise((r) => setTimeout(r, Math.min(30000, 1000 * 2 ** Math.min(attempt, 5))));
      }
    }
    void run();
    return () => {
      stopped = true;
      controller.abort();
      setConnected(false);
    };
  }, [token]);

  const value = useMemo<LiveEventsState>(() => ({
    connected,
    subscribe: (listener) => {
      listeners.current.add(listener);
      return () => listeners.current.delete(listener);
    },
  }), [connected]);

  return <LiveEventsContext.Provider value={value}>{children}</LiveEventsContext.Provider>;
}

/**
 * Re-run `onChange` when a matching event arrives (and on resync). Returns the polling interval to
 * use: long while the stream is live, short as a fallback when it is not.
 */
export function useLiveRefresh(kinds: string[], onChange: () => void, match?: (e: LiveEvent) => boolean, intervals = { live: 60000, fallback: 5000 }): number {
  const ctx = useContext(LiveEventsContext);
  const handler = useRef(onChange);
  const matcher = useRef(match);
  useEffect(() => {
    handler.current = onChange;
    matcher.current = match;
  });
  const key = kinds.join(",");
  useEffect(() => {
    if (!ctx) return;
    const wanted = new Set(key.split(","));
    let timer: ReturnType<typeof setTimeout> | null = null;
    return ctx.subscribe((event) => {
      if (event.kind !== "resync" && (!wanted.has(event.kind) || (matcher.current && !matcher.current(event)))) return;
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => handler.current(), 150); // coalesce bursts (a loop turn fires several)
    });
  }, [ctx, key]);
  return ctx?.connected ? intervals.live : intervals.fallback;
}
