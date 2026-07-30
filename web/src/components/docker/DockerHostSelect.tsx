import { useEffect, useState } from "react";
import { apiFetch } from "@/lib/api/errors";

interface HostInfo {
  id: number;
  name: string;
}

interface DockerHostSelectProps {
  value: string;
  onChange: (value: string) => void;
  testId?: string;
}

export function DockerHostSelect({ value, onChange, testId }: DockerHostSelectProps) {
  const [hosts, setHosts] = useState<HostInfo[]>([]);

  useEffect(() => {
    apiFetch<{ hosts: HostInfo[] }>("/api/v1/hosts").then(({ data: hd }) => {
      if (hd) setHosts(hd.hosts);
    });
  }, []);

  return (
    <select
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="h-7 rounded border border-input bg-transparent px-2 text-xs font-data"
      aria-label="Docker host"
      data-testid={testId || "docker-host-select"}
    >
      <option value="">local (default)</option>
      {hosts.map((h) => (
        <option key={h.id} value={h.name}>{h.name}</option>
      ))}
    </select>
  );
}