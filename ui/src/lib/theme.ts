import { createContext, useContext } from "react";

export type ThemePreference = "light" | "dark" | "system";
export type ResolvedTheme = "light" | "dark";

export type ThemeContextValue = {
  preference: ThemePreference;
  resolved: ResolvedTheme;
  setPreference: (preference: ThemePreference) => void;
};

export const themeStorageKey = "runcoil-theme";
export const darkQuery = "(prefers-color-scheme: dark)";

const preferences: readonly ThemePreference[] = ["light", "dark", "system"];

export function isThemePreference(value: unknown): value is ThemePreference {
  return typeof value === "string" && (preferences as readonly string[]).includes(value);
}

export function resolveTheme(
  preference: ThemePreference,
  systemPrefersDark: boolean,
): ResolvedTheme {
  if (preference === "system") {
    return systemPrefersDark ? "dark" : "light";
  }

  return preference;
}

// Reads the stored preference. A missing, unreadable or unknown value is "system".
export function readStoredPreference(): ThemePreference {
  try {
    const stored = window.localStorage.getItem(themeStorageKey);
    return isThemePreference(stored) ? stored : "system";
  } catch {
    return "system";
  }
}

// Storage can be blocked (private windows, disabled site data); the theme still applies for this page.
export function storePreference(preference: ThemePreference): void {
  try {
    window.localStorage.setItem(themeStorageKey, preference);
  } catch {
    // Ignored on purpose: the preference lasts until the page is closed.
  }
}

export function applyResolvedTheme(resolved: ResolvedTheme): void {
  document.documentElement.classList.toggle("dark", resolved === "dark");
}

export function systemPrefersDark(): boolean {
  return window.matchMedia(darkQuery).matches;
}

export const ThemeContext = createContext<ThemeContextValue | undefined>(undefined);

export function useTheme(): ThemeContextValue {
  const value = useContext(ThemeContext);
  if (value === undefined) {
    throw new Error("useTheme must be used inside ThemeProvider");
  }

  return value;
}
