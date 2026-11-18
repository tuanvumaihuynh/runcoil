import { describe, expect, it } from "vite-plus/test";

import { runDuration } from "@/features/runs/duration";

const now = new Date("2026-11-16T08:00:10Z");

describe("runDuration", () => {
  it("is undefined before the Run starts", () => {
    expect(runDuration({}, now)).toBeUndefined();
  });

  it("runs up to now while the Run has not ended", () => {
    expect(runDuration({ startedAt: "2026-11-16T08:00:04Z" }, now)).toBe(6000);
  });

  it("stops at the end", () => {
    expect(
      runDuration({ startedAt: "2026-11-16T08:00:01Z", endedAt: "2026-11-16T08:00:09Z" }, now),
    ).toBe(8000);
  });
});
