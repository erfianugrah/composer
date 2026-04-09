import { useState, useRef, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";

interface ConsoleEntry {
  command: string;
  stdout: string;
  stderr: string;
  exitCode: number;
}

export function DockerConsole() {
  const [command, setCommand] = useState("");
  const [history, setHistory] = useState<ConsoleEntry[]>([]);
  const [running, setRunning] = useState(false);
  const [cmdHistory, setCmdHistory] = useState<string[]>([]);
  const [historyIdx, setHistoryIdx] = useState(-1);
  const inputRef = useRef<HTMLInputElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [history]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!command.trim() || running) return;

    const cmd = command.trim();
    setRunning(true);
    setCmdHistory((prev) => [...prev.filter((c) => c !== cmd), cmd]);
    setHistoryIdx(-1);
    setCommand("");

    const { data, error } = await apiFetch<{ stdout: string; stderr: string; exit_code: number }>(
      "/api/v1/docker/exec",
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ command: cmd }),
      },
    );

    setHistory((prev) => [...prev, {
      command: cmd,
      stdout: data?.stdout || "",
      stderr: data?.stderr || (error || ""),
      exitCode: data?.exit_code ?? (error ? 1 : 0),
    }]);
    setRunning(false);
    inputRef.current?.focus();
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowUp") {
      e.preventDefault();
      if (cmdHistory.length === 0) return;
      const idx = historyIdx < 0 ? cmdHistory.length - 1 : Math.max(0, historyIdx - 1);
      setHistoryIdx(idx);
      setCommand(cmdHistory[idx]);
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      if (historyIdx < 0) return;
      const idx = historyIdx + 1;
      if (idx >= cmdHistory.length) { setHistoryIdx(-1); setCommand(""); }
      else { setHistoryIdx(idx); setCommand(cmdHistory[idx]); }
    }
  }

  return (
    <div className="space-y-2">
      <div className="text-xs text-muted-foreground">
        Runs <code className="font-data text-cp-cyan">docker &lt;command&gt;</code> on the host. Admin only. No SSH needed.
      </div>
      <div
        className="rounded-lg border border-border bg-cp-950 overflow-y-auto font-data text-xs leading-5 p-3"
        style={{ height: "300px" }}
      >
        {history.length === 0 && (
          <div className="text-muted-foreground">
            Try: <code>ps -a</code>, <code>images</code>, <code>network ls</code>, <code>volume ls</code>, <code>system df</code>, <code>stats --no-stream</code>
          </div>
        )}
        {history.map((entry, i) => (
          <div key={i} className="mb-3">
            <div className="text-cp-purple"><span className="text-cp-cyan select-none">$ docker </span>{entry.command}</div>
            {entry.stdout && <pre className="text-cp-green/80 whitespace-pre-wrap">{entry.stdout}</pre>}
            {entry.stderr && <pre className="text-cp-red/80 whitespace-pre-wrap">{entry.stderr}</pre>}
            {entry.exitCode !== 0 && <div className="text-cp-peach text-[10px]">exit code: {entry.exitCode}</div>}
          </div>
        ))}
        {running && <div className="text-muted-foreground animate-pulse">Running...</div>}
        <div ref={bottomRef} />
      </div>
      <form onSubmit={handleSubmit} className="flex gap-2">
        <div className="flex-1 flex items-center gap-2 rounded-md border border-border bg-cp-950 px-3">
          <span className="text-xs text-cp-cyan font-data select-none">docker</span>
          <input
            ref={inputRef}
            type="text"
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="ps -a"
            disabled={running}
            autoFocus
            className="flex-1 bg-transparent text-sm font-data py-2 outline-none placeholder:text-muted-foreground"
          />
        </div>
        <Button type="submit" size="sm" disabled={running || !command.trim()}>
          {running ? "..." : "Run"}
        </Button>
        {history.length > 0 && (
          <Button type="button" size="sm" variant="ghost" onClick={() => setHistory([])}>Clear</Button>
        )}
      </form>
    </div>
  );
}
