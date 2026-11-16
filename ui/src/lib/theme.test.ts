import { describe, expect, it } from "vite-plus/test";

import { isThemePreference, resolveTheme, type ThemePreference } from "@/lib/theme";

describe("resolveTheme", () => {
  it.each<[ThemePreference, boolean, "light" | "dark"]>([
    ["light", false, "light"],
    ["light", true, "light"],
    ["dark", false, "dark"],
    ["dark", true, "dark"],
    ["system", false, "light"],
    ["system", true, "dark"],
  ])("resolves %s with systemPrefersDark=%s to %s", (preference, systemDark, expected) => {
    expect(resolveTheme(preference, systemDark)).toBe(expected);
  });
});

describe("isThemePreference", () => {
  it("accepts the three preferences", () => {
    expect(isThemePreference("light")).toBe(true);
    expect(isThemePreference("dark")).toBe(true);
    expect(isThemePreference("system")).toBe(true);
  });

  it("rejects anything else", () => {
    expect(isThemePreference(null)).toBe(false);
    expect(isThemePreference("")).toBe(false);
    expect(isThemePreference("Dark")).toBe(false);
    expect(isThemePreference(1)).toBe(false);
  });
});
