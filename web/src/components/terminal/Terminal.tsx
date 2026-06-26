import { useCallback, useEffect, useRef, useState } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { Button } from "@/components/ui/button";
import { TERMINAL_THEME, ALLOWED_SHELLS, type ShellOption } from "@/lib/terminal-theme";

interface TerminalProps {
  containerId: string;
  shell?: ShellOption;
  onShellChange?: (shell: ShellOption) => void;
  /** Initial height in pixels. User can resize with the drag handle. */
  initialHeight?: number;
  /** Minimum height when resized. */
  minHeight?: number;
}

export function Terminal({
  containerId,
  shell: initialShell = "/bin/sh",
  onShellChange,
  initialHeight = 400,
  minHeight = 120,
}: TerminalProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState("");
  const [shell, setShell] = useState<ShellOption>(initialShell);
  const [height, setHeight] = useState(initialHeight);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttemptsRef = useRef(0);
  const intentionalCloseRef = useRef(false);
  // Track whether we've ever had a successful connection (for reconnect banner)
  const [wasConnected, setWasConnected] = useState(false);

  // ── Resize drag handle ───────────────────────────────────────────
  const [dragging, setDragging] = useState(false);
  const dragStartY = useRef(0);
  const dragStartHeight = useRef(0);

  const onDragStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    dragStartY.current = e.clientY;
    dragStartHeight.current = height;
    setDragging(true);
  }, [height]);

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

  // Fit xterm when height changes
  useEffect(() => {
    if (dragging) {
      // Throttle: only fit every other frame while dragging
      const id = requestAnimationFrame(() => fitRef.current?.fit());
      return () => cancelAnimationFrame(id);
    }
  }, [height, dragging]);

  // ── Copy-on-select helper ────────────────────────────────────────
  const copySelection = useCallback(() => {
    const term = xtermRef.current;
    if (!term) return;
    const selection = term.getSelection();
    if (selection) {
      navigator.clipboard.writeText(selection).catch(() => {});
    }
  }, []);

  // ── Connect ──────────────────────────────────────────────────────
  const connect = useCallback(() => {
    if (!termRef.current) return;

    // Clean up previous
    if (xtermRef.current) xtermRef.current.dispose();
    if (wsRef.current) {
      intentionalCloseRef.current = true;
      wsRef.current.close();
      intentionalCloseRef.current = false;
    }
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }

    setError("");
    setConnected(false);

    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: '"JetBrains Mono", "Fira Code", monospace',
      scrollback: 5000,
      theme: TERMINAL_THEME,
      allowProposedApi: true,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(new WebLinksAddon());
    term.open(termRef.current);

    // Set padding on .xterm element before fit() measures dimensions
    const xtermEl = termRef.current.querySelector(".xterm") as HTMLElement;
    if (xtermEl) {
      xtermEl.style.padding = "12px";
    }

    // Double rAF ensures CSS is applied and layout is done before fit()
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        fitAddon.fit();
      });
    });

    xtermRef.current = term;
    fitRef.current = fitAddon;

    // ── WezTerm-style keyboard shortcuts ──────────────────────────
    term.attachCustomKeyEventHandler((e: KeyboardEvent) => {
      // Ctrl+Shift+C: Copy selection to clipboard
      if (e.ctrlKey && e.shiftKey && e.key === "C" && !e.metaKey) {
        copySelection();
        return false; // prevent default, don't send to shell
      }
      // Ctrl+Shift+V: Paste from clipboard
      if (e.ctrlKey && e.shiftKey && e.key === "V" && !e.metaKey) {
        navigator.clipboard.readText().then((text) => {
          if (wsRef.current?.readyState === WebSocket.OPEN) {
            wsRef.current.send(new TextEncoder().encode(text));
          }
        }).catch(() => {});
        return false;
      }
      // Ctrl+Shift+K: Clear terminal (sends Ctrl+L)
      if (e.ctrlKey && e.shiftKey && e.key === "K" && !e.metaKey) {
        term.clear();
        return false;
      }
      return true;
    });

    // ── Copy on selection: copy to clipboard when mouse is released ──
    term.onSelectionChange(() => {
      // We don't auto-copy here because xterm fires this continuously
      // during selection. Instead we copy on mouseup via the element.
    });

    // Auto-copy selection on mouseup
    const mouseUpHandler = () => {
      const sel = term.getSelection();
      if (sel) {
        navigator.clipboard.writeText(sel).catch(() => {});
      }
    };
    termRef.current.addEventListener("mouseup", mouseUpHandler);

    // ── WebSocket connection ──────────────────────────────────────
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const cols = term.cols;
    const rows = term.rows;
    const wsUrl = `${protocol}//${window.location.host}/api/v1/ws/terminal/${encodeURIComponent(containerId)}?shell=${encodeURIComponent(shell)}&cols=${cols}&rows=${rows}`;

    const ws = new WebSocket(wsUrl);
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onopen = () => {
      setConnected(true);
      setWasConnected(true);
      reconnectAttemptsRef.current = 0;
      term.focus();
    };

    ws.onmessage = (event) => {
      const data = new Uint8Array(event.data);
      term.write(data);
    };

    ws.onclose = () => {
      setConnected(false);
      // If this wasn't an intentional close and we didn't already error,
      // try to auto-reconnect with exponential backoff.
      if (!intentionalCloseRef.current) {
        const attempt = reconnectAttemptsRef.current;
        if (attempt < 5) {
          const delay = Math.min(500 * Math.pow(2, attempt), 8000); // 500ms, 1s, 2s, 4s, 8s
          term.write(`\r\n\x1b[33m[Disconnected — reconnecting in ${delay / 1000}s...]\x1b[0m\r\n`);
          reconnectTimerRef.current = setTimeout(() => {
            reconnectAttemptsRef.current++;
            connect();
          }, delay);
        } else {
          term.write("\r\n\x1b[31m[Connection lost — max reconnect attempts reached]\x1b[0m\r\n");
        }
      } else {
        term.write("\r\n\x1b[33m[Session ended]\x1b[0m\r\n");
      }
    };

    ws.onerror = () => {
      setError("WebSocket connection failed");
      setConnected(false);
    };

    // stdin: terminal → WebSocket
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
  }, [containerId, shell, copySelection]);

  // ── Auto-connect on mount ────────────────────────────────────────
  useEffect(() => {
    connect();

    // Handle window resize (debounced)
    let resizeTimer: ReturnType<typeof setTimeout>;
    const handleResize = () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(() => fitRef.current?.fit(), 100);
    };
    window.addEventListener("resize", handleResize);

    return () => {
      clearTimeout(resizeTimer);
      window.removeEventListener("resize", handleResize);
      intentionalCloseRef.current = true;
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current);
      wsRef.current?.close();
      xtermRef.current?.dispose();
    };
  }, [connect]);

  // ── Shell change: reconnect ──────────────────────────────────────
  const handleShellChange = useCallback((newShell: ShellOption) => {
    setShell(newShell);
    onShellChange?.(newShell);
    // connect() will be re-triggered by the useEffect dependency on `shell`
  }, [onShellChange]);

  // ── Render ───────────────────────────────────────────────────────
  return (
    <div className="space-y-2" data-testid="terminal">
      {/* Header bar */}
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span
            className={`h-2 w-2 rounded-full ${connected ? "bg-cp-green" : "bg-cp-red"}`}
          />
          <span className="text-xs text-muted-foreground">
            {connected ? "Connected" : wasConnected ? "Reconnecting…" : "Disconnected"}
          </span>
          {!connected && wasConnected && (
            <span className="text-xs text-cp-yellow animate-pulse">
              Attempt {reconnectAttemptsRef.current + 1}/5
            </span>
          )}
        </div>

        <div className="flex items-center gap-2">
          {/* Copy button */}
          <Button
            size="xs"
            variant="ghost"
            className="text-xs"
            onClick={copySelection}
            title="Copy selection (Ctrl+Shift+C)"
            data-testid="terminal-copy"
          >
            Copy
          </Button>

          {/* Shell selector */}
          <select
            value={shell}
            onChange={(e) => handleShellChange(e.target.value as ShellOption)}
            className="text-xs rounded border border-input bg-transparent px-1.5 py-0.5 font-data"
            title="Select shell"
            data-testid="terminal-shell-select"
          >
            {ALLOWED_SHELLS.map((s) => (
              <option key={s} value={s}>
                {s.split("/").pop() /* sh, bash, ash, zsh */}
              </option>
            ))}
          </select>

          {/* Reconnect button */}
          {!connected && (
            <Button
              size="xs"
              variant="outline"
              onClick={() => { reconnectAttemptsRef.current = 0; connect(); }}
              data-testid="terminal-reconnect"
            >
              Reconnect
            </Button>
          )}
        </div>
      </div>

      {error && (
        <div className="text-xs text-cp-red" data-testid="terminal-error">{error}</div>
      )}

      {/* Terminal container with dynamic height + drag handle */}
      <div
        ref={containerRef}
        className="relative rounded-lg border border-border overflow-hidden"
        style={{ height }}
        data-testid="terminal-container"
      >
        <div ref={termRef} className="size-full" />

        {/* Drag handle at bottom */}
        <div
          className={`absolute bottom-0 left-0 right-0 h-1.5 cursor-ns-resize transition-colors ${
            dragging ? "bg-cp-purple/40" : "hover:bg-cp-purple/20"
          }`}
          onMouseDown={onDragStart}
          title="Drag to resize terminal"
          data-testid="terminal-drag-handle"
        />
      </div>
    </div>
  );
}
