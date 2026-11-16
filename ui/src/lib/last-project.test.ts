import { afterEach, describe, expect, it, vi } from "vite-plus/test";

import { lastProjectStorageKey, readLastProject, storeLastProject } from "@/lib/last-project";

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
