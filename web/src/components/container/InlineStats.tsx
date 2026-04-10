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
  stats?: StatsData | null; // from parent batch fetch
}

function formatMB(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(0);
}

export function InlineStats({ containerId, stats: propStats }: Props) {
  const [stats, setStats] = useState<StatsData | null>(propStats || null);

  // Update from props when parent provides batch stats
  useEffect(() => {
    if (propStats) setStats(propStats);
  }, [propStats]);

  // Fallback: if no parent stats, do a single REST fetch (not SSE)
  useEffect(() => {
    if (propStats !== undefined) return; // parent is managing stats
    let cancelled = false;
    const fetchStats = async () => {
      const es = new EventSource(`/api/v1/sse/containers/${containerId}/stats`, { withCredentials: true });
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
      setTimeout(() => es.close(), 3000);
    };
    fetchStats();
    // Re-fetch every 10 seconds instead of holding SSE open
    const interval = setInterval(fetchStats, 10000);
    return () => { cancelled = true; clearInterval(interval); };
  }, [containerId, propStats]);

  if (!stats) return <span className="text-[10px] text-muted-foreground font-data">--</span>;

  return (
    <span className="text-[10px] font-data tabular-nums inline-flex gap-2">
      <span className="text-cp-cyan">{stats.cpu_percent.toFixed(1)}%</span>
      <span className="text-cp-green">{formatMB(stats.mem_usage)}M{stats.mem_limit ? `/${formatMB(stats.mem_limit)}M` : ""}</span>
    </span>
  );
}
