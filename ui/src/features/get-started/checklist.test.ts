import { describe, expect, it } from "vite-plus/test";

import { checklistSteps } from "@/features/get-started/checklist";
import type { ProjectProgress } from "@/features/get-started/checklist";

const empty: ProjectProgress = {
  hasApiKey: false,
  hasLiveWorker: false,
  hasTask: false,
  hasRun: false,
};

function summary(progress: ProjectProgress) {
  return checklistSteps(progress).map(({ id, done, current }) =>
    current ? `${id}:current` : `${id}:${done ? "done" : "todo"}`,
  );
}

describe("checklistSteps", () => {
  it("starts at the API key on an empty project", () => {
    expect(summary(empty)).toEqual(["apiKey:current", "worker:todo", "tasks:todo", "run:todo"]);
  });

  it("moves to the Worker once a key exists", () => {
    expect(summary({ ...empty, hasApiKey: true })).toEqual([
      "apiKey:done",
      "worker:current",
      "tasks:todo",
      "run:todo",
    ]);
  });

  it("waits for the first Run once Tasks are registered", () => {
    expect(summary({ ...empty, hasApiKey: true, hasLiveWorker: true, hasTask: true })).toEqual([
      "apiKey:done",
      "worker:done",
      "tasks:done",
      "run:current",
    ]);
  });

  it("ticks the earlier steps from a later one", () => {
    expect(summary({ ...empty, hasTask: true })).toEqual([
      "apiKey:done",
      "worker:done",
      "tasks:done",
      "run:current",
    ]);
  });

  it("has no current step once a Run exists", () => {
    expect(
      checklistSteps({ ...empty, hasRun: true }).every((step) => step.done && !step.current),
    ).toBe(true);
  });
});
