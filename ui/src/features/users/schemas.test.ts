import { describe, expect, it } from "vite-plus/test";

import {
  createUserSchema,
  editUserSchema,
  resetPasswordSchema,
  updateUserBody,
} from "@/features/users/schemas";

const validCreate = {
  username: "ana",
  email: "",
  password: "Secret-123",
  isServerAdmin: false,
};

describe("createUserSchema", () => {
  it("accepts a user without an email", () => {
    expect(createUserSchema.safeParse(validCreate).success).toBe(true);
  });

  it("trims the username and rejects an empty one", () => {
    const parsed = createUserSchema.parse({ ...validCreate, username: "  ana  " });
    expect(parsed.username).toBe("ana");
    expect(createUserSchema.safeParse({ ...validCreate, username: "   " }).success).toBe(false);
  });

  it("rejects a malformed email", () => {
    expect(createUserSchema.safeParse({ ...validCreate, email: "ana" }).success).toBe(false);
  });

  it("rejects a weak password", () => {
    expect(createUserSchema.safeParse({ ...validCreate, password: "secret" }).success).toBe(false);
  });
});

describe("editUserSchema", () => {
  it("accepts an empty email", () => {
    expect(
      editUserSchema.safeParse({ email: "  ", isServerAdmin: true, disabled: false }).success,
    ).toBe(true);
  });
});

describe("updateUserBody", () => {
  it("sends a null email when the field is empty", () => {
    expect(updateUserBody({ email: "   ", isServerAdmin: false, disabled: true })).toEqual({
      email: null,
      isServerAdmin: false,
      disabled: true,
    });
  });

  it("keeps a trimmed email and the flags", () => {
    expect(
      updateUserBody({ email: " ana@example.com ", isServerAdmin: true, disabled: false }),
    ).toEqual({ email: "ana@example.com", isServerAdmin: true, disabled: false });
  });
});

describe("resetPasswordSchema", () => {
  it("accepts matching passwords", () => {
    expect(
      resetPasswordSchema.safeParse({ newPassword: "Secret-123", confirmPassword: "Secret-123" })
        .success,
    ).toBe(true);
  });

  it("reports a mismatch on the confirmation", () => {
    const result = resetPasswordSchema.safeParse({
      newPassword: "Secret-123",
      confirmPassword: "Secret-124",
    });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.path).toEqual(["confirmPassword"]);
    expect(result.error?.issues[0]?.message).toBe("The passwords do not match");
  });
});
