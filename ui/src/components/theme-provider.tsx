import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import {
  applyResolvedTheme,
  darkQuery,
  readStoredPreference,
  resolveTheme,
  storePreference,
  systemPrefersDark,
  ThemeContext,
  type ThemePreference,
  type ThemeContextValue,
} from "@/lib/theme";

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<ThemePreference>(readStoredPreference);
  const [systemDark, setSystemDark] = useState(() => systemPrefersDark());

  useEffect(() => {
    const media = window.matchMedia(darkQuery);
    const onChange = (event: MediaQueryListEvent) => setSystemDark(event.matches);

    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, []);

  const resolved = resolveTheme(preference, systemDark);

  useEffect(() => {
    applyResolvedTheme(resolved);
  }, [resolved]);

  const setPreference = useCallback((next: ThemePreference) => {
    storePreference(next);
    setPreferenceState(next);
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({ preference, resolved, setPreference }),
    [preference, resolved, setPreference],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}
