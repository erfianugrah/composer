/**
 * The one status vocabulary for the whole UI.
 *
 * Four surfaces (dashboard, stack detail, container list, jobs drawer) each
 * carried their own copy of this map. They agreed on the states they happened
 * to share and silently disagreed elsewhere: the container list fell back to
 * the "created" style for an unrecognised status while the stack page fell
 * back to "unknown", and three real docker states - restarting, removing and
 * dead - were in none of them, so a dead container rendered in the same
 * neutral grey as one you stopped on purpose.
 *
 * Colour policy:
 *   green   steady and correct        running, healthy
 *   red     true alert, needs a human unhealthy, dead
 *   yellow  transient, will resolve   restarting, removing, in-flight actions
 *   peach   deliberate hold / partial paused, partial
 *   neutral steady and unremarkable   created, exited, stopped, none, unknown
 *
 * Red is reserved for states a human must act on. A container you stopped is
 * neutral, not an error - the UI should not cry wolf about an intended state.
 */
const GREEN = "bg-cp-green/20 text-cp-green border-cp-green/30";
const RED = "bg-cp-red/20 text-cp-red border-cp-red/30";
const YELLOW = "bg-cp-yellow/20 text-cp-yellow border-cp-yellow/30";
const PEACH = "bg-cp-peach/20 text-cp-peach border-cp-peach/30";
const NEUTRAL = "bg-cp-600/20 text-muted-foreground border-cp-600/30";

export const statusColor: Record<string, string> = {
  // Container states, as emitted by the docker daemon.
  created: NEUTRAL,
  running: GREEN,
  paused: PEACH,
  restarting: YELLOW,
  removing: YELLOW,
  exited: NEUTRAL,
  dead: RED,

  // Health, when a healthcheck is configured.
  healthy: GREEN,
  unhealthy: RED,
  starting: YELLOW,
  none: NEUTRAL,

  // Aggregate stack states.
  stopped: NEUTRAL,
  partial: PEACH,
  unknown: NEUTRAL,

  // A one-off task that ran to completion, as opposed to a service that
  // stopped. Distinct from "exited" on purpose.
  completed: "bg-cp-blue/20 text-cp-blue border-cp-blue/30",
};

/** Style for a client-side transitional state ("stopping", "restarting"). */
export const transitionalColor = YELLOW;

/**
 * Style for a status, with a single defined fallback.
 *
 * Callers used to inline `statusColor[s] || statusColor.created` or
 * `|| statusColor.unknown`, which is how the two container tables ended up
 * disagreeing about what an unrecognised status looks like.
 */
export function statusClass(status: string | undefined): string {
  return (status && statusColor[status]) || statusColor.unknown;
}
