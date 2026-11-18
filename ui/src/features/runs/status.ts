import type { components } from "@/api/schema";

export type RunStatus = components["schemas"]["RunStatus"];

export const runStatuses = [
  "queued",
  "running",
  "succeeded",
  "failed",
] as const satisfies readonly RunStatus[];

export const runStatusLabels: Record<RunStatus, string> = {
  queued: "Queued",
  running: "Running",
  succeeded: "Succeeded",
  failed: "Failed",
};

export const runStatusVariants: Record<
  RunStatus,
  "outline" | "secondary" | "default" | "destructive"
> = {
  queued: "outline",
  running: "secondary",
  succeeded: "default",
  failed: "destructive",
};

export function hasRunEnded(status: RunStatus): boolean {
  return status === "succeeded" || status === "failed";
}

// What each status means and what happens next, for the badge's tooltip.
export const runStatusHints: Record<RunStatus, string> = {
  queued: "Waiting for a Worker that serves its Task to start it.",
  running:
    "A Worker started it and it has not ended. It stays Running while a failed Step waits to retry.",
  succeeded: "Finished. Its output is saved and nothing else runs.",
  failed: "Ended with an error and will not run again. The error code says why.",
};

export type StepStatus = components["schemas"]["Step"]["status"];

export const stepStatusHints: Record<StepStatus, string> = {
  succeeded: "Finished and saved. A retry of the Run skips this Step and reuses its result.",
  failed: "Failed for good, so it is not tried again.",
};
