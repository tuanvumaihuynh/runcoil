import { describe, expect, it } from "vite-plus/test";

import type { components } from "@/api/schema";
import { runLifecycle } from "@/features/runs/lifecycle";
import { formatDateTime } from "@/lib/format";

type Run = components["schemas"]["Run"];
type Step = components["schemas"]["Step"];

const createdAt = "2026-11-16T08:00:00Z";
const now = new Date("2026-11-16T08:00:20Z");
const created = { state: "done", title: "Created", detail: formatDateTime(createdAt) };

function run(overrides: Partial<Run>): Run {
  return {
    id: "019a5b1e-7c2d-7000-8000-4b1d2c3e4f55",
    status: "queued",
    task: { id: "019a5b1e-7c2d-7000-8000-4b1d2c3e4f54", app: "shop", name: "nightly-report" },
    createdAt,
    config: {},
    steps: [],
    ...overrides,
  };
}

function step(id: string, tryCount: number): Step {
  return { id, kind: "do", status: "succeeded", tryCount, recordedAt: "2026-11-16T08:00:05Z" };
}

describe("runLifecycle", () => {
  it("says how long a queued Run has waited for a Worker", () => {
    const value = run({ waiting: { reason: "worker", readyAt: createdAt, retryCount: 0 } });

    expect(runLifecycle(value, now)).toEqual([
      created,
      { state: "current", title: "Waiting for a Worker", detail: "For 20s" },
    ]);
  });

  it("adds the start deadline", () => {
    const startDeadlineAt = "2026-11-16T09:00:00Z";
    const value = run({
      waiting: { reason: "worker", readyAt: createdAt, retryCount: 0, startDeadlineAt },
    });

    expect(runLifecycle(value, now)[1]?.detail).toBe(
      `For 20s. It fails at ${formatDateTime(startDeadlineAt)} if no Worker starts it`,
    );
  });

  it("names the Worker a running Run is on", () => {
    const value = run({
      status: "running",
      startedAt: "2026-11-16T08:00:00.008Z",
      attempt: {
        workerId: "019a5b1e-7c2d-7000-8000-4b1d2c3e4f56",
        startedAt: "2026-11-16T08:00:00.008Z",
      },
      steps: [step("load-orders", 1)],
    });

    expect(runLifecycle(value, now)).toEqual([
      created,
      { state: "done", title: "Started", detail: "Waited 8ms for a Worker" },
      { state: "done", title: "Recorded 1 Step", detail: undefined },
      { state: "current", title: "Running on Worker 019a5b1e…4f56", detail: "For 19s" },
    ]);
  });

  it("counts down to the next try of a retrying Step", () => {
    const value = run({
      status: "running",
      startedAt: createdAt,
      config: { retry: { maxAttempts: 4 } },
      waiting: {
        reason: "retry",
        readyAt: "2026-11-16T08:00:23.200Z",
        retryStepId: "sum-totals",
        retryCount: 1,
      },
    });

    expect(runLifecycle(value, now).at(-1)).toEqual({
      state: "current",
      title: "Retrying sum-totals",
      detail: "Try 2 in 4s",
    });
  });

  it("rounds a countdown past an hour up to the minute", () => {
    const value = run({
      status: "running",
      startedAt: createdAt,
      waiting: {
        reason: "retry",
        readyAt: "2026-11-16T09:01:19Z",
        retryStepId: "sum-totals",
        retryCount: 1,
      },
    });

    expect(runLifecycle(value, now).at(-1)?.detail).toBe("Try 2 in 1h 1m");
  });

  it("says a retry is due once its time has passed", () => {
    const value = run({
      status: "running",
      startedAt: createdAt,
      waiting: { reason: "retry", readyAt: createdAt, retryStepId: "sum-totals", retryCount: 1 },
    });

    expect(runLifecycle(value, now).at(-1)?.detail).toBe("Try 2 is due now");
  });

  it("says a ready retry waits for a Worker", () => {
    const value = run({
      status: "running",
      startedAt: createdAt,
      config: { retry: { maxAttempts: 3 } },
      waiting: {
        reason: "worker",
        readyAt: "2026-11-16T08:00:17Z",
        retryStepId: "sum-totals",
        retryCount: 2,
      },
    });

    expect(runLifecycle(value, now).at(-1)).toEqual({
      state: "current",
      title: "Waiting for a Worker to retry sum-totals",
      detail: "Try 3, ready for 3s",
    });
  });

  it("counts the Steps that needed more than one try", () => {
    const value = run({
      status: "succeeded",
      startedAt: createdAt,
      endedAt: "2026-11-16T08:00:08.100Z",
      steps: [step("load-orders", 1), step("sum-totals", 3), step("send", 2)],
    });

    expect(runLifecycle(value, now).slice(2)).toEqual([
      { state: "done", title: "Recorded 3 Steps", detail: "2 took more than one try" },
      { state: "done", title: "Succeeded", detail: "After 8.1s" },
    ]);
  });

  it("says a Run failed before any Worker started it", () => {
    const value = run({
      status: "failed",
      endedAt: "2026-11-16T08:05:00Z",
      error: { code: "start_deadline", message: "no worker started the run in time" },
    });

    expect(runLifecycle(value, now)).toEqual([
      created,
      {
        state: "failed",
        title: "Failed with start_deadline",
        detail: "Before a Worker started it",
      },
    ]);
  });
});
