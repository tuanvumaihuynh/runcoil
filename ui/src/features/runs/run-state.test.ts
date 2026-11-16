import { describe, expect, it } from "vite-plus/test";

import type { components } from "@/api/schema";
import { describeRunState } from "@/features/runs/run-state";
import { formatDateTime } from "@/lib/format";

type Run = components["schemas"]["Run"];

const readyAt = "2026-11-16T08:00:04Z";
const at = formatDateTime(readyAt);

function run(overrides: Partial<Run>): Run {
  return {
    id: "019a5b1e-7c2d-7000-8000-4b1d2c3e4f55",
    status: "queued",
    task: { id: "019a5b1e-7c2d-7000-8000-4b1d2c3e4f54", app: "shop", name: "nightly-report" },
    createdAt: "2026-11-16T08:00:00Z",
    config: {},
    steps: [],
    ...overrides,
  };
}

describe("describeRunState", () => {
  it("says a Run waits for a worker", () => {
    const value = run({ waiting: { reason: "worker", readyAt, retryCount: 0 } });

    expect(describeRunState(value)).toBe(`Waiting for a worker since ${at}.`);
  });

  it("names the Step a worker will retry", () => {
    const value = run({
      waiting: { reason: "worker", readyAt, retryStepId: "sum-totals", retryCount: 1 },
    });

    expect(describeRunState(value)).toBe("Waiting for a worker to retry sum-totals, try 2.");
  });

  it("says when a retry is due and how many attempts there are", () => {
    const value = run({
      config: { retry: { maxAttempts: 4 } },
      waiting: { reason: "retry", readyAt, retryStepId: "sum-totals", retryCount: 1 },
    });

    expect(describeRunState(value)).toBe(`Waiting to retry sum-totals at ${at}, try 2 of 4.`);
  });

  it("drops the attempt limit when the Run has none", () => {
    const value = run({
      waiting: { reason: "retry", readyAt, retryStepId: "sum-totals", retryCount: 0 },
    });

    expect(describeRunState(value)).toBe(`Waiting to retry sum-totals at ${at}, try 1.`);
  });

  it("adds the start deadline", () => {
    const startDeadlineAt = "2026-11-16T09:00:00Z";
    const value = run({ waiting: { reason: "worker", readyAt, retryCount: 0, startDeadlineAt } });

    expect(describeRunState(value)).toBe(
      `Waiting for a worker since ${at}. It fails at ${formatDateTime(startDeadlineAt)} if no worker starts it.`,
    );
  });

  it("says where a running Run is", () => {
    const value = run({
      status: "running",
      attempt: { workerId: "019a5b1e-7c2d-7000-8000-4b1d2c3e4f56", startedAt: readyAt },
    });

    expect(describeRunState(value)).toBe(`Running on worker 019a5b1e…4f56 since ${at}.`);
  });

  it("says nothing without a wait or an attempt", () => {
    expect(describeRunState(run({ status: "succeeded" }))).toBeUndefined();
  });
});
