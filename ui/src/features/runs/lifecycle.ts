import type { components } from "@/api/schema";
import { hasRunEnded } from "@/features/runs/status";
import { formatDateTime, formatDuration } from "@/lib/format";
import { shortId } from "@/lib/short-id";

type Run = components["schemas"]["Run"];
type RunWaiting = components["schemas"]["RunWaiting"];

export type LifecycleEntry = {
  state: "done" | "current" | "failed";
  title: string;
  detail?: string;
};

// Where a Run has been and where it is now, in words, oldest first.
export function runLifecycle(run: Run, now: Date): LifecycleEntry[] {
  const entries: LifecycleEntry[] = [
    { state: "done", title: "Created", detail: formatDateTime(run.createdAt) },
  ];

  if (run.startedAt !== undefined) {
    entries.push({
      state: "done",
      title: "Started",
      detail: `Waited ${between(run.createdAt, run.startedAt)} for a Worker`,
    });
  }

  if (run.steps.length > 0) {
    entries.push(stepsEntry(run.steps));
  }

  entries.push(hasRunEnded(run.status) ? endEntry(run) : currentEntry(run, now));

  return entries;
}

function stepsEntry(steps: Run["steps"]): LifecycleEntry {
  const retried = steps.filter((step) => step.tryCount > 1).length;

  return {
    state: "done",
    title: steps.length === 1 ? "Recorded 1 Step" : `Recorded ${steps.length} Steps`,
    detail: retried === 0 ? undefined : `${retried} took more than one try`,
  };
}

function currentEntry(run: Run, now: Date): LifecycleEntry {
  const { waiting, attempt } = run;

  if (waiting?.reason === "retry") {
    return {
      state: "current",
      title:
        waiting.retryStepId === undefined ? "Waiting to retry" : `Retrying ${waiting.retryStepId}`,
      detail: `${tryOf(run, waiting)} ${countdown(waiting.readyAt, now)}`,
    };
  }

  if (waiting?.reason === "worker") {
    if (waiting.retryStepId !== undefined) {
      return {
        state: "current",
        title: `Waiting for a Worker to retry ${waiting.retryStepId}`,
        detail: `${tryOf(run, waiting)}, ready for ${elapsed(waiting.readyAt, now)}`,
      };
    }

    const deadline =
      waiting.startDeadlineAt === undefined
        ? ""
        : `. It fails at ${formatDateTime(waiting.startDeadlineAt)} if no Worker starts it`;

    return {
      state: "current",
      title: "Waiting for a Worker",
      detail: `For ${elapsed(waiting.readyAt, now)}${deadline}`,
    };
  }

  if (attempt !== undefined) {
    return {
      state: "current",
      title: `Running on Worker ${shortId(attempt.workerId)}`,
      detail: `For ${elapsed(attempt.startedAt, now)}`,
    };
  }

  return { state: "current", title: run.status === "queued" ? "Waiting for a Worker" : "Running" };
}

function endEntry(run: Run): LifecycleEntry {
  const ended = run.endedAt === undefined ? "" : formatDateTime(run.endedAt);
  const took =
    run.startedAt !== undefined && run.endedAt !== undefined
      ? `after ${between(run.startedAt, run.endedAt)}`
      : undefined;

  if (run.status === "succeeded") {
    return {
      state: "done",
      title: "Succeeded",
      detail: took === undefined ? ended : capitalize(took),
    };
  }

  const why = run.startedAt === undefined ? "before a Worker started it" : took;
  const code = run.error?.code;

  return {
    state: "failed",
    title: code === undefined ? "Failed" : `Failed with ${code}`,
    detail: why === undefined ? ended : capitalize(why),
  };
}

function tryOf(run: Run, waiting: RunWaiting): string {
  // A Step can set its own retry limit, which the API does not return, so only the Run's tries
  // get a total.
  const maxAttempts = waiting.retryStepId === undefined ? run.config.retry?.maxAttempts : undefined;
  const of = maxAttempts === undefined ? "" : ` of ${maxAttempts}`;

  return `Try ${waiting.retryCount + 1}${of}`;
}

// Rounded up to what formatDuration shows, whole seconds or whole minutes past an hour, so a
// countdown never shows less time than is left.
function countdown(readyAt: string, now: Date): string {
  const left = new Date(readyAt).getTime() - now.getTime();
  if (left <= 0) {
    return "is due now";
  }

  const unit = left > 3_600_000 ? 60_000 : 1000;

  return `in ${formatDuration(Math.ceil(left / unit) * unit)}`;
}

// Whole seconds once past one, so a live value does not flicker through tenths.
function elapsed(since: string, now: Date): string {
  const ms = now.getTime() - new Date(since).getTime();

  return formatDuration(ms < 1000 ? ms : Math.floor(ms / 1000) * 1000);
}

function between(from: string, to: string): string {
  return formatDuration(new Date(to).getTime() - new Date(from).getTime());
}

function capitalize(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1);
}
