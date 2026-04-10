import { useState, useEffect, useCallback, useRef } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { apiFetch } from "@/lib/api/errors";

interface Job {
  id: string;
  type: string;
  target: string;
  status: "pending" | "running" | "completed" | "failed";
  output: string;
  error: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
}

function formatDuration(start?: string, end?: string): string {
  if (!start) return "--";
  const s = new Date(start).getTime();
  const e = end ? new Date(end).getTime() : Date.now();
  const ms = e - s;
  if (ms < 1000) return `${ms}ms`;
  const secs = Math.floor(ms / 1000);
  if (secs < 60) return `${secs}s`;
  const mins = Math.floor(secs / 60);
  return `${mins}m ${secs % 60}s`;
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function statusColor(status: Job["status"]): string {
  switch (status) {
    case "running":
      return "text-cp-blue";
    case "completed":
      return "text-cp-green";
    case "failed":
      return "text-cp-red";
    default:
      return "text-muted-foreground";
  }
}

function statusIcon(status: Job["status"]): string {
  switch (status) {
    case "pending":
      return "\u25CB"; // ○
    case "running":
      return "\u25CF"; // ●
    case "completed":
      return "\u2713"; // ✓
    case "failed":
      return "\u2717"; // ✗
  }
}

function jobTypeLabel(type: string): string {
  switch (type) {
    case "deploy":
      return "Deploy";
    case "build_deploy":
      return "Build & Deploy";
    case "stop":
      return "Stop";
    case "restart":
      return "Restart";
    case "pull":
      return "Pull";
    case "sync_redeploy":
      return "Git Sync";
    default:
      return type;
  }
}

export function JobsDrawer() {
  const [open, setOpen] = useState(false);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [expandedJob, setExpandedJob] = useState<string | null>(null);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const runningCount = jobs.filter(
    (j) => j.status === "running" || j.status === "pending",
  ).length;
  const hasActive = runningCount > 0;

  const fetchJobs = useCallback(async () => {
    const { data } = await apiFetch<{ jobs: Job[] }>("/api/v1/jobs");
    if (data?.jobs) {
      setJobs(data.jobs);
    }
  }, []);

  // Poll when drawer is open OR there are active jobs
  useEffect(() => {
    // Always do an initial fetch
    fetchJobs();

    function startPolling() {
      if (intervalRef.current) return;
      intervalRef.current = setInterval(fetchJobs, 2000);
    }

    function stopPolling() {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    }

    if (open || hasActive) {
      startPolling();
    } else {
      stopPolling();
    }

    return () => stopPolling();
  }, [open, hasActive, fetchJobs]);

  // Close on Escape
  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open]);

  return (
    <>
      {/* Trigger button */}
      <button
        onClick={() => setOpen((prev) => !prev)}
        className="relative flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors px-2 py-1 rounded border border-transparent hover:border-border"
        title="Background jobs"
        data-testid="jobs-trigger"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M16 16h6" />
          <path d="M16 20h6" />
          <path d="M16 12h6" />
          <path d="M10 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h6" />
          <path d="M10 4l4 4-4 4" />
        </svg>
        Jobs
        {hasActive && (
          <Badge variant="default" className="ml-0.5 px-1.5 py-0 text-[10px] min-w-[18px] h-[18px] flex items-center justify-center animate-pulse">
            {runningCount}
          </Badge>
        )}
      </button>

      {/* Drawer */}
      {open && (
        <div className="fixed inset-0 z-50" role="dialog" aria-modal="true" aria-label="Background jobs" data-testid="jobs-drawer">
          {/* Backdrop */}
          <div
            className="fixed inset-0 bg-black/40"
            onClick={() => setOpen(false)}
          />

          {/* Panel -- slides from right */}
          <div className="fixed inset-y-0 right-0 w-full max-w-md border-l border-border bg-card shadow-2xl flex flex-col animate-in slide-in-from-right duration-200">
            {/* Header */}
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <div className="flex items-center gap-2">
                <h2 className="text-sm font-semibold">Background Jobs</h2>
                {hasActive && (
                  <Badge variant="default" className="text-[10px]">
                    {runningCount} running
                  </Badge>
                )}
              </div>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => setOpen(false)}
              >
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <line x1="18" y1="6" x2="6" y2="18" />
                  <line x1="6" y1="6" x2="18" y2="18" />
                </svg>
              </Button>
            </div>

            {/* Job list */}
            <div className="flex-1 overflow-y-auto">
              {jobs.length === 0 ? (
                <div className="flex items-center justify-center h-32 text-sm text-muted-foreground">
                  No background jobs
                </div>
              ) : (
                <div className="divide-y divide-border">
                  {jobs.map((job) => (
                    <div key={job.id} className="group">
                      <button
                        className="w-full text-left px-4 py-3 hover:bg-accent/50 transition-colors"
                        onClick={() =>
                          setExpandedJob(
                            expandedJob === job.id ? null : job.id,
                          )
                        }
                      >
                        <div className="flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <span
                              className={`text-sm font-mono ${statusColor(job.status)}`}
                            >
                              {statusIcon(job.status)}
                            </span>
                            <span className="text-sm font-medium">
                              {jobTypeLabel(job.type)}
                            </span>
                            <span className="text-xs text-muted-foreground font-data">
                              {job.target}
                            </span>
                          </div>
                          <div className="flex items-center gap-2">
                            <span className="text-xs text-muted-foreground font-data">
                              {formatDuration(job.started_at, job.finished_at)}
                            </span>
                            <span className="text-xs text-muted-foreground">
                              {formatTime(job.created_at)}
                            </span>
                          </div>
                        </div>
                      </button>

                      {/* Expanded detail */}
                      {expandedJob === job.id && (
                        <div className="px-4 pb-3 space-y-2">
                          <div className="flex gap-4 text-xs text-muted-foreground">
                            <span>
                              Status:{" "}
                              <span className={statusColor(job.status)}>
                                {job.status}
                              </span>
                            </span>
                            <span className="font-data">ID: {job.id}</span>
                          </div>
                          {job.output && (
                            <div>
                              <div className="text-xs text-muted-foreground mb-1">
                                Output
                              </div>
                              <pre className="text-xs font-data bg-cp-950 border border-border rounded p-2 max-h-40 overflow-auto whitespace-pre-wrap">
                                {job.output}
                              </pre>
                            </div>
                          )}
                          {job.error && (
                            <div>
                              <div className="text-xs text-cp-red mb-1">
                                {job.status === "failed" ? "Error" : "Stderr"}
                              </div>
                              <pre className="text-xs font-data bg-cp-950 border border-cp-red/30 rounded p-2 max-h-40 overflow-auto whitespace-pre-wrap text-cp-red/80">
                                {job.error}
                              </pre>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
}
