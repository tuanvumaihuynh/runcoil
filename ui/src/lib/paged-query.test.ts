import { describe, expect, it } from "vite-plus/test";

import { pagedRefetchInterval } from "@/lib/paged-query";

function queryWith(pages: number | undefined) {
  return { state: { data: pages === undefined ? undefined : { pages: Array(pages).fill({}) } } };
}

describe("pagedRefetchInterval", () => {
  it("polls while no page or one page is loaded", () => {
    expect(pagedRefetchInterval(5_000)(queryWith(undefined))).toBe(5_000);
    expect(pagedRefetchInterval(5_000)(queryWith(1))).toBe(5_000);
  });

  it("stops once a second page is loaded", () => {
    expect(pagedRefetchInterval(5_000)(queryWith(2))).toBe(false);
    expect(pagedRefetchInterval(5_000)(queryWith(6))).toBe(false);
  });

  it("stays off when polling is off", () => {
    expect(pagedRefetchInterval(false)(queryWith(1))).toBe(false);
    expect(pagedRefetchInterval(undefined)(queryWith(1))).toBe(false);
  });
});
