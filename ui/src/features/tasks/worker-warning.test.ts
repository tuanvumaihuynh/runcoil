import { describe, expect, it } from "vite-plus/test";

import { noLiveWorkerSince } from "@/features/tasks/worker-warning";

const now = new Date("2026-11-18T12:00:00Z");
const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "always" });

describe("noLiveWorkerSince", () => {
  it("is undefined while a Worker serves the Task", () => {
    expect(
      noLiveWorkerSince({ liveWorkerCount: 1, lastRegisteredAt: "2026-11-15T12:00:00Z" }, now),
    ).toBeUndefined();
  });

  it("says when a Worker last registered the Task", () => {
    expect(
      noLiveWorkerSince({ liveWorkerCount: 0, lastRegisteredAt: "2026-11-15T11:00:00Z" }, now),
    ).toBe(`Last registered ${relative.format(-3, "day")}`);
  });

  it("uses smaller units for a recent registration", () => {
    expect(
      noLiveWorkerSince({ liveWorkerCount: 0, lastRegisteredAt: "2026-11-18T11:55:00Z" }, now),
    ).toBe(`Last registered ${relative.format(-5, "minute")}`);
  });

  it("does not put a registration newer than the clock in the future", () => {
    expect(
      noLiveWorkerSince({ liveWorkerCount: 0, lastRegisteredAt: "2026-11-18T12:00:02Z" }, now),
    ).toBe("Last registered just now");
  });
});
