import { describe, expect, it } from "vite-plus/test";

import { createProjectSchema } from "@/features/projects/schemas";

describe("createProjectSchema", () => {
  it("trims the name", () => {
    expect(createProjectSchema.parse({ name: "  billing  " }).name).toBe("billing");
  });

  it("rejects an empty name", () => {
    expect(createProjectSchema.safeParse({ name: "   " }).success).toBe(false);
  });

  it("accepts a 64 character name and rejects 65", () => {
    expect(createProjectSchema.safeParse({ name: "a".repeat(64) }).success).toBe(true);
    expect(createProjectSchema.safeParse({ name: "a".repeat(65) }).success).toBe(false);
  });
});
