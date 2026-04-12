import { useEffect, useRef, useState, useCallback } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";

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
  const termRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [phase, setPhase] = useState<string>("");
  const [status, setStatus] = useState<"connecting" | "running" | "done" | "error">("connecting");
  const [exitCode, setExitCode] = useState<number | null>(null);
  const finishedRef = useRef(false); // tracks done/error to prevent stale closure in onclose

  const connect = useCallback(() => {
    if (!termRef.current) return;

    // Clean up previous
    if (xtermRef.current) xtermRef.current.dispose();
    if (wsRef.current) wsRef.current.close();

    setStatus("connecting");
    setPhase("");
    setExitCode(null);
    finishedRef.current = false;

    const term = new XTerm({
      cursorBlink: false,
      disableStdin: true,
      fontSize: 13,
      fontFamily: '"JetBrains Mono", "Fira Code", monospace',
      scrollback: 5000,
      theme: {
        background: "#1d1f28",
        foreground: "#e0e0e0",
        cursor: "#1d1f28", // hide cursor (read-only)
        selectionBackground: "#c574dd40",
        black: "#15161e",
        red: "#f37e96",
        green: "#5adecd",
        yellow: "#ffd866",
        blue: "#8796f4",
        magenta: "#c574dd",
        cyan: "#79e6f3",
        white: "#e0e0e0",
        brightBlack: "#414457",
        brightRed: "#ff4870",
        brightGreen: "#17e2c7",
        brightYellow: "#ffd866",
        brightBlue: "#546eff",
        brightMagenta: "#af43d1",
        brightCyan: "#3edced",
        brightWhite: "#fcfcfc",
      },
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(new WebLinksAddon());
    term.open(termRef.current);

    const xtermEl = termRef.current.querySelector('.xterm') as HTMLElement;
    if (xtermEl) {
      xtermEl.style.padding = '12px';
    }

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
            // Write phase separator to terminal
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
      if (!finishedRef.current) {
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

  const actionLabel = action.charAt(0).toUpperCase() + action.slice(1);

  return (
    <div className="rounded-lg border border-border bg-cp-950 overflow-hidden" data-testid="action-terminal">
      <div className="flex items-center justify-between px-3 py-2 border-b border-border">
        <div className="flex items-center gap-2">
          <span
            className={`h-2 w-2 rounded-full ${
              status === "running" ? "bg-cp-yellow animate-pulse" :
              status === "done" && exitCode === 0 ? "bg-cp-green" :
              status === "error" || (status === "done" && exitCode !== 0) ? "bg-cp-red" :
              "bg-muted-foreground"
            }`}
          />
          <span className="text-xs text-muted-foreground uppercase tracking-wider">
            {status === "connecting" && "Connecting..."}
            {status === "running" && `${actionLabel}: ${phase || "starting"}...`}
            {status === "done" && exitCode === 0 && `${actionLabel} complete`}
            {status === "done" && exitCode !== 0 && `${actionLabel} failed`}
            {status === "error" && `${actionLabel} error`}
          </span>
        </div>
        <button
          className="text-xs text-muted-foreground hover:text-foreground"
          onClick={onClose}
        >
          close
        </button>
      </div>
      <div
        ref={termRef}
        style={{ height: "350px" }}
        data-testid="action-terminal-output"
      />
    </div>
  );
}
