import { useEffect, useRef, useState, useCallback } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

interface DockerEvent {
  id: number;
  type: string;
  data: Record<string, unknown>;
  ts: string;
}

let nextEventId = 0;

export function EventStream() {
  const [events, setEvents] = useState<DockerEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [paused, setPaused] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // Fetch recent Docker events on mount to pre-populate
  useEffect(() => {
    apiFetch<{ events: { type: string; action: string; actor: string; id: string; time: string }[] }>(
      "/api/v1/docker/events?since=5m"
    ).then(({ data }) => {
      if (data?.events?.length) {
        setEvents(data.events.map((e) => ({
          id: nextEventId++,
          type: `${e.type}.${e.action}`,
          data: { actor: e.actor, id: e.id },
          ts: e.time,
        })));
      }
    });
  }, []);

  useEffect(() => {
    let es: EventSource | null = null;
    let retryCount = 0;
    let retryTimer: ReturnType<typeof setTimeout>;
    let unmounted = false;

    function connect() {
      if (unmounted) return;
      try {
        es = new EventSource("/api/v1/sse/events", { withCredentials: true });
      } catch {
        setConnected(false);
        return;
      }
      setConnected(true);

      const handler = (e: MessageEvent) => {
        retryCount = 0; // Reset on successful data
        try {
          const data = JSON.parse(e.data);
          const evt: DockerEvent = {
            id: nextEventId++,
            type: e.type || "event",
            data,
            ts: new Date().toISOString(),
          };
          setEvents((prev) => {
            const next = [...prev, evt];
            return next.length > 200 ? next.slice(-200) : next;
          });
        } catch {}
      };

      for (const type of [
        "stack.deployed", "stack.stopped", "stack.updated", "stack.deleted", "stack.error",
        "container.state", "container.health",
      ]) {
        es.addEventListener(type, handler);
      }

      es.onerror = () => {
        setConnected(false);
        es?.close();
        if (unmounted) return;
        const delay = Math.min(1000 * Math.pow(2, retryCount), 30000);
        retryCount++;
        retryTimer = setTimeout(connect, delay);
      };
    }

    connect();
    return () => {
      unmounted = true;
      clearTimeout(retryTimer);
      es?.close();
      setConnected(false);
    };
  }, []);

  useEffect(() => {
    if (!paused && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [events, paused]);

  const handleScroll = useCallback(() => {
    if (!containerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current;
    setPaused(scrollHeight - scrollTop - clientHeight > 50);
  }, []);

  function getEventColor(type: string): string {
    if (type.includes("start") || type.includes("deploy")) return "bg-cp-green/20 text-cp-green border-cp-green/30";
    if (type.includes("stop") || type.includes("die") || type.includes("kill") || type.includes("delete") || type.includes("destroy")) return "bg-cp-red/20 text-cp-red border-cp-red/30";
    if (type.includes("create") || type.includes("update") || type.includes("pull")) return "bg-cp-blue/20 text-cp-blue border-cp-blue/30";
    if (type.includes("error") || type.includes("fail")) return "bg-cp-red/20 text-cp-red border-cp-red/30";
    if (type.includes("health")) return "bg-cp-purple/20 text-cp-purple border-cp-purple/30";
    if (type.includes("connect") || type.includes("network")) return "bg-cp-cyan/20 text-cp-cyan border-cp-cyan/30";
    return "bg-cp-600/20 text-muted-foreground border-cp-600/30";
  }

  return (
    <ErrorBoundary>
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-xs">
        <span className={`h-2 w-2 rounded-full ${connected ? "bg-cp-green" : "bg-cp-red"}`} />
        <span className="text-muted-foreground">{connected ? "Streaming" : "Disconnected"}</span>
        <span className="text-muted-foreground font-data">{events.length} events</span>
        {paused && (
          <span className="text-cp-peach">Scroll paused</span>
        )}
        <div className="ml-auto flex gap-1">
          <Button size="xs" variant="ghost" onClick={() => setEvents([])}>Clear</Button>
          {paused && (
            <Button size="xs" variant="ghost" onClick={() => {
              setPaused(false);
              bottomRef.current?.scrollIntoView({ behavior: "smooth" });
            }}>
              Resume
            </Button>
          )}
        </div>
      </div>
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="rounded-lg border border-border bg-cp-950 overflow-y-auto text-xs"
        style={{ height: "300px" }}
        data-testid="event-stream"
      >
        {events.length === 0 ? (
          <div className="flex items-center justify-center h-full text-muted-foreground">
            {connected ? "No recent events. Deploy or stop a stack to see events here." : "Not connected"}
          </div>
        ) : (
          <div className="p-3 space-y-1">
            {events.map((evt) => (
              <div key={evt.id} className="flex items-start gap-2 py-0.5">
                <span className="text-muted-foreground font-data select-none shrink-0 tabular-nums whitespace-nowrap">
                  {new Date(evt.ts).toISOString().slice(11, 19)}
                </span>
                <Badge className={`shrink-0 ${getEventColor(evt.type)}`}>
                  {evt.type}
                </Badge>
                <span className="text-muted-foreground font-data break-all text-[10px] min-w-0">
                  {JSON.stringify(evt.data)}
                </span>
              </div>
            ))}
            <div ref={bottomRef} />
          </div>
        )}
      </div>
    </div>
    </ErrorBoundary>
  );
}
