import { describe, expect, it } from "vite-plus/test";

import { shortId } from "@/lib/short-id";

describe("shortId", () => {
  it("keeps the first 8 and the last 4 characters", () => {
    expect(shortId("01a1e4a7-5c2e-7d3b-9f10-6b8d2c9d45ab")).toBe("01a1e4a7…45ab");
  });
});
