import { useEffect, useRef, useState, useCallback } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { TERMINAL_THEME } from "@/lib/terminal-theme";
import { Button } from "@/components/ui/button";

interface ActionTerminalProps {
  stackName: string;
  action: string; // "update", "pull", "up", "build", "down", "restart"
  onClose: () => void;
  onDone?: (exitCode: number) => void;
}

interface StatusMessage {
  type: "phase" | "done" | "error";
  phase?: string;
  message?: string;
  exit_code?: number;
}

export function ActionTerminal({ stackName, action, onClose, onDone }: ActionTerminalProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [phase, setPhase] = useState<string>("");
  const [status, setStatus] = useState<"connecting" | "running" | "done" | "error" | "cancelling">("connecting");
  const [exitCode, setExitCode] = useState<number | null>(null);
  const finishedRef = useRef(false); // tracks done/error to prevent stale closure in onclose
  const [cancelled, setCancelled] = useState(false);
  const [height, setHeight] = useState(350);
  const minHeight = 120;

  // ── Resize drag handle ───────────────────────────────────────────
  const [dragging, setDragging] = useState(false);
  const dragStartY = useRef(0);
  const dragStartHeight = useRef(0);

  const onDragStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      dragStartY.current = e.clientY;
      dragStartHeight.current = height;
      setDragging(true);
    },
    [height],
  );

  useEffect(() => {
    if (!dragging) return;
    const onMove = (e: MouseEvent) => {
      const delta = e.clientY - dragStartY.current;
      setHeight(Math.max(minHeight, dragStartHeight.current + delta));
    };
    const onUp = () => setDragging(false);
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, [dragging, minHeight]);

  useEffect(() => {
    if (dragging) {
      const id = requestAnimationFrame(() => fitRef.current?.fit());
      return () => cancelAnimationFrame(id);
    }
  }, [height, dragging]);

  const connect = useCallback(() => {
    if (!termRef.current) return;

    // Clean up previous
    if (xtermRef.current) xtermRef.current.dispose();
    if (wsRef.current) wsRef.current.close();

    setStatus("connecting");
    setPhase("");
    setExitCode(null);
    setCancelled(false);
    finishedRef.current = false;

    const term = new XTerm({
      cursorBlink: false,
      disableStdin: true,
      fontSize: 13,
      fontFamily: '"JetBrains Mono", "Fira Code", monospace',
      scrollback: 5000,
      theme: TERMINAL_THEME,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(new WebLinksAddon());
    term.open(termRef.current);

    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        fitAddon.fit();
      });
    });

    xtermRef.current = term;
    fitRef.current = fitAddon;

    // Connect WebSocket
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const cols = term.cols;
    const rows = term.rows;
    const wsUrl = `${protocol}//${window.location.host}/api/v1/ws/stacks/${encodeURIComponent(stackName)}/action?action=${encodeURIComponent(action)}&cols=${cols}&rows=${rows}`;

    const ws = new WebSocket(wsUrl);
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onopen = () => {
      setStatus("running");
    };

    ws.onmessage = (event) => {
      if (typeof event.data === "string") {
        // Text message: status/phase/done/error
        try {
          const msg: StatusMessage = JSON.parse(event.data);
          if (msg.type === "phase") {
            setPhase(msg.phase || "");
            const phaseLabel = msg.phase?.toUpperCase() || "";
            term.write(`\r\n\x1b[1;35m--- ${phaseLabel} ---\x1b[0m\r\n`);
          } else if (msg.type === "done") {
            finishedRef.current = true;
            setStatus("done");
            setExitCode(msg.exit_code ?? 0);
            const code = msg.exit_code ?? 0;
            if (code === 0) {
              term.write(`\r\n\x1b[1;32m✓ Completed successfully\x1b[0m\r\n`);
            } else {
              term.write(`\r\n\x1b[1;31m✗ Failed (exit code ${code})\x1b[0m\r\n`);
            }
            onDone?.(code);
          } else if (msg.type === "error") {
            finishedRef.current = true;
            setStatus("error");
            term.write(`\r\n\x1b[1;31m✗ Error: ${msg.message}\x1b[0m\r\n`);
          }
        } catch {
          // ignore parse errors
        }
      } else {
        // Binary message: PTY output
        const data = new Uint8Array(event.data);
        term.write(data);
      }
    };

    ws.onclose = () => {
      if (!finishedRef.current && !cancelled) {
        setStatus("error");
      }
    };

    ws.onerror = () => {
      setStatus("error");
    };

    // Send resize events
    term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols, rows }));
      }
    });
  }, [stackName, action]);

  useEffect(() => {
    connect();

    let resizeTimer: ReturnType<typeof setTimeout>;
    const handleResize = () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => fitRef.current?.fit(), 100);
    };
    window.addEventListener("resize", handleResize);

    return () => {
      clearTimeout(resizeTimer);
      window.removeEventListener("resize", handleResize);
      wsRef.current?.close();
      xtermRef.current?.dispose();
    };
  }, [connect]);

  // ── Cancel: send cancel message then close WS ──────────────────
  const handleCancel = useCallback(() => {
    setCancelled(true);
    setStatus("cancelling");
    const term = xtermRef.current;
    if (term) {
      term.write(`\r\n\x1b[1;33m⏸ Cancelling…\x1b[0m\r\n`);
    }
    // Send cancel message if WS is still open
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: "cancel" }));
      // Give the server a moment to process, then close
      setTimeout(() => wsRef.current?.close(), 300);
    } else {
      wsRef.current?.close();
    }
  }, []);

  const actionLabel = action.charAt(0).toUpperCase() + action.slice(1);
  const canCancel = status === "running" || status === "connecting";
  const isTerminal = status !== "done" && status !== "error";

  return (
    <div className="rounded-lg border border-border bg-cp-950 overflow-hidden" data-testid="action-terminal">
      <div className="flex items-center justify-between px-3 py-2 border-b border-border">
        <div className="flex items-center gap-2">
          <span
            className={`h-2 w-2 rounded-full ${
              status === "running" ? "bg-cp-yellow animate-pulse" :
              status === "cancelling" ? "bg-cp-red animate-pulse" :
              status === "done" && exitCode === 0 ? "bg-cp-green" :
              status === "error" || (status === "done" && exitCode !== 0) ? "bg-cp-red" :
              "bg-muted-foreground"
            }`}
          />
          <span className="text-xs text-muted-foreground uppercase tracking-wider">
            {status === "connecting" && "Connecting…"}
            {status === "running" && `${actionLabel}: ${phase || "starting"}…`}
            {status === "cancelling" && `${actionLabel}: cancelling…`}
            {status === "done" && exitCode === 0 && `${actionLabel} complete`}
            {status === "done" && exitCode !== 0 && `${actionLabel} failed`}
            {status === "error" && `${actionLabel} error`}
          </span>
        </div>
        <div className="flex items-center gap-2">
          {canCancel && (
            <Button
              size="xs"
              variant="destructive"
              onClick={handleCancel}
              data-testid="action-terminal-cancel"
            >
              Cancel
            </Button>
          )}
          <button
            className="text-xs text-muted-foreground hover:text-foreground"
            onClick={onClose}
            data-testid="action-terminal-close"
          >
            close
          </button>
        </div>
      </div>
      <div ref={containerRef} className="relative" style={{ height }}>
        <div ref={termRef} className="p-3 size-full" data-testid="action-terminal-output" />
        {/* Drag handle */}
        {isTerminal && (
          <div
            className={`absolute bottom-0 left-0 right-0 h-1.5 cursor-ns-resize transition-colors ${
              dragging ? "bg-cp-purple/40" : "hover:bg-cp-purple/20"
            }`}
            onMouseDown={onDragStart}
            title="Drag to resize terminal"
          />
        )}
      </div>
    </div>
  );
}
