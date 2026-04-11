import { useEffect, useRef, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

interface StatsPoint {
  ts: number;
  cpu: number;
  mem: number;
  memLimit: number;
  netRx: number;
  netTx: number;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + " MB";
  return (bytes / (1024 * 1024 * 1024)).toFixed(2) + " GB";
}

export function ContainerStats({ containerId }: { containerId: string }) {
  const [stats, setStats] = useState<StatsPoint[]>([]);
  const [current, setCurrent] = useState<StatsPoint | null>(null);
  const eventSourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    let es: EventSource | null = null;
    let retryCount = 0;
    let retryTimer: ReturnType<typeof setTimeout>;
    let unmounted = false;

    function connect() {
      if (unmounted) return;
      const url = `/api/v1/sse/containers/${containerId}/stats`;
      es = new EventSource(url, { withCredentials: true });
      eventSourceRef.current = es;

      es.addEventListener("stats", (e) => {
        retryCount = 0; // Reset on successful data
        const data = JSON.parse(e.data);
        const point: StatsPoint = {
          ts: Date.now(),
          cpu: data.cpu_percent,
          mem: data.mem_usage,
          memLimit: data.mem_limit,
          netRx: data.net_rx,
          netTx: data.net_tx,
        };
        setCurrent(point);
        setStats((prev) => [...prev.slice(-59), point]);
      });

      es.onerror = () => {
        es?.close();
        if (unmounted) return;
        // Exponential backoff: 1s, 2s, 4s, 8s, ... max 30s
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
    };
  }, [containerId]);

  return (
    <div className="space-y-4">
      {/* Current values */}
      <div className="grid grid-cols-4 gap-3">
        <StatBox label="CPU" value={current ? `${current.cpu.toFixed(1)}%` : "--"} color="text-cp-cyan" />
        <StatBox label="Memory" value={current ? `${formatBytes(current.mem)} / ${formatBytes(current.memLimit)}` : "--"} color="text-cp-green" />
        <StatBox label="Net RX" value={current ? formatBytes(current.netRx) : "--"} color="text-cp-blue" />
        <StatBox label="Net TX" value={current ? formatBytes(current.netTx) : "--"} color="text-cp-peach" />
      </div>

      {/* Simple text-based sparkline (Recharts would be better but this works without extra deps) */}
      {stats.length > 1 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">CPU History (last 60s)</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-16 flex items-end gap-px" data-testid="cpu-chart">
              {stats.map((s, i) => (
                <div
                  key={i}
                  className="flex-1 bg-cp-cyan/60 rounded-t-sm transition-all"
                  style={{ height: `${Math.min(100, Math.max(2, s.cpu))}%` }}
                  title={`${s.cpu.toFixed(1)}%`}
                />
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function StatBox({ label, value, color }: { label: string; value: string; color: string }) {
  return (
    <div className="rounded-lg border border-border p-3">
      <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{label}</p>
      <p className={`text-lg font-bold tabular-nums font-data ${color}`}>{value}</p>
    </div>
  );
}
