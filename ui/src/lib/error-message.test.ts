import { describe, expect, it } from "vite-plus/test";

import { ApiError } from "@/lib/api-errors";
import { errorMessage } from "@/lib/error-message";

describe("errorMessage", () => {
  it("returns the API message, capitalized", () => {
    const error = new ApiError(409, {
      code: "already_exists",
      message: "username is taken",
    });
    expect(errorMessage(error, "Fallback")).toBe("Username is taken");
  });

  it("returns the fallback for an unknown error", () => {
    expect(errorMessage(new Error("boom"), "Fallback")).toBe("Fallback");
  });
});
