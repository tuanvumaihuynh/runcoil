import { formatRelativeTime } from "@/lib/format";

type TaskWorkers = { liveWorkerCount: number; lastRegisteredAt: string };

// When no live Worker serves a Task, how long ago one last registered it. A Worker can stay
// connected long after it registered, so this is not when the last one left.
export function noLiveWorkerSince(task: TaskWorkers, now: Date): string | undefined {
  if (task.liveWorkerCount > 0) {
    return undefined;
  }

  return `Last registered ${formatRelativeTime(task.lastRegisteredAt, now)}`;
}
