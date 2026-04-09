import { useEffect, useState } from "react";

interface Props {
  containerId: string;
}

function formatMB(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(0);
}

export function InlineStats({ containerId }: Props) {
  const [cpu, setCpu] = useState<number | null>(null);
  const [mem, setMem] = useState<number | null>(null);
  const [memLimit, setMemLimit] = useState<number | null>(null);

  useEffect(() => {
    const es = new EventSource(`/api/v1/sse/containers/${containerId}/stats`, { withCredentials: true });

    es.addEventListener("stats", (e) => {
      try {
        const d = JSON.parse(e.data);
        setCpu(d.cpu_percent);
        setMem(d.mem_usage);
        setMemLimit(d.mem_limit);
      } catch {}
    });

    es.onerror = () => es.close();
    return () => es.close();
  }, [containerId]);

  if (cpu === null) return <span className="text-[10px] text-muted-foreground font-data">--</span>;

  return (
    <span className="text-[10px] font-data tabular-nums inline-flex gap-2">
      <span className="text-cp-cyan">{cpu.toFixed(1)}%</span>
      <span className="text-cp-green">{formatMB(mem || 0)}M{memLimit ? `/${formatMB(memLimit)}M` : ""}</span>
    </span>
  );
}
