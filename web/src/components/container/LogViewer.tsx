import { useEffect, useRef, useState, useCallback } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
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
          // P21: avoid full array copy when under the limit
          if (prev.length < maxLines) {
            return [...prev, line];
          }
          // Over limit: drop oldest, append new
          const next = prev.slice(-(maxLines - 1));
          next.push(line);
          return next;
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
      <VirtualizedLogView
        lines={lines}
        connected={connected}
        containerRef={containerRef}
        bottomRef={bottomRef}
        onScroll={handleScroll}
      />
    </div>
  );
}

// P6: Virtualized log renderer -- only renders visible rows
function VirtualizedLogView({ lines, connected, containerRef, bottomRef, onScroll }: {
  lines: LogLine[];
  connected: boolean;
  containerRef: React.RefObject<HTMLDivElement | null>;
  bottomRef: React.RefObject<HTMLDivElement | null>;
  onScroll: () => void;
}) {
  const virtualizer = useVirtualizer({
    count: lines.length,
    getScrollElement: () => containerRef.current,
    estimateSize: () => 20, // ~20px per log line
    overscan: 50,
  });

  return (
    <div
      ref={containerRef}
      onScroll={onScroll}
      className="rounded-lg border border-border bg-cp-950 overflow-y-auto font-data text-xs leading-5"
      style={{ height: "400px" }}
      data-testid="log-viewer"
    >
      {lines.length === 0 ? (
        <div className="flex items-center justify-center h-full text-muted-foreground">
          {connected ? "Waiting for logs..." : "No log data"}
        </div>
      ) : (
        <div style={{ height: `${virtualizer.getTotalSize()}px`, width: "100%", position: "relative" }}>
          {virtualizer.getVirtualItems().map((virtualItem) => {
            const line = lines[virtualItem.index];
            return (
              <div
                key={virtualItem.index}
                className={`absolute left-0 w-full px-3 whitespace-pre-wrap break-all ${line.stream === "stderr" ? "text-cp-red/90" : ""}`}
                style={{ top: `${virtualItem.start}px`, height: `${virtualItem.size}px` }}
              >
                <span className="text-muted-foreground select-none">
                  {new Date(line.ts).toISOString().slice(11, 19)}{" "}
                </span>
                {line.stream === "stderr" ? (
                  line.message
                ) : (
                  <span dangerouslySetInnerHTML={{ __html: highlightLog(line.message) }} />
                )}
              </div>
            );
          })}
          <div ref={bottomRef} />
        </div>
      )}
    </div>
  );
}
