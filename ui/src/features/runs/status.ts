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
