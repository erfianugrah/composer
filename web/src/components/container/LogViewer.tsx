import { useEffect, useRef, useState, useMemo } from "react";
import { Button } from "@/components/ui/button";
import { highlightLog } from "@/lib/log-highlight";

interface LogLine {
  stream: string;
  message: string;
  ts: string;
  containerId?: string;
}

interface Props {
  /** Single container log stream */
  containerId?: string;
  /** Stack-level aggregated logs (all containers) */
  stackName?: string;
  /** Number of tail lines */
  tail?: string;
  /** Max lines to keep in buffer */
  maxLines?: number;
}

export function LogViewer({ containerId, stackName, tail = "100", maxLines = 1000 }: Props) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [connected, setConnected] = useState(false);
  const [paused, setPaused] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    let url: string;
    if (containerId) {
      url = `/api/v1/sse/containers/${containerId}/logs?tail=${tail}`;
    } else if (stackName) {
      url = `/api/v1/sse/stacks/${stackName}/logs?tail=${tail}`;
    } else {
      return;
    }

    const es = new EventSource(url, { withCredentials: true });
    esRef.current = es;
    setConnected(true);

    es.addEventListener("log", (e) => {
      try {
        const data = JSON.parse(e.data);
        const line: LogLine = {
          stream: data.stream || "stdout",
          message: data.message || "",
          ts: data.ts || new Date().toISOString(),
          containerId: data.container_id,
        };
        setLines((prev) => {
          const next = [...prev, line];
          return next.length > maxLines ? next.slice(-maxLines) : next;
        });
      } catch {
        // Skip malformed events
      }
    });

    es.onerror = () => {
      setConnected(false);
      es.close();
    };

    return () => {
      es.close();
      setConnected(false);
    };
  }, [containerId, stackName, tail]);

  // Auto-scroll to bottom unless paused
  useEffect(() => {
    if (!paused && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [lines, paused]);

  // Detect if user scrolled up (pause auto-scroll)
  function handleScroll() {
    if (!containerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current;
    const atBottom = scrollHeight - scrollTop - clientHeight < 50;
    setPaused(!atBottom);
  }

  function clearLogs() {
    setLines([]);
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-xs">
        <span className={`h-2 w-2 rounded-full ${connected ? "bg-cp-green" : "bg-cp-red"}`} />
        <span className="text-muted-foreground">{connected ? "Streaming" : "Disconnected"}</span>
        <span className="text-muted-foreground font-data">{lines.length} lines</span>
        {paused && (
          <span className="text-cp-peach">Scroll paused</span>
        )}
        <div className="ml-auto flex gap-1">
          <Button size="xs" variant="ghost" onClick={clearLogs}>Clear</Button>
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
        className="rounded-lg border border-border bg-cp-950 overflow-y-auto font-data text-xs leading-5"
        style={{ height: "400px" }}
        data-testid="log-viewer"
      >
        {lines.length === 0 ? (
          <div className="flex items-center justify-center h-full text-muted-foreground">
            {connected ? "Waiting for logs..." : "No log data"}
          </div>
        ) : (
          <pre className="p-3 whitespace-pre-wrap break-all">
            {lines.map((line, i) => (
              <div key={i} className={line.stream === "stderr" ? "text-cp-red/90" : ""}>
                <span className="text-muted-foreground select-none">
                  {new Date(line.ts).toLocaleTimeString()}{" "}
                </span>
                {line.stream === "stderr" ? (
                  line.message
                ) : (
                  <span dangerouslySetInnerHTML={{ __html: highlightLog(line.message) }} />
                )}
              </div>
            ))}
            <div ref={bottomRef} />
          </pre>
        )}
      </div>
    </div>
  );
}
