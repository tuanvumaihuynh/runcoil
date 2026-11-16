import { describe, expect, it } from "vite-plus/test";

import { hasPermission } from "@/lib/permissions";

describe("hasPermission", () => {
  it("finds a permission in the list", () => {
    expect(hasPermission(["users:read", "users:write"], "users:write")).toBe(true);
  });

  it("is false when the permission is missing", () => {
    expect(hasPermission(["users:read"], "users:write")).toBe(false);
  });

  it("is false for an empty or missing list", () => {
    expect(hasPermission([], "users:read")).toBe(false);
    expect(hasPermission(undefined, "users:read")).toBe(false);
  });
});
