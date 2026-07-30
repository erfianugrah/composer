/**
 * InlineStats displays CPU/memory for a container.
 * P1: Accepts stats as props instead of opening its own SSE connection.
 * The parent component should poll/stream stats and pass them down.
 * Falls back to a single REST fetch if no props provided.
 */
import { useEffect, useState } from "react";
import { apiFetch } from "@/lib/api/errors";

interface StatsData {
  cpu_percent: number;
  mem_usage: number;
  mem_limit: number;
}

interface Props {
  containerId: string;
  host?: string;
  stats?: StatsData | null; // from parent batch fetch
}

function formatMB(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(0);
}

export function InlineStats({ containerId, host, stats: propStats }: Props) {
  const [stats, setStats] = useState<StatsData | null>(propStats || null);

  // Update from props when parent provides batch stats
  useEffect(() => {
    if (propStats) setStats(propStats);
  }, [propStats]);

  // Fallback: if no parent stats, do a single REST fetch (not SSE)
  useEffect(() => {
    if (propStats !== undefined) return; // parent is managing stats
    let cancelled = false;
    let activeES: EventSource | null = null;
    let activeTimeout: ReturnType<typeof setTimeout> | null = null;
    const fetchStats = async () => {
      const es = new EventSource(`/api/v1/sse/containers/${containerId}/stats${host ? `?host=${encodeURIComponent(host)}` : ""}`, { withCredentials: true });
      activeES = es;
      const handler = (e: MessageEvent) => {
        try {
          const d = JSON.parse(e.data);
          if (!cancelled) setStats({ cpu_percent: d.cpu_percent, mem_usage: d.mem_usage, mem_limit: d.mem_limit });
        } catch {}
        // Close after first stat to avoid holding connection
        es.close();
      };
      es.addEventListener("stats", handler);
      es.onerror = () => es.close();
      // Auto-close after 3 seconds if no data
      activeTimeout = setTimeout(() => es.close(), 3000);
    };
    fetchStats();
    // Re-fetch every 10 seconds instead of holding SSE open
    const interval = setInterval(fetchStats, 10000);
    // Close the in-flight EventSource on unmount so a connection opened just
    // before unmount is not orphaned (leaking a server goroutine + fd until
    // its 3s timeout) and cannot fire setStats on an unmounted component.
    return () => {
      cancelled = true;
      clearInterval(interval);
      if (activeTimeout) clearTimeout(activeTimeout);
      if (activeES) activeES.close();
    };
  }, [containerId, host, propStats]);

  if (!stats) return <span className="text-[10px] text-muted-foreground font-data">--</span>;

  return (
    <span className="text-[10px] font-data tabular-nums inline-flex gap-2">
      <span className="text-cp-cyan">{stats.cpu_percent.toFixed(1)}%</span>
      <span className="text-cp-green">{formatMB(stats.mem_usage)}M{stats.mem_limit ? `/${formatMB(stats.mem_limit)}M` : ""}</span>
    </span>
  );
}
