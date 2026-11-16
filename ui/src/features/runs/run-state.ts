import type { components } from "@/api/schema";
import { formatDateTime } from "@/lib/format";
import { shortId } from "@/lib/short-id";

type Run = components["schemas"]["Run"];

// Where a Run that has not ended is: waiting for a worker or a retry, or running on a worker.
export function describeRunState(run: Run): string | undefined {
  const { waiting, attempt } = run;

  if (waiting !== undefined) {
    return waitingSentence(run, waiting) + deadlineSentence(waiting);
  }

  if (attempt !== undefined) {
    return `Running on worker ${shortId(attempt.workerId)} since ${formatDateTime(attempt.startedAt)}.`;
  }

  return undefined;
}

function waitingSentence(run: Run, waiting: NonNullable<Run["waiting"]>): string {
  const { retryStepId, retryCount } = waiting;

  if (waiting.reason === "worker") {
    return retryStepId === undefined
      ? `Waiting for a worker since ${formatDateTime(waiting.readyAt)}.`
      : `Waiting for a worker to retry ${retryStepId}, try ${retryCount + 1}.`;
  }

  const step = retryStepId === undefined ? "" : ` ${retryStepId}`;
  const maxAttempts = run.config.retry?.maxAttempts;
  const of = maxAttempts === undefined ? "" : ` of ${maxAttempts}`;

  return `Waiting to retry${step} at ${formatDateTime(waiting.readyAt)}, try ${retryCount + 1}${of}.`;
}

function deadlineSentence(waiting: NonNullable<Run["waiting"]>): string {
  return waiting.startDeadlineAt === undefined
    ? ""
    : ` It fails at ${formatDateTime(waiting.startDeadlineAt)} if no worker starts it.`;
}
