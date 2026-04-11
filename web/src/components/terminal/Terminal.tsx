import { useEffect, useRef, useState } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { Button } from "@/components/ui/button";

interface TerminalProps {
  containerId: string;
  shell?: string;
}

export function Terminal({ containerId, shell = "/bin/sh" }: TerminalProps) {
  const termRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState("");

  function connect() {
    if (!termRef.current) return;

    // Clean up previous
    if (xtermRef.current) xtermRef.current.dispose();
    if (wsRef.current) wsRef.current.close();

    setError("");
    setConnected(false);

    // Create terminal
    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: '"JetBrains Mono", "Fira Code", monospace',
      theme: {
        background: "#1d1f28",
        foreground: "#e0e0e0",
        cursor: "#c574dd",
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

    // Set padding on the .xterm element before fit() measures dimensions
    const xtermEl = termRef.current.querySelector('.xterm') as HTMLElement;
    if (xtermEl) {
      xtermEl.style.padding = '12px';
    }

    // Double rAF ensures CSS is applied and layout is done before fit() measures
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        fitAddon.fit();
      });
    });

    xtermRef.current = term;
    fitRef.current = fitAddon;

    // WebSocket connection
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const cols = term.cols;
    const rows = term.rows;
    const wsUrl = `${protocol}//${window.location.host}/api/v1/ws/terminal/${encodeURIComponent(containerId)}?shell=${encodeURIComponent(shell)}&cols=${cols}&rows=${rows}`;

    const ws = new WebSocket(wsUrl);
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onopen = () => {
      setConnected(true);
      term.focus();
    };

    ws.onmessage = (event) => {
      const data = new Uint8Array(event.data);
      term.write(data);
    };

    ws.onclose = () => {
      setConnected(false);
      term.write("\r\n\x1b[33m[Session ended]\x1b[0m\r\n");
    };

    ws.onerror = () => {
      setError("WebSocket connection failed");
      setConnected(false);
    };

    // stdin: terminal -> WebSocket
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data));
      }
    });

    // Resize handling
    term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols, rows }));
      }
    });
  }

  // Auto-connect on mount
  useEffect(() => {
    connect();

    // Handle window resize (debounced to avoid jank)
    let resizeTimer: ReturnType<typeof setTimeout>;
    const handleResize = () => { clearTimeout(resizeTimer); resizeTimer = setTimeout(() => fitRef.current?.fit(), 100); };
    window.addEventListener("resize", handleResize);

    return () => {
      clearTimeout(resizeTimer);
      window.removeEventListener("resize", handleResize);
      wsRef.current?.close();
      xtermRef.current?.dispose();
    };
  }, [containerId, shell]);

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <div className="flex items-center gap-2">
          <span
            className={`h-2 w-2 rounded-full ${connected ? "bg-cp-green" : "bg-cp-red"}`}
          />
          <span className="text-xs text-muted-foreground">
            {connected ? "Connected" : "Disconnected"}
          </span>
        </div>
        {!connected && (
          <Button size="xs" variant="outline" onClick={connect} data-testid="terminal-reconnect">
            Reconnect
          </Button>
        )}
      </div>
      {error && (
        <div className="text-xs text-cp-red" data-testid="terminal-error">{error}</div>
      )}
      <div
        ref={termRef}
        className="rounded-lg border border-border overflow-hidden"
        style={{ height: "400px" }}
        data-testid="terminal-container"
      />
    </div>
  );
}
