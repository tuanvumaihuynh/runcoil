import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import {
  lastProjectStorageKey,
  pickLandingProject,
  readLastProject,
  storeLastProject,
} from "@/lib/last-project";

function stubStorage(storage: Partial<Storage>) {
  vi.stubGlobal("window", { localStorage: storage });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("last project", () => {
  it("stores and reads the id", () => {
    const data = new Map<string, string>();
    stubStorage({
      getItem: (key) => data.get(key) ?? null,
      setItem: (key, value) => void data.set(key, value),
    });

    storeLastProject("p1");

    expect(data.get(lastProjectStorageKey)).toBe("p1");
    expect(readLastProject()).toBe("p1");
  });

  it("reads nothing when none is stored", () => {
    stubStorage({ getItem: () => null });

    expect(readLastProject()).toBeUndefined();
  });

  it("survives blocked storage", () => {
    const blocked = () => {
      throw new Error("blocked");
    };
    stubStorage({ getItem: blocked, setItem: blocked });

    expect(readLastProject()).toBeUndefined();
    expect(() => storeLastProject("p1")).not.toThrow();
  });
});

describe("pickLandingProject", () => {
  const projects = [{ id: "p1" }, { id: "p2" }];

  it("picks the last project when the user can still open it", () => {
    expect(pickLandingProject(projects, "p2")).toBe(projects[1]);
  });

  it("falls back to the first project", () => {
    expect(pickLandingProject(projects, "gone")).toBe(projects[0]);
    expect(pickLandingProject(projects, undefined)).toBe(projects[0]);
  });

  it("picks nothing when there are no projects", () => {
    expect(pickLandingProject([], "p1")).toBeUndefined();
  });
});
