import { describe, expect, it } from "vite-plus/test";

import { createApiKeySchema, maskedKey } from "@/features/api-keys/schemas";

describe("createApiKeySchema", () => {
  it("trims the name", () => {
    expect(createApiKeySchema.parse({ name: " production workers " }).name).toBe(
      "production workers",
    );
  });

  it("rejects an empty name", () => {
    const result = createApiKeySchema.safeParse({ name: "  " });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.message).toBe("Enter a name");
  });

  it("accepts 64 characters and rejects 65", () => {
    expect(createApiKeySchema.safeParse({ name: "a".repeat(64) }).success).toBe(true);
    const result = createApiKeySchema.safeParse({ name: "a".repeat(65) });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.message).toBe("Use at most 64 characters");
  });
});

describe("maskedKey", () => {
  it("shows the prefix and the hint", () => {
    expect(maskedKey("bQlp")).toBe("rc_api_…bQlp");
  });

  it("shows only the prefix without a hint", () => {
    expect(maskedKey()).toBe("rc_api_…");
  });
});
