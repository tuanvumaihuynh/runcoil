import { describe, expect, it } from "vite-plus/test";

import { addMemberSchema, roleHelp, roles } from "@/features/members/schemas";

describe("addMemberSchema", () => {
  it("trims the username", () => {
    expect(addMemberSchema.parse({ username: " ana ", role: "viewer" }).username).toBe("ana");
  });

  it("rejects an empty username", () => {
    const result = addMemberSchema.safeParse({ username: "  ", role: "viewer" });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.message).toBe("Select a user");
  });

  it("accepts each role and rejects an unknown one", () => {
    for (const role of roles) {
      expect(addMemberSchema.safeParse({ username: "ana", role }).success).toBe(true);
    }
    expect(addMemberSchema.safeParse({ username: "ana", role: "owner" }).success).toBe(false);
  });
});

describe("roleHelp", () => {
  it("describes every role", () => {
    expect(Object.keys(roleHelp).sort()).toEqual(["admin", "editor", "viewer"]);
  });
});
