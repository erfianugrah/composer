import { useEffect, useRef, useState, useCallback } from "react";

/**
 * Shared hook for SSE connections with auto-reconnect on error.
 * Exponential backoff: 1s, 2s, 4s, 8s, max 30s.
 */
export function useEventSource(
  url: string,
  eventTypes: string[],
  onEvent: (type: string, data: unknown) => void,
  enabled = true,
) {
  const [connected, setConnected] = useState(false);
  const esRef = useRef<EventSource | null>(null);
  const retryRef = useRef(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const connect = useCallback(() => {
    if (!enabled || !url) return;

    try {
      const es = new EventSource(url, { withCredentials: true });
      esRef.current = es;

      es.onopen = () => {
        setConnected(true);
        retryRef.current = 0; // reset backoff on successful connection
      };

      for (const type of eventTypes) {
        es.addEventListener(type, (e: MessageEvent) => {
          try {
            onEvent(type, JSON.parse(e.data));
          } catch { /* skip malformed */ }
        });
      }

      es.onerror = () => {
        setConnected(false);
        es.close();
        esRef.current = null;

        // Exponential backoff reconnect
        const delay = Math.min(1000 * Math.pow(2, retryRef.current), 30000);
        retryRef.current++;
        timerRef.current = setTimeout(connect, delay);
      };
    } catch {
      setConnected(false);
    }
  }, [url, eventTypes, onEvent, enabled]);

  useEffect(() => {
    connect();
    return () => {
      esRef.current?.close();
      esRef.current = null;
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [connect]);

  return { connected };
}
