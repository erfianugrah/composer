import { useEffect, useRef, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { ConfirmButton } from "@/components/ui/confirm-button";
import { Input } from "@/components/ui/input";
import { Table, TBody, TR, TH, TD } from "@/components/ui/data-table";
import { apiFetch } from "@/lib/api/errors";
import { useAction } from "@/lib/use-action";
import { ErrorBoundary } from "@/components/ui/ErrorBoundary";

/**
 * SelfUpgrade - trigger and observe composer self-upgrades.
 *
 * POST /api/v1/system/upgrade (admin) launches a detached helper container
 * that pulls the target image and recreates composer. GET
 * /api/v1/system/upgrade/status (public) tracks the singleton upgrade row.
 *
 * Composer is unreachable for a few seconds while the container is
 * recreated. Polls treat a network error during an in-flight upgrade as
 * "restarting" rather than a failure.
 */

interface UpgradeStatus {
  status: "pending" | "helper_running" | "completed" | "failed";
  helper_id?: string;
  started_by?: string;
  from_version: string;
  target_image: string;
  deployment_type: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
}

interface VersionInfo {
  version: string;
  go_version: string;
  os: string;
  arch: string;
}

const DEFAULT_IMAGE = "ghcr.io/erfianugrah/composer:latest-amd64";
const NETWORK_ERROR = "Network error -- cannot connect to the server.";

const statusVariant: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
  pending: "secondary",
  helper_running: "secondary",
  completed: "default",
  failed: "destructive",
};

export function SelfUpgrade() {
  const [version, setVersion] = useState<VersionInfo | null>(null);
  const [row, setRow] = useState<UpgradeStatus | null>(null);
  const [image, setImage] = useState(DEFAULT_IMAGE);
  const [error, setError] = useState("");
  const [restarting, setRestarting] = useState(false);
  const [loading, setLoading] = useState(true);
  const pollRef = useRef<number | null>(null);
  const act = useAction();

  const inFlight = row?.status === "pending" || row?.status === "helper_running";

  async function fetchStatus() {
    const { data, error: err } = await apiFetch<UpgradeStatus>("/api/v1/system/upgrade/status");
    if (err) {
      // During an upgrade the server goes away while the container is
      // recreated - that is expected, not a failure.
      if (err === NETWORK_ERROR) {
        setRestarting(true);
        return;
      }
      setError(err);
      return;
    }
    const wasRestarting = restarting;
    setRestarting(false);
    setRow(data);
    setLoading(false);
    if (wasRestarting || (data && (data.status === "completed" || data.status === "failed"))) {
      // New instance is back - refresh the displayed version.
      apiFetch<VersionInfo>("/api/v1/system/version").then(({ data: v }) => v && setVersion(v));
    }
  }

  // Initial load.
  useEffect(() => {
    apiFetch<VersionInfo>("/api/v1/system/version").then(({ data: v }) => v && setVersion(v));
    fetchStatus();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Poll while an upgrade is in flight (or composer is restarting).
  useEffect(() => {
    if (!inFlight && !restarting) {
      if (pollRef.current) { window.clearInterval(pollRef.current); pollRef.current = null; }
      return;
    }
    if (pollRef.current) return;
    pollRef.current = window.setInterval(fetchStatus, 3000);
    return () => { if (pollRef.current) { window.clearInterval(pollRef.current); pollRef.current = null; } };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inFlight, restarting]);

  async function trigger() {
    setError("");
    const result = await act.run<{ helper_id: string }>(
      "system-upgrade",
      () => apiFetch<{ helper_id: string }>("/api/v1/system/upgrade", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ image: image.trim() }),
      }),
      {
        running: "Starting upgrade",
        success: "Upgrade helper started",
        failure: "Upgrade failed to start",
      },
    );
    if (result.error) {
      setError(result.error);
      return;
    }
    if (result.data) {
      setRow((r) => r ? { ...r, status: "helper_running", target_image: image.trim() } : r);
      setRestarting(false);
      fetchStatus();
    }
  }

  return (
    <ErrorBoundary>
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Self-Upgrade</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <Table>
            <TBody>
              <TR>
                <TH className="w-40">Current version</TH>
                <TD className="font-data">{version ? `v${version.version}` : "..."}</TD>
              </TR>
              <TR>
                <TH>Status</TH>
                <TD>
                  {loading ? (
                    <span className="text-muted-foreground">...</span>
                  ) : restarting ? (
                    <Badge variant="secondary">restarting</Badge>
                  ) : row ? (
                    <Badge variant={statusVariant[row.status] || "outline"}>{row.status.replace("_", " ")}</Badge>
                  ) : (
                    <span className="text-muted-foreground">never upgraded</span>
                  )}
                </TD>
              </TR>
              {row && row.target_image && (
                <TR>
                  <TH>Target image</TH>
                  <TD className="font-data">{row.target_image}</TD>
                </TR>
              )}
              {row?.deployment_type && row.deployment_type !== "unknown" && (
                <TR>
                  <TH>Deployment</TH>
                  <TD className="font-data">{row.deployment_type}</TD>
                </TR>
              )}
              {row?.started_by && (
                <TR>
                  <TH>Started by</TH>
                  <TD className="font-data">{row.started_by}</TD>
                </TR>
              )}
              {row?.status === "failed" && row.error_message && (
                <TR>
                  <TH>Error</TH>
                  <TD className="font-data text-destructive">{row.error_message}</TD>
                </TR>
              )}
            </TBody>
          </Table>

          {(inFlight || restarting) && (
            <p className="text-sm text-muted-foreground">
              Upgrade in progress. Composer will be unreachable for a few seconds while the
              container is recreated; this page resumes polling automatically.
            </p>
          )}

          <form
            className="flex items-center gap-2"
            onSubmit={(e) => { e.preventDefault(); }}
          >
            <Input
              value={image}
              onChange={(e) => setImage(e.target.value)}
              placeholder="ghcr.io/erfianugrah/composer:<tag>"
              className="font-data flex-1"
              disabled={inFlight || restarting}
            />
            <ConfirmButton
              message="Recreate composer with this image?"
              confirmLabel="Upgrade"
              disabled={inFlight || restarting || !image.trim()}
              loading={act.pending("system-upgrade")}
              onConfirm={trigger}
            >
              Upgrade
            </ConfirmButton>
          </form>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <p className="text-xs text-muted-foreground">
            Admin only. The image must match COMPOSER_UPGRADE_IMAGE_PREFIX. A second request while
            one is in flight is rejected (409). Rollback: re-run with the previous tag.
          </p>
        </CardContent>
      </Card>
    </ErrorBoundary>
  );
}
