import type { ActionLabels } from "@/lib/use-action";

/**
 * Shared vocabulary for container lifecycle actions.
 *
 * Both container tables (the stack detail page and the containers page) render
 * the same five verbs. They previously each carried their own copies of the
 * key format and the wording, which is how one table ended up showing a
 * transitional row status and the other not. Anything a table needs to say
 * about a lifecycle action lives here so the two cannot drift.
 */
export const lifecycleVerbs = ["start", "stop", "restart", "pause", "unpause"] as const;

export type LifecycleVerb = (typeof lifecycleVerbs)[number];

/**
 * Pending key for one container's verb. Scoped per container AND per verb so
 * an action on one row never disables another row's buttons.
 */
export function actionKey(id: string, verb: string): string {
  return `${id}:${verb}`;
}

/**
 * Row status shown while the verb is in flight. These are real intermediate
 * states, not an optimistic guess at the final one - the row says "stopping"
 * because that is what is happening, and resolves to whatever the refetch
 * actually reports.
 */
const transitional: Record<string, string> = {
  start: "starting",
  stop: "stopping",
  restart: "restarting",
  pause: "pausing",
  unpause: "resuming",
};

const past: Record<string, string> = {
  start: "Started",
  stop: "Stopped",
  restart: "Restarted",
  pause: "Paused",
  unpause: "Resumed",
};

/**
 * Toast + pending wording for one verb against one named container.
 *
 * `running` doubles as the transitional row status, so it is a short lowercase
 * gerund rather than a sentence - it has to read correctly inside a badge.
 */
export function containerActionLabels(verb: string, name: string): ActionLabels {
  return {
    running: transitional[verb] ?? `${verb}ing`,
    success: `${past[verb] ?? `${verb}ed`} ${name}`,
    failure: `Failed to ${verb} ${name}`,
  };
}

/**
 * Past-tense stem `runBulk` appends "ed" to. Kept next to the singular
 * wording so the bulk bar and the per-row buttons cannot drift apart.
 */
const bulkStem: Record<LifecycleVerb, string> = {
  start: "Start",
  stop: "Stopp",
  restart: "Restart",
  pause: "Paus",
  unpause: "Unpaus",
};

/**
 * Toast wording for a bulk lifecycle action across N containers. Both
 * container tables (stack detail, containers page) feed this straight into
 * `runBulk`.
 */
export function bulkContainerLabels(verb: LifecycleVerb) {
  return { verb: bulkStem[verb], noun: "container", infinitive: verb };
}

/**
 * The transitional label for whichever lifecycle verb is currently running on
 * this container, or null when it is idle. Drives the row badge.
 */
export function transitionalStatusFor(
  id: string,
  pendingLabel: (key: string) => string | null,
): string | null {
  for (const verb of lifecycleVerbs) {
    const label = pendingLabel(actionKey(id, verb));
    if (label) return label;
  }
  return null;
}
